require "yaml"

module Csd
  # Finds and reads gh csd's optional YAML config file, following the
  # gh-dash convention for gh extensions:
  #
  #   $GH_CSD_CONFIG                         if set
  #   $XDG_CONFIG_HOME/gh-csd/config.yml     if XDG_CONFIG_HOME is set
  #   ~/.config/gh-csd/config.yml            otherwise
  #
  # gh csd only reads the file; a missing file means built-in defaults.
  class Config
    # What gh csd init writes: every setting, commented out.
    TEMPLATE = <<~YAML
      # gh csd config. Environment variables override these settings.
      # Uncomment what you need. Top-level keys apply to every repo; a repo's
      # section under repos: wins over them.

      # up requires machine. To list a repo's machine types, run:
      #   gh api repos/<owner/name>/codespaces/machines --jq '.machines[].name'
      # machine: standardLinux32gb
      # devcontainer: .devcontainer/devcontainer.json
      # default_permissions: false
      # dir: /workspaces/<repo name>
      # branch: main
      # A command that succeeds once the codespace's services accept
      # connections; up retries it for up to 5 minutes.
      # ready_check: none

      # repos:
      #   <owner/name>:
      #     machine: standardLinux32gb
      #     ready_check: nc -z localhost 5432
    YAML

    # owner/name from a github.com origin URL (SSH or HTTPS), or "".
    #: (String) -> String
    def self.repo_from(url)
      url[%r{github\.com[:/]([^/]+/[^/]+?)(?:\.git)?/?\z}, 1].to_s
    end

    #: (Hash[String, String]) -> String
    def self.path(env)
      explicit = env["GH_CSD_CONFIG"].to_s
      return explicit unless explicit.empty?

      base = env["XDG_CONFIG_HOME"].to_s
      base = File.join(env["HOME"].to_s, ".config") if base.empty?
      File.join(base, "gh-csd", "config.yml")
    end

    #: (Hash[String, String], String) -> Hash[String, String]
    def self.load(env, repo)
      file = path(env)
      return {} unless File.exist?(file)

      parse(File.read(file), file, repo)
    end

    # Flattens the file to strings, repo's section over the top level, so
    # nothing untyped from YAML leaves this method (hence its length).
    #: (String, String, String) -> Hash[String, String]
    def self.parse(text, file, repo)
      data = begin
        YAML.safe_load(text)
      rescue StandardError => e
        raise Error, "#{file} is not valid YAML: #{e.message}"
      end
      return {} if data.nil?
      raise Error, "#{file} must be a mapping of settings" unless data.is_a?(Hash)

      repos = data["repos"]
      section = repos.is_a?(Hash) ? repos[repo] : nil
      settings = {}
      [data, section.is_a?(Hash) ? section : {}].each do |source|
        source.each do |key, value|
          next if key == "repos" || value.nil? || value.is_a?(Hash) || value.is_a?(Array)

          settings[key.to_s] = value.to_s
        end
      end
      settings
    end
  end
end
