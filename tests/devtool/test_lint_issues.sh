#!/bin/bash
#
# devtool/bin/lint-issues の self-test。
#   - 合う名前・合わない名前・飛ばす名前 (README・番号の無いもの・状態のディレクトリの下)
#   - ディレクトリが無い・引数が多いときは rc 2 (検査できないので緑にしない)
#   - type の語が dotfiles の issue 規約 (_claude/issue-rules.md の「type 語彙」の表) と同じ
set -uo pipefail
unset CDPATH  # export された CDPATH の下では、相対パスの cd が解決先を stdout に出し $(cd … && pwd) を壊す

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
TOOL="$ROOT_DIR/devtool/bin/lint-issues"
[ -x "$TOOL" ] || { echo "NG: devtool/bin/lint-issues が無い"; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
fails=0
ok() { echo "  ok   - $1"; }
ng() { echo "  NG   - $1"; fails=$((fails + 1)); }

echo "[test-lint-issues]"
mkdir -p "$work/good/done" "$work/bad"
touch "$work/good/001-feat-a.md" "$work/good/1000-retro-b-2026-11-19.md" "$work/good/README.md" "$work/good/notes.md" \
  "$work/good/done/002-oldstyle-x.md"
"$TOOL" "$work/good" > "$work/out" 2>&1; rc=$?
if [ "$rc" -eq 0 ] && grep -qF '(2 件を確認)' "$work/out"; then ok "合う名前だけなら rc 0 で、数えたのは番号つきの直下の 2 件"
else ng "合う名前で rc=$rc: $(cat "$work/out")"; fi

touch "$work/bad/003-feat-ok.md" "$work/bad/004-fix-ng.md" "$work/bad/005-nodash.md"
"$TOOL" "$work/bad" > "$work/out" 2>&1; rc=$?
if [ "$rc" -eq 1 ] && grep -qF '004-fix-ng.md' "$work/out" && grep -qF '005-nodash.md' "$work/out" && ! grep -qF '003-feat-ok.md' "$work/out" \
  && grep -qF '2 件の命名規則違反があります (3 件を確認)' "$work/out"; then
  ok "語に無い type と type の無い名前を挙げて rc 1"
else ng "合わない名前で rc=$rc: $(cat "$work/out")"; fi

"$TOOL" "$work/absent" > "$work/out" 2>&1; rc=$?
if [ "$rc" -eq 2 ]; then ok "ディレクトリが無ければ rc 2"; else ng "無いディレクトリで rc=$rc"; fi
"$TOOL" "$work/good" extra > "$work/out" 2>&1; rc=$?
if [ "$rc" -eq 2 ]; then ok "引数が 2 つなら rc 2"; else ng "引数 2 つで rc=$rc"; fi

mkdir -p "$work/repo/issues" && git -C "$work/repo" init -q && touch "$work/repo/issues/006-human-x.md"
mkdir -p "$work/repo/sub"
(cd "$work/repo/sub" && env -u GIT_DIR -u GIT_WORK_TREE "$TOOL") > "$work/out" 2>&1; rc=$?
if [ "$rc" -eq 0 ] && grep -qF '(1 件を確認)' "$work/out"; then ok "引数が無ければ cwd の git の root の issues/ を見る"
else ng "引数なしで rc=$rc: $(cat "$work/out")"; fi

# 各 repo の呼び口が通る経路: dotfiles の bin/devtool-run から道具の名前で起こし、issues のディレクトリを渡す
"$ROOT_DIR/bin/devtool-run" lint-issues "$work/bad" > "$work/out" 2>&1; rc=$?
if [ "$rc" -eq 1 ] && grep -qF '(3 件を確認)' "$work/out"; then ok "bin/devtool-run lint-issues <dir> で本体に届き、rc をそのまま返す"
else ng "devtool-run 経由で rc=$rc: $(cat "$work/out")"; fi

# 規約の表の type と、道具が持つ語を比べる (道具は devtool の外を読まないので写しを持つ)
rules="$ROOT_DIR/_claude/issue-rules.md"
want="$(awk '/^### type 語彙/{f=1;next} f&&/^#/{exit} f&&/^\| `/{split($0,a,"`"); print a[2]}' "$rules" | sort)"
have="$(sed -n 's/^TYPES="\(.*\)"$/\1/p' "$TOOL" | tr '|' '\n' | sort)"
if [ -n "$want" ] && [ "$(printf '%s\n' "$want" | wc -l)" -ge 10 ] && [ "$want" = "$have" ]; then
  ok "type の語が issue 規約の表と同じ ($(printf '%s\n' "$want" | wc -l | tr -d ' ') 語)"
else
  # shellcheck disable=SC2086 # 語を 1 行に並べるために分割させる
  ng "type の語が issue 規約の表とずれた: 規約=[$(printf "%s " $want)] 道具=[$(printf "%s " $have)]"
fi

[ "$fails" -eq 0 ] || { echo "[test-lint-issues] $fails 件 NG"; exit 1; }
echo "[test-lint-issues] すべて ok"
