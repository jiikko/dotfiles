SHELL := /bin/sh

# lint 対象の shell script は scripts/discover_shell_scripts.sh が機械的に発見する
# (拡張子 or shebang)。手動維持するのは下の ZSH_SYNTAX_FILES (zsh 例外) だけ。
#
# zsh 固有構文のため shellcheck が解析できない (SC1071) スクリプト。zsh -n で構文チェックする
# (test-zsh-syntax)。zsh 専用構文か否かは意味的性質で shebang/拡張子から機械判定できないため、
# この例外リストだけは手動維持する (同じ .zsh でも zshlib/_av1ify.zsh は sh 互換で shellcheck 側)。
# 新規スクリプトは既定で shellcheck 側に入る: zsh 専用構文なら test-shellcheck が SC1071 で
# 落ちるので、そのときここへ移す。
ZSH_SYNTAX_FILES := \
  bin/av1c \
  bin/av1ify \
  bin/concat \
  bin/disassemble_excel \
  bin/glog \
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

# shellcheck で静的解析する shell スクリプト (sh/bash 互換) = 発見された全 shell script から
# zsh 例外を除いた補集合。手書き列挙しない (発見された script は登録なしで自動的に lint 対象)。
SHELLCHECK_FILES := $(filter-out $(ZSH_SYNTAX_FILES),$(shell scripts/discover_shell_scripts.sh))

YAML_FILES := theme/colors.yml pre-commit-config.yml .github/dependabot.yml .github/workflows/tests.yml .github/workflows/lint.yml .github/workflows/karabiner.yml .github/workflows/bench.yml .github/workflows/src_glog.yml .github/workflows/src_git-popup.yml .github/workflows/src_parallel-each.yml .github/workflows/src_disassemble_excel.yml .github/actions/setup-nvim/action.yml
JSON_FILES := mac/karabiner.json _claude/settings.json _claude/keybindings.json
# ruby -c で構文チェックする ruby ファイル (Brewfile は brew の ruby DSL)。
# _gemrc は YAML だが yamllint default (document-start 必須等) に通らない形式のため
# YAML_FILES に入れず test-ruby-syntax 側で ruby -ryaml パースする。
RUBY_SYNTAX_FILES := Brewfile _pryrc
KARABINER_CLI := /Library/Application Support/org.pqrs/Karabiner-Elements/bin/karabiner_cli

.PHONY: pull test test-runtime test-discovered test-nvim test-tmux test-setup test-zshrc test-bats test-syntax test-shellcheck test-zsh-syntax test-yaml test-json test-karabiner test-actionlint test-gitconfig test-ruby-syntax test-lint test-go-lint test-go test-src

# settings.json の揮発キー (model/effort 等) を settings.local.json へ退避してから
# pull する。追跡対象の settings.json に混ざるマシンローカルな churn を取り除き、
# 複数セッション常駐中でも pull がコンフリクトしないようにする。
# 詳細: _claude/hooks/normalize-settings.sh
pull:
	@_claude/hooks/normalize-settings.sh
	@git pull --rebase

test: test-lint test-runtime test-go

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
# 自動で拾われる (ディレクトリ単位の死蔵も発生しない)。
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

# 1 行目の素実行は発見処理の失敗検知: $(shell) は discover script の exit code を捨てるため、
# recipe 側で一度実行して find の失敗 (ディレクトリ不在等) を顕在化する。
test-shellcheck:
	@scripts/discover_shell_scripts.sh >/dev/null
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

test-lint: test-shellcheck test-zsh-syntax test-yaml test-json test-karabiner test-actionlint test-gitconfig test-ruby-syntax

# Go プロジェクトの静的解析とテスト。実体は各ディレクトリの Makefile の lint / test
# ターゲットに閉じており、ここはそれへ委譲するだけ (ローカルのコミット前検証用。root の
# `make test` に test-go を含める = Go テストの漏れ防止)。CI ではプロジェクトごとの専用
# workflow (.github/workflows/src_*.yml、paths filter 付き) が同じ lint / test を回す。
# どちらも Go 未インストール環境では skip する。Go プロジェクトを追加したら
# ①各プロジェクトに Makefile (lint/test) ②ここへ列挙 ③src_*.yml を対で作る、の 3 点セット。
GO_PROJECT_DIRS := src/parallel-each src/glog src/disassemble_excel src/git-popup

test-go-lint:
	@if command -v go >/dev/null 2>&1; then \
		for dir in $(GO_PROJECT_DIRS); do $(MAKE) -C $$dir lint || exit 1; done; \
	else \
		echo "[go-lint] go not found; skipping golangci-lint"; \
	fi

test-go:
	@if command -v go >/dev/null 2>&1; then \
		for dir in $(GO_PROJECT_DIRS); do $(MAKE) -C $$dir test || exit 1; done; \
	else \
		echo "[go-test] go not found; skipping go tests"; \
	fi

# src/ 配下の全プロジェクトを lint + test 一括で回す集約ターゲット (人間の選択実行用)。
# root の `make test` は test-go (テストのみ) を含むが golangci-lint は含まないため、
# src/ を触った後のコミット前検証はこれ 1 発で CI (src_*.yml の lint / test 両 job) と揃う。
test-src: test-go-lint test-go

