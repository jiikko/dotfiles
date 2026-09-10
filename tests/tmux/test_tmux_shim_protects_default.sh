#!/usr/bin/env bash
#
# bin/tmux shim が「本番 tmux サーバ (default) を非対話シェルから kill させない」ことを固定する。
#
# 🚨 なぜ (実害): 2026-07-30 / 2026-09-11 に Claude 起因で本番 default が 2 度 kill された。
# 2 度目はテストスクリプト内部の `tmux -L default kill-server` で、TMPDIR が存在せず
# TMUX_TMPDIR が空に落ち、tmux が /tmp へフォールバックして本番を直撃した。
# shim は PATH 先頭 (~/dotfiles/bin) に居るので script 内部の bare tmux も傍受できる。
#
# 🚨 本番には絶対に触れない検査にする:
#   - 決定 (block か素通しか) は REAL をスタブに差し替えたコピーで見る (exec しても記録するだけ)。
#     今日の正確な形 (空 TMUX_TMPDIR + -L default) もこれで安全に確認できる。
#   - 実 exec の証明だけ、隔離したデコイサーバ (-L <ユニーク名> + 短い TMUX_TMPDIR) で行う。
#     デコイは protect ファイルに載せて「実 kill が止まる」を見る (guard が load-bearing)。
#
# 🚨 検出しないと決めた形 (脅威モデル = うっかり。意図的回避は対象外): 実体を絶対パスで
#   直接呼ぶ形 / 対話 TTY からの kill。前者は正規のエスケープ、後者は「ユーザー本人の意図」。
set -uo pipefail
unset CDPATH
unset TMUX TMUX_PANE   # 🚨 $TMUX は TMUX_TMPDIR より優先される。残すと解決が本番を向く

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SHIM="$ROOT_DIR/bin/tmux"

fails=0; checks=0
ok()  { checks=$((checks + 1)); printf '✓ %s\n' "$1"; }
bad() { checks=$((checks + 1)); printf '✗ %s\n' "$1" >&2; fails=$((fails + 1)); }

# --- 後始末: ファイル全体を 1 本の trap で覆う -------------------------------------------------
WORK=""; DECOY_TMPDIR=""; DECOY_NAME=""; REAL_TMUX=""
cleanup() {
  [ -n "$DECOY_NAME" ] && [ -n "$REAL_TMUX" ] && \
    TMUX_TMPDIR="$DECOY_TMPDIR" "$REAL_TMUX" -L "$DECOY_NAME" kill-server 2>/dev/null || :
  [ -n "$DECOY_TMPDIR" ] && rm -rf -- "$DECOY_TMPDIR"
  [ -n "$WORK" ] && rm -rf -- "$WORK"
  return 0
}
trap cleanup EXIT INT TERM HUP

[ -x "$SHIM" ] || { bad "shim が実行可能でない: $SHIM"; printf '\n検査 %d 件: fail=%d\n' "$checks" "$fails"; exit 1; }

# shim が固定している実体 tmux のパスを読む (テストからも同じ判断で decoy を起こす)。
REAL_TMUX=$(sed -n 's/^REAL=\(.*\)$/\1/p' "$SHIM" | head -1)
[ -n "$REAL_TMUX" ] || bad "shim から REAL= を読めない (shim の形が変わった?)"

WORK=$(mktemp -d "${TMPDIR:-/tmp}/tmux_shim_test.XXXXXX")

# --- スタブ経由の決定テスト (本番に触れない) --------------------------------------------------
# REAL を「実行されたら記録して 0」のスタブに差し替えたコピーを作る。
stub="$WORK/fake_tmux"
cat > "$stub" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
chmod +x "$stub"
shim_copy="$WORK/shim_copy"
# REAL 行だけを差し替える。自己参照ガードはコピーが dotfiles/bin 配下でないので発火しない。
sed "s#^REAL=.*#REAL=$stub#" "$SHIM" > "$shim_copy"
chmod +x "$shim_copy"
bash -n "$shim_copy" || bad "shim のスタブコピーが構文エラー"

uid=$(id -u)
# 🚨 サブシェルを使わない。( decide ... ) にすると checks/fails が子で増えて親へ届かず、
#   件数 assert が壊れ、しかも決定テスト内の失敗が親の fails に伝播しない (false green)。
#   per-case の環境は env(1) でその 1 コマンドにだけ与える (プロセス汚染も残さない)。
# check <期待 BLOCK|PASS> <label> <cmd...>   ← cmd は env ... bash "$shim_copy" ... の形
check() {
  local exp="$1" label="$2"; shift 2
  local rc; "$@" </dev/null >/dev/null 2>&1; rc=$?
  local got; if [ "$rc" -ge 3 ] && [ "$rc" -le 4 ]; then got=BLOCK; elif [ "$rc" = 0 ]; then got=PASS; else got="ERR$rc"; fi
  if [ "$got" = "$exp" ]; then ok "$label ($got)"; else bad "$label: want=$exp got=$got (rc=$rc)"; fi
}

# 素通し (fast path / 読み取り系)
check PASS "非 kill (-V) は素通し"                 env -u TMUX bash "$shim_copy" -V
check PASS "読み取り系 (ls) は素通し"               env -u TMUX bash "$shim_copy" ls
check PASS "send-keys の引数に kill 文字列 → 素通し" env -u TMUX bash "$shim_copy" send-keys "tmux kill-server" Enter
# 隔離サーバの kill は素通し (over-block しない)
check PASS "隔離 -L tt-cleanup-x の kill は素通し"   env -u TMUX bash "$shim_copy" -L tt-cleanup-x-1 kill-server
check PASS "隔離 -S /tmp/iso/foo の kill は素通し"   env -u TMUX bash "$shim_copy" -S /tmp/iso-xyz/foo kill-server
# 本番 default への kill は block (今日の形を含む)
check BLOCK "今日の形: 空 TMUX_TMPDIR + -L default"  env -u TMUX TMUX_TMPDIR= bash "$shim_copy" -L default kill-server
check BLOCK "不在 TMUX_TMPDIR + -L default"          env -u TMUX TMUX_TMPDIR=/nonexistent-xyz bash "$shim_copy" -L default kill-server
check BLOCK "-L default kill-server (TMUX unset)"    env -u TMUX bash "$shim_copy" -L default kill-server
check BLOCK "-S /private/tmp/../default kill-server" env -u TMUX bash "$shim_copy" -S "/private/tmp/tmux-$uid/default" kill-server
check BLOCK "-S /tmp/../default (symlink 正規化)"    env -u TMUX bash "$shim_copy" -S "/tmp/tmux-$uid/default" kill-server
check BLOCK "-L default kill-session も block (非対話)" env -u TMUX bash "$shim_copy" -L default kill-session -t x
check BLOCK "bare kill-server (TMUX=本番)"           env TMUX="/private/tmp/tmux-$uid/default,1,0" bash "$shim_copy" kill-server
# 🚨 P1: 大文字小文字違いの綴りも本番に届く (macOS FS は case-insensitive)。両防御を貫通していた。
check BLOCK "-L DEFAULT (大文字) kill-server"        env -u TMUX bash "$shim_copy" -L DEFAULT kill-server
check BLOCK "-L Default (混在) kill-server"          env -u TMUX bash "$shim_copy" -L Default kill-server
check BLOCK "-S <...>/DEFAULT kill-server"           env -u TMUX bash "$shim_copy" -S "/private/tmp/tmux-$uid/DEFAULT" kill-server
check BLOCK "-L DEFAULT kill-session (非対話)"       env -u TMUX bash "$shim_copy" -L DEFAULT kill-session -t x

# protect ファイル経由の decoy が block されること (名前が default でなくても守れる)
pf="$WORK/protect"; echo "/private/tmp/tmux-$uid/mydecoy" > "$pf"
check BLOCK "protect ファイルの socket は block"     env -u TMUX TMUX_PROTECT_FILE="$pf" bash "$shim_copy" -L mydecoy kill-server
check PASS  "protect に無い名前は素通し"             env -u TMUX TMUX_PROTECT_FILE="$pf" bash "$shim_copy" -L notlisted kill-server

# --- 実 exec の証明 (隔離デコイ。guard が load-bearing であること) ---------------------------
if [ -z "$REAL_TMUX" ] || [ ! -x "$REAL_TMUX" ]; then
  # 決定テストは既に済んでいる。実 tmux が無い環境では実 exec 証明だけ skip する。
  # 🚨 skip を緑に畳まない: ここまでで違反が出ていれば失敗のまま終える。
  ok "(skip) 実体 tmux ($REAL_TMUX) が無いので実 exec 証明は省略 (決定テストは実施済み)"
else
  DECOY_TMPDIR="/tmp/tsd$$"; mkdir -p "$DECOY_TMPDIR"
  DECOY_NAME="zzdecoy$$"
  alive() { TMUX_TMPDIR="$DECOY_TMPDIR" "$REAL_TMUX" -L "$DECOY_NAME" has-session 2>/dev/null && echo A || echo D; }
  ( unset TMUX; TMUX_TMPDIR="$DECOY_TMPDIR" "$REAL_TMUX" -L "$DECOY_NAME" -f /dev/null new-session -d 'sleep 300' 2>/dev/null )
  if [ "$(alive)" != A ]; then
    ok "(skip) デコイ起動に失敗したので実 exec 証明は省略 (決定テストは実施済み)"
  else
    decoy_path="$DECOY_TMPDIR/tmux-$uid/$DECOY_NAME"
    echo "$decoy_path" > "$WORK/protect_decoy"
    # protect 登録あり → 非対話 kill は止まり、デコイは生存 (guard が実 kill を止めた)
    ( unset TMUX; TMUX_TMPDIR="$DECOY_TMPDIR" TMUX_PROTECT_FILE="$WORK/protect_decoy" \
        "$SHIM" -L "$DECOY_NAME" kill-server </dev/null >/dev/null 2>&1 )
    if [ "$(alive)" = A ]; then ok "protect 登録時: 実 shim が実 kill を止め、デコイ生存"
    else bad "protect 登録したのにデコイが死んだ (guard が効いていない)"; fi
    # protect 登録なし → 素通しで実 kill、デコイ消滅 (guard が load-bearing の証明)
    ( unset TMUX; TMUX_TMPDIR="$DECOY_TMPDIR" TMUX_PROTECT_FILE="/dev/null" \
        "$SHIM" -L "$DECOY_NAME" kill-server </dev/null >/dev/null 2>&1 )
    # 消えるまで少し待つ
    for _ in $(seq 20); do [ "$(alive)" = D ] && break; sleep 0.05; done
    if [ "$(alive)" = D ]; then ok "protect 非登録時: 素通しで実 kill が通りデコイ消滅 (guard は load-bearing)"
    else bad "protect 非登録なのにデコイが生存 (shim が実 tmux を exec していない = 素通しが壊れている)"; fi
  fi
fi

# 決定テストは常に 18 件走る (skip で満たされる余地を無くすため実 exec 抜きの下限にする)。
want_checks=18
[ "$checks" -ge "$want_checks" ] || bad "検査が $checks 件しか走っていない (${want_checks} 件以上のはず)"
printf '\n検査 %d 件: fail=%d\n' "$checks" "$fails"
[ "$fails" -eq 0 ] || exit 1
echo "OK tmux shim protects default"
