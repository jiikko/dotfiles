SHELL := /bin/sh

SHELLCHECK_FILES := setup.sh zshlib/_av1ify.zsh
YAML_FILES := pre-commit-config.yml .github/workflows/tests.yml .github/workflows/lint.yml
JSON_FILES := mac/karabiner.json _coc-settings.json Brewfile.lock.json

.PHONY: test test-runtime test-nvim test-tmux test-setup test-zshrc test-bats test-syntax test-shellcheck test-yaml test-json test-lint

test: test-lint test-runtime

test-runtime: test-syntax test-zshrc test-bats test-nvim test-tmux test-setup

test-nvim:
	@./scripts/test_nvim.zsh

test-tmux:
	@./scripts/test_tmux.zsh

test-setup:
	@./scripts/test_setup.zsh

test-zshrc:
	@tests/zshrc/test_zshrc.sh
	@tests/zshrc/ai-commands/test_ai_commands.sh
	@tests/zshrc/av1ify/test_av1ify.sh
	@tests/zshrc/lazy-loading/test_version_managers.sh

test-bats:
	@if command -v bats >/dev/null 2>&1; then \
		bats tests/_ensure_cli_with_brew.bats; \
	else \
		echo "bats not found, skipping bats tests"; \
	fi

test-syntax:
	@./scripts/check_syntax.zsh

test-shellcheck:
	@shellcheck $(SHELLCHECK_FILES)

test-yaml:
	@if command -v yamllint >/dev/null 2>&1; then \
		yamllint $(YAML_FILES); \
	else \
		echo "yamllint not found; falling back to ruby -ryaml syntax check"; \
		for file in $(YAML_FILES); do \
			ruby -ryaml -e "YAML.safe_load(File.read(ARGV.first))" "$$file"; \
		done; \
	fi

test-json:
	@for file in $(JSON_FILES); do jq empty "$$file"; done

test-lint: test-shellcheck test-yaml test-json
