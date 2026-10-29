#!/usr/bin/env bash
# _claude/hooks/inject-dupscan-candidates-on-new-func.sh (PreToolUse(Write / Edit): 新しい関数を足す直前に、名前の近い既存の定義を注入する) の unit テスト。
#
# なぜ: 注入が出なくなる退行は無音 (書き込みはそのまま走る)。出すぎる退行は毎回の Edit に雑音を足す。どちらもテストでしか見えない。
# dupscan は偽物 (index の行を名前で返す)。本物の索引の形は src/dupscan の index の text 出力に合わせてある。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/inject-dupscan-candidates-on-new-func.sh"
HOOK_TIMEOUT=10
# shellcheck source=bin/lib/runtimeout.sh
. "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR" || exit 1
command -v jq >/dev/null 2>&1 || { echo "✗ jq が無い" >&2; exit 1; }

ok=0; fail=0
TMP_ROOT=$(mktemp -d); trap 'rm -rf "$TMP_ROOT"' EXIT
export TMPDIR="$TMP_ROOT" XDG_CACHE_HOME="$TMP_ROOT/cache"

FAKE_BIN="$TMP_ROOT/bin"; mkdir -p "$FAKE_BIN"
# sleep-ok: stub: 偽物の slowWidget は予算より長くかかる dupscan を演じる (待ちではない。hook の側が時間切れで打ち切る)
cat > "$FAKE_BIN/dupscan" <<'FAKE'
#!/bin/sh
# 引数: index -name NAME ...  → NAME に応じた行を返す
returns=""
while [ $# -gt 0 ]; do [ "$1" = "-name" ] && name="$2"; [ "$1" = "-returns" ] && returns="$2"; shift; done
echo "構文近似: 名前と引数ラベル/個数で照合。"
# location は作った値: 名前だけでは同じ名前で別の型を返す定義が上位 20 件を埋めて location(at:) が出ず、戻り型と組み合わせると出る形を模す
if [ "$name" = location ]; then
  if [ "$returns" = StorageLocation ]; then echo "src/core/Store.swift:40 StorageConnection.location(at: path: String) -> StorageLocation (呼び出し ~12)"; exit 0; fi
  # 汎用の型で引くと、同じ型を返す無関係な定義が並ぶ (hook は汎用の型では -returns を付けないので、これが出たら退行)
  if [ "$returns" = String ]; then echo "src/fmt/Fmt.swift:9 Fmt.location(of: v: Int) -> String (呼び出し ~3)"; exit 0; fi
  for i in $(seq 1 20); do echo "src/ui/V$i.swift:3 View$i.location(in: x: Int) -> CGPoint (呼び出し ~1)"; done
  exit 0
fi
# slowWidget の戻り型の問い合わせは、hook の予算 (6 秒) より長くかかる大きい repo を模す (名前の照合の候補が残るかを見る)
# exec で置き換える: hook の時間切れが殺すのは子の pid なので、sh の子として起こした待ちの道具は孤児で残る
if [ "$name" = slowWidget ] && [ -n "$returns" ]; then exec sleep 30; fi
[ -n "$returns" ] && exit 0
case "$name" in
  slowWidget) echo "src/a/w.go:1-9 slowWidget(x int) Widget (呼び出し ~2)" ;;
  readCookies) echo "src/chromecookie/cookie.go:291-349 Workspace.ReadCookies(profile string) (Result, error) (呼び出し ~1) // doc" ;;
  loadSettings) echo "src/a/s.go:10-20 loadSettingsFile(p string) error (呼び出し ~2)"; echo "src/b/s.go:5-9 reloadSettings() (呼び出し ~0)" ;;
  TestReadCookies) echo "src/a/t_test.go:1-9 TestReadCookies(t *testing.T) (呼び出し ~0)" ;;
  read) echo "src/a/r.go:1-2 read() (呼び出し ~9)" ;;
  parseThing) echo "src/a/p.go:1-9 parse() (呼び出し ~0)" ;;
  collectWidgets) echo "src/a/c.go:1-9 collectWidgets(xs []int) []int (呼び出し ~3)" ;;
  gather) echo "src/a/g.go:1-2 gatherAll() (呼び出し ~0)" ;;
  resume) echo "src/a/r.go:1-2 resume() (呼び出し ~1)" ;;
  RESUME|RESUMEReady) echo "src/a/r.go:1-2 RESUME() (呼び出し ~1)" ;;
  BASE64|BASE64Decode) echo "src/a/b.go:1-2 BASE64() (呼び出し ~1)" ;;
  fakeRunner) echo "src/other/x_test.go:3-9 fakeRunner(m map[string]int) int [test,unexported] (呼び出し ~2)"; echo "src/foo/sub/y_test.go:3-9 fakeRunner() int [test,unexported] (呼び出し ~1)"; echo "src/other/p.go:3-9 fakeRunnerProd() int [unexported] (呼び出し ~1)" ;;
esac
# collectWidgetsFast / gatherThings は、どの語でも本物の index -name が返さない (既存の名前が引く語を含むものだけを返すため)
FAKE
chmod +x "$FAKE_BIN/dupscan"
WITH_DUPSCAN="$FAKE_BIN:$PATH"
NO_DUPSCAN_BIN="$TMP_ROOT/nodup"; mkdir -p "$NO_DUPSCAN_BIN"
for t in bash env python3 git jq dirname cat; do ln -s "$(command -v "$t")" "$NO_DUPSCAN_BIN/$t"; done

REPO=$(mktemp -d "$TMP_ROOT/repo.XXXXXX")
( cd "$REPO" && git init -q . && mkdir -p src/foo && printf 'package foo\n\nfunc existingHelper() {}\n' > src/foo/a.go &&
  printf 'final class ArrivalGate {\n    class func make() -> ArrivalGate { ArrivalGate() }\n}\n' > src/foo/Gate.swift &&
  printf 'package foo\n\ntype existingThing struct{}\n' > src/foo/thing.go &&
  printf 'package struct PackagedThing {}\n' > src/foo/Pkg.swift && git add -A &&
  printf 'struct LooseType {}\n' > src/foo/Loose.swift &&
  printf 'package foo\n\nconst fixture = `package a\n\nfunc placeholder() {}\n`\n' > src/foo/fx_test.go &&
  printf 'module foo\n' > src/foo/go.mod && mkdir -p src/other && printf 'module other\n' > src/other/go.mod &&
  printf 'tmp/\n' > .gitignore ) >/dev/null 2>&1
session=0
next_session() { session=$((session + 1)); }

# rc と stderr を捨てない: PreToolUse の hook は exit 2 で書き込みを止める。どの入力でも rc=0・stderr 空でなければならない。
run() { # $1=tool $2=file $3=new_string [$4=old_string] [$5=PATH]
  local rc
  jq -n --arg t "$1" --arg f "$2" --arg n "$3" --arg o "${4:-}" --arg s "s$session" \
    '{tool_name: $t, session_id: $s, tool_input: ({file_path: $f} + (if $t == "Edit" then {new_string: $n, old_string: $o} else {content: $n} end))}' |
    PATH="${5:-$WITH_DUPSCAN}" "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK" > "$TMP_ROOT/out" 2> "$TMP_ROOT/err"
  rc=$?
  if [ "$rc" -ne 0 ] || [ -s "$TMP_ROOT/err" ]; then
    printf 'HOOK-FAILED rc=%s stderr=%s\n' "$rc" "$(head -c 200 "$TMP_ROOT/err")"
    return 0
  fi
  cat "$TMP_ROOT/out"
}

expect_has() { # $1=説明 $2=stdout $3=含むべき文字列
  case "$2" in HOOK-FAILED*) echo "✗ hook が rc≠0 か stderr を出した: $1: $2"; fail=$((fail+1)); return ;; esac
  if ! printf '%s' "$2" | jq -e --arg w "$3" '.hookSpecificOutput.additionalContext | contains($w)' >/dev/null 2>&1; then
    echo "✗ 注入に「$3」が無い / JSON が壊れている: $1"; printf '%s\n' "$2" | head -3; fail=$((fail+1)); return
  fi
  if printf '%s' "$2" | jq -e '.hookSpecificOutput.permissionDecision' >/dev/null 2>&1; then
    echo "✗ permissionDecision を返している (注入だけのはず): $1"; fail=$((fail+1)); return
  fi
  echo "✓ $1"; ok=$((ok+1))
}
expect_lacks() { # $1=説明 $2=stdout $3=含んではいけない文字列
  case "$2" in HOOK-FAILED*) echo "✗ hook が rc≠0 か stderr を出した: $1: $2"; fail=$((fail+1)); return ;; esac
  case "$2" in *"$3"*) echo "✗ 「$3」が出ている: $1"; fail=$((fail+1)); return ;; esac
  echo "✓ $1"; ok=$((ok+1))
}
expect_silent() {
  case "$2" in HOOK-FAILED*) echo "✗ hook が rc≠0 か stderr を出した: $1: $2"; fail=$((fail+1)); return ;; esac
  if [ -n "$2" ]; then echo "✗ 注入が出るべきでない: $1"; printf '%s\n' "$2" | head -3; fail=$((fail+1)); return; fi
  echo "✓ $1"; ok=$((ok+1))
}

F="$REPO/src/foo/a.go"
next_session; out=$(run Edit "$F" $'func readCookies(x int) int { return x }\n')
expect_has "Go: 大小を無視して名前が等しい既存の定義 (メソッドの接頭辞つき) を注入する" "$out" "Workspace.ReadCookies"
next_session; out=$(run Edit "$F" $'func loadSettings() {}\n')
expect_has "前方一致 (共通部分が 6 文字以上) の既存の定義を注入する" "$out" "loadSettingsFile"
expect_lacks "共通の前方が無い名前 (reloadSettings) は雑音なので出さない" "$out" "reloadSettings"
next_session; out=$(run Edit "$F" $'func collectWidgetsFast(xs []int) []int { return xs }\n')
expect_has "引く名前より短い既存の定義 (collectWidgets ⊂ collectWidgetsFast) は、語の境目の前方で引いて注入する" "$out" "collectWidgets(xs"
next_session; expect_silent "前方の語で引いて出た別の名前 (gatherAll) は、既存の名前が前方になっていないので出さない" "$(run Edit "$F" $'func gatherThings() {}\n')"
next_session; out=$(run Edit "$REPO/src/foo/new_test.go" $'func fakeRunner() int { return 1 }\n')
expect_has "Go: 同じ module の別の package のテストの helper は、共通の置き場へ寄せる候補なので出す" "$out" "src/foo/sub/y_test.go"
expect_lacks "Go: 別の module のテストの非公開の helper は呼べず寄せられないので出さない (署名の map[string] を印と読まない)" "$out" "src/other/x_test.go"
expect_has "Go: 別の module でも test でない単位は出す" "$out" "src/other/p.go"
next_session; expect_silent "候補が無ければ何も出さない" "$(run Edit "$F" $'func brandNewName() {}\n')"
next_session; expect_silent "共通部分が 6 文字未満の前方一致 (parse ⊂ parseThing) は出さない" "$(run Edit "$F" $'func parseThing() {}\n')"
next_session; expect_silent "Edit の old_string に既にある関数は新しくない" "$(run Edit "$F" $'func readCookies(x int) int { return x }\n' $'func readCookies(x int) int { return 0 }\n')"
next_session; expect_silent "書き換え前のファイルに既にある関数は新しくない (Write の上書き)" "$(run Write "$F" $'package foo\n\nfunc existingHelper() {}\n')"
next_session; expect_silent "Test で始まる関数は引かない" "$(run Edit "$F" $'func TestReadCookies(t *testing.T) {}\n')"
next_session; expect_silent "Go のメソッドは引かない (型ごとに同名が並ぶのは普通)" "$(run Edit "$F" $'func (w *W) readCookies() {}\n')"
next_session; expect_silent "5 文字未満の名前は引かない" "$(run Edit "$F" $'func read() {}\n')"
next_session; expect_silent "対象でない拡張子" "$(run Edit "$REPO/src/foo/x.py" $'func readCookies() {}\n')"
next_session; expect_silent "相対パス" "$(run Edit "src/foo/a.go" $'func readCookies() {}\n')"
next_session; expect_silent "git の repo の外" "$(run Edit "$TMP_ROOT/plain/x.go" $'func readCookies() {}\n')"
next_session; expect_silent "dupscan が PATH に無い" "$(run Edit "$F" $'func readCookies() {}\n' "" "$NO_DUPSCAN_BIN")"
next_session; expect_silent "前方一致でも、短い方の名前が 1 語 (resume ⊂ resumeReady) なら語が同じだけなので出さない" "$(run Edit "$F" $'func resumeReady() {}\n')"
S="$REPO/src/foo/New.swift"
next_session; out=$(run Write "$S" $'@MainActor\nprivate final class ArrivalGate {}\n')
expect_has "Swift: 足す型と同じ名前の型の宣言が別のファイルにあれば注入する" "$out" "src/foo/Gate.swift:1:final class ArrivalGate"
next_session; expect_has "Swift: git の追跡外 (ignore されていない untracked) の型も照らす" "$(run Write "$S" $'struct LooseType {}\n')" "src/foo/Loose.swift"
next_session; expect_has "Go: 行頭の type の名前が同じなら注入する" "$(run Edit "$REPO/src/foo/b.go" $'type existingThing struct{ n int }\n')" "src/foo/thing.go:3:type existingThing"
next_session; expect_silent "同じ名前の型が無ければ何も出さない" "$(run Write "$S" $'final class BrandNewGate {}\n')"
next_session; expect_silent "名前を含むだけの型 (ArrivalGateway) は同じ名前ではない" "$(run Write "$S" $'final class ArrivalGateway {}\n')"
next_session; expect_silent "既存の型の名前の前方 (Loose ⊂ LooseType) は同じ名前ではない" "$(run Write "$S" $'struct Loose {}\n')"
next_session; expect_has "Go: generic の型 (名前の直後が [) も照らす" "$(run Edit "$REPO/src/foo/b.go" $'type existingThing[T any] struct{ v T }\n')" "src/foo/thing.go:3:type existingThing"
next_session; expect_has "Swift: package 修飾子の付いた型も照らす (既存の側も package)" "$(run Write "$S" $'package struct PackagedThing {}\n')" "src/foo/Pkg.swift"
next_session; expect_silent "全大文字の 1 語 (RESUME ⊂ RESUMEReady) は 1 語と数えて出さない" "$(run Edit "$F" $'func RESUMEReady() {}\n')"
next_session; expect_silent "全大文字に数字が続く 1 語 (BASE64 ⊂ BASE64Decode) も 1 語と数えて出さない" "$(run Edit "$F" $'func BASE64Decode() {}\n')"
next_session; expect_has "Go の型の注入は、衝突する範囲を package と書く" "$(run Edit "$REPO/src/foo/b.go" $'type existingThing struct{ n int }\n')" "Go は同じ package"
next_session; expect_has "Swift の型の注入は、private / fileprivate の型は別のファイルどうしでは衝突しないと書く" "$(run Write "$S" $'private struct LooseType {}\n')" "private / fileprivate の型は同じファイルの中だけ"
next_session; expect_silent "class func は型の宣言ではない" "$(run Edit "$S" $'    class func make() {}\n')"
next_session; expect_silent "書き換え前 (old_string) に既にある型は新しくない (別のファイルに同じ名前があっても出さない)" "$(run Edit "$S" $'final class ArrivalGate {\n}\n' $'final class ArrivalGate {}\n')"
next_session; run Write "$S" $'final class ArrivalGate {}\n' >/dev/null
expect_silent "同じ session × 型の 2 回目は出さない" "$(run Write "$S" $'final class ArrivalGate {}\n')"
next_session; out=$(run Edit "$REPO/src/foo/B.swift" $'    private func readCookies() {}\n')
expect_has "Swift: 字下げのある func も新しい関数として引く" "$out" "ReadCookies"
next_session; run Edit "$F" $'func readCookies() {}\n' >/dev/null
expect_silent "同じ session × 名前の 2 回目は出さない" "$(run Edit "$F" $'func readCookies() {}\n')"
next_session; expect_silent "DUPSCAN_INJECT=off では、注入の出る名前でも出さない (A-B の対照腕)" "$(DUPSCAN_INJECT=off run Edit "$F" $'func readCookies(x int) int { return x }\n')"
next_session; expect_has "DUPSCAN_INJECT が off 以外 (空・on) なら通常どおり注入する" "$(DUPSCAN_INJECT=on run Edit "$F" $'func readCookies(x int) int { return x }\n')" "Workspace.ReadCookies"
# 文字列リテラルとコメントの中の func は新しい関数ではない (test が Go のソースを fixture として文字列に持つ)
next_session; expect_silent "Go: raw string の fixture の中の func は引かない" "$(run Write "$REPO/src/foo/fx2_test.go" $'package foo\n\nconst src = `package a\n\nfunc readCookies() {}\n`\n')"
next_session; expect_silent "Go: 既存の raw string の中を書き換える Edit は、new_string が文字列の途中からでも引かない" "$(run Edit "$REPO/src/foo/fx_test.go" $'func readCookies() {}' $'func placeholder() {}')"
next_session; expect_has "Go: fixture の後ろにある本物の関数は引く" "$(run Write "$REPO/src/foo/fx3_test.go" $'package foo\n\nconst src = `func other() {}`\n\nfunc readCookies() {}\n')" "Workspace.ReadCookies"
next_session; expect_has "Go: backquote の rune は raw string の始まりと読まない (後ろの関数を引く)" "$(run Write "$REPO/src/foo/rune.go" $'package foo\n\nvar bq = \'`\'\n\nfunc readCookies() {}\n')" "Workspace.ReadCookies"
next_session; expect_silent "Go: /* */ のコメントの中の func は引かない" "$(run Write "$REPO/src/foo/c.go" $'package foo\n\n/*\nfunc readCookies() {}\n*/\n')"
next_session; expect_silent "Swift: 三重引用符の文字列の中の func は引かない" "$(run Write "$REPO/src/foo/Fx.swift" $'let src = """\nfunc readCookies() {}\n"""\n')"
next_session; expect_has "Swift: 三重引用符の中の \\\"\"\" を閉じと読まない (後ろの関数を引く)" "$(run Write "$REPO/src/foo/Esc.swift" $'let s = """\n x \\""" y\n"""\nfunc readCookies() {}\n')" "ReadCookies"
next_session; expect_has "Swift: #\"\"\"…\"\"\"# の raw string の中の \"\"\" を閉じと読まない (後ろの関数を引く)" "$(run Write "$REPO/src/foo/Raw.swift" $'let s = #"""\n x """ y\n"""#\nfunc readCookies() {}\n')" "ReadCookies"
next_session; expect_silent "Swift: #\"…\"# の raw string の中の func は引かない" "$(run Write "$REPO/src/foo/Raw2.swift" $'let s = #"""\nfunc readCookies() {}\n"""#\n')"
next_session; expect_silent "git が ignore するパス (tmp/) では引かない" "$(run Write "$REPO/tmp/tool.go" $'package main\n\nfunc readCookies() {}\n')"

next_session; expect_has "Swift: 戻り型が取れたら名前と組み合わせて引き、同じ名前の別の型の定義に埋もれた既存の関数を出す (954 の形)" "$(run Write "$REPO/src/foo/Ctx.swift" $'struct Ctx {\n    func location(path: String) -> StorageLocation {\n        fatalError()\n    }\n}\n')" "StorageConnection.location(at:"
next_session; expect_lacks "Swift: 汎用の戻り型 (String) では型で引かない (名前だけの結果になる)" "$(run Write "$REPO/src/foo/Ctx2.swift" $'struct Ctx2 {\n    func location(path: String) -> Swift.String {\n        fatalError()\n    }\n}\n')" "Fmt.location(of:"
next_session; expect_has "Go: 複数の戻り (T, error) は error でない最初の型で引く" "$(run Write "$REPO/src/foo/loc.go" $'package foo\n\nfunc location(p string) (*StorageLocation, error) {\n\treturn nil, nil\n}\n')" "StorageConnection.location(at:"

next_session; expect_has "Swift: 汎用の戻り型でも名前の照合の結果は出す (何も出さない退行を見分ける)" "$(run Write "$REPO/src/foo/Ctx3.swift" $'struct Ctx3 {\n    func location(path: String) -> String {\n        fatalError()\n    }\n}\n')" "View1.location(in:"
next_session; expect_lacks "Swift: 本体の無い protocol の要件では、次の行の別の要件の戻り型で引かない" "$(run Write "$REPO/src/foo/Proto.swift" $'protocol P {\n    func location(path: String)\n    subscript(i: Int) -> StorageLocation { get }\n}\n')" "StorageConnection.location(at:"

next_session; expect_has "戻り型の問い合わせが時間切れでも、名前の照合で見つけた候補は出す (例外で全体を黙らせない)" "$(run Write "$REPO/src/foo/slow.go" $'package foo\n\nfunc slowWidget(x int) Widget {\n\treturn Widget{}\n}\n')" "slowWidget(x int) Widget"

n=$(grep -c . "$TMP_ROOT/cache/claude-dupscan-inject.log" || true)
if [ "$n" -ge 5 ] && grep -q $'\treadCookies\t1\tname$' "$TMP_ROOT/cache/claude-dupscan-inject.log"; then
  echo "✓ 注入の計測ログに「session 名前 候補数 引き方」が残る"; ok=$((ok+1))
else
  echo "✗ 計測ログが想定と違う (行数 $n)"; fail=$((fail+1))
fi

if jq -e '[.hooks.PreToolUse[] | select(.matcher == "Write|Edit") | .hooks[].command] | index("~/dotfiles/_claude/hooks/inject-dupscan-candidates-on-new-func.sh")' \
  "$ROOT_DIR/_claude/settings.json" >/dev/null; then
  echo "✓ settings.json の PreToolUse(Write|Edit) に配線されている"; ok=$((ok+1))
else
  echo "✗ settings.json の PreToolUse(Write|Edit) に配線されていない"; fail=$((fail+1))
fi

echo "---"
echo "ok=$ok fail=$fail"
[ "$fail" -eq 0 ]
