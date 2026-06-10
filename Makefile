SHELL := /bin/sh

SHELLCHECK_FILES := setup.sh zshlib/_av1ify.zsh zshlib/_av1ify_postcheck.zsh zshlib/_av1ify_encode.zsh zshlib/_validate_mp4.zsh scripts/tmux_resurrect_debounced_save.sh scripts/tmux_resurrect_save.sh
YAML_FILES := pre-commit-config.yml .github/workflows/tests.yml .github/workflows/lint.yml
JSON_FILES := mac/karabiner.json _coc-settings.json
KARABINER_CLI := /Library/Application Support/org.pqrs/Karabiner-Elements/bin/karabiner_cli

.PHONY: test test-runtime test-nvim test-tmux test-setup test-zshrc test-bats test-syntax test-shellcheck test-yaml test-json test-karabiner test-lint

test: test-lint test-runtime

test-runtime: test-syntax test-zshrc test-bats test-nvim test-tmux test-setup

test-nvim:
	@tests/nvim/test_nvim.sh

test-tmux:
	@tests/tmux/test_tmux.sh

test-setup:
	@tests/setup/test_setup.sh

test-zshrc:
	@tests/zshrc/test_zshrc.sh
	@tests/zshrc/ai-commands/test_ai_commands.sh
	@tests/zshrc/tmux-window-name/test_tmux_window_name.sh
	@tests/zshrc/tmux-session/test_tt.sh
	@tests/zshrc/tmux-session/test_debounced_save.sh
	@tests/zshrc/av1ify/test_av1ify_basic.sh
	@tests/zshrc/av1ify/test_av1ify_audio.sh
	@tests/zshrc/av1ify/test_av1ify_options.sh
	@tests/zshrc/av1ify/test_av1ify_postcheck.sh
	@tests/zshrc/av1ify/test_av1ify_variants.sh
	@tests/zshrc/av1ify/test_av1ify_force.sh
	@tests/zshrc/av1ify/test_av1ify_ng_list.sh
	@tests/zshrc/av1ify/test_av1ify_prefetch.sh
	@tests/zshrc/av1ify/test_av1ify_avsync.sh
	@tests/zshrc/validate-mp4/test_validate_mp4.sh
	@tests/zshrc/concat/test_concat_basic.sh
	@tests/zshrc/concat/test_concat_edge.sh
	@tests/zshrc/concat/test_concat_missing.sh
	@tests/zshrc/concat/test_concat_frame_hash_seek.sh
	@tests/zshrc/concat/test_concat_cleanup.sh
	@tests/zshrc/concat/test_concat_force.sh
	@tests/zshrc/concat/test_concat_option_position.sh
	@tests/zshrc/concat/test_concat_output_info.sh
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

# karabiner.json の complex modifications を karabiner_cli で意味レベル lint する
# (test-json の jq empty は構文のみ。karabiner_cli は未知キー/型違いを検出する)。
# karabiner_cli は asset 形式 ({title, rules}) を期待するため jq で抽出して渡す。
# Karabiner-Elements 未インストール環境 (Linux CI 等) では skip
test-karabiner:
	@if [ -x "$(KARABINER_CLI)" ]; then \
		mkdir -p tmp; \
		jq '{title: "dotfiles karabiner rules", rules: .profiles[0].complex_modifications.rules}' mac/karabiner.json > tmp/karabiner-lint.json; \
		"$(KARABINER_CLI)" --lint-complex-modifications tmp/karabiner-lint.json; \
		rm -f tmp/karabiner-lint.json; \
	else \
		echo "[karabiner] karabiner_cli not found; skipping lint"; \
	fi

test-lint: test-shellcheck test-yaml test-json test-karabiner
