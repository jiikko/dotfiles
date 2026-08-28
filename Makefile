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
  bin/av1ify \
  bin/binav1c \
  bin/concat \
  bin/disassemble_excel \
  bin/glogx \
  bin/lib/go_autobuild.zsh \
  bin/lockman \
  bin/parallel-each \
  bin/repair-mp4-timebase \
  bin/schedkeys \
  bin/validate-mp4 \
  bin/video_health \
  scripts/check_syntax.zsh \
  zshlib/_concat.zsh \
  zshlib/_concat_helpers.zsh \
  zshlib/_ensure_cli_with_brew.zsh \
  zshlib/_ffprobe_helpers.zsh \
  zshlib/_fs_helpers.zsh \
  zshlib/_repair.zsh \
  zshlib/_tmux_session.zsh \
  zshlib/_tmux_window_name.zsh

# shellcheck で静的解析する shell スクリプト (sh/bash 互換) = 発見された全 shell script から
# zsh 例外を除いた補集合。手書き列挙しない (発見された script は登録なしで自動的に lint 対象)。
SHELLCHECK_FILES := $(filter-out $(ZSH_SYNTAX_FILES),$(shell scripts/discover_shell_scripts.sh))

YAML_FILES := theme/colors.yml pre-commit-config.yml .github/dependabot.yml .github/workflows/tests.yml .github/workflows/lint.yml .github/workflows/karabiner.yml .github/workflows/bench.yml .github/workflows/src_glogx.yml .github/workflows/src_parallel-each.yml .github/workflows/src_disassemble_excel.yml .github/workflows/src_lockman.yml .github/workflows/src_schedkeys.yml .github/actions/setup-nvim/action.yml .github/actions/run-bench/action.yml
JSON_FILES := mac/karabiner.json _claude/settings.json _claude/keybindings.json
# ruby -c で構文チェックする ruby ファイル (Brewfile は brew の ruby DSL)。
# _gemrc は YAML だが yamllint default (document-start 必須等) に通らない形式のため
# YAML_FILES に入れず test-ruby-syntax 側で ruby -ryaml パースする。
RUBY_SYNTAX_FILES := Brewfile _pryrc
KARABINER_CLI := /Library/Application Support/org.pqrs/Karabiner-Elements/bin/karabiner_cli

.PHONY: ci-commands-heavy ci-commands-rest pull test test-changed test-runtime test-runtime-rest test-discovered test-discovered-heavy test-discovered-rest test-nvim test-tmux test-setup test-zshrc test-bats test-syntax test-shellcheck test-zsh-syntax test-yaml test-json test-karabiner test-actionlint test-gitconfig test-ruby-syntax test-gnu test-lint test-lint-tests test-ci-group-deps test-pipefail-grep-q test-trigger-log-writers test-platform-dialect test-go-lint test-go test-src

# settings.json の揮発キー (model/effort 等) を settings.local.json へ退避してから
# pull する。追跡対象の settings.json に混ざるマシンローカルな churn を取り除き、
# 複数セッション常駐中でも pull がコンフリクトしないようにする。
# 詳細: _claude/hooks/normalize-settings.sh
pull:
	@_claude/hooks/normalize-settings.sh
	@git pull --rebase

# test-src (= go lint + test) を含める。かつては test-go (テストのみ) だったが、
# CI (_go-project.yml) と test-changed の src 腕は lint + test を回すため、部分実行の
# 方が全体実行より厳しいねじれがあった。golangci-lint は各 src Makefile が go run 経由・
# バージョン固定で自己完結しており、追加のツールインストールは不要
# ⚠️ **集約にする** (issue 130)。prerequisite に並べる形 (`test: a b c`) だと、lint が 1 つ
#   落ちた日は **テストが 1 本も走らないまま赤を見る**。コミット前ゲートとして常用する入口なので、
#   全部走らせてから失敗をまとめて返す (所要時間より「その日の全ての赤が 1 回で見える」を採る)。
#   lint だけ先に見たいときは `make test-lint`。
test:
	@+$(call run_all_targets,test-lint test-runtime test-src)

# 変更したパスだけ検証する (CI の paths filter のローカル対応物)。写像とヘルプの
# 正本は scripts/test_changed.sh (--help)。共有 working tree では dirty に並行
# セッションの変更が混ざるため、PATHS は自動推定せず必ず明示で渡す。
# DRY_RUN は 1 のときだけ有効 (非空 truthy にすると DRY_RUN=0 が「無効の
# つもりでテスト未実行のまま green」になるため filter で厳密一致させる)。
# 例: make test-changed PATHS="_claude/settings.json src/glogx/tui.go"
test-changed:
	@./scripts/test_changed.sh $(if $(filter 1,$(DRY_RUN)),--dry-run) $(PATHS)

# tests/ 配下の任意ディレクトリを単体実行する汎用入口 (test-changed の写像先)。
# test-nvim 等の名前付きターゲットと同じ run_tests を使うため挙動は等価
test-dir:
	@[ -n "$(DIR)" ] || { echo "✗ DIR を指定してください (例: make test-dir DIR=tests/claude)" >&2; exit 1; }
	@$(call run_tests,$(DIR))

# run_all_targets は列挙したターゲットを**全部走らせてから**集約して失敗を返す。
#
# ⚠️ prerequisite に並べる形 (`test-runtime: a b c`) だと、先に落ちた 1 本で make が中断し、
#   後続が **1 度も実行されないまま CI ログから消える** (issue 109)。2026-08-25 に 2 回起き、
#   push 済みの tests/codex_fanout.bats の修正が数時間 CI 未検証のまま放置された
#   (赤の原因が別にある間、bats は永久に走らない)。
#   「CI が緑になったら確認する」は、この形では成立しない。
#
# ⚠️ 失敗したターゲット名を最後にまとめて出す。1 本目の失敗で埋もれると、後続の失敗が
#   スクロールの上に隠れて「1 件だけ直せばよい」と誤読される。
define run_all_targets
targets="$(1)"; \
[ -n "$$targets" ] || { echo "✗ run_all_targets に対象が 0 件 (呼び出しが壊れている)" >&2; exit 1; }; \
failed=""; \
for t in $$targets; do \
	$(MAKE) $$t || failed="$$failed $$t"; \
done; \
if [ -n "$$failed" ]; then \
	{ echo ""; echo "✗ 失敗したターゲット:$$failed"; } >&2; \
	exit 1; \
fi
endef

test-runtime:
	@+$(call run_all_targets,test-syntax test-discovered test-bats)

# tests/ 配下のテストを自動発見して実行する共通ルール。発見規約: test_*.sh (ファイル名に
# *helper* を含むものは除く。ヘルパーは lib/ か非 test_ 名で置く)。この規約を満たすファイルを
# 置くだけで実行対象になる = Makefile への登録が不要で、死蔵テスト (書いたのに CI で走らない)
# が構造的に発生しない。ファイル名の空白・改行は非対応 (旧・手動列挙時代と同じ前提)。
# 発見 0 件は fail にする (テストを持つディレクトリしか対象にしないため、0 件 = ディレクトリの
# 改名/不在や find の失敗がパイプに隠れて「未実行なのに成功」する状態。それを弾く)。
# ⚠️ **fail-fast しない** (並列版 run_tests_parallel と揃える)。1 本目で止めると、
#   ソート順で後ろのテストが **1 度も走らないまま CI ログから消える** (issue 109)。
#   壁をターゲット境界からファイル境界へ動かしただけになる — 実際 109 の 1 次修正は
#   test-bats を救っただけで、`.sh` 側の隠れは残っていた (敵対的レビューが実証)。
#   失敗は一時ファイルへ集め、最後にまとめて出す (`while` はパイプの subshell なので変数が返らない)。
define run_tests
tests=$$(find $(1) -type f -name 'test_*.sh' ! -name '*helper*' -print | sort); \
[ -n "$$tests" ] || { echo "✗ $(1) 配下にテストが見つかりません (find 失敗 or 0 件)。本当に test_*.sh が無いディレクトリなら、テストを足すか scripts/test_changed.sh の写像の振り先を直す (issue 063 の同型)" >&2; exit 1; }; \
fails=$$(mktemp); \
printf '%s\n' "$$tests" | while IFS= read -r t; do echo "[run] $$t"; "$$t" || echo "$$t" >> "$$fails"; done; \
if [ -s "$$fails" ]; then { echo ""; echo "✗ 失敗したテスト:"; sed 's/^/  /' "$$fails"; } >&2; rm -f "$$fails"; exit 1; fi; \
rm -f "$$fails"
endef

# run_tests の並列版。各テストは独自 tempdir で独立しているものだけに使うこと
# (nvim/tmux 系は共有資源の競合が未検証のため直列の run_tests のまま)。出力は
# テストごとにファイルへ隔離し、失敗時にまとめて吐く (並列で行が混ざるのを防ぐ)。
# fail-fast はしない (xargs は失敗後も残りを流し、最後に非 0 (123) を返す)。
# parallel-each は不採用: CI runner に Go が無くビルドできない・retries/resume の
# 既定がテスト用途と合わない (状態ファイルを repo に作る) ため、素の xargs -P を使う。
NPROC := $(shell getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)
define run_tests_parallel
tests=$$(find $(1) -type f -name 'test_*.sh' ! -name '*helper*' -print | sort); \
[ -n "$$tests" ] || { echo "✗ $(1) 配下にテストが見つかりません (find 失敗 or 0 件)。本当に test_*.sh が無いディレクトリなら、テストを足すか scripts/test_changed.sh の写像の振り先を直す (issue 063 の同型)" >&2; exit 1; }; \
printf '%s\n' "$$tests" | xargs -P $(NPROC) -n 1 sh -c \
	'out=$$(mktemp); if "$$0" >"$$out" 2>&1; then echo "[ok] $$0"; rm -f "$$out"; else echo "[FAIL] $$0"; cat "$$out"; rm -f "$$out"; exit 1; fi'
endef

# test-runtime の実行本体。tests/ 全体を走査するため、新ディレクトリ tests/foo/ を作っても
# 自動で拾われる (ディレクトリ単位の死蔵も発生しない)。
test-discovered:
	@$(call run_tests,tests)

# tests/ 全体を「GNU grep が grep として見える」環境で回す (issue 108)。
# BSD grep (macOS) と GNU grep (Linux CI) は正規表現の方言が違うため、テストが手元だけ
# 緑・CI だけ赤になる。観測ログ系の assert や grep のパターンを触ったら、push 前にこれを
# 回すと CI の往復が減る。opt-in なのは `make test` の所要時間を倍にしないため。
# GNU grep が無い macOS では **失敗する** (skip して緑を返さない)。詳細は
# scripts/with_gnu_grep.sh の冒頭コメント。
test-gnu:
	@rc=; scripts/with_gnu_grep.sh $(MAKE) test-discovered || rc=1; \
		scripts/with_gnu_grep.sh $(MAKE) test-bats || rc=1; \
		[ -z "$${rc:-}" ] || { echo "" >&2; echo "✗ test-gnu: 失敗あり (上の出力を確認)" >&2; exit 1; }

# CI (tests.yml) の並列分割用。実行時間の大きい ffmpeg 系 (av1ify/concat) を heavy として
# 分離し、rest は「tests/ 全体から heavy を除外」の除外方式にする。新ディレクトリは自動で
# rest 側に入るため、グループ列挙の更新漏れによる死蔵は発生しない。heavy のパスを
# 移動/改名した場合は run_tests の 0 件検知が heavy 側を fail させるのでここを更新する。
CI_HEAVY_TEST_DIRS := tests/zshrc/av1ify tests/zshrc/concat
CI_HEAVY_PRUNE := \( $(foreach d,$(CI_HEAVY_TEST_DIRS),-path $(d) -o) -false \) -prune -o

# CI (tests.yml) が各グループで用意するランタイム依存 (コマンド名)。brew の formula 名が
# 違うもの (bats → bats-core / gtimeout → coreutils) の写像は tests.yml の case 文にある。
# グループ定義 (上の
# CI_HEAVY_TEST_DIRS) と同じ場所に置く: workflow 側にハードコードすると、heavy に bats/tmux
# 依存のテストを足したとき「Makefile だけ直して CI が command not found で落ちる」まで
# 気づけない (どのディレクトリが heavy かの出典はここ、依存の出典は workflow、の二重管理)。
# heavy は zsh テストのみなので tmux/bats を省いて ~60s 節約している。
CI_COMMANDS_HEAVY := zsh make
CI_COMMANDS_REST  := tmux zsh make bats gtimeout
# rest にはあるが heavy には無い = heavy で使うと CI が落ちるコマンド (乖離検査の対象)
CI_COMMANDS_ONLY_REST := $(filter-out $(CI_COMMANDS_HEAVY),$(CI_COMMANDS_REST))

# 値の確認用 (人間と test-ci-group-deps の照合対象)。
# ⚠️ workflow はこのターゲットを呼べない: 依存を解決する時点で make があるとは限らないため。
# workflow は Makefile の生の行を sed で読む = 実際の契約は「1 行の `:=` で書くこと」で、
# make の意味論 (+= / 行継続 / $(OTHER)) は通らない。
# その食い違いは test-ci-group-deps が make の値と sed の結果を突き合わせて検出する。
ci-commands-heavy:
	@echo '$(CI_COMMANDS_HEAVY)'
ci-commands-rest:
	@echo '$(CI_COMMANDS_REST)'

# heavy グループのテストが「heavy に入れていないコマンド」へ依存していないか検査する。
# 依存の決定は人間が行う (= 二重管理そのものは残る) ので、乖離だけを機械的に落とす。
test-ci-group-deps:
	@CI_HEAVY_TEST_DIRS='$(CI_HEAVY_TEST_DIRS)' CI_COMMANDS_ONLY_REST='$(CI_COMMANDS_ONLY_REST)' \
		CI_COMMANDS_HEAVY='$(CI_COMMANDS_HEAVY)' CI_COMMANDS_REST='$(CI_COMMANDS_REST)' \
		scripts/check_ci_group_deps.sh

# `… | grep -q` が pipefail 下で判定を反転させる形を落とす (issue 096)。
# 正本は scripts/check_pipefail_grep_q.sh (なぜ危険か・直し方・例外マーカーはそこに書いてある)。
test-pipefail-grep-q:
	@scripts/check_pipefail_grep_q.sh

# BSD (macOS) 専用の stat / date の書き方が Linux (CI) で壊れる形を落とす。
# 正本は scripts/check_platform_dialect.sh (実測・直し方・例外マーカーはそこに書いてある)。
test-platform-dialect:
	@scripts/check_platform_dialect.sh

# 共有観測ログ (tt-restore-trigger.log) の書き手が guards.sh の tt_trigger_log 以外に増えるのを落とす。
# 正本は scripts/check_trigger_log_writers.sh (なぜ危険か・例外マーカーはそこに書いてある)。
test-trigger-log-writers:
	@scripts/check_trigger_log_writers.sh

# heavy は 21 本 × ~16s (CI 実測) の直列で 5.6 分に育ったため並列実行する
# (av1ify/concat は tempdir 独立で並列安全。2026-07-20 に 338s → 数十秒へ)
test-discovered-heavy:
	@$(call run_tests_parallel,$(CI_HEAVY_TEST_DIRS))

test-discovered-rest:
	@$(call run_tests,tests $(CI_HEAVY_PRUNE))

# CI の rest ジョブ入口 (test-runtime から test-discovered を rest に差し替えたもの)
test-runtime-rest:
	@+$(call run_all_targets,test-syntax test-discovered-rest test-bats)

# 以下の test-<領域> は人間の選択実行用の便宜フィルタ (test-dir の別名)。test-runtime の
# 実行経路は test-discovered に一本化されているため、新領域をここに足し忘れても死蔵は生まない。
test-nvim:
	@$(MAKE) test-dir DIR=tests/nvim

test-tmux:
	@$(MAKE) test-dir DIR=tests/tmux

test-setup:
	@$(MAKE) test-dir DIR=tests/setup

test-zshrc:
	@$(MAKE) test-dir DIR=tests/zshrc

# .bats も同じ規約で自動発見する (発見 0 件なら何もせず成功)。bats 未インストール環境では skip。
test-bats:
	@if command -v bats >/dev/null 2>&1; then \
		fails=$$(mktemp); \
		find tests -type f -name '*.bats' ! -name '*helper*' | sort | \
			while IFS= read -r t; do echo "[run] $$t"; bats "$$t" || echo "$$t" >> "$$fails"; done; \
		if [ -s "$$fails" ]; then { echo ""; echo "✗ 失敗した bats:"; sed 's/^/  /' "$$fails"; } >&2; rm -f "$$fails"; exit 1; fi; \
		rm -f "$$fails"; \
	else \
		echo "bats not found, skipping bats tests"; \
	fi

test-syntax:
	@./scripts/check_syntax.zsh

# 1 行目の素実行は発見処理の失敗検知: $(shell) は discover script の exit code を捨てるため、
# recipe 側で一度実行して find の失敗 (ディレクトリ不在等) を顕在化する。
# CI (lint.yml) は shellcheck v0.11.0 をリリースバイナリで固定導入する = 手元の brew 0.11 と同版。
# ⚠️ 手元を上げたら lint.yml の SHELLCHECK_VERSION も同じ版へ上げること。版が離れると「手元 green /
# CI 赤」が復活する (実例 2026-07-25: apt の 0.9 系だけが `[ -n "$$x" ] && y || true` を SC2015 で落とした)。
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

# tests/ 配下のテストスクリプト lint。本体スクリプトの発見 (discover_shell_scripts.sh)
# が tests/ を対象外にしているのを補完する (shebang 機械分類なので例外リスト不要)。
# 方言判定・severity の理由はスクリプト冒頭コメントが正本
test-lint-tests:
	@./scripts/lint_test_scripts.sh

# ⚠️ prerequisite に並べない (issue 109 と同型)。12 本の直列だと 1 本目の失敗で残りが
#   1 度も走らず、lint.yml は `make test-lint` の 1 ステップなので CI ログにも出ない。
#   **末尾の新設検査ほど隠れやすい** (実測: test-json を落とすと後続 7 本が未実行)。
test-lint:
	@+$(call run_all_targets,test-shellcheck test-zsh-syntax test-lint-tests test-yaml test-json test-karabiner test-actionlint test-gitconfig test-ruby-syntax test-ci-group-deps test-pipefail-grep-q test-trigger-log-writers test-platform-dialect)

# Go プロジェクトの静的解析とテスト。実体は各ディレクトリの Makefile の lint / test
# ターゲットに閉じており、ここはそれへ委譲するだけ (ローカルのコミット前検証用。root の
# `make test` に test-go を含める = Go テストの漏れ防止)。CI ではプロジェクトごとの専用
# workflow (.github/workflows/src_*.yml、paths filter 付き) が同じ lint / test を回す。
# 各 src_*.yml は再利用 workflow _go-project.yml を呼ぶだけの薄い caller。
# どちらも Go 未インストール環境では skip する。Go プロジェクトを追加したら
# ①各プロジェクトに Makefile (lint/test) ②src_<project>.yml (caller) を作る、の 2 点セット。
#
# ⚠️ 対象は **`src/*/go.mod` の存在で発見する**。手で列挙すると、新しく src/foo を切ったときに
#   lint / test から**無音で外れる** (make は緑のまま通り「lint も test も通っている」と読める)。
#   この repo は他の全域で「登録なしで対象になる」を徹底しており (shellcheck は
#   scripts/discover_shell_scripts.sh の発見、テストは test-dir の自動発見)、Go だけ手動なのは
#   非対称だった (issue 080)。
# ⚠️ 発見 0 件は**失敗させる** (下の recipe のガード)。発見式のゲートが 0 件で緑になるのは
#   `_claude/rules/adversarial-review-own-safeguards.md` が禁じる false green そのもの。
GO_PROJECT_DIRS := $(patsubst %/,%,$(dir $(wildcard src/*/go.mod)))

# ⚠️ **最初のプロジェクトの失敗で残りを隠さない** (issue 130)。全プロジェクトを回してから
#   失敗したディレクトリをまとめて返す (run_all_targets と同じ規律。CI は src_*.yml で
#   プロジェクト別に分かれているため無傷だったが、ローカルの方が弱い状態だった)。
# ⚠️ go 未導入は skip して緑にする。0 件は上のガードで失敗させるので、
#   「発見が壊れた」と「go が無い」は別の結果として出る。
define run_go_projects
if [ -z "$(strip $(GO_PROJECT_DIRS))" ]; then \
	echo "[go-$(1)] src/*/go.mod が 1 つも見つからない (発見の仕方が壊れている)" >&2; exit 1; \
fi; \
if ! command -v go >/dev/null 2>&1; then \
	echo "[go-$(1)] go not found; skipping"; exit 0; \
fi; \
failed=""; \
for dir in $(GO_PROJECT_DIRS); do \
	$(MAKE) -C $$dir $(1) || failed="$$failed $$dir"; \
done; \
if [ -n "$$failed" ]; then \
	{ echo ""; echo "✗ [go-$(1)] 失敗したプロジェクト:$$failed"; } >&2; \
	exit 1; \
fi
endef

test-go-lint:
	@+$(call run_go_projects,lint)

test-go:
	@+$(call run_go_projects,test)

# src/ 配下の全プロジェクトを lint + test 一括で回す集約ターゲット (人間の選択実行用)。
# root の `make test` は test-go (テストのみ) を含むが golangci-lint は含まないため、
# src/ を触った後のコミット前検証はこれ 1 発で CI (src_*.yml の lint / test 両 job) と揃う。
# ⚠️ ここも集約 (issue 130)。prerequisite に並べると lint の失敗で test が走らない
test-src:
	@+$(call run_all_targets,test-go-lint test-go)

