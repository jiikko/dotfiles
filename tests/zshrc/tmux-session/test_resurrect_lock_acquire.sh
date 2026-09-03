#!/usr/bin/env bash
# scripts/lib/tmux_resurrect_guards.sh の lock 取得・掃除の契約テスト (issue 078)
#
# なぜ必要か: `tt_lock_acquire` / `tt_lock_sweep_stale` は 3 本のスクリプトに逐語コピーされて
# いた手順を集約したもので、**関数そのものを直接呼ぶテストが 1 本も無かった**。呼び出し元 3 本の
# 統合テスト経由で偶発的に踏まれているだけで、特に **rc=2 (取得に失敗した) はどの経路からも
# 到達しない** (敵対レビューの指摘、2026-08-25)。rc=2 を rc=0 に潰す変異が全 green で通る状態
# だったので、ここで 3 つの戻り値と掃除の条件を直接固定する。
#
# 契約: 0 = 取得した / 1 = 先任が同一プロセスとして生きている / 2 = 取得に失敗した
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
# shellcheck source=scripts/lib/tmux_resurrect_guards.sh
. "$ROOT_DIR/scripts/lib/tmux_resurrect_guards.sh"

fails=0
ok() { printf '✓ %s\n' "$1"; }
ng() { printf '✗ %s\n' "$1" >&2; fails=$(( fails + 1 )); }

BASE="$(mktemp -d)"
FAKE_PIDS=()
cleanup() {
  local p
  for p in ${FAKE_PIDS+"${FAKE_PIDS[@]}"}; do kill "$p" 2>/dev/null || true; done
  rm -rf "$BASE"
}
trap cleanup EXIT

# 補助プロセスは `( trap - EXIT; exec ... ) &` で起こす (素の `cmd &` は fork 直後に kill されると
# 子が EXIT trap を継承して cleanup の rm -rf をテスト途中で走らせる)。
spawn_live() { ( trap - EXIT; exec sleep 300 ) & FAKE_PIDS+=("$!"); REPLY_PID="$!"; }
free_pid() { local p="${1:-999999}"; while kill -0 "$p" 2>/dev/null; do p=$(( p - 1 )); done; REPLY_PID="$p"; }

rc_of() { local rc=0; tt_lock_acquire "$1" || rc=$?; printf '%s' "$rc"; }

printf '\n=== tt_lock_acquire の戻り値 ===\n\n'

# --- rc=0: 空きなら取得し、owner を記録する ---------------------------------------
d="$BASE/a.lock"
rc="$(rc_of "$d")"
[ "$rc" = 0 ] && ok "空きなら取得できる (rc=0)" || ng "空きなのに rc=$rc"
[ -s "$d/pid" ] && ok "取得時に owner を記録する" || ng "owner が記録されていない"
# 🚨 pid だけでなく起動時刻まで記録すること (pid 再利用で先任と誤認すると装置が張られない)
if [ "$(awk '{print NF}' "$d/pid" 2>/dev/null)" = 2 ]; then
  ok "owner は pid + 起動時刻の 2 列"
else
  # lstart が取れない環境では 1 列に縮退する。skip を沈黙させない
  printf '🚨 owner が 1 列 (ps -o lstart= が使えない環境と判断して skip)\n'
fi

# --- rc=1: 先任が生きているなら奪わない -------------------------------------------
d="$BASE/b.lock"; mkdir "$d"
spawn_live; tt_lock_write_owner "$d" "$REPLY_PID"
rc="$(rc_of "$d")"
[ "$rc" = 1 ] && ok "先任が生きているなら奪わない (rc=1)" || ng "生存 owner の lock を rc=$rc で扱った"
[ -s "$d/pid" ] && ok "rc=1 のとき owner を上書きしない" || ng "rc=1 なのに owner を書き換えた"

# --- rc=0 (奪う): owner 不在の取り残しは回収する ----------------------------------
# 🚨 ここが緩むと、異常終了で残った lock が永久に残り **その経路が二度と走らない**
OLD_STAMP="$(date -v-1H '+%Y%m%d%H%M' 2>/dev/null || date -d '1 hour ago' '+%Y%m%d%H%M')"
d="$BASE/c.lock"; mkdir "$d"; touch -t "$OLD_STAMP" "$d"
rc="$(rc_of "$d")"
[ "$rc" = 0 ] && ok "owner 不在の取り残しは奪って続行する (rc=0)" || ng "取り残しを rc=$rc で扱った"

# --- rc=1: 作りたての owner 不在 lock は奪わない (記録中かもしれない) --------------
# 🚨 `mkdir` と owner 記録は原子的でないので、正当な取得者が記録中の一瞬は owner 不在に見える。
#   ここを奪うと**両方が rc=0 になる** (issue 103 の原症状)。古さで区別する。
d="$BASE/c2.lock"; mkdir "$d"
rc="$(rc_of "$d")"
[ "$rc" = 1 ] && ok "作りたての owner 不在 lock は奪わない (rc=1)" || ng "owner 記録中の lock を rc=$rc で奪った"

# --- rc=0 (奪う): owner が死んだ pid でも回収する ---------------------------------
d="$BASE/d.lock"; mkdir "$d"; free_pid; printf '%s\n' "$REPLY_PID" > "$d/pid"
rc="$(rc_of "$d")"
[ "$rc" = 0 ] && ok "死んだ owner の lock は奪う (rc=0)" || ng "死んだ owner の lock を rc=$rc で扱った"

# --- rc=2: mkdir が通らないなら「取れなかった」を返す ------------------------------
# 🚨 rc=2 を rc=0 に潰すと、呼び出し側は「取れた」と誤認して lock 無しで本処理へ進む
if [ "$(id -u)" = 0 ]; then
  printf '🚨 root では書き込み不可ディレクトリを作れないため rc=2 のテストを skip した\n'
else
  ro="$BASE/ro"; mkdir -p "$ro"; chmod 500 "$ro"
  rc="$(rc_of "$ro/x.lock")"
  chmod 700 "$ro"
  [ "$rc" = 2 ] && ok "取得に失敗したら rc=2" || ng "mkdir 不能なのに rc=$rc"
fi

printf '\n=== tt_lock_sweep_stale の掃除条件 ===\n\n'

S="$BASE/sweep"; mkdir -p "$S"

# 死んだサーバ + 死んだ owner → 掃除する
free_pid; dead_srv="$REPLY_PID"
mkdir "$S/$dead_srv.lock"; free_pid $(( dead_srv - 1 )); printf '%s\n' "$REPLY_PID" > "$S/$dead_srv.lock/pid"
# 生きているサーバ → 残す
spawn_live; live_srv="$REPLY_PID"; mkdir "$S/$live_srv.lock"
printf '%s\n' "$live_srv" > "$S/$live_srv.lock/pid"
# 死んだサーバ + 生きている owner → 残す (保存の実行中にサーバだけ落ちた形)
free_pid $(( dead_srv - 2 )); dead_srv2="$REPLY_PID"
mkdir "$S/$dead_srv2.lock"; spawn_live; tt_lock_write_owner "$S/$dead_srv2.lock" "$REPLY_PID"
# 生きているサーバ + 死んだ owner → 残す (掃除の条件は「両方死んでいる」の連言。
# 🚨 ここを pin しないと、条件から `kill -0 "$spid"` を落とす変異が green のまま通る)
spawn_live; live_srv2="$REPLY_PID"; mkdir "$S/$live_srv2.lock"
free_pid $(( dead_srv - 3 )); printf '%s\n' "$REPLY_PID" > "$S/$live_srv2.lock/pid"
# ディレクトリでない `*.lock` は触らない (`[ -d "$d" ] || continue` を pin する)
: > "$S/notadir.lock"

tt_lock_sweep_stale "$S"

[ -d "$S/$dead_srv.lock" ]  && ng "死んだサーバの stale lock が残っている" || ok "死んだサーバ + 死んだ owner は掃除する"
[ -d "$S/$live_srv.lock" ]  && ok "生きているサーバの lock は残す" || ng "生きているサーバの lock を消した"
[ -d "$S/$dead_srv2.lock" ] && ok "owner が生きている lock は残す (実行中を奪わない)" || ng "実行中の owner の lock を消した"
[ -d "$S/$live_srv2.lock" ] && ok "サーバが生きていれば owner が死んでいても残す" || ng "サーバ生存中の lock を消した"
[ -f "$S/notadir.lock" ] && ok "ディレクトリでない *.lock は触らない" || ng "ディレクトリでない *.lock を消した"

# --- 空ディレクトリで落ちない (nullglob 無しのリテラル *.lock を掴む) --------------
E="$BASE/empty"; mkdir -p "$E"
if tt_lock_sweep_stale "$E"; then ok "対象 0 件でも落ちない"; else ng "対象 0 件で失敗した"; fi
[ -d "$E" ] && ok "対象 0 件で親ディレクトリを消さない" || ng "対象 0 件で親を消した"


printf '\n=== 取り残し lock の奪取レース (issue 103) ===\n\n'

# 🚨 別プロセスで起こすこと。同一シェルの subshell だと `$$` が同じになり、
#   tt_lock_write_owner が同じ owner を書くので「2 プロセスが競る」形にならない。
race_worker() { # $1=guards.sh $2=lock dir $3=結果ファイル $4=窓を広げるか(0/1) $5=解放マーカー
  bash -c '
    . "$1" || exit 9
    # $4=1 のとき tt_proc_starttime を遅くして、mkdir と owner 記録の間の窓を広げる。
    # 窓は実測 4.6ms しかなく、素で回すとレースが「たまたま出ない」ので確率的にしか
    # 落とせない。ここを広げると**決定論的に**再現でき、変異検証の当たりも安定する。
    if [ "$4" = 1 ]; then tt_proc_starttime() { sleep 0.4; printf "WIDE_%s\n" "$1"; }; fi
    rc=0
    tt_lock_acquire "$2" || rc=$?
    printf "%s\n" "$rc" >> "$3"
    # 🚨 取得したら**親が解放を許すまで保持する**。すぐ終了すると owner が死んだ lock が残り、
    #    後続は「記録済みで死んだ owner」を**正当に**奪える。その再取得を「二重取得」と数えると
    #    機械の速さ次第で落ちる (実測 2026-08-28: CI (macOS) で 形状=leftover delay=0.005s が
    #    2 winners)。固定 sleep で凌ぐと「どれだけ待てば十分か」が未実測のマジックナンバーに
    #    なるので、マーカー待ちにして定数を消す (敵対的レビューの指摘 R1)。
    i=0
    while [ ! -e "$5" ] && [ "$i" -lt 200 ]; do sleep 0.05; i=$(( i + 1 )); done
  ' _ "$1" "$2" "$3" "$4" "$5" &
}

# 2 プロセスを delay 秒ずらして走らせ、取得に成功した数 (rc=0) を返す。**1 でなければならない。**
#
# 🚨 $2 の「形状」を必ず両方掃くこと。`leftover` (取り残しを事前に作る) だけを試すと**両者が
#   奪取経路に入る**ので、奪取権 lock で直列化されている経路しか検査しない。production の
#   呼び出し元 3 本はどれも **lock dir が存在しない状態から始まる** (`fresh`) ため、片方が素の
#   `mkdir` で入り、奪取権 lock を一切持たないまま owner を記録する — そこが issue 103 の
#   原症状の窓だった。leftover だけ緑にして「閉じた」と誤判定した実例がある (2026-08-28)。
race_winners() { # $1=delay $2=形状(leftover|fresh) $3=窓を広げるか(0/1)
  local dir="$BASE/race.lock" out="$BASE/race.out" rel="$BASE/race.release" g p1 p2 i
  g="$ROOT_DIR/scripts/lib/tmux_resurrect_guards.sh"
  rm -rf "$dir" "$dir.steal" "$out" "$rel"
  # 🚨 取り残しは**古く**すること。作りたての owner 不在 dir は「今まさに owner を記録中」と
  #   区別できないので奪えないのが正しい (それが production 形状の二重取得を止めている)。
  #   ここを `mkdir` だけにすると本物の取り残しではなくなり、勝者 0 で落ちる。
  if [ "$2" = leftover ]; then mkdir "$dir"; touch -t "$OLD_STAMP" "$dir"; fi
  # owner を記録して死んだ形 (クラッシュした先任)。記録済みなので TTL 待ちなしで奪える
  if [ "$2" = deadowner ]; then
    mkdir "$dir"; spawn_live; tt_lock_write_owner "$dir" "$REPLY_PID"
    kill "$REPLY_PID" 2>/dev/null || true; wait "$REPLY_PID" 2>/dev/null || true
  fi
  : > "$out"
  race_worker "$g" "$dir" "$out" "$3" "$rel"; p1=$!
  sleep "$1"
  race_worker "$g" "$dir" "$out" "$3" "$rel"; p2=$!
  # 2 本の結果が出揃ってから解放を許す。これで「片方が保持している間に、もう片方も取得できたか」
  # だけを見る形になる (出揃わない場合は下の harness: 分類が拾う)
  i=0
  while [ "$(grep -c . "$out" 2>/dev/null || true)" -lt 2 ] && [ "$i" -lt 200 ]; do sleep 0.05; i=$(( i + 1 )); done
  : > "$rel"
  # 🚨 素の `wait` は使わない。このファイルは前段で補助プロセスを起こしており、
  #   bare wait がそれらを拾って「子ではない」警告を出す
  wait "$p1" 2>/dev/null || true
  wait "$p2" 2>/dev/null || true
  # 🚨 「ハーネスが壊れた」を「レースが壊れた」と混ぜないこと。負荷が高いと worker の起動自体が
  #   失敗しうるが、それは**判定が存在しない**のであって「取得が 2 つ」ではない
  #   (adversarial-review-own-safeguards の「判定不能は allow/deny のどちらにも丸めない」)。
  local got
  got="$(grep -c . "$out" 2>/dev/null || true)"; got="${got:-0}"
  if [ "$got" != 2 ]; then
    printf 'harness:%s\n' "$got"
    return 0
  fi
  # 🚨 `grep -c ... || echo 0` にしないこと。grep -c は無マッチでも "0" を出した上で rc=1 を
  #   返すので、両方が出力されて "0\n0" になり失敗メッセージが壊れる (実測 2026-08-28)。
  local won
  won="$(grep -c '^0$' "$out" 2>/dev/null || true)"
  printf '%s\n' "${won:-0}"
}

# 敵対的レビューの実測では 1〜5ms がクリティカルウィンドウ (0s と 8ms 以上では出ない)。
# その帯を含めて掃く。
race_bad=0
race_trials=0
for shape in leftover deadowner; do
for delay in 0 0.001 0.002 0.003 0.005; do
  for _ in 1 2 3; do
    n="$(race_winners "$delay" "$shape" 0)"
    race_trials=$(( race_trials + 1 ))
    case "$n" in
      1) ;;
      harness:*)
        # 判定不能。レースの結論には使わないが、黙って緑にもしない
        printf '🚨 delay=%ss でハーネスが結果を %s 件しか残せなかった (負荷?)。この試行は判定不能\n' \
          "$delay" "${n#harness:}"
        ;;
      *)
        ng "形状=$shape delay=${delay}s で取得に成功したのが $n プロセス (1 でなければならない)"
        race_bad=$(( race_bad + 1 ))
        break 3
        ;;
    esac
  done
done
done
if [ "$race_bad" -eq 0 ]; then
  ok "取り残し (owner 不在 / 死んだ owner) を 2 プロセスが同時に奪いに来ても取得は常に 1 つ ($race_trials 回)"
fi

# --- production 形状 (lock 不在から 2 プロセス) を窓を広げて決定論的に検査する ----
# 🚨 ここが本ファイルで最も重要。素の窓 (実測 4.6ms) だと二重取得は 2/30 程度でしか出ず、
#   確率的にしか落とせない。tt_proc_starttime を 0.4s 遅らせると **ガードが無い実装では
#   15/15 で二重取得**し、あると 0/15 になる (実測 2026-08-28)。
fresh_bad=0
fresh_trials=0
for _ in 1 2 3 4 5; do
  n="$(race_winners 0.05 fresh 1)"
  fresh_trials=$(( fresh_trials + 1 ))
  case "$n" in
    1) ;;
    harness:*) printf '🚨 production 形状のハーネスが結果を %s 件しか残せなかった。この試行は判定不能\n' "${n#harness:}" ;;
    *) ng "lock 不在から 2 プロセスが来たとき $n プロセスが取得した (owner 記録中の lock を奪っている)"
       fresh_bad=$(( fresh_bad + 1 )); break ;;
  esac
done
if [ "$fresh_bad" -eq 0 ]; then
  ok "owner 記録中 (mkdir 直後) の lock は奪われない ($fresh_trials 回・窓を 0.4s に拡大)"
fi

# 奪取権 lock が取り残されたら TTL で回収する (回収しないとこの経路が二度と走らない)
stale_dir="$BASE/ttl.lock"
rm -rf "$stale_dir" "$stale_dir.steal"
mkdir "$stale_dir" "$stale_dir.steal"
# mtime を 1 時間前にして TTL 超過を作る。**本体も**古くすること: 本体が新しいと
# 「owner を記録中かもしれない」ガードが手前で効いて、.steal の TTL 回収まで到達しない
touch -t "$OLD_STAMP" "$stale_dir" "$stale_dir.steal"
rc="$(rc_of "$stale_dir")"
[ "$rc" = 0 ] && ok "奪取権 lock の取り残しは TTL で回収して奪える (rc=0)" || ng "TTL 超過の奪取権 lock を回収できない (rc=$rc)"

# TTL 内の奪取権 lock は尊重する (奪取中の相手を蹴散らさない)
fresh_dir="$BASE/fresh.lock"
rm -rf "$fresh_dir" "$fresh_dir.steal"
mkdir "$fresh_dir" "$fresh_dir.steal"
rc="$(rc_of "$fresh_dir")"
[ "$rc" = 1 ] && ok "奪取中 (TTL 内の奪取権 lock) には譲る (rc=1)" || ng "奪取中の相手を蹴散らした (rc=$rc)"

# 🚨 ヘルパーを直接 pin する。acquire からの経路では手前に `[ -d "$steal" ] || return 2` が
#   あるため冗長になり、ガードを外しても acquire のテストは通る (実測)。だが汎用ヘルパーなので、
#   存在しない dir を「古い」と答えると次の呼び出し側が「作れなかった」を「取り残し」と誤判定する。
tt_lock_dir_older_than "$BASE/nosuch-dir" 1 \
  && ng "存在しない dir を「古い」と答えた" \
  || ok "存在しない dir は「古い」と答えない"

# --- 記録済みで死んだ owner は「即」奪える (TTL 待ちで回復を遅らせない) ----------
# 🚨 上のガード (owner 未記録なら TTL まで待つ) を「owner が生きていないなら待つ」に広げると、
#   クラッシュした先任の lock が TTL の間ずっと奪えなくなり、復元・保存の再開が遅れる。
#   区別の根拠は owner ファイルの**有無**であって生死ではない。
dead_dir="$BASE/deadowner.lock"
rm -rf "$dead_dir" "$dead_dir.steal"; mkdir "$dead_dir"
spawn_live; dead_pid="$REPLY_PID"; tt_lock_write_owner "$dead_dir" "$dead_pid"
kill "$dead_pid" 2>/dev/null || true; wait "$dead_pid" 2>/dev/null || true
rc="$(rc_of "$dead_dir")"
[ "$rc" = 0 ] && ok "記録済みで死んだ owner の lock は待たずに奪える (rc=0)" || ng "死んだ owner の lock を奪えない (rc=$rc)"

# --- 未来 mtime の取り残しを回収できる ------------------------------------------
# 🚨 `find -mmin -N` は**未来 mtime にもマッチする**ため「新しい」と誤判定し、NTP の
#   ステップバック / スリープ復帰 / VM の suspend-resume 後に取り残しが**永久に回収されず**、
#   呼び出し側は無音で退くので保存も watchdog も二度と張られない。
future_stamp="$(date -v+3y '+%Y%m%d%H%M' 2>/dev/null || date -d '3 years' '+%Y%m%d%H%M')"
fut="$BASE/future.lock"
rm -rf "$fut" "$fut.steal"; mkdir "$fut"; touch -t "$future_stamp" "$fut"
rc="$(rc_of "$fut")"
[ "$rc" = 0 ] && ok "未来 mtime の取り残し lock を回収して奪える (rc=0)" || ng "未来 mtime の取り残しを回収できない (rc=$rc)"
rm -rf "$fut" "$fut.steal"; mkdir "$fut" "$fut.steal"; touch -t "$future_stamp" "$fut" "$fut.steal"
rc="$(rc_of "$fut")"
[ "$rc" = 0 ] && ok "未来 mtime の奪取権 lock も回収する (rc=0)" || ng "未来 mtime の .steal で永久ロックアウトする (rc=$rc)"

# --- mtime が読めないときは rc=2 (無音の「先任がいる」に丸めない) -----------------
# 🚨 判定不能を rc=1 に丸めると、呼び出し元 3 本はどれも無音 exit 0 なので**装置が張られない
#   ことがログに 1 行も出ない**。判定不能は 0/1 のどちらでもない第 3 の結果として返す。
unk="$BASE/unknown.lock"
rm -rf "$unk" "$unk.steal"; mkdir "$unk"
stat_shim="$BASE/statshim"; mkdir -p "$stat_shim"
printf '#!/bin/sh\nexit 127\n' > "$stat_shim/stat"; chmod +x "$stat_shim/stat"
rc=0; ( PATH="$stat_shim:$PATH"; tt_lock_acquire "$unk" ) || rc=$?
[ "$rc" = 2 ] && ok "mtime を読めないときは rc=2 (判定不能を無音にしない)" || ng "mtime 判定不能を rc=$rc に丸めた"

# 奪取権 lock 側の年齢が読めない経路も同じ扱いにする (owner 記録済みで死んだ形から入る)
unk2="$BASE/unknown2.lock"
rm -rf "$unk2" "$unk2.steal"; mkdir "$unk2" "$unk2.steal"
spawn_live; tt_lock_write_owner "$unk2" "$REPLY_PID"
kill "$REPLY_PID" 2>/dev/null || true; wait "$REPLY_PID" 2>/dev/null || true
rc=0; ( PATH="$stat_shim:$PATH"; tt_lock_acquire "$unk2" ) || rc=$?
[ "$rc" = 2 ] && ok "奪取権 lock の mtime を読めないときも rc=2" || ng ".steal の判定不能を rc=$rc に丸めた"

# --- .steal の孤児は sweep で回収する (上限なく溜まらない) -----------------------
sw="$BASE/sweep-steal"; rm -rf "$sw"; mkdir -p "$sw/999999.lock" "$sw/999999.lock.steal" "$sw/888888.lock.steal" "$sw/777777.lock.steal"
touch -t 202001010000 "$sw/888888.lock.steal"
tt_lock_sweep_stale "$sw"
[ -d "$sw/999999.lock.steal" ] && ng "本体を消した lock の .steal が残った" || ok "掃除した lock の .steal も道連れにする"
[ -d "$sw/888888.lock.steal" ] && ng "TTL 超過の .steal 孤児が残った" || ok "TTL 超過の .steal 孤児を回収する"
[ -d "$sw/777777.lock.steal" ] && ok "新しい .steal (奪取中かもしれない) は消さない" || ng "奪取中かもしれない .steal を消した"

# --- stat の GNU/BSD 方言差で mtime が壊れないこと ------------------------------
# 🚨 順序が命。`stat -f %m "$x" || stat -c %Y "$x"` (BSD 優先) と書くと **Linux で壊れる**。
#   GNU stat の `-f` は書式指定ではなく「ファイルシステム情報の表示」なので `%m` はファイル名
#   として扱われ、`%m` が無いエラーで rc=1 になる一方 **"$x" 側の fs 情報は stdout に出す**。
#   コマンド置換がその複数行を拾い、フォールバックの epoch と連結されて数値でなくなる。
#   GNU に無い `-c` を先に試せば、BSD では invalid option で**何も出さずに**失敗して `-f` へ落ちる。
#   実害 (実測 2026-08-28): 取り残しの回収が Linux でだけ全滅し (run 33136841310)、
#   schedkeys の prune も Linux で一切走らなかった (run 33138075381)。
#   手元 (macOS) は BSD stat なので両方の順序で緑になる。**両方の方言をここで作る**。
stat_dialect_dir="$BASE/statdialect"; mkdir -p "$stat_dialect_dir"
dialect_target="$BASE/dialect-target"; mkdir -p "$dialect_target"
dialect_epoch="$(tt_mtime_of "$dialect_target")"
write_stat_shim() { # $1=方言 (bsd|gnu)
  cat > "$stat_dialect_dir/stat" <<SHIM
#!/bin/sh
# $1 方言の stat を模す。epoch は固定値 ($dialect_epoch)
case "\$1:$1" in
  -c:gnu) printf '%s\n' "$dialect_epoch" ;;
  -c:bsd) echo "stat: illegal option -- c" >&2; exit 1 ;;
  -f:gnu) printf '  File: "%s"\n' "\$3"; printf 'Block size: 4096\n'
          printf "stat: cannot read file system information for '%%m'\n" >&2; exit 1 ;;
  -f:bsd) printf '%s\n' "$dialect_epoch" ;;
  *) exit 64 ;;
esac
SHIM
  chmod +x "$stat_dialect_dir/stat"
}
for dialect in bsd gnu; do
  write_stat_shim "$dialect"
  got="$(PATH="$stat_dialect_dir:$PATH" tt_mtime_of "$dialect_target" || true)"
  [ "$got" = "$dialect_epoch" ] \
    && ok "$dialect 方言の stat でも mtime を epoch 1 行で読める" \
    || ng "$dialect 方言の stat で mtime が壊れる (得た値: [$(printf '%s' "$got" | tr '\n' '|')])"
done
# 年齢判定まで通ること (helper だけ緑で呼び出し側が壊れる形を作らない)
touch -t "$OLD_STAMP" "$dialect_target"
dialect_epoch="$(tt_mtime_of "$dialect_target")"
for dialect in bsd gnu; do
  write_stat_shim "$dialect"
  rc=0; ( PATH="$stat_dialect_dir:$PATH"; tt_lock_dir_older_than "$dialect_target" 30 ) || rc=$?
  [ "$rc" = 0 ] \
    && ok "$dialect 方言の stat でも TTL 超過を「古い」と判定する" \
    || ng "$dialect 方言の stat で年齢判定が rc=$rc (取り残しを回収できない)"
done

printf '\n=== 条件付き解放 (issue 103) ===\n\n'

# 自分が owner なら解放する
rel="$BASE/rel.lock"
rm -rf "$rel"; mkdir "$rel"; tt_lock_write_owner "$rel"
tt_lock_release_if_owner "$rel"
[ -d "$rel" ] && ng "自分の lock を解放できない" || ok "自分が owner なら解放する"

# 🚨 **他人の lock は消さない**。奪取のレース中は 2 プロセスが同じパスを保持しうるので、
#   先に終わった側が、まだ走っている側の lock を消すと多重実行になる (issue 103)。
rm -rf "$rel"; mkdir "$rel"
spawn_live; tt_lock_write_owner "$rel" "$REPLY_PID"
tt_lock_release_if_owner "$rel"
[ -d "$rel" ] && ok "他人が owner の lock は消さない (多重実行を招かない)" || ng "他人の lock を消した"

# owner の記録が無い lock (mkdir 直後に死んだ形) も消さない
rm -rf "$rel"; mkdir "$rel"
tt_lock_release_if_owner "$rel"
[ -d "$rel" ] && ok "owner 未記録の lock は消さない (取り残しは acquire 側が回収する)" || ng "owner 未記録の lock を消した"

# 🚨 起動時刻が**記録時は取れて解放時に取れない**環境 (ps が壊れた/権限が変わった) では、
#   生文字列比較だと必ず外れて自分の lock を解放できず取り残す。判定材料が欠けたときは
#   pid 一致だけで自分とみなす (tt_same_proc の fail-open と揃える)。
rm -rf "$rel"; mkdir "$rel"; tt_lock_write_owner "$rel"
( tt_proc_starttime() { printf ''; }; tt_lock_release_if_owner "$rel" )
[ -d "$rel" ] && ng "解放時に起動時刻が取れないと自分の lock を取り残す" || ok "解放時に起動時刻が取れなくても自分の lock は解放する"

printf '\n'
if (( fails > 0 )); then
  printf '[test-lock-acquire] %d 件失敗\n' "$fails" >&2
  exit 1
fi
printf '[test-lock-acquire] すべて成功\n'
