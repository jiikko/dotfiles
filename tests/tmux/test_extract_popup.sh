#!/usr/bin/env bash
# scripts/tmux_extract_popup.sh (extrakto 型の抽出 popup) の unit テスト。
#
# 抽出 awk とアクション分岐が本体で、fzf UI は --expect の出力契約 (1 行目 = キー or 空、
# 2 行目 = 選択行) 越しに stub で差し替えられる。stub tmux / fzf / pbcopy / open で全経路を
# 傍受し、実サーバ・実クリップボード・実ブラウザには触れない。
#
# 固定する不変条件:
#   - 抽出: URL の末尾句読点除去 / 新しい行 (画面下) 優先 / dedup
#   - send-keys は必ず `-l --` 付き (先頭 '-' の抽出語で invalid flag 死する回帰の防止。
#     スクリプト内コメントの一次情報を挙動で pin)
#   - ctrl-o は URL → open / path → $EDITOR + 行番号除去
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_extract_popup.sh"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

CALLS="$TMP_DIR/calls.log"; : > "$CALLS"; export CALLS
FZF_IN="$TMP_DIR/fzf_in.log"; export FZF_IN
PB_LOG="$TMP_DIR/pbcopy.log"; export PB_LOG
mkdir -p "$TMP_DIR/bin"
cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$1" in
  display)      printf '%%3\n' ;;
  capture-pane) printf '%b' "${STUB_CAPTURE:-}" ;;
esac
exit 0
EOS
cat > "$TMP_DIR/bin/fzf" <<'EOS'
#!/bin/sh
cat > "$FZF_IN"
echo "fzf $*" >> "$CALLS"
[ -n "${STUB_FZF_EXIT:-}" ] && exit "$STUB_FZF_EXIT"
printf '%b' "${STUB_FZF_OUT:-}"
EOS
cat > "$TMP_DIR/bin/pbcopy" <<'EOS'
#!/bin/sh
cat > "$PB_LOG"
echo "pbcopy" >> "$CALLS"
EOS
cat > "$TMP_DIR/bin/open" <<'EOS'
#!/bin/sh
echo "open $*" >> "$CALLS"
EOS
chmod +x "$TMP_DIR/bin/"*
STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"

. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"

printf '## 抽出パイプライン (capture → 候補構築)\n'
reset_calls; rm -f "$FZF_IN"
CAP='old line with src/main.go:12 and deadbeef1\nnewest https://example.com/a). plus src/main.go:12\n'
STUB_CAPTURE="$CAP" STUB_FZF_OUT='' STUB_FZF_EXIT=1 run "$STUB_PATH" "$SCRIPT"
grep -q $'url \x1b\[0m\thttps://example.com/a$' "$FZF_IN" \
  || { printf '✗ URL の末尾句読点/括弧が除去されていない:\n'; grep url "$FZF_IN" || true; exit 1; }
printf '✓ URL 抽出 + 末尾の「).」除去\n'
[[ "$(grep -c 'src/main.go:12' "$FZF_IN")" -eq 1 ]] || { printf '✗ dedup されていない\n'; exit 1; }
printf '✓ 同一断片は dedup される\n'
first_url_line=$(grep -n 'https://example.com/a' "$FZF_IN" | cut -d: -f1)
hash_line=$(grep -n 'deadbeef1' "$FZF_IN" | cut -d: -f1)
[[ "$first_url_line" -lt "$hash_line" ]] || { printf '✗ 新しい行 (画面下) の断片が優先されていない\n'; exit 1; }
printf '✓ 画面下 (新しい行) の断片が先に並ぶ\n'
[[ "$RC" -eq 0 ]] || { printf '✗ fzf キャンセルで exit %s (0 のはず)\n' "$RC"; exit 1; }
printf '✓ fzf キャンセル → 何もせず exit 0\n'

printf '\n## 候補ゼロ\n'
reset_calls; rm -f "$FZF_IN"
STUB_CAPTURE='ab cd\n' run "$STUB_PATH" "$SCRIPT"
[[ "$RC" -eq 0 && ! -e "$FZF_IN" ]] || { printf '✗ 候補ゼロで fzf が呼ばれた/非 0 (RC=%s)\n' "$RC"; exit 1; }
printf '✓ 候補ゼロ → fzf を呼ばず exit 0\n'

printf '\n## アクション分岐 (--expect 契約)\n'
reset_calls
STUB_CAPTURE='seedword1\n' STUB_FZF_OUT='\nword\tfoobar123\n' run "$STUB_PATH" "$SCRIPT"
assert_called "send-keys -t %3 -l -- foobar123" "Enter → 元ペインへ -l -- 付きで貼り付け"
reset_calls; : > "$PB_LOG"
STUB_CAPTURE='seedword1\n' STUB_FZF_OUT='ctrl-y\nword\thello123\n' run "$STUB_PATH" "$SCRIPT"
assert_called "pbcopy" "C-y → クリップボードへ"
[[ "$(cat "$PB_LOG")" == "hello123" ]] || { printf '✗ pbcopy へ渡った内容が %s\n' "$(cat "$PB_LOG")"; exit 1; }
printf '✓ コピー内容は選択断片そのもの\n'
assert_not_called "send-keys" "C-y では貼り付けない"
reset_calls
STUB_CAPTURE='seedword1\n' STUB_FZF_OUT='ctrl-o\nurl\thttps://x.test/y\n' run "$STUB_PATH" "$SCRIPT"
assert_called "open https://x.test/y" "C-o + URL → open"
assert_not_called "send-keys" "C-o + URL では send-keys しない"
reset_calls
STUB_CAPTURE='seedword1\n' STUB_FZF_OUT='ctrl-o\npath\tsrc/foo.rb:42\n' EDITOR=vim run "$STUB_PATH" "$SCRIPT"
assert_called "send-keys -t %3 -l -- vim src/foo.rb" "C-o + path → \$EDITOR + 行番号 (:42) 除去"
reset_calls
STUB_CAPTURE='seedword1\n' STUB_FZF_OUT='\nword\t--force-flag\n' run "$STUB_PATH" "$SCRIPT"
assert_called "send-keys -t %3 -l -- --force-flag" "先頭 '-' の断片も -l -- ガードで literal 貼り付け"

printf '\nAll extract-popup tests passed successfully!\n'
