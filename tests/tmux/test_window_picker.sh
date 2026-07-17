#!/usr/bin/env bash
# scripts/lib/tmux_fzf_window_picker.sh (tt_fzf_window_picker) の unit テスト。
#
# jump (bind f) と pane 移動 (bind g/G) が共有する候補構築ロジックの回帰ガード。
# test_fork_scratch.sh の検査 D は「変数と lib の参照が配線されているか」の静的 grep のみで、
# フィルタ/ソート/整形の実ロジックは一度も実行していない (フィルタ条件が反転しても通る)。
# ここでは stub tmux / fzf / date で実際に関数を駆動して固定する:
#   - popup 専用セッション (scratch 等) の除外は実物の TT_POPUP_SESSION_RE を source して検証
#   - 選択結果は column 整形の影響を受けない安定キー window_id (ヘッダコメント記載の旧回帰)
#   - activity 降順 / いまここマーク / exclude-current / 相対時刻バケツ境界
#
# ⚠️ TT_POPUP_SESSION_RE が空だと awk の空パターンが全行にマッチして候補ゼロに落ち、
#    「何も検証せず成功」する誤検知になるため、happy path は必ず候補非空を先に assert する。
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

CALLS="$TMP_DIR/calls.log"; : > "$CALLS"; export CALLS
FZF_IN="$TMP_DIR/fzf_in.log"; export FZF_IN
mkdir -p "$TMP_DIR/bin"

cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$1" in
  display)      printf '%s\n' "${STUB_CURRENT:-main:1}" ;;
  list-windows) printf '%b' "${STUB_WINDOWS:-}" ;;
  *) exit 1 ;;  # 想定外のサブコマンド = 契約違反として失敗させる
esac
EOS
cat > "$TMP_DIR/bin/fzf" <<'EOS'
#!/bin/sh
cat > "$FZF_IN"
echo "fzf $*" >> "$CALLS"
case "${STUB_FZF_MODE:-first}" in
  first)  head -1 "$FZF_IN" ;;
  cancel) exit 130 ;;
esac
EOS
cat > "$TMP_DIR/bin/date" <<'EOS'
#!/bin/sh
[ -n "${STUB_NOW:-}" ] && { printf '%s\n' "$STUB_NOW"; exit 0; }
exec /bin/date "$@"
EOS
chmod +x "$TMP_DIR/bin/tmux" "$TMP_DIR/bin/fzf" "$TMP_DIR/bin/date"
STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"

. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"
# 実物の除外パターンと picker 本体を source (RE を自前定義するとテストが実装から乖離する)
# shellcheck disable=SC1091
. "$ROOT_DIR/scripts/lib/tmux_popup_sessions.sh"
# shellcheck disable=SC1091
. "$ROOT_DIR/scripts/lib/tmux_fzf_window_picker.sh"
[ -n "${TT_POPUP_SESSION_RE:-}" ] || { printf '✗ TT_POPUP_SESSION_RE が空 (popup_sessions lib の変更?)\n'; exit 1; }

pick() {  # $1=exclude_current, 環境: STUB_* — stdout を $OUT, exit を $RC に
  RC=0
  OUT=$(PATH="$STUB_PATH" tt_fzf_window_picker "test> " "$1" 2>"$TMP_DIR/err.log") || RC=$?
}

export STUB_NOW=1000000
# 形式: window_activity \t window_id \t session:index \t 表示名
ROWS='999990\t@10\tmain:1\tvim\n999940\t@11\tmain:2\trelease notes draft\n999000\t@12\tsub:1\tserver\n999999\t@13\tscratch:0\tzsh\n'

printf '## 候補構築 (jump モード: exclude なし)\n'
reset_calls; rm -f "$FZF_IN"
STUB_WINDOWS="$ROWS" STUB_CURRENT="main:1" pick ""
[ -s "$FZF_IN" ] || { printf '✗ 候補が空 (誤検知ガード: RE が全行を食っている?)\n'; exit 1; }
printf '✓ 候補が非空 (誤検知ガード)\n'
[[ "$RC" -eq 0 && "$OUT" == "@10" ]] || { printf '✗ 選択結果が window_id でない: RC=%s OUT=%s\n' "$RC" "$OUT"; exit 1; }
printf '✓ 選択結果は先頭候補の window_id (@10)\n'
grep -q '@13' "$FZF_IN" && { printf '✗ popup 専用セッション scratch:0 が候補に混入\n'; exit 1; }
printf '✓ popup 専用セッション (scratch) は候補から除外\n'
grep 'いまここ' "$FZF_IN" | grep -q '@10' || { printf '✗ 現在 window に「いまここ」マークが無い\n'; exit 1; }
grep 'いまここ' "$FZF_IN" | grep -q '@11' && { printf '✗ 現在以外に「いまここ」マークが付いた\n'; exit 1; }
printf '✓ 「いまここ」マークは現在 window の行だけ\n'
ids=$(cut -f1 "$FZF_IN" | tr '\n' ' ')
[[ "$ids" == "@10 @11 @12 " ]] || { printf '✗ activity 降順になっていない: %s\n' "$ids"; exit 1; }
printf '✓ 候補は activity の新しい順 (sort -k1,1rn の回帰ガード)\n'

printf '\n## pane 移動モード (exclude-current)\n'
reset_calls; rm -f "$FZF_IN"
STUB_WINDOWS="$ROWS" STUB_CURRENT="main:1" pick "exclude-current"
grep -q '@10' "$FZF_IN" && { printf '✗ 現在 window が候補に残っている (自 window へ join 不可のはず)\n'; exit 1; }
printf '✓ 現在 window は候補から完全除外\n'
grep -q 'いまここ' "$FZF_IN" && { printf '✗ exclude モードでマークが出た\n'; exit 1; }
printf '✓ exclude モードではマーク列なし\n'

printf '\n## 表示名に空白があっても選択キーは千切れない (ヘッダ記載の旧回帰)\n'
[[ "$OUT" == "@11" ]] || { printf '✗ 空白入り名の行 (release notes draft) の選択結果が @11 でない: %s\n' "$OUT"; exit 1; }
printf '✓ 空白入り表示名の行でも window_id が正しく返る\n'

printf '\n## 相対時刻バケツの境界 (stub date で決定論化)\n'
reset_calls; rm -f "$FZF_IN"
B='999941\t@20\tmain:2\ts59\n999940\t@21\tmain:3\tm1\n996400\t@22\tmain:4\th1\n913600\t@23\tmain:5\td1\n'
STUB_WINDOWS="$B" STUB_CURRENT="other:9" pick ""
for want in '59秒前' '1分前' '1時間前' '1日前'; do
  grep -q "$want" "$FZF_IN" || { printf '✗ 相対時刻 %s が出ない (境界 off-by-one?)\n' "$want"; cat "$FZF_IN"; exit 1; }
done
printf '✓ 秒/分/時間/日のバケツ境界 (59s→59秒前, 60s→1分前, 3600s→1時間前, 86400s→1日前)\n'

printf '\n## 早期 return\n'
reset_calls; rm -f "$FZF_IN"
STUB_WINDOWS='999999\t@13\tscratch:0\tzsh\n' STUB_CURRENT="main:1" pick ""
[[ "$RC" -ne 0 && ! -e "$FZF_IN" ]] || { printf '✗ 候補ゼロで fzf が呼ばれた/RC=0 (RC=%s)\n' "$RC"; exit 1; }
printf '✓ 候補ゼロ (全て popup セッション) → fzf を呼ばず非 0\n'
reset_calls; rm -f "$FZF_IN"
STUB_WINDOWS="$ROWS" STUB_CURRENT="main:1" STUB_FZF_MODE=cancel pick ""
[[ "$RC" -ne 0 && -z "$OUT" ]] || { printf '✗ fzf キャンセルで RC=%s OUT=%s\n' "$RC" "$OUT"; exit 1; }
printf '✓ fzf キャンセル → 非 0 + 空出力\n'

printf '\nAll window-picker tests passed successfully!\n'
