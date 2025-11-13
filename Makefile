SHELL := /bin/sh

.PHONY: test test-nvim test-tmux test-setup test-nvim-zsh test-tmux-zsh test-setup-zsh

test: test-nvim test-tmux test-setup

test-nvim:
	@./scripts/test_nvim.sh

test-tmux:
	@./scripts/test_tmux.sh

test-setup:
	@./scripts/test_setup.sh

test-nvim-zsh:
	@./scripts/test_nvim.zsh

test-tmux-zsh:
	@./scripts/test_tmux.zsh

test-setup-zsh:
	@./scripts/test_setup.zsh
