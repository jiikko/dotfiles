#!/usr/bin/env bash
# scripts/tmux_resurrect_save.sh の archive 完全性ガード (tt_archive_ok / tt_archive_finalize) の
# unit テスト。issue 070 の 3 件を pin する:
#
#   1. tt_archive_ok が「entry >= 1」を実装していること。0 entry の tar.gz は非空で gzip -t も
#      通るため、そこを健全と読むと下の掃除が唯一の良い退避コピーを消す (silent なデータ喪失)
#   2. tt_archive_finalize が「壊れていれば退避から復旧」「健全と確認できたときだけ退避を掃除」
#      の両方向を守ること (確認できないまま捨てると復旧手段が消える)
#   3. tt_archive の在処が退行ガード (TT_SAVE_ALLOW_REGRESSION) の外で決まること。内側にあると
#      正当な bypass 実行で archive 検証がまるごと skip される
#
# スクリプトは TT_SAVE_SOURCE_ONLY=1 で source すると本体を実行しないので関数を直接呼ぶ
# (test_resurrect_save_lock.sh と同方式)。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_resurrect_save.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
fails=0

ok()  { printf '✓ %s\n' "$1"; }
ng()  { printf '✗ %s\n' "$1"; fails=$((fails + 1)); }

[ -x "$SCRIPT" ] || { ng "スクリプトが無い/実行不可: $SCRIPT"; exit 1; }

# 0 entry / 1 entry の archive を作る (tar は entry 無しでも成功し、gzip -t も通る)
tar -czf "$WORK/empty.tar.gz" -T /dev/null
printf 'pane body\n' >"$WORK/f.txt"
tar -czf "$WORK/one.tar.gz" -C "$WORK" f.txt
printf 'not gzip at all' >"$WORK/garbage.tar.gz"
: >"$WORK/zero.tar.gz"

run_case() {  # $1=ケース名 … bash -c の中で関数を呼び、結果行を返す
  TT_SAVE_SOURCE_ONLY=1 HOME="$WORK/home" TT_SAVE_STATE_DIR="$WORK/state" \
    bash -c 'source "$1"; shift; "$@"' _ "$SCRIPT" "$@"
}

# --- 1. tt_archive_ok の判定 ------------------------------------------------------------
for spec in "one.tar.gz:健全" "empty.tar.gz:壊れ" "garbage.tar.gz:壊れ" "zero.tar.gz:壊れ"; do
  f="${spec%%:*}"; want="${spec##*:}"
  if run_case tt_archive_ok "$WORK/$f"; then got=健全; else got=壊れ; fi
  if [ "$got" = "$want" ]; then ok "tt_archive_ok: $f → $got"; else ng "tt_archive_ok: $f → $got (want $want)"; fi
done

# --- 2. finalize: 壊れた archive は退避から復旧する ---------------------------------------
cp "$WORK/empty.tar.gz" "$WORK/cur.tar.gz"        # 現 archive = 0 entry (壊れ扱い)
cp "$WORK/one.tar.gz" "$WORK/bak.tar.gz"          # 退避 = 健全
run_case tt_archive_finalize "$WORK/cur.tar.gz" "$WORK/bak.tar.gz" 0
if run_case tt_archive_ok "$WORK/cur.tar.gz" && [ ! -f "$WORK/bak.tar.gz" ]; then
  ok "finalize: 壊れた archive を退避から復旧し、退避は消費された"
else
  ng "finalize: 復旧されていない (cur が健全でない / bak が残っている)"
fi

# --- 3. finalize: 健全と確認できないときは退避を消さない -----------------------------------
cp "$WORK/empty.tar.gz" "$WORK/cur2.tar.gz"
cp "$WORK/empty.tar.gz" "$WORK/bak2.tar.gz"       # 退避も壊れている = 直せない
run_case tt_archive_finalize "$WORK/cur2.tar.gz" "$WORK/bak2.tar.gz" 0
if [ -f "$WORK/bak2.tar.gz" ]; then
  ok "finalize: 復旧できないとき退避を消さない (復旧手段を残す)"
else
  ng "finalize: 直せないのに退避を消した (旧実装の無条件削除と同じ穴)"
fi

# --- 4. finalize: 健全なら退避を掃除する --------------------------------------------------
cp "$WORK/one.tar.gz" "$WORK/cur3.tar.gz"
cp "$WORK/one.tar.gz" "$WORK/bak3.tar.gz"
run_case tt_archive_finalize "$WORK/cur3.tar.gz" "$WORK/bak3.tar.gz" 0
if [ ! -f "$WORK/bak3.tar.gz" ]; then
  ok "finalize: 健全なら退避を掃除する"
else
  ng "finalize: 健全なのに退避が残った (毎保存でゴミが積む)"
fi

# --- 5. archive の在処が退行ガードの外で決まる --------------------------------------------
# bypass 実行 (TT_SAVE_ALLOW_REGRESSION=1) でも tt_archive が空にならないこと。ソースを読んで
# 「tt_archive= の代入がガードの外側にある」ことを構造で pin する (本体実行は tmux が要るため)。
# shellcheck disable=SC2016  # grep のパターンなので展開させない
guard_line="$(grep -n 'if \[ "${TT_SAVE_ALLOW_REGRESSION:-}" != "1" \]' "$SCRIPT" | head -1 | cut -d: -f1)"
# shellcheck disable=SC2016  # grep のパターンなので展開させない
assign_line="$(grep -n 'tt_archive="\$tt_rdir/pane_contents.tar.gz"' "$SCRIPT" | head -1 | cut -d: -f1)"
if [ -n "$guard_line" ] && [ -n "$assign_line" ] && [ "$assign_line" -lt "$guard_line" ]; then
  ok "tt_archive の在処が退行ガードの外で決まる (bypass でも検証が回る)"
else
  ng "tt_archive の代入がガードの内側 (行 $assign_line / ガード $guard_line) = bypass で検証が skip される"
fi

# --- 5b. reject 経路も finalize を通る (archive-broken の観測を落とさない) -------------------
# 回帰 2026-08-21: finalize のコメントは「全 return 経路から呼ぶ」と宣言していたのに reject 経路
# (退行を検知して last を戻す経路) だけ呼んでいなかった。上の mv で退避は消費済みなので修復は
# できないが、戻した archive 自体が壊れていたときに archive-broken を残せないと「退行を戻したので
# 安全」と読めてしまう。構造で pin する (実行系は tmux を要するため)。
reject_line="$(grep -n 'reject したら非 0 を返す' "$SCRIPT" | head -1 | cut -d: -f1)"
if [ -n "$reject_line" ] && grep -q 'tt_archive_finalize' <<< "$(sed -n "$((reject_line - 8)),${reject_line}p" "$SCRIPT")"; then
  ok "reject 経路も return 前に archive 検証を通す"
else
  ng "reject 経路が finalize を飛ばして return している (archive-broken の観測が落ちる)"
fi

# --- 6. escape hatch が退避を消す前に検証を通す --------------------------------------------
# 構造で pin する: regression-stuck-override のログ行の直後、return 0 より前に finalize がある
hatch="$(grep -n 'regression-stuck-override' "$SCRIPT" | head -1 | cut -d: -f1)"
if [ -n "$hatch" ] && grep -q 'tt_archive_finalize' <<< "$(sed -n "${hatch},$((hatch + 8))p" "$SCRIPT")"; then
  ok "escape hatch が return する前に archive 検証を通す"
else
  ng "escape hatch が検証を飛ばして return している (唯一の復旧手段を消してから返る)"
fi

if [ "$fails" -gt 0 ]; then
  printf '\nFAIL: archive ガードのテストが %d 件失敗\n' "$fails"
  exit 1
fi
printf '\nAll resurrect-save archive tests passed successfully!\n'
