SHELL := /bin/sh

SHELLCHECK_FILES := setup.sh zshlib/_av1ify.zsh

.PHONY: test test-nvim test-tmux test-setup test-zshrc test-syntax test-shellcheck

test: test-shellcheck test-syntax test-zshrc test-nvim test-tmux test-setup

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
