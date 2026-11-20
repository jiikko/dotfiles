#!/bin/bash
# scripts/lib/issue_links.awk + issue_done_links.awk (issue を done/ へ動かしたときのリンクの張り直し) を直接通す self-test。
# dotfiles の scripts/issue_done.sh と obaket の bin/issue-done・bin/issue-resolve-link-conflicts が同じ 2 本を読む。
# issue_done.sh の canary が見ない形を足す: 非 ASCII の fragment・拡張子の無い相対リンク・URL / 絶対パス / ページ内リンク / インラインコードは触らない・report モード。
set -uo pipefail
unset CDPATH
export LC_ALL=C  # issue_done.sh と同じく bytes で読む (非 ASCII の fragment も壊さずに通るか)

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
LINKS=(-f "$ROOT_DIR/scripts/lib/issue_links.awk" -f "$ROOT_DIR/scripts/lib/issue_done_links.awk")
fails=0
check() {  # check <名前> <old_base> <new_base> <入力> <期待>
  local got
  got="$(printf '%s\n' "$4" | awk -v old_base="$2" -v new_base="$3" -v moved_old="issues/347-x.md" -v moved_new="issues/done/347-x.md" "${LINKS[@]}" 2>/dev/null)"
  if [ "$got" = "$5" ]; then echo "  ok   - $1"; else echo "  NG   - $1: got [$got] want [$5]"; fails=$((fails + 1)); fi
}

echo "[test-issue-links-awk]"
check "他のファイルから動いた issue への非 ASCII の fragment つきリンクを張り直す" issues issues \
  '[x](347-x.md#サービス別-機能比較)' '[x](done/347-x.md#サービス別-機能比較)'
check "動かした当人の中の、拡張子の無い相対リンク (ディレクトリ) も新しい位置から張り直す" issues issues/done \
  '[d](../docs/guides)' '[d](../../docs/guides)'
# shellcheck disable=SC2016 # バッククォートは展開させないインラインコードの入力
check "URL・絶対パス・ページ内リンク・インラインコードは触らない" issues issues/done \
  '[u](https://example.com/a.md) [a](/abs/x.md) [p](#sec) `[c](../x.md)`' '[u](https://example.com/a.md) [a](/abs/x.md) [p](#sec) `[c](../x.md)`'
check "開き括弧をエスケープした文字列 (\\[x](y)) はリンクと読まない" issues issues/done \
  '\[guide](../docs/guides) と [g](../docs/guides)' '\[guide](../docs/guides) と [g](../../docs/guides)'
check "エスケープしたバックスラッシュの後ろ (\\\\[x](y)) は本物のリンク" issues issues/done \
  '\\[d](../docs/guides)' '\\[d](../../docs/guides)'
check "リンクの文字の中にエスケープした [ があっても外側のリンクは張り直す" issues issues/done \
  '[a \[b]](../docs/guides) [c [d]](../x)' '[a \[b]](../../docs/guides) [c [d]](../../x)'
check "外側の開き括弧をエスケープし、内側に本物の括弧がある文字列 (\\[a [b]](x)) はリンクと読まない" issues issues/done \
  '\[a [b]](../docs/guides)' '\[a [b]](../docs/guides)'
check "無関係なリンクは書き方を変えない" issues issues \
  '[z](./done/311-b.md)' '[z](./done/311-b.md)'
got="$(printf '%s\n' '[a](347-x.md#見出し) [b](done/311-b.md)' | awk -v old_base=issues -v new_base=issues -v report=1 -v want=issues/347-x.md "${LINKS[@]}" 2>/dev/null)"
if [ "$got" = 'HIT 347-x.md#見出し' ]; then echo "  ok   - report モードは動いた issue を指すリンクだけを出す (fragment つきも)"
else echo "  NG   - report モード: got [$got]"; fails=$((fails + 1)); fi

[ "$fails" -eq 0 ] || { echo "[test-issue-links-awk] $fails 件 NG"; exit 1; }
echo "[test-issue-links-awk] すべて ok"
