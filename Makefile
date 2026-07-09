SHELL := /bin/sh

# shellcheck で静的解析する shell スクリプト (sh/bash 互換のもの)。
# zsh 固有構文のファイルは shellcheck が解析できない (SC1071) ため ZSH_SYNTAX_FILES へ。
SHELLCHECK_FILES := \
  setup.sh \
  bin/backup_karabiner_config.sh \
  bin/concat_movies \
  bin/lgtm.sh \
  bin/mp \
  bin/repair_avi_vorbis_audio.sh \
  bin/reset-universalcontrol \
  bin/restore_karabiner_config.sh \
  bin/sync_ratelimit_calendar.sh \
  bin/total_duration \
  bin/update-claudecode \
  bin/lib/video_args.sh \
  scripts/tmux_fork_popup.sh \
  scripts/tmux_fzf_jump.sh \
  scripts/tmux_fzf_pane_move.sh \
  scripts/tmux_log_session_closed.sh \
  scripts/tmux_reap_orphan_servers.sh \
  scripts/tmux_refresh_all_clients.sh \
  scripts/tmux_resurrect_debounced_save.sh \
  scripts/tmux_extract_popup.sh \
  scripts/lib/tmux_popup_sessions.sh \
  scripts/lib/tmux_resurrect_guards.sh \
  scripts/tmux_launcher_run.sh \
  scripts/tmux_version_gte.sh \
  scripts/tmux_resurrect_save.sh \
  scripts/tmux_scratch_popup.sh \
  zshlib/_av1ify.zsh \
  zshlib/_av1ify_encode.zsh \
  zshlib/_av1ify_postcheck.zsh \
  zshlib/_repair_mp4.zsh \
  zshlib/_repair_mp4_timebase.zsh \
  zshlib/_validate_mp4.zsh \
  zshlib/_video_health.zsh

# zsh 固有構文のため shellcheck できないスクリプト。zsh -n で構文チェックする (test-zsh-syntax)。
ZSH_SYNTAX_FILES := \
  bin/av1c \
  bin/av1ify \
  bin/concat \
  bin/disassemble_excel \
  bin/parallel-each \
  bin/repair-mp4-timebase \
  bin/validate-mp4 \
  bin/video_health \
  scripts/check_syntax.zsh \
  zshlib/_concat.zsh \
  zshlib/_concat_helpers.zsh \
  zshlib/_ensure_cli_with_brew.zsh \
  zshlib/_repair.zsh \
  zshlib/_tmux_session.zsh \
  zshlib/_tmux_window_name.zsh
YAML_FILES := pre-commit-config.yml .github/dependabot.yml .github/workflows/tests.yml .github/workflows/lint.yml .github/workflows/karabiner.yml
JSON_FILES := mac/karabiner.json
KARABINER_CLI := /Library/Application Support/org.pqrs/Karabiner-Elements/bin/karabiner_cli

.PHONY: pull test test-runtime test-nvim test-tmux test-setup test-zshrc test-bats test-syntax test-shellcheck test-zsh-syntax test-yaml test-json test-karabiner test-lint test-go-lint test-registration

# settings.json の揮発キー (model/effort 等) を settings.local.json へ退避してから
# pull する。追跡対象の settings.json に混ざるマシンローカルな churn を取り除き、
# 複数セッション常駐中でも pull がコンフリクトしないようにする。
# 詳細: _claude/hooks/normalize-settings.sh
pull:
	@_claude/hooks/normalize-settings.sh
	@git pull --rebase

test: test-lint test-runtime

test-runtime: test-syntax test-zshrc test-bats test-nvim test-tmux test-setup test-registration

test-nvim:
	@tests/nvim/test_nvim.sh
	@tests/nvim/test_ftplugins.sh

test-tmux:
	@tests/tmux/test_tmux.sh
	@tests/tmux/test_fork_scratch.sh
	@tests/tmux/test_reap_orphan_servers.sh
	@tests/tmux/test_version_gte.sh

test-setup:
	@tests/setup/test_setup.sh

test-zshrc:
	@tests/zshrc/test_zshrc.sh
	@tests/zshrc/ai-commands/test_ai_commands.sh
	@tests/zshrc/tmux-window-name/test_tmux_window_name.sh
	@tests/zshrc/tmux-session/test_tt.sh
	@tests/zshrc/tmux-session/test_debounced_save.sh
	@tests/zshrc/tmux-session/test_resurrect_save_lock.sh
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
	@tests/zshrc/concat/test_concat_space_grouping.sh
	@tests/zshrc/concat/test_concat_stdout_leak.sh
	@tests/zshrc/concat/test_concat_time_base.sh
	@tests/zshrc/concat/test_concat_verify_order.sh
	@tests/zshrc/repair_mp4/test_repair_mp4.sh
	@tests/zshrc/repair_mp4/test_repair.sh
	@tests/zshrc/test_video_health.sh
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

# zsh 固有構文で shellcheck できないスクリプトを zsh -n で構文チェックする。
# zsh 未インストール環境では skip (lint.yml は zsh を入れているので CI では走る)。
test-zsh-syntax:
	@if command -v zsh >/dev/null 2>&1; then \
		for file in $(ZSH_SYNTAX_FILES); do zsh -n "$$file" || exit 1; done; \
		echo "[zsh-syntax] $(words $(ZSH_SYNTAX_FILES)) ファイル OK"; \
	else \
		echo "[zsh-syntax] zsh not found; skipping"; \
	fi

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

test-lint: test-shellcheck test-zsh-syntax test-yaml test-json test-karabiner

# src/parallel-each (Go) の静的解析。実体は src/parallel-each/Makefile の lint
# ターゲット (go run で golangci-lint をバージョン固定実行) に閉じており、ここは
# それへ委譲するだけ。zsh 系の test-lint とは分離し、CI では Go を用意した専用
# ジョブから呼ぶ。Go 未インストール環境では skip する。
test-go-lint:
	@if command -v go >/dev/null 2>&1; then \
		$(MAKE) -C src/parallel-each lint; \
	else \
		echo "[go-lint] go not found; skipping golangci-lint"; \
	fi

# tests/ 配下のテストが Makefile に登録されているか検証し、死蔵テストを防ぐ meta テスト。
test-registration:
	@tests/test_registration.sh
