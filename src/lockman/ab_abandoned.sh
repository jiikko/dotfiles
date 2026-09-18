#!/bin/bash
# issue 362 の A-B 計測ハーネス (**手動実行**。make test からは走らない)。
#
# 何を測るか: 「--io-timeout で見捨てた goroutine が、失敗を報告した後に副作用を置いていくか」を
# 修正前後の**実バイナリ**で比べる。in-process の go test は goroutine が必ず完走しプロセスも
# 終了しないので、窓が縮んだかは原理的に測れない (src/lockman/abandoned_test.go 冒頭の 🚨)。
#
# 使い方:
#   ./ab_abandoned.sh <修正前の revision> [修正後の revision]   # 既定の修正後は HEAD
#   N=300 ./ab_abandoned.sh a75ffe3d 5804776d
#
# 🚨 **落とし穴 3 つ** (どれも実際に踏んだ。issue 362 の「実装後の A-B」節が正本):
#  1. `--io-timeout 1ms` は使えないし、使っても何も測れない。製品の下限は 100ms で CLI が弾く。
#     両腕へ同じ 1 行を当てて通しても、**タイムアウトが早すぎて goroutine が link() へ到達しない**
#     ので両腕とも漏れ 0 になる。下の sweep が有効な窓を毎回探すのはこのため
#  2. **有効な窓はマシン依存**。この開発機は 3〜6ms だった。値を固定して持ち歩かないこと
#  3. **素朴に数えると直っていない側が緑に見える**。引き継ぎ後に意図的に残る claim (②b) と、
#     成功パスで残る mark は**どちらも設計どおりで害ではない**。素朴に数えるとどちらも 150/150 出る
#
# 🚨 測っているのは「**一過性に遅い**ローカル FS」だけ。詰まり続けるマウントでは取り消し
#    (ReleaseTimed = 同じマウントへ 4 回 I/O) ごと固まるので、ここの数字は上限側の見積もり。
set -uo pipefail
BEFORE_REV="${1:?使い方: ./ab_abandoned.sh <修正前の revision> [修正後の revision]}"
AFTER_REV="${2:-HEAD}"
N="${N:-150}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/lockman-ab.XXXXXX")" || exit 1
trap 'rm -rf "$WORK"' EXIT
OLD=$(date -v-1H +%Y%m%d%H%M); OLD2=$(date -v-3H +%Y%m%d%H%M); NOW=$(date +%s)

# --- 両腕をビルド。計測用の 1 行 (minIOTimeout) は **両腕に同じものを** 当てる ---
for pair in "before:$BEFORE_REV" "after:$AFTER_REV"; do
  arm="${pair%%:*}"; rev="${pair#*:}"
  mkdir -p "$WORK/$arm"
  git -C "$ROOT" archive "$rev" src/lockman | tar -x -C "$WORK/$arm" || exit 1
  python3 - "$WORK/$arm/src/lockman/main.go" <<'PY' || exit 1
import io,sys
p=sys.argv[1]; s=io.open(p,encoding="utf-8").read()
old='\tminIOTimeout     = 100 * time.Millisecond'
if old not in s: sys.exit("minIOTimeout の行が見つからない (計測用の改変を当てられない)")
io.open(p,"w",encoding="utf-8").write(s.replace(old,'\tminIOTimeout     = 1 * time.Millisecond',1))
PY
  (cd "$WORK/$arm/src/lockman" && go build -o "$WORK/lockman-$arm" .) || exit 1
done
# canary: 両腕が「意図どおり違う」ことを確認する (同じものを 2 回測っていないか)
b=$(grep -c errAbandoned "$WORK/before/src/lockman/lock.go" "$WORK/before/src/lockman/timeout.go" | awk -F: '{s+=$2}END{print s}')
a=$(grep -c errAbandoned "$WORK/after/src/lockman/lock.go" "$WORK/after/src/lockman/timeout.go" | awk -F: '{s+=$2}END{print s}')
echo "canary: errAbandoned の出現 before=$b after=$a"
[ "$b" -eq 0 ] && [ "$a" -gt 0 ] || { echo "🚨 両腕が意図どおり違わない。revision を確認すること" >&2; exit 1; }

seed() { # $1=dir $2=stale lock を置くか $3=放棄された claim も置くか
  rm -rf "$1"; mkdir -p "$1/.lockman/graveyard"
  for g in $(seq 1 200); do : > "$1/.lockman/graveyard/g$g"; done
  touch -t "$OLD" "$1/.lockman/graveyard"/g* 2>/dev/null
  SEEDTOK=""
  [ "$2" = yes ] || return 0
  "$WORK/lockman-before" acquire "$1" --ttl 30s --io-timeout 5s --token-file "$WORK/seed.tok" >/dev/null 2>&1
  SEEDTOK=$(cat "$WORK/seed.tok" 2>/dev/null)
  touch -t "$OLD" "$1/.lockman/lock" 2>/dev/null
  [ "$3" = yes ] || return 0
  printf '{}' > "$1/.lockman/tmp/$SEEDTOK.takeover"; touch -t "$OLD2" "$1/.lockman/tmp/$SEEDTOK.takeover"
}

# --- 1) 有効な窓を探す (毎回やる。固定値を持ち歩かない) ---
echo; echo "=== sweep: 漏れが出る io-timeout を探す (修正前の腕, 60 試行) ==="
WINDOW=""
for to in 1ms 2ms 3ms 4ms 5ms 6ms 8ms 12ms 20ms; do
  f=0; l=0
  for _ in $(seq 1 60); do
    seed "$WORK/w" no no
    if ! "$WORK/lockman-before" acquire "$WORK/w" --io-timeout "$to" --ttl 30s --token-file "$WORK/t" >/dev/null 2>&1; then
      f=$((f+1)); [ -e "$WORK/w/.lockman/lock" ] && l=$((l+1))
    fi
  done
  printf '  %-5s rc!=0=%2d/60  ①=%2d/60\n' "$to" "$f" "$l"
  [ "$l" -gt 10 ] && WINDOW="$WINDOW $to"
done
[ -n "$WINDOW" ] || { echo "🚨 漏れが出る窓が見つからない。この環境ではこの A-B は何も測れない" >&2; exit 1; }
echo "  → 使う窓:$WINDOW"

# --- 2) 本計測 ---
echo; echo "=== 計測 (N=$N) ==="
# 🚨 **regime を 3 つに分ける**。放棄された claim を seed する regime (reclaim) は ③ を発火させる
# ためのものだが、**その claim 自身が ②a の条件 (claim 残 + stale lock 生存) を満たす**ので、
# 同じ regime で ②a を数えると自分の fixture を漏れとして数える (実際に踏んだ)。
#   empty   : 空 dir           → ① だけが出うる
#   stale   : 期限切れ lock    → ①②a②b (引き継ぎ経路)
#   reclaim : + 放棄された目印 → ③ (回収経路)。②a は数えない
printf '%-10s %-6s %-6s %6s %6s %6s %6s\n' regime io arm ① ②a ②b ③害
for regime in empty stale reclaim; do
 for to in $WINDOW; do
  for arm in before after; do
    c1=0; c2a=0; c2b=0; c3=0
    for _ in $(seq 1 "$N"); do
      case "$regime" in
        empty)   seed "$WORK/w" no  no  ;;
        stale)   seed "$WORK/w" yes no  ;;
        reclaim) seed "$WORK/w" yes yes ;;
      esac
      if "$WORK/lockman-$arm" acquire "$WORK/w" --io-timeout "$to" --ttl 30s --token-file "$WORK/t" >/dev/null 2>&1; then
        continue
      fi
      alive=no
      if [ -e "$WORK/w/.lockman/lock" ]; then
        if [ -n "$SEEDTOK" ] && grep -q "\"token\":\"$SEEDTOK\"" "$WORK/w/.lockman/lock" 2>/dev/null; then
          alive=yes            # seed の期限切れ lock がそのまま = 漏れではない
        else
          c1=$((c1+1))         # ① 見捨てた側が置いた lock
        fi
      fi
      # ②a その世代を塞ぐ / ②b 引き継ぎ済みで意図的に残る分 (害ではない)。
      # 🚨 reclaim regime では claim を自分で seed しているので ②a は数えない (自分の fixture を数える)
      if [ "$regime" != reclaim ] && compgen -G "$WORK/w/.lockman/tmp/*.takeover" >/dev/null 2>&1; then
        if [ "$alive" = yes ]; then c2a=$((c2a+1)); else c2b=$((c2b+1)); fi
      fi
      # ③ は「mark 残 かつ claim の打刻が戻っていない」ときだけ害 (戻っていれば別名が計算される)
      if compgen -G "$WORK/w/.lockman/tmp/*.takeover.*" >/dev/null 2>&1 && [ -n "$SEEDTOK" ]; then
        cl="$WORK/w/.lockman/tmp/$SEEDTOK.takeover"
        [ -e "$cl" ] && [ $((NOW - $(stat -f %m "$cl"))) -gt 3600 ] && c3=$((c3+1))
      fi
    done
    [ "$regime" = reclaim ] && { c2a="-"; c2b="-"; }
    [ "$regime" = empty ] && c3="-"
    printf '%-10s %-6s %-6s %6s %6s %6s %6s\n' "$regime" "$to" "$arm" "$c1" "$c2a" "$c2b" "$c3"
  done
 done
done
