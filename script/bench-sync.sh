#!/usr/bin/env bash
# Times how long gh csd's rsync sync and a Mutagen session take to make a
# codespace match N edited files. Run it inside a worktree that `gh csd up`
# paired with a codespace. The Mutagen arm runs only if mutagen is on PATH,
# and syncs into a scratch dir in the codespace, never the checkout.
#
#   SIZES="0 1 10 100 1000" REPS=5 script/bench-sync.sh
#
# CSD picks the gh csd to time (for example, a local build), and MUTAGEN=0
# skips the Mutagen arm.
set -euo pipefail

read -ra sizes <<<"${SIZES:-0 1 10 100 1000}"
reps=${REPS:-5}
read -ra csd <<<"${CSD:-gh csd}"
bench=.csd-bench
# Names unique to this run, so cleanup never touches another run's session.
session=csd-bench-$$
scratch=/tmp/csd-bench-$$
cs=$(cat "$(git rev-parse --git-dir)/cs-dev-codespace")
dir=${CSD_DIR:-/workspaces/$(basename -s .git "$(git remote get-url origin)")}
qdir=$(printf %q "$dir")

tmp=$(mktemp -d)
gh codespace ssh --config -c "$cs" >"$tmp/config"
host=$(awk '/^Host /{print $2; exit}' "$tmp/config")
# The same options as Remote.SSHCommand, so connections are shared and fast.
opts=(-F "$tmp/config" -o IdentityAgent=none -o ControlMaster=auto
  -o "ControlPath=$HOME/.ssh/gh-csd-%C" -o ControlPersist=15m)

# cleanup removes only what this run made. It deletes the bench dir in the
# codespace directly: a full sync would also reset unrelated codespace edits.
synced='' mutagen_started=''
cleanup() {
  if [ -n "$mutagen_started" ]; then
    mutagen sync terminate "$session" >/dev/null 2>&1 || true
    ssh "${opts[@]}" "$host" "rm -rf $scratch" || true
  fi
  rm -rf "$bench"
  if [ -n "$synced" ]; then
    ssh "${opts[@]}" "$host" "rm -rf $qdir/$bench" || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

now() { perl -MTime::HiRes=time -e 'printf "%.3f\n", time'; }

# write N gives the bench dir exactly N files, each with new content.
write() {
  rm -rf "$bench"
  [ "$1" -eq 0 ] && return
  mkdir -p "$bench"
  for i in $(seq "$1"); do echo "$RANDOM $(now)" >"$bench/f$i.txt"; done
}

# measure ARM N CMD... prints ARM, N, and the median, min, and max of REPS
# timed runs of CMD, after one untimed run that settles leftovers.
measure() {
  local arm=$1 n=$2 times=()
  shift 2
  write "$n"
  "$@" >/dev/null
  for _ in $(seq "$reps"); do
    write "$n"
    local t0 t1
    t0=$(now)
    "$@" >/dev/null
    t1=$(now)
    times+=("$(awk "BEGIN{print $t1 - $t0}")")
  done
  printf '%s\n' "${times[@]}" | sort -n |
    awk -v arm="$arm" -v n="$n" '{t[NR]=$1} END{printf "%-14s %6d %8.3f %8.3f %8.3f\n", arm, n, t[int((NR+1)/2)], t[1], t[NR]}'
}

printf '%-14s %6s %8s %8s %8s\n' arm files median min max
measure ssh-roundtrip 0 ssh "${opts[@]}" "$host" true
measure remote-status 0 ssh "${opts[@]}" "$host" \
  "cd $qdir && git status --porcelain -z --no-renames --untracked-files=all"
synced=1
for n in "${sizes[@]}"; do measure rsync "$n" "${csd[@]}" run; done

if [ "${MUTAGEN:-1}" = 0 ] || ! command -v mutagen >/dev/null; then
  echo "skipped the Mutagen arm" >&2
  exit 0
fi

# Mutagen runs plain ssh and scp, so wrappers add the codespace's config.
mkdir "$tmp/bin"
for tool in ssh scp; do
  printf '#!/bin/sh\nexec /usr/bin/%s %s "$@"\n' "$tool" "$(printf '%q ' "${opts[@]}")" >"$tmp/bin/$tool"
  chmod +x "$tmp/bin/$tool"
done
export MUTAGEN_SSH_PATH=$tmp/bin

rm -rf "$bench"
mutagen_started=1
t0=$(now)
mutagen sync create --name "$session" --mode one-way-replica --ignore-vcs . "$host:$scratch" >/dev/null
mutagen sync flush "$session" >/dev/null
t1=$(now)
printf '%-14s %6s %8.3f\n' mutagen-initial all "$(awk "BEGIN{print $t1 - $t0}")"
for n in "${sizes[@]}"; do measure mutagen "$n" mutagen sync flush "$session"; done
