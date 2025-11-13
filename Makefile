SHELL := /bin/sh

SHELLCHECK_FILES := setup.sh zshlib/_av1ify.zsh
YAML_FILES := pre-commit-config.yml .github/workflows/tests.yml
JSON_FILES := mac/karabiner.json _coc-settings.json Brewfile.lock.json


.PHONY: test test-nvim test-tmux test-setup test-zshrc test-syntax test-shellcheck test-yaml test-json

test: test-shellcheck test-yaml test-json test-syntax test-zshrc test-nvim test-tmux test-setup

test-nvim:
	@./scripts/test_nvim.zsh

test-tmux:
	@./scripts/test_tmux.zsh

test-setup:
	@./scripts/test_setup.zsh

test-zshrc:
	@tests/zshrc/test_zshrc.sh

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
