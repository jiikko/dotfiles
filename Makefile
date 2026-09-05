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
  bin/diskdoctor \
  bin/glogx \
  bin/lib/go_autobuild.zsh \
  bin/lockman \
  bin/parallel-each \
  bin/repair-mp4-timebase \
  bin/svcdoctor \
  bin/schedkeys \
  bin/validate-mp4 \
  bin/video_health \
  scripts/check_syntax.zsh \
  zshlib/_concat.zsh \
  zshlib/_concat_helpers.zsh \
  zshlib/_ensure_cli_with_brew.zsh \
  zshlib/_ffprobe_helpers.zsh \
  zshlib/_fs_helpers.zsh \
  zshlib/_reload_then_call.zsh \
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

.PHONY: ci-commands-heavy ci-commands-rest pull test test-changed clean-tmp test-runtime test-runtime-rest test-discovered test-discovered-parallel test-discovered-serial test-discovered-heavy test-discovered-rest test-discovered-rest-parallel test-nvim test-tmux test-setup test-zshrc test-bats test-syntax test-shellcheck test-zsh-syntax test-yaml test-json test-karabiner test-actionlint test-gitconfig test-ruby-syntax test-lint test-lint-tests test-ci-group-deps test-pipefail-grep-q test-cd-rc test-trigger-log-writers test-skip-exit-code test-workflow-action-pins test-go-project-lanes test-go-lint test-go test-src test-fresh

# ./tmp のスクラッチを掃除する (既定は 30 日より古いトップレベルのエントリ)。
#
# ./tmp は「Claude がセッション中に作る成果物 (レポート・中間生成物)」の置き場
# (~/.claude/CLAUDE.md「一時ファイルの配置」)。gitignore なので放っておくと溜まる一方で、
# 2026-09-02 時点で 309 エントリ / 831MB あった (最古は 7 月)。
#
# 🚨 **消す前に、その中身の結論が issue かコードへ移っているか確かめる**
# (`_claude/rules/move-report-conclusions-to-issues.md`)。レポート本体は消えてよいが、
# 却下理由と全数勘定が tmp にしか無い状態で消すと、次の audit が同じ指摘を再生成する。
#
# 🚨 **issue やドキュメントが指している tmp のパスは消すと参照が切れる**。DAYS を絞るだけでは
# 防げないので、まず `make clean-tmp DRY_RUN=1` で一覧を見る。参照の有無は
# `grep -rn 'tmp/' issues/ _claude/` で確かめる (指している側が間違っていることも多い)。
#
# 使い方:
#   make clean-tmp              # 30 日より古いものを消す
#   make clean-tmp DRY_RUN=1    # 消さずに一覧と合計サイズだけ出す
#   make clean-tmp DAYS=7       # 7 日より古いものを消す
clean-tmp: DAYS ?= 30
clean-tmp:
	@[ -d tmp ] || { echo "tmp/ が無い (掃除するものなし)"; exit 0; }
	@list="$$(find tmp -maxdepth 1 -mindepth 1 -mtime +$(DAYS) | sort)"; \
	if [ -z "$$list" ]; then echo "✓ $(DAYS) 日より古い tmp のエントリは 0 件"; exit 0; fi; \
	n="$$(printf '%s\n' "$$list" | wc -l | tr -d ' ')"; \
	size="$$(printf '%s\n' "$$list" | tr '\n' '\0' | xargs -0 du -sk 2>/dev/null | awk '{s+=$$1} END {printf "%.0f", s/1024}')"; \
	printf '%s\n' "$$list" | sed 's/^/  /'; \
	if [ "$(DRY_RUN)" = "1" ]; then \
		echo "(DRY_RUN) $${n} 件 / $${size}MB が対象。消すには DRY_RUN を外す"; \
	else \
		printf '%s\n' "$$list" | tr '\n' '\0' | xargs -0 rm -rf; \
		echo "✓ $${n} 件 / $${size}MB を削除した (残り $$(ls tmp | wc -l | tr -d ' ') エントリ)"; \
	fi

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
# 🚨 **集約にする** (issue 130)。prerequisite に並べる形 (`test: a b c`) だと、lint が 1 つ
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
# 🚨 prerequisite に並べる形 (`test-runtime: a b c`) だと、先に落ちた 1 本で make が中断し、
#   後続が **1 度も実行されないまま CI ログから消える** (issue 109)。2026-08-25 に 2 回起き、
#   push 済みの tests/codex_fanout.bats の修正が数時間 CI 未検証のまま放置された
#   (赤の原因が別にある間、bats は永久に走らない)。
#   「CI が緑になったら確認する」は、この形では成立しない。
#
# 🚨 失敗したターゲット名を最後にまとめて出す。1 本目の失敗で埋もれると、後続の失敗が
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

# run_tests / run_tests_parallel が出した失敗・skip の一覧を、腕をまたいで合算するための受け口。
# TEST_FAILS_SUMMARY / TEST_SKIPS_SUMMARY (追記先のファイルパス) が環境に在るときだけ追記する。
# 単体で `make test-tmux` 等を叩いたときは未設定なので何もしない。
define append_test_summary
if [ -n "$${TEST_FAILS_SUMMARY:-}" ]; then cat "$(1)" >> "$$TEST_FAILS_SUMMARY"; fi; \
if [ -n "$${TEST_SKIPS_SUMMARY:-}" ]; then cat "$(2)" >> "$$TEST_SKIPS_SUMMARY"; fi
endef

# run_all_targets の上に「両腕の失敗テスト名 / skip 件数の合算」を重ねる (test-discovered 系専用)。
# 腕ごとに出る `✗ 失敗したテスト:` は後続の腕のログで上へ流れ、run_all_targets の末尾には
# ターゲット名しか出ないので、最後にテスト名を再掲する (1 本目の失敗で埋もれて「1 件だけ直せば
# よい」と誤読される、を腕の単位で防ぐ)。skip も腕ごとの数を人が足さずに済むよう合計を出す。
# 集約の中身 (全部走らせる / ターゲット名をまとめる) は run_all_targets のまま (subshell で包み、
# その exit は rc として受け取る)。skip は失敗ではないので rc に影響させない。
define run_test_arms
sum_f=$$(mktemp); sum_s=$$(mktemp); rc=0; \
( export TEST_FAILS_SUMMARY="$$sum_f" TEST_SKIPS_SUMMARY="$$sum_s"; $(call run_all_targets,$(1)) ) || rc=$$?; \
if [ -s "$$sum_s" ]; then { echo ""; echo "[skip] 全腕合計: 丸ごと skip したテスト $$(wc -l < "$$sum_s" | tr -d ' ') 件 (失敗ではない。増えていたら理由を確かめる):"; sed 's/^/  /' "$$sum_s"; }; fi; \
if [ -s "$$sum_f" ]; then { echo ""; echo "✗ 全腕合計: 失敗したテスト $$(wc -l < "$$sum_f" | tr -d ' ') 件:"; sed 's/^/  /' "$$sum_f"; } >&2; fi; \
rm -f "$$sum_f" "$$sum_s"; \
exit $$rc
endef

test-runtime:
	@+$(call run_all_targets,test-syntax test-discovered test-bats)

# tests/ 配下のテストを自動発見して実行する共通ルール。発見規約: test_*.sh (ファイル名に
# *helper* を含むものは除く。ヘルパーは lib/ か非 test_ 名で置く)。この規約を満たすファイルを
# 置くだけで実行対象になる = Makefile への登録が不要で、死蔵テスト (書いたのに CI で走らない)
# が構造的に発生しない。ファイル名の空白・改行は非対応 (旧・手動列挙時代と同じ前提)。
# 発見 0 件は fail にする (テストを持つディレクトリしか対象にしないため、0 件 = ディレクトリの
# 改名/不在や find の失敗がパイプに隠れて「未実行なのに成功」する状態。それを弾く)。
# 🚨 **fail-fast しない** (並列版 run_tests_parallel と揃える)。1 本目で止めると、
#   ソート順で後ろのテストが **1 度も走らないまま CI ログから消える** (issue 109)。
#   壁をターゲット境界からファイル境界へ動かしただけになる — 実際 109 の 1 次修正は
#   test-bats を救っただけで、`.sh` 側の隠れは残っていた (敵対的レビューが実証)。
#   失敗は一時ファイルへ集め、最後にまとめて出す (`while` はパイプの subshell なので変数が返らない)。
define run_tests
tests=$$(find $(1) -type f -name 'test_*.sh' ! -name '*helper*' -print | sort); \
[ -n "$$tests" ] || { echo "✗ $(1) 配下にテストが見つかりません (find 失敗 or 0 件)。本当に test_*.sh が無いディレクトリなら、テストを足すか scripts/test_changed.sh の写像の振り先を直す (issue 063 の同型)" >&2; exit 1; }; \
fails=$$(mktemp); skips=$$(mktemp); \
printf '%s\n' "$$tests" | while IFS= read -r t; do echo "[run] $$t"; \
	if "$$t"; then :; else rc=$$?; if [ "$$rc" -eq 77 ]; then echo "$$t" >> "$$skips"; else echo "$$t" >> "$$fails"; fi; fi; done; \
$(call append_test_summary,$$fails,$$skips); \
if [ -s "$$skips" ]; then { echo ""; echo "[skip] 丸ごと skip したテスト $$(wc -l < "$$skips" | tr -d ' ') 件 (失敗ではない。増えていたら理由を確かめる):"; sed 's/^/  /' "$$skips"; }; fi; \
if [ -s "$$fails" ]; then { echo ""; echo "✗ 失敗したテスト:"; sed 's/^/  /' "$$fails"; } >&2; rm -f "$$fails" "$$skips"; exit 1; fi; \
rm -f "$$fails" "$$skips"
endef

# run_tests の並列版。各テストは独自 tempdir で独立しているものだけに使うこと
# (共有資源に触る領域は SERIAL_TEST_DIRS に列挙して直列の run_tests に回す)。
# 🚨 **exit 77 = そのファイルは丸ごと skip した** (automake の慣例)。0 と区別しないと、依存が
# 無くて何も検査しなかったテストが合格と同じ `[ok]` になる。実害: 2026-08-29 に
# test_deny_bare_tmux_kill.sh が timeout(1) 不在で丸ごと skip し、**60 件の assert が消えたのに
# `[ok]`** と集計されていた (issue 139)。skip 自体は失敗ではないので緑のままにするが、
# **件数と一覧を出して「増えた」に気づける**ようにする (直列版と同じ形で最後に出す)。
# テストごとにファイルへ隔離し、失敗時にまとめて吐く (並列で行が混ざるのを防ぐ)。
# fail-fast はしない (xargs は失敗後も残りを流す)。失敗・skip・実行済みの一覧は追記専用の一時
# ファイルに集める (短い 1 行の O_APPEND は並列でも混ざらない)。判定はその一覧で行い、xargs の
# 終了コードは「一覧が空なのに非 0」= runner 自体の故障を拾うためだけに見る (sh 不在等を緑にしない)。
# 🚨 **入力の本数と「結果を報告した本数」を突き合わせる**。xargs は utility がシグナルで死ぬか
# exit 255 で終わると **残りの入力を捨てて** 打ち切る (man xargs)。このとき捨てられたテストは
# 失敗一覧にも skip 一覧にも出ないので、他に 1 本でも普通に落ちていれば「失敗 1 件」に見えて
# **未実行が緑の中に埋もれる**。実測 2026-09-05: 12 本中 1 本が sh ごと kill されると報告は 11 本に
# なり、出力は通常失敗 1 件だけだった。件数を出すのは「走った証拠」を毎回残すため
# (_claude/rules/verify-execution-not-just-exit-code.md)。
# parallel-each は不採用: CI runner に Go が無くビルドできない・retries/resume の
# 既定がテスト用途と合わない (状態ファイルを repo に作る) ため、素の xargs -P を使う。
NPROC := $(shell getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)
define run_tests_parallel
tests=$$(find $(1) -type f -name 'test_*.sh' ! -name '*helper*' -print | sort); \
[ -n "$$tests" ] || { echo "✗ $(1) 配下にテストが見つかりません (find 失敗 or 0 件)。本当に test_*.sh が無いディレクトリなら、テストを足すか scripts/test_changed.sh の写像の振り先を直す (issue 063 の同型)" >&2; exit 1; }; \
fails=$$(mktemp); skips=$$(mktemp); ran=$$(mktemp); exp=$$(mktemp); miss=$$(mktemp); xrc=0; rc=0; \
printf '%s\n' "$$tests" | FAILS="$$fails" SKIPS="$$skips" RAN="$$ran" xargs -P $(NPROC) -n 1 sh -c \
	'out=$$(mktemp); "$$0" >"$$out" 2>&1; rc=$$?; echo "$$0" >> "$$RAN"; \
	if [ "$$rc" -eq 0 ]; then echo "[ok] $$0"; \
	elif [ "$$rc" -eq 77 ]; then echo "[skip] $$0"; cat "$$out"; echo "$$0" >> "$$SKIPS"; \
	else echo "[FAIL] $$0"; cat "$$out"; echo "$$0" >> "$$FAILS"; rm -f "$$out"; exit 1; fi; rm -f "$$out"' || xrc=$$?; \
printf '%s\n' "$$tests" | LC_ALL=C sort > "$$exp"; LC_ALL=C sort "$$ran" > "$$ran.s"; \
LC_ALL=C comm -23 "$$exp" "$$ran.s" > "$$miss"; \
echo ""; echo "[並列] 対象 $$(wc -l < "$$exp" | tr -d ' ') 件 / 結果を報告 $$(wc -l < "$$ran.s" | tr -d ' ') 件"; \
if [ -s "$$skips" ]; then { echo ""; echo "[skip] 丸ごと skip したテスト $$(wc -l < "$$skips" | tr -d ' ') 件 (失敗ではない。増えていたら理由を確かめる):"; sed 's/^/  /' "$$skips"; }; fi; \
if [ -s "$$miss" ]; then { echo ""; echo "✗ 結果を報告しなかったテスト $$(wc -l < "$$miss" | tr -d ' ') 件 (走ったかどうか自体が不明。xargs は utility がシグナルで死ぬか exit 255 だと残りの入力を捨てる):"; sed 's/^/  /' "$$miss"; } >&2; rc=1; fi; \
if [ -s "$$fails" ]; then { echo ""; echo "✗ 失敗したテスト:"; sed 's/^/  /' "$$fails"; } >&2; rc=1; fi; \
sed 's/$$/ (結果を報告しなかった)/' "$$miss" >> "$$fails"; $(call append_test_summary,$$fails,$$skips); \
rm -f "$$fails" "$$skips" "$$ran" "$$ran.s" "$$exp" "$$miss"; \
[ "$$rc" -eq 0 ] || exit 1; \
[ "$$xrc" -eq 0 ] || { echo "✗ 並列 runner (xargs) が rc=$$xrc で終わったが、失敗したテストの記録が無い (runner 自体の故障)" >&2; exit 1; }
endef

# 新品チェックアウト (= 追跡されているものだけがある状態) で回す opt-in レーン (issue 132)。
# 「手元には在るが git に載っていないもの」への依存を push 前に出す。判定は「ignore か」では
# なく「git に載っているか」: 空ディレクトリ (issues/next/) も untracked も同じ形で壊れる。
# 🚨 test からは呼ばない (所要時間を倍にしない。push 前に自分で叩く)。HEAD で回るので
#    未コミットの変更は検査しない — これは「新品チェックアウト」の定義そのもの。
# 後始末と残骸掃除の正本は scripts/with_fresh_worktree.sh。
test-fresh:
	@./scripts/with_fresh_worktree.sh $(MAKE) test-discovered test-bats

# test-runtime の実行本体。tests/ 全体を走査するため、新ディレクトリ tests/foo/ を作っても
# 自動で拾われる (ディレクトリ単位の死蔵も発生しない。新ディレクトリは並列腕に入る)。
#
# 並列腕 (tests/ から SERIAL_TEST_DIRS を除いた全部) と直列腕 (SERIAL_TEST_DIRS) の 2 本に分け、
# run_test_arms (= run_all_targets + 両腕の失敗テスト名 / skip 件数の合算) で**両方走らせてから**
# 失敗をまとめる (片方が落ちてももう片方は必ず走る)。
# 直列に据え置くのは共有資源 (tmux サーバ / nvim の状態) に触り、並列時の競合を検証していない領域。
# 実測 2026-09-05 (14 コア): 直列 1 本で 367s → 並列腕 122s + 直列腕 89s。
# 🚨 SERIAL_TEST_DIRS のパスを移動/改名したら直列腕の 0 件検知が fail するのでここを更新する
#   (CI_HEAVY_TEST_DIRS と同じ契約)。並列腕へ移すときは、そのテストが独自 tempdir だけで閉じていて
#   共有資源に触らないことを確かめてから。
SERIAL_TEST_DIRS := tests/tmux tests/nvim tests/zshrc/tmux-session
SERIAL_PRUNE := \( $(foreach d,$(SERIAL_TEST_DIRS),-path $(d) -o) -false \) -prune -o

test-discovered:
	@+$(call run_test_arms,test-discovered-parallel test-discovered-serial)

test-discovered-parallel:
	@$(call run_tests_parallel,tests $(SERIAL_PRUNE))

test-discovered-serial:
	@$(call run_tests,$(SERIAL_TEST_DIRS))

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
# 🚨 workflow はこのターゲットを呼べない: 依存を解決する時点で make があるとは限らないため。
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

# tests/ で `cd` の rc を見ていない行を落とす (issue 204)。cd が失敗しても CWD (= repo root)
# のまま先へ進み、fixture が repo に書かれる形を止める。意図的な例外は行内の `cd-rc: allow`。
test-cd-rc:
	@scripts/check_cd_rc_in_tests.sh

# 共有観測ログ (tt-restore-trigger.log) の書き手が guards.sh の tt_trigger_log 以外に増えるのを落とす。
# 同じ GitHub Action が workflow 間で違う版に固定されるのを落とす (issue 073 §1)。
# 版が割れていても workflow は動くので actionlint は緑のまま = 気づけない。
test-workflow-action-pins:
	@scripts/check_workflow_action_pins.sh

# 丸ごと skip なのに exit 0 で抜ける形を落とす (runner が [ok] と数え、assert が 1 本も
# 走っていないことが緑に埋もれる。issue 139 / tests/CLAUDE.md)。
# 正本は scripts/check_skip_exit_code.sh (なぜ危険か・例外マーカーはそこに書いてある)。
test-skip-exit-code:
	@scripts/check_skip_exit_code.sh

# 正本は scripts/check_trigger_log_writers.sh (なぜ危険か・例外マーカーはそこに書いてある)。
test-trigger-log-writers:
	@scripts/check_trigger_log_writers.sh

# 新しい Go プロジェクトが「CI レーン無し」で入るのを落とす (issue 203 候補 B / 出典 080・087)。
# Makefile 側は wildcard で発見するが、CI の paths filter とプロジェクトの lint/test target は
# 手で用意するので、そこだけ穴が残っている。正本は scripts/check_go_project_lanes.sh。
test-go-project-lanes:
	@scripts/check_go_project_lanes.sh

# heavy は 21 本 × ~16s (CI 実測) の直列で 5.6 分に育ったため並列実行する
# (av1ify/concat は tempdir 独立で並列安全。2026-07-20 に 338s → 数十秒へ)
test-discovered-heavy:
	@$(call run_tests_parallel,$(CI_HEAVY_TEST_DIRS))

# rest も test-discovered と同じ 2 腕 (並列 / 直列) で回す。heavy の除外は並列腕側で行う
# (SERIAL_TEST_DIRS と CI_HEAVY_TEST_DIRS は重ならないので直列腕は test-discovered と共用)。
test-discovered-rest:
	@+$(call run_test_arms,test-discovered-rest-parallel test-discovered-serial)

test-discovered-rest-parallel:
	@$(call run_tests_parallel,tests $(CI_HEAVY_PRUNE) $(SERIAL_PRUNE))

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
# 🚨 手元を上げたら lint.yml の SHELLCHECK_VERSION も同じ版へ上げること。版が離れると「手元 green /
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

# 🚨 prerequisite に並べない (issue 109 と同型)。12 本の直列だと 1 本目の失敗で残りが
#   1 度も走らず、lint.yml は `make test-lint` の 1 ステップなので CI ログにも出ない。
#   **末尾の新設検査ほど隠れやすい** (実測: test-json を落とすと後続 7 本が未実行)。
#
# ここだけ run_all_targets ではなく並列版を使う (通しで 21.4 秒 → 8.3 秒。14 コア機、各 1 run)。
# 内訳は test-shellcheck 5.6s / test-lint-tests 5.4s / 残り 14 本が各 0.4〜2.1s で、
# 並べ替えでは詰まらない (最長の極が律速)。集約の契約 (全部走らせてから失敗をまとめる) と
# 出力順は並列版も保つ。
# 🚨 **他の run_all_targets 呼び出しへ広げないこと**。test-discovered / test-discovered-rest は
#   「並列腕 + 直列腕」を束ねており、直列腕は tmux サーバに触るので同時実行の安全性が未検証
#   (59f9e48c の分割の前提)。ここが並列でよいのは、互いに独立した静的検査だけだから。
test-lint:
	@+scripts/run_make_targets_parallel.sh test-shellcheck test-zsh-syntax test-lint-tests test-yaml test-json test-karabiner test-actionlint test-gitconfig test-ruby-syntax test-ci-group-deps test-pipefail-grep-q test-cd-rc test-trigger-log-writers test-skip-exit-code test-workflow-action-pins test-go-project-lanes

# Go プロジェクトの静的解析とテスト。実体は各ディレクトリの Makefile の lint / test
# ターゲットに閉じており、ここはそれへ委譲するだけ (ローカルのコミット前検証用。root の
# `make test` に test-go を含める = Go テストの漏れ防止)。CI ではプロジェクトごとの専用
# workflow (.github/workflows/src_*.yml、paths filter 付き) が同じ lint / test を回す。
# 各 src_*.yml は再利用 workflow _go-project.yml を呼ぶだけの薄い caller。
# どちらも Go 未インストール環境では skip する。Go プロジェクトを追加したら
# ①各プロジェクトに Makefile (lint/test) ②src_<project>.yml (caller) を作る、の 2 点セット。
#
# 🚨 対象は **`src/*/go.mod` の存在で発見する**。手で列挙すると、新しく src/foo を切ったときに
#   lint / test から**無音で外れる** (make は緑のまま通り「lint も test も通っている」と読める)。
#   この repo は他の全域で「登録なしで対象になる」を徹底しており (shellcheck は
#   scripts/discover_shell_scripts.sh の発見、テストは test-dir の自動発見)、Go だけ手動なのは
#   非対称だった (issue 080)。
# 🚨 発見 0 件は**失敗させる** (下の recipe のガード)。発見式のゲートが 0 件で緑になるのは
#   `_claude/rules/adversarial-review-own-safeguards.md` が禁じる false green そのもの。
GO_PROJECT_DIRS := $(patsubst %/,%,$(dir $(wildcard src/*/go.mod)))

# 🚨 **最初のプロジェクトの失敗で残りを隠さない** (issue 130)。全プロジェクトを回してから
#   失敗したディレクトリをまとめて返す (run_all_targets と同じ規律。CI は src_*.yml で
#   プロジェクト別に分かれているため無傷だったが、ローカルの方が弱い状態だった)。
# 🚨 go 未導入は skip して緑にする。0 件は上のガードで失敗させるので、
#   「発見が壊れた」と「go が無い」は別の結果として出る。
# test も lint も並列に回す (go の build cache は並行アクセスに対して自前でロックする)。
# 🚨 lint の並列は各 src/*/.golangci.yml の `run.allow-parallel-runners: true` が前提 (issue 258)。
#   golangci-lint は既定で os.TempDir() のグローバル file lock を取り、別インスタンスが走っていると
#   "parallel golangci-lint is running" で落ちる。**1 プロジェクトでも設定が抜けると、そのプロジェクト
#   だけが他の起動タイミング次第で落ちる** (flaky に見える)。新しい src/ を切ったら .golangci.yml に
#   同じ run: 節を入れること (tests/scripts/test_golangci_parallel_runners.sh が漏れを検出する)。
# 出力はプロジェクトごとに一時ファイルへ隔離し、全部終わってから発見順に吐く (run_tests_parallel と
# 同じ理由: 並列で行が混ざると、どの失敗がどのプロジェクトか読めない)。rc ファイルが無い =
# サブシェルが結果を書く前に死んだ、なので失敗に数える (沈黙を緑にしない)。
define run_go_projects
if [ -z "$(strip $(GO_PROJECT_DIRS))" ]; then \
	echo "[go-$(1)] src/*/go.mod が 1 つも見つからない (発見の仕方が壊れている)" >&2; exit 1; \
fi; \
if ! command -v go >/dev/null 2>&1; then \
	echo "[go-$(1)] go not found; skipping"; exit 0; \
fi; \
outdir=$$(mktemp -d); i=0; \
for dir in $(GO_PROJECT_DIRS); do \
	i=$$((i + 1)); \
	( $(MAKE) -C $$dir $(1) >"$$outdir/$$i.out" 2>&1; echo $$? >"$$outdir/$$i.rc" ) & \
done; \
wait; \
failed=""; i=0; \
for dir in $(GO_PROJECT_DIRS); do \
	i=$$((i + 1)); \
	rc=$$(cat "$$outdir/$$i.rc" 2>/dev/null || echo 99); \
	echo "[go-$(1)] $$dir (rc=$$rc)"; cat "$$outdir/$$i.out"; \
	[ "$$rc" -eq 0 ] || failed="$$failed $$dir"; \
done; \
rm -rf "$$outdir"; \
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
# 🚨 ここも集約 (issue 130)。prerequisite に並べると lint の失敗で test が走らない
test-src:
	@+$(call run_all_targets,test-go-lint test-go)

