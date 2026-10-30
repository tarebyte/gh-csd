SPINEL ?= $(HOME)/src/matz/spinel/spinel

.PHONY: build test spinel-check types check clean

# The extension binary: gh runs ./gh-csd for `gh csd`.
build:
	$(SPINEL) bin/gh-csd.rb -o gh-csd

# Unit tests run on CRuby, since Spinel cannot compile Minitest.
test:
	ruby -e 'Dir.glob("test/**/*_test.rb").sort.each { |f| require File.expand_path(f) }'

# Proves the library still compiles under Spinel and behaves as on CRuby.
spinel-check:
	$(SPINEL) diff spinel/smoke.rb

# Fails when Spinel cannot type one of gh-csd's own parameters or returns
# and boxes it as untyped. Fix it with an inline RBS signature (#:).
types:
	@out=$$($(SPINEL) bin/gh-csd.rb -o spinel/types-check --warn-widen 2>&1 | grep 'warning:' | grep -v -e '/builtins/' -e '/packages/' | sed 's#^spinel: ##; s#^.*/gh-csd/##'); \
	rm -f spinel/types-check; \
	if [ -n "$$out" ]; then echo "$$out"; echo "$$(echo "$$out" | wc -l | tr -d ' ') untyped slots"; exit 1; fi; \
	echo "types: every parameter and return is typed"

check: test spinel-check types build

clean:
	rm -f gh-csd spinel/smoke spinel/types-check
