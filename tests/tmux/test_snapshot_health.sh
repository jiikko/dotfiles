#!/usr/bin/env bash
# scripts/tmux_snapshot_health.sh (スナップショット機構の健全性チェック) の unit テスト。
#
# このチェックは「保存/復元が静かに壊れている」を検出する唯一の自動手段なので、
# 誤って OK を返す (= 壊れているのに気づけない) 退行を pin するのが主目的。
#   (1) 正常状態は OK / exit 0
#   (2) last が古い → NG (保存経路が止まっている)
#   (3) last が無い → NG
#   (4) 常駐プロセス不在 → NG
#   (5) archive が last より古い → NG (window は復元されるが pane 内容が別世代)
#   (6) 非 default socket → 判定対象外で exit 0 (隔離テストサーバを NG にしない)
#   (7) --quiet は NG のときだけ 1 行出す
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_snapshot_health.sh"
TMP_DIR="$(mktemp -d)"
HELPER_PIDS=()
cleanup() {
  for p in "${HELPER_PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

# 補助プロセスは fork/exec レースで EXIT trap が子で発火するのを避ける形で起こす
# (理由は tests/tmux/test_periodic_save.sh 冒頭の注記と同じ)
spawn_helper() {
  ( trap - EXIT; exec sleep 300 ) >/dev/null 2>&1 &
  REPLY_PID=$!
  HELPER_PIDS+=("$REPLY_PID")
}

CALLS="$TMP_DIR/calls.log"; : > "$CALLS"; export CALLS
mkdir -p "$TMP_DIR/bin" "$TMP_DIR/rdir" "$TMP_DIR/wd" "$TMP_DIR/ps"
DEFAULT_SOCK="$(realpath /tmp 2>/dev/null || echo /tmp)/tmux-$(id -u)/default"

cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$1" in
  display-message) printf '%s\n' "${STUB_SOCKET_PATH:-}" ;;
  list-sessions)   printf '%b' "${STUB_SESSIONS:-}" ;;
  show)
    case "$*" in
      *@resurrect-dir*)                  printf '%s\n' "${STUB_RDIR:-}" ;;
      *@continuum-save-interval*)        printf '%s\n' "${STUB_INTERVAL:-}" ;;
      *@resurrect-capture-pane-contents*) printf '%s\n' "${STUB_CAPTURE:-on}" ;;
    esac ;;
esac
EOS
chmod +x "$TMP_DIR/bin/tmux"
STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"

. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"

RDIR="$TMP_DIR/rdir"
mk_snapshot() {  # $1=名前 $2=セッション数 $3=何秒前にするか
  local f="$RDIR/$1" i
  : > "$f"
  for i in $(seq 1 "$2"); do
    printf 'pane\tsess%s\t1\t1\t:*\t1\t zsh\t:/tmp\t1\tzsh\t:\n' "$i" >> "$f"
    printf 'window\tsess%s\t1\t: zsh\t1\t:*\tlayout\t:\n' "$i" >> "$f"
  done
  ln -sfn "$1" "$RDIR/last"
  # mtime を過去にずらす (touch -t は秒精度で十分)
  if [ "${3:-0}" -gt 0 ]; then
    touch -t "$(date -v-"$3"S '+%Y%m%d%H%M.%S' 2>/dev/null || date -d "-$3 seconds" '+%Y%m%d%H%M.%S')" "$f"
  fi
}

# last の pane 行に対応する健全な archive を作る ($1=セッション数)
mk_archive() {
  local d="$TMP_DIR/pc" i
  rm -rf "$d"; mkdir -p "$d/pane_contents"
  for i in $(seq 1 "$1"); do printf 'scrollback\n' > "$d/pane_contents/pane-sess$i:1.1"; done
  ( cd "$d" && tar czf "$RDIR/pane_contents.tar.gz" ./pane_contents/ ) 2>/dev/null
}

health() {  # 共通 env で実行。追加 env は呼び出し側が前置きする
  run "$STUB_PATH" env \
    TT_WATCHDOG_DIR="$TMP_DIR/wd" TT_PERIODIC_STATE_DIR="$TMP_DIR/ps" \
    STUB_SOCKET_PATH="${SOCK_OVERRIDE:-$DEFAULT_SOCK}" STUB_RDIR="$RDIR" \
    STUB_INTERVAL="${INTERVAL_OVERRIDE:-15}" STUB_SESSIONS="${SESSIONS_OVERRIDE:-$DEF_SESSIONS}" \
    STUB_CAPTURE="${CAPTURE_OVERRIDE:-on}" \
    bash "$SCRIPT" "$@"
}

DEF_SESSIONS='sess1\nsess2\nsess3\n'
# shellcheck disable=SC2034 # RUN_ERR は source する stub_assert_helper.sh の run() が参照する
RUN_OUT="$TMP_DIR/out"; RUN_ERR="$TMP_DIR/err"

# 常駐プロセス 2 つを「生きている」状態にする。
# ⚠️ owner ファイルの中身をここで組み立てないこと: 書式 ("pid<TAB>lstart") を production から
# コピペすると書式変更に追従できず、実物とずれた fixture で常に緑になる (2026-08-20 の誤報を
# このテストが通してしまった原因)。書き手 tt_lock_write_owner を呼ぶ。
. "$ROOT_DIR/scripts/lib/tmux_resurrect_guards.sh"
spawn_helper; PERIODIC="$REPLY_PID"; mkdir -p "$TMP_DIR/ps/1.lock"; tt_lock_write_owner "$TMP_DIR/ps/1.lock" "$PERIODIC"
spawn_helper; WATCH="$REPLY_PID";    mkdir -p "$TMP_DIR/wd/1.lock"; tt_lock_write_owner "$TMP_DIR/wd/1.lock" "$WATCH"

# 書き手が「pid<TAB>lstart」形式で書いていること。pid のみの旧形式へ退行すると、読み手は
# 旧形式フォールバック (pid 生存のみ) に落ちて pid 再利用の照合が黙って無効化される
# (2026-08-20 の red team が、この検査が無いと退行が緑のまま通ることを実証)。
owner_line="$(cat "$TMP_DIR/ps/1.lock/pid")"
case "$owner_line" in
  *"$(printf '\t')"*) printf '✓ 書き手は pid<TAB>lstart 形式で owner を記録する\n' ;;
  *) printf '✗ owner が pid<TAB>lstart 形式でない (再利用照合が無効化される): [%s]\n' "$owner_line"; exit 1 ;;
esac

# --- (1) 正常 -------------------------------------------------------------------------
mk_snapshot snap_ok.txt 3 0
mk_archive 3
health
[[ "$RC" -eq 0 ]] || { printf '✗ 正常状態で exit %s (0 のはず):\n' "$RC"; cat "$RUN_OUT"; exit 1; }
grep -q 'OK' "$RUN_OUT" || { printf '✗ OK と表示されない:\n'; cat "$RUN_OUT"; exit 1; }
printf '✓ 正常状態は OK / exit 0\n'

# --- (7) --quiet は正常時に無出力 ------------------------------------------------------
health --quiet
[[ "$RC" -eq 0 && ! -s "$RUN_OUT" ]] || { printf '✗ --quiet が正常時に出力した:\n'; cat "$RUN_OUT"; exit 1; }
printf '✓ --quiet は正常時に無出力\n'

# --- (2) 最後の保存が古い → NG ---------------------------------------------------------
# 鮮度の入力は「last と archive の新しい方」なので、両方を過去にずらす必要がある
# (dedup で last の mtime が動かない世代でも archive は更新されるため。誤検知防止の設計)
mk_snapshot snap_old.txt 3 7200   # 2 時間前 (閾値 = 15 分 × 2 だが下限 30 分 → 30 分)
mk_archive 3
touch -t "$(date -v-7200S '+%Y%m%d%H%M.%S' 2>/dev/null || date -d '-7200 seconds' '+%Y%m%d%H%M.%S')" "$RDIR/pane_contents.tar.gz"
health
[[ "$RC" -eq 1 ]] || { printf '✗ 古い last で exit %s (1 のはず):\n' "$RC"; cat "$RUN_OUT"; exit 1; }
grep -q '古い' "$RUN_OUT" || { printf '✗ 鮮度異常が報告されない:\n'; cat "$RUN_OUT"; exit 1; }
printf '✓ last が古いと NG (保存経路の停止を検出)\n'
health --quiet
grep -q 'スナップショット異常' "$RUN_OUT" || { printf '✗ --quiet が NG を 1 行で出さない:\n'; cat "$RUN_OUT"; exit 1; }
printf '✓ --quiet は NG のときだけ 1 行出力\n'

# --- (3) last が無い → NG --------------------------------------------------------------
rm -f "$RDIR/last"
health
[[ "$RC" -eq 1 ]] || { printf '✗ last 不在で exit %s (1 のはず)\n' "$RC"; exit 1; }
grep -q 'last スナップショットが無い' "$RUN_OUT" || { printf '✗ last 不在が報告されない:\n'; cat "$RUN_OUT"; exit 1; }
printf '✓ last が無いと NG\n'

# --- (5a) archive が truncate されている → NG (mtime は新しいので中身で判定できるか) ----
# レビューで実証された欠陥の回帰テスト: 旧実装は mtime 比較だったため、たった今 truncate された
# 壊れた archive を「新しいから OK」と判定していた。復元すると全 pane の scrollback が空になる。
mk_snapshot snap_now.txt 3 0
printf '\037\213broken' > "$RDIR/pane_contents.tar.gz"   # gzip magic だけの壊れたファイル (mtime は今)
health
[[ "$RC" -eq 1 ]] || { printf '✗ 壊れた archive で exit %s (1 のはず):\n' "$RC"; cat "$RUN_OUT"; exit 1; }
grep -q '壊れている' "$RUN_OUT" || { printf '✗ archive の破損が報告されない:\n'; cat "$RUN_OUT"; exit 1; }
printf '✓ archive が壊れていると NG (mtime が新しくても中身で判定する)\n'

# --- (5b) archive に last の pane が入っていない → NG (世代不一致・部分欠落) -----------
mk_snapshot snap_now.txt 5 0   # last は 5 セッション
mk_archive 2                   # archive は 2 セッション分しか無い
health
[[ "$RC" -eq 1 ]] || { printf '✗ archive の pane 欠落で exit %s (1 のはず):\n' "$RC"; cat "$RUN_OUT"; exit 1; }
grep -q 'last の pane が 3 個入っていない' "$RUN_OUT" \
  || { printf '✗ pane 欠落数が正しく報告されない:\n'; cat "$RUN_OUT"; exit 1; }
printf '✓ archive に last の pane が欠けていると NG (その pane は復元しても空になる)\n'
mk_snapshot snap_now.txt 3 0
mk_archive 3

# --- (4b) owner の起動時刻が記録と違う (pid 再利用) → NG --------------------------------
# ⚠️ ここが「pid<TAB>lstart 形式で書く唯一の理由」を守る検査。生きている pid を owner に
# しつつ lstart だけ食い違わせると、pid 生存だけを見る実装 (旧 cat|kill -0 / start を捨てる
# 実装) では「稼働中」に見えてしまう。2026-08-20 の red team が、この検査が無いと
# 「書き手が pid だけ書く退行」も「読み手が start を捨てる退行」も緑のまま通ることを実証した。
printf '%s\t%s\n' "$PERIODIC" "Thu Jan  1 00:00:00 1970" > "$TMP_DIR/ps/1.lock/pid"
health
[[ "$RC" -eq 1 ]] || { printf '✗ lstart 不一致 (pid 再利用) を稼働中と誤認した (RC=%s):\n' "$RC"; cat "$RUN_OUT"; exit 1; }
grep -q '周期保存 が居ない' "$RUN_OUT" \
  || { printf '✗ lstart 不一致が報告されない:\n'; cat "$RUN_OUT"; exit 1; }
printf '✓ owner の起動時刻が記録と違えば不在扱い (pid 再利用の照合が生きている)\n'
tt_lock_write_owner "$TMP_DIR/ps/1.lock" "$PERIODIC"   # 正常状態へ戻す
health
[[ "$RC" -eq 0 ]] || { printf '✗ owner を書き直しても回復しない (RC=%s)\n' "$RC"; cat "$RUN_OUT"; exit 1; }

# --- (4) 常駐プロセス不在 → NG ---------------------------------------------------------
kill "$PERIODIC" 2>/dev/null; sleep 0.3
health
[[ "$RC" -eq 1 ]] || { printf '✗ 周期保存不在で exit %s (1 のはず):\n' "$RC"; cat "$RUN_OUT"; exit 1; }
grep -q '周期保存 が居ない' "$RUN_OUT" || { printf '✗ 常駐プロセス不在が報告されない:\n'; cat "$RUN_OUT"; exit 1; }
printf '✓ 周期保存プロセスが居ないと NG\n'

# --- (6) 非 default socket → 判定対象外 ------------------------------------------------
SOCK_OVERRIDE="/nowhere/tmux-501/lab" health
[[ "$RC" -eq 0 && ! -s "$RUN_OUT" ]] || { printf '✗ 非 default socket で判定した (RC=%s):\n' "$RC"; cat "$RUN_OUT"; exit 1; }
printf '✓ 非 default socket (テストサーバ) は判定対象外で exit 0\n'

printf '\nAll snapshot-health tests passed successfully!\n'
