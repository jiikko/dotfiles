#!/usr/bin/env bash
# ローカルの入口 (make test / test-go* / test-discovered* / test_changed.sh) が「1 本目の失敗で残りを隠さない」
# ことを実験で確かめる (issue 130)。
#
# なぜテストにするか: 集約かどうかは**失敗した日にしか観測されない**。緑の日は fail-fast でも
# 集約でも同じ出力になるため、prerequisite 形へ戻す変更が無音で通る。
# 実際 109 の 1 次修正では、集約にしたつもりの隣で 3 つの入口が短絡のまま残っていた。
#
# 手段: 本物の lint / テストは回さず、**呼ばれ方だけ**を偽の make で観測する
#   - Makefile 側: GO_PROJECT_DIRS を上書きして偽プロジェクト 2 つを回す (1 つ目が必ず落ちる)
#   - test-discovered 側: 本物の Makefile を include した一時 Makefile で腕のレシピだけを偽物にする
#   - test_changed.sh 側: PATH 先頭に偽 make を置いて、呼び出しを記録させる
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR" || exit 1
# 🚨 repo 内の `tmp/` に作らないこと。あの ignore は `~/.gitignore_global` 由来で repo の
#    .gitignore には無く、追跡もされないので **新品チェックアウトと CI には tmp/ が存在しない**
#    (CI run 33168462220 で "mkdtemp failed ... No such file or directory")。手元は tmp/ が
#    あるので緑になり、Linux でだけ落ちる。隔離ディレクトリの既定は OS の一時領域。
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

fail=0
note() { printf '✓ %s\n' "$1"; }
bad() { printf '✗ %s\n' "$1"; fail=1; }

# --- Makefile: test-go-lint / test-go は全プロジェクトを回してから落ちる ---------------
# 偽プロジェクト: bad が先 (アルファベット順でも先) で必ず落ちる。good が走れば集約
mkdir -p "$TMP_DIR/bad" "$TMP_DIR/good"
printf 'lint:\n\t@echo bad-lint >> %s\n\t@exit 1\ntest:\n\t@echo bad-test >> %s\n\t@exit 1\n' \
  "$TMP_DIR/calls" "$TMP_DIR/calls" > "$TMP_DIR/bad/Makefile"
printf 'lint:\n\t@echo good-lint >> %s\ntest:\n\t@echo good-test >> %s\n' \
  "$TMP_DIR/calls" "$TMP_DIR/calls" > "$TMP_DIR/good/Makefile"

# 🚨 `go` のスタブを PATH 先頭に置く。Makefile の run_go_projects は `command -v go` が
#    無ければ "go not found; skipping" で **exit 0** するため、go が入っていない環境では
#    この節の 6 つの assert がまるごと空振りする。dotfiles の CI (Tests ジョブ) には go を
#    入れていないので、置かないと **CI では何も検査していない**ことになる (実測: run
#    33169546465 で 6 件が false green ではなく false red として露出した)。
#    ここで見たいのは「集約して回すか」だけで go の挙動ではない。偽プロジェクトの Makefile は
#    go を呼ばないので、スタブは**呼ばれたら失敗する**形にする (黙って 0 を返すと、将来
#    recipe が本当に go を使い始めたときに嘘の緑になる)。
mkdir -p "$TMP_DIR/bin"
printf '#!/bin/sh\necho "test stub: go must not be executed here" >&2\nexit 97\n' > "$TMP_DIR/bin/go"
chmod +x "$TMP_DIR/bin/go"

for goal in lint test; do
  : > "$TMP_DIR/calls"
  target="test-go"; [ "$goal" = lint ] && target="test-go-lint"
  if PATH="$TMP_DIR/bin:$PATH" make "$target" GO_PROJECT_DIRS="$TMP_DIR/bad $TMP_DIR/good" > "$TMP_DIR/out" 2>&1; then
    bad "$target: 失敗したプロジェクトがあるのに成功した"
  else
    note "$target: 失敗を返す"
  fi
  grep -q "good-$goal" "$TMP_DIR/calls" \
    || bad "$target: 1 つ目が落ちた後、2 つ目が走っていない ($(tr '\n' ' ' < "$TMP_DIR/calls"))"
  grep -q "失敗したプロジェクト.*bad" "$TMP_DIR/out" \
    || bad "$target: 失敗したプロジェクト名がまとめて出ない: $(cat "$TMP_DIR/out")"
done
note "test-go-lint / test-go は全プロジェクトを回してから集約して落ちる"

# go が無い環境では skip して緑になる (0 件の失敗と混ぜない)。
# 🚨 make の shim は作らない。PATH を絞った中で相対名の make を exec すると自分自身に
#   解決して無音で無限再帰する (_claude/rules/path-shim-must-resolve-real-binary.md。実際に踏んだ)。
#   go を PATH から外すだけにして、make は絶対パスで呼ぶ
real_make="$(command -v make)"
if PATH="/usr/bin:/bin" "$real_make" test-go GO_PROJECT_DIRS="$TMP_DIR/bad" >"$TMP_DIR/out" 2>&1; then
  grep -q 'go not found' "$TMP_DIR/out" || bad "go 無しで緑だが skip の理由が出ていない: $(cat "$TMP_DIR/out")"
  note "go 未導入は理由を出して skip する"
else
  # go が /usr/bin にある環境では検証できない。緑に畳まず「未検証」として出す
  printf 'skipped: go を PATH から外せなかったため skip 経路は未検証\n'
fi

# 0 件は「発見が壊れている」として落ちる (skip と別の結果)
if make test-go GO_PROJECT_DIRS="" >"$TMP_DIR/out" 2>&1; then
  bad "GO_PROJECT_DIRS が 0 件でも緑になる (発見の false green)"
else
  note "GO_PROJECT_DIRS 0 件は失敗する"
fi

# --- Makefile: test / test-src が集約であること -----------------------------------------
# $(MAKE) を偽物へ差し替えて「どのターゲットが呼ばれたか」だけを見る (本物は回さない)
cat > "$TMP_DIR/fakemake" <<EOF
#!/bin/sh
echo "\$@" >> "$TMP_DIR/calls"
case "\$1" in test-lint|test-go-lint) exit 1 ;; esac
EOF
chmod +x "$TMP_DIR/fakemake"

: > "$TMP_DIR/calls"
if make test MAKE="$TMP_DIR/fakemake" >"$TMP_DIR/out" 2>&1; then
  bad "make test: 子ターゲットが落ちたのに成功した"
fi
for t in test-lint test-runtime test-src; do
  grep -qx "$t" "$TMP_DIR/calls" || bad "make test: $t が呼ばれていない ($(tr '\n' ' ' < "$TMP_DIR/calls"))"
done
note "make test は lint が落ちても test-runtime / test-src を回す"

: > "$TMP_DIR/calls"
if make test-src MAKE="$TMP_DIR/fakemake" >"$TMP_DIR/out" 2>&1; then
  bad "make test-src: 子ターゲットが落ちたのに成功した"
fi
grep -qx "test-go" "$TMP_DIR/calls" \
  || bad "make test-src: lint の失敗で test-go が走っていない ($(tr '\n' ' ' < "$TMP_DIR/calls"))"
note "make test-src は test-go-lint が落ちても test-go を回す"

# --- Makefile: test-discovered / test-discovered-rest は片方の腕が落ちてももう片方を回す ---
# 腕はターゲット名が固定で変数では差し替えられないので、本物の Makefile を include した一時
# Makefile で腕のレシピだけを上書きする (make は「overriding commands」を警告して後勝ち)。
# 上書き後の腕は本物の run_tests_parallel / run_tests に**偽のテストディレクトリ**を渡す形にする。
# 束ねる側 (run_test_arms / run_all_targets) と runner (一覧の合算ファイルへの追記) は本物が
# そのまま動くので、その破損を検出できる (偽の腕が合算ファイルへ直接書く形だと、runner 側の
# 追記を外しても緑のまま通る — 実際に変異で確かめた)。
# 🚨 再帰側にも `-f` を渡す: `-f` は MAKEFLAGS で子へ伝わらないため、run_all_targets の
#    `$(MAKE) <腕>` が本物の Makefile を読んで本物の腕 (数分) を回してしまう。
#    prerequisite 形へ戻す退行 (`test-discovered: 腕1 腕2`) では偽の腕1 が落ちた時点で make が
#    止まり、腕2 の記録が無いので red になる。
# 偽の並列腕: 失敗 1 本 + skip (exit 77) 1 本。偽の直列腕: 成功 1 本 (走った証拠を残す) + 失敗 1 本。
# 最後に両腕の失敗 2 本と skip 1 本がテスト名で再掲されること (issue 261 P2-1 / P2-2) も見る。
mkdir -p "$TMP_DIR/arm_p" "$TMP_DIR/arm_s"
printf '#!/bin/sh\nexit 1\n' > "$TMP_DIR/arm_p/test_p_fail.sh"
printf '#!/bin/sh\nexit 77\n' > "$TMP_DIR/arm_p/test_p_skip.sh"
printf '#!/bin/sh\necho serial-ran >> %s\n' "$TMP_DIR/calls" > "$TMP_DIR/arm_s/test_s_ok.sh"
printf '#!/bin/sh\nexit 1\n' > "$TMP_DIR/arm_s/test_s_fail.sh"
chmod +x "$TMP_DIR"/arm_?/test_*.sh
cat > "$TMP_DIR/Makefile" <<EOF
include Makefile
test-discovered-parallel test-discovered-rest-parallel:
	@\$(call run_tests_parallel,$TMP_DIR/arm_p)
test-discovered-serial:
	@\$(call run_tests,$TMP_DIR/arm_s)
EOF
for entry in test-discovered test-discovered-rest; do
  arm="test-discovered-parallel"; [ "$entry" = test-discovered-rest ] && arm="test-discovered-rest-parallel"
  : > "$TMP_DIR/calls"
  if make -f "$TMP_DIR/Makefile" "$entry" MAKE="$real_make -f $TMP_DIR/Makefile" >"$TMP_DIR/out" 2>&1; then
    bad "$entry: 並列腕が落ちたのに成功した"
  else
    note "$entry: 失敗を返す"
  fi
  grep -q '\[FAIL\] .*test_p_fail.sh' "$TMP_DIR/out" || bad "$entry: 偽の並列腕が走っていない (実験の配線が壊れている): $(cat "$TMP_DIR/out")"
  grep -qx serial-ran "$TMP_DIR/calls" \
    || bad "$entry: 並列腕が落ちた後、直列腕が走っていない: $(cat "$TMP_DIR/out")"
  grep -q "失敗したターゲット:.*$arm.*test-discovered-serial" "$TMP_DIR/out" \
    || bad "$entry: 失敗したターゲット名が両腕まとめて出ない: $(cat "$TMP_DIR/out")"
  if ! { grep -q "全腕合計: 失敗したテスト 2 件" "$TMP_DIR/out" \
         && grep -q '^  .*test_p_fail.sh$' "$TMP_DIR/out" && grep -q '^  .*test_s_fail.sh$' "$TMP_DIR/out"; }; then
    bad "$entry: 両腕の失敗テスト名が最後に合算されない: $(cat "$TMP_DIR/out")"
  fi
  if ! { grep -q "全腕合計: 丸ごと skip したテスト 1 件" "$TMP_DIR/out" && grep -q '^  .*test_p_skip.sh$' "$TMP_DIR/out"; }; then
    bad "$entry: 腕の skip 件数が最後に合算されない: $(cat "$TMP_DIR/out")"
  fi
done
note "test-discovered / test-discovered-rest は並列腕が落ちても直列腕を回し、テスト名と skip を合算する"

# --- test_changed.sh: 3 つのループが途中で止まらない -------------------------------------
mkdir -p "$TMP_DIR/bin"
cat > "$TMP_DIR/bin/make" <<EOF
#!/bin/sh
echo "\$*" >> "$TMP_DIR/tc_calls"
exit 1
EOF
chmod +x "$TMP_DIR/bin/make"
: > "$TMP_DIR/tc_calls"
if PATH="$TMP_DIR/bin:$PATH" ./scripts/test_changed.sh \
     _claude/settings.json tests/claude/test_statusline.sh src/glogx/tui.go \
     >"$TMP_DIR/out" 2>&1; then
  bad "test_changed.sh: make が全部落ちたのに成功した"
else
  note "test_changed.sh: 失敗を返す"
fi
# 3 種のループ (targets / test_dirs / go_dirs) が全部最後まで回ること。
# go は lint と test を**別々に**呼ぶ (1 make 2 goal だと lint の失敗で test が走らない)
for expected in 'test-json' 'test-dir DIR=tests/claude' '-C src/glogx lint' '-C src/glogx test'; do
  # -- を付ける: 期待文字列が "-C ..." で始まるとオプションとして食われる (実際に踏んだ)
  grep -qF -- "$expected" "$TMP_DIR/tc_calls" \
    || bad "test_changed.sh: '$expected' が呼ばれていない (途中で止まった):"$'\n'"$(cat "$TMP_DIR/tc_calls")"
done
grep -q '✗ \[test-changed\] 失敗:' "$TMP_DIR/out" \
  || bad "test_changed.sh: 失敗の一覧が出ない: $(cat "$TMP_DIR/out")"
note "test_changed.sh は 3 つのループを最後まで回して失敗をまとめる"

[ "$fail" -eq 0 ] || exit 1
printf '\n[test-no-failfast-entrypoints] all ok\n'
