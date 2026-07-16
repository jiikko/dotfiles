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
  scripts/tmux_resurrect_debounced_save.sh \
  scripts/tmux_extract_popup.sh \
  scripts/lib/tmux_popup_sessions.sh \
  scripts/lib/tmux_fzf_window_picker.sh \
  scripts/lib/tmux_resurrect_guards.sh \
  scripts/tmux_jump_last_touched.sh \
  scripts/tmux_launcher_run.sh \
  scripts/tmux_version_gte.sh \
  scripts/tmux_resurrect_save.sh \
  scripts/tmux_scratch_popup.sh \
  scripts/tmux_kill_confirm.sh \
  zshlib/_av1ify.zsh \
  zshlib/_av1ify_encode.zsh \
  zshlib/_av1ify_postcheck.zsh \
  zshlib/_repair_mp4.zsh \
  zshlib/_repair_mp4_timebase.zsh \
  zshlib/_validate_mp4.zsh \
  zshlib/_video_health.zsh \
  _claude/hooks/git-state-verify.sh \
  _claude/hooks/normalize-settings.sh \
  _claude/hooks/tmux-mark-seen.sh \
  _claude/hooks/tmux-pane-state.sh

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
  zshlib/_ffprobe_helpers.zsh \
  zshlib/_repair.zsh \
  zshlib/_tmux_session.zsh \
  zshlib/_tmux_window_name.zsh
YAML_FILES := pre-commit-config.yml .github/dependabot.yml .github/workflows/tests.yml .github/workflows/lint.yml .github/workflows/karabiner.yml
JSON_FILES := mac/karabiner.json _claude/settings.json _claude/keybindings.json
# ruby -c で構文チェックする ruby ファイル (Brewfile は brew の ruby DSL)。
# _gemrc は YAML だが yamllint default (document-start 必須等) に通らない形式のため
# YAML_FILES に入れず test-ruby-syntax 側で ruby -ryaml パースする。
RUBY_SYNTAX_FILES := Brewfile _pryrc
KARABINER_CLI := /Library/Application Support/org.pqrs/Karabiner-Elements/bin/karabiner_cli

.PHONY: pull test test-runtime test-discovered test-nvim test-tmux test-setup test-zshrc test-bats test-syntax test-shellcheck test-zsh-syntax test-yaml test-json test-karabiner test-actionlint test-gitconfig test-ruby-syntax test-lint test-go-lint test-lint-coverage print-lint-files

# settings.json の揮発キー (model/effort 等) を settings.local.json へ退避してから
# pull する。追跡対象の settings.json に混ざるマシンローカルな churn を取り除き、
# 複数セッション常駐中でも pull がコンフリクトしないようにする。
# 詳細: _claude/hooks/normalize-settings.sh
pull:
	@_claude/hooks/normalize-settings.sh
	@git pull --rebase

test: test-lint test-runtime

test-runtime: test-syntax test-discovered test-bats

# tests/ 配下のテストを自動発見して実行する共通ルール。発見規約: test_*.sh (ファイル名に
# *helper* を含むものは除く。ヘルパーは lib/ か非 test_ 名で置く)。この規約を満たすファイルを
# 置くだけで実行対象になる = Makefile への登録が不要で、死蔵テスト (書いたのに CI で走らない)
# が構造的に発生しない。ファイル名の空白・改行は非対応 (旧・手動列挙時代と同じ前提)。
# 発見 0 件は fail にする (テストを持つディレクトリしか対象にしないため、0 件 = ディレクトリの
# 改名/不在や find の失敗がパイプに隠れて「未実行なのに成功」する状態。それを弾く)。
define run_tests
tests=$$(find $(1) -type f -name 'test_*.sh' ! -name '*helper*' | sort); \
[ -n "$$tests" ] || { echo "✗ $(1) 配下にテストが見つかりません (find 失敗 or 0 件)" >&2; exit 1; }; \
printf '%s\n' "$$tests" | while IFS= read -r t; do echo "[run] $$t"; "$$t" || exit 1; done
endef

# test-runtime の実行本体。tests/ 全体を走査するため、新ディレクトリ tests/foo/ を作っても
# 自動で拾われる (ディレクトリ単位の死蔵も発生しない)。tests/ 直下の meta テスト
# (test_lint_coverage.sh) もここで走る (test-lint 側と重複実行になるが高速なので許容)。
test-discovered:
	@$(call run_tests,tests)

# 以下の test-<領域> は人間の選択実行用の便宜フィルタ。test-runtime の実行経路は
# test-discovered に一本化されているため、新領域をここに足し忘れても死蔵は生まない。
test-nvim:
	@$(call run_tests,tests/nvim)

test-tmux:
	@$(call run_tests,tests/tmux)

test-setup:
	@$(call run_tests,tests/setup)

test-zshrc:
	@$(call run_tests,tests/zshrc)

# .bats も同じ規約で自動発見する (発見 0 件なら何もせず成功)。bats 未インストール環境では skip。
test-bats:
	@if command -v bats >/dev/null 2>&1; then \
		find tests -type f -name '*.bats' ! -name '*helper*' | sort | \
			while IFS= read -r t; do echo "[run] $$t"; bats "$$t" || exit 1; done; \
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

# workflow yml を actionlint で意味レベル lint する (test-yaml の yamllint は YAML 構文のみ。
# actionlint は ${{ }} 式の typo・不正キー・run: ブロックの shellcheck を検出する)。
# actionlint 未インストール環境では skip (lint.yml が公式バイナリを入れるので CI では走る)。
test-actionlint:
	@if command -v actionlint >/dev/null 2>&1; then \
		actionlint; \
		echo "[actionlint] .github/workflows OK"; \
	else \
		echo "[actionlint] actionlint not found; skipping"; \
	fi

# _gitconfig の構文チェック。壊れた gitconfig は全 git コマンドを道連れにするため
# 専用ターゲットで守る (git config -f は parse エラーで非 0 を返す)。
test-gitconfig:
	@git config -f _gitconfig -l > /dev/null
	@echo "[gitconfig] _gitconfig OK"

# ruby 系設定ファイルの構文チェック (RUBY_SYNTAX_FILES) + _gemrc の YAML パース。
# ruby 未インストール環境では skip (lint.yml は ruby を入れているので CI では走る)。
test-ruby-syntax:
	@if command -v ruby >/dev/null 2>&1; then \
		for file in $(RUBY_SYNTAX_FILES); do ruby -c "$$file" > /dev/null || exit 1; done; \
		ruby -ryaml -e "YAML.safe_load(File.read('_gemrc'))" || exit 1; \
		echo "[ruby-syntax] $(words $(RUBY_SYNTAX_FILES)) ファイル + _gemrc OK"; \
	else \
		echo "[ruby-syntax] ruby not found; skipping"; \
	fi

test-lint: test-shellcheck test-zsh-syntax test-yaml test-json test-karabiner test-actionlint test-gitconfig test-ruby-syntax test-lint-coverage

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

# 全 shell script が lint リスト (SHELLCHECK_FILES / ZSH_SYNTAX_FILES) に登録され、かつ列挙が
# 実在するか検証する meta テスト。script 増減時のリスト追従漏れ (未 lint / 削除残りで shellcheck が
# "does not exist" 落ち) を構造的に防ぐ。test-lint に組み込み Lint CI で走る。
test-lint-coverage:
	@tests/test_lint_coverage.sh

# lint 対象リストを1行ずつ出力 (test_lint_coverage.sh が権威的に読むため。手動 grep パースを避ける)。
print-lint-files:
	@printf '%s\n' $(SHELLCHECK_FILES) $(ZSH_SYNTAX_FILES)
