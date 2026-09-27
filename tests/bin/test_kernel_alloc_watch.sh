#!/usr/bin/env bash
# bin/kernel-alloc-watch のテスト (issue 500)。実 zprint は使わず、偽の zprint で在庫の値と書式を注入する。
# 現在時刻は KERNEL_ALLOC_WATCH_NOW で固定する (7 日の境界を壁時計に依存させない)。
# pin したいもの:
#   - record が zprint の inuse (7 列目) を記録する
#   - 7 日より古い行だけを落とす (境界ちょうどは残す / epoch の読めない行は消さない)
#   - zprint が失敗する・書式が変わったときは記録せずに失敗する (空や別の列を記録しない)
#   - 並行した record の行を落とさない (lockf の下で書き換える)
#   - claude のプロセス数と tmux のクライアント数を一緒に記録する。数えられなければ "-" (0 と区別) で、inuse は記録する
#   - check / snapshot の判定 (閾値・増え方は今回の起動以降の記録だけから出す・記録の幅が短いと増え方を出さない)
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN="$ROOT_DIR/bin/kernel-alloc-watch"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
# 既定の記録先も一時 dir の下へ向ける (KERNEL_ALLOC_WATCH_DIR を渡し忘れたケースが本物の ~/.cache に書かないように)
export XDG_CACHE_HOME="$TMP_DIR/xdg"
unset KERNEL_ALLOC_WATCH_DIR

fail=0
pass() { printf '✓ %s\n' "$1"; }
ng() { printf '✗ %s\n' "$1"; fail=1; }

# 偽 zprint: FAKE_INUSE を data.kalloc.1024 の inuse に出す。FAKE_MODE=fail で失敗、noinuse で見出しを変える。
# 実物の `zprint -L <名前>` は部分一致なので、名前の似た zone (前後に 1 行ずつ) も混ぜて完全一致を pin する
# 引数を渡されたら失敗する: 漏れているマシン (macOS 15.7.7) で実績があるのは引数なしの zprint だけ
cat > "$TMP_DIR/zprint" <<'EOF'
#!/bin/bash
[[ $# -eq 0 ]] || exit 4
[[ ${FAKE_MODE:-} == fail ]] && exit 3
h=inuse; [[ ${FAKE_MODE:-} == noinuse ]] && h=other
cat <<Z
                            elem         cur         max        cur         max         cur  alloc  alloc
zone name                   size        size        size      #elts       #elts       $h   size  count
-------------------------------------------------------------------------------------------------------------
kalloc.1024                 1024          0K          0K          0           0        111     0K      0
data.kalloc.1024            1024          0K          0K          0           0        ${FAKE_INUSE:-1234}     0K      0
data_shared.kalloc.1024     1024          0K          0K          0           0        222     0K      0
Z
EOF
chmod +x "$TMP_DIR/zprint"

# 偽 ps: claude が 3 つ (パス付き / 素の名前 / claude.exe)。claudette と node は数えない。FAKE_PS=fail で失敗
cat > "$TMP_DIR/ps" <<'EOF'
#!/bin/bash
[[ ${FAKE_PS:-} == fail ]] && exit 1
[[ "$*" == "-Axo comm=" ]] || exit 5
printf '%s\n' /Users/x/.local/bin/claude claude claude.exe /usr/bin/claudette node
EOF
# 偽 tmux: attach しているクライアントが 2 つ。FAKE_TMUX=fail でサーバ無し (rc=1)、
# hang で固まったサーバ (自分の pid を FAKE_MARK に書いて止まる)
cat > "$TMP_DIR/tmux" <<'EOF'
#!/bin/bash
[[ ${FAKE_TMUX:-} == fail ]] && { echo "no server running" >&2; exit 1; }
[[ ${FAKE_TMUX:-} == none ]] && exit 0   # attach しているクライアントが 0 (何も出さず rc=0)
[[ ${FAKE_TMUX:-} == hang ]] && { echo $$ > "$FAKE_MARK"; exec sleep 60; }
[[ $1 == list-clients ]] || exit 5
printf 'x\nx\n'
EOF
# 偽 sysctl: kern.boottime を実機 (macOS 27) と同じ書式で出す。FAKE_BOOT=fail で失敗
cat > "$TMP_DIR/sysctl" <<'EOF'
#!/bin/bash
[[ ${FAKE_BOOT:-} == fail ]] && exit 1
[[ "$*" == "-n kern.boottime" ]] || exit 5
printf '{ sec = %s, usec = 666590 } Fri Sep 18 10:18:16 2026\n' "${FAKE_BOOT:-1}"
exit "${FAKE_BOOT_RC:-0}"   # 値を出してから失敗する形も作れる
EOF
chmod +x "$TMP_DIR/ps" "$TMP_DIR/tmux" "$TMP_DIR/sysctl"

NOW=2000000000
DAY=86400
run() {  # $1=記録先 dir, 残り=kernel-alloc-watch の引数
  local d=$1; shift
  KERNEL_ALLOC_WATCH_DIR="$d" KERNEL_ALLOC_WATCH_ZPRINT="$TMP_DIR/zprint" KERNEL_ALLOC_WATCH_PS="$TMP_DIR/ps" KERNEL_ALLOC_WATCH_TMUX="$TMP_DIR/tmux" \
    KERNEL_ALLOC_WATCH_SYSCTL="$TMP_DIR/sysctl" KERNEL_ALLOC_WATCH_NOW="$NOW" FAKE_BOOT="${BOOT:-1}" "$BIN" "$@"
}

# 1. record が inuse を記録する
d="$TMP_DIR/t1"
if FAKE_INUSE=4321 run "$d" >/dev/null && [[ "$(cut -f1,3 "$d/log.tsv")" == "$NOW"$'\t'4321 ]]; then
  pass "record が inuse を epoch と一緒に記録する"
else
  ng "record の記録内容が違う: $(cat "$d/log.tsv" 2>/dev/null)"
fi

# 1a. inuse は数値に正規化して記録する (先頭の 0 が JSON の数値を壊さないように)
d="$TMP_DIR/t1a"
if FAKE_INUSE=0077 run "$d" >/dev/null && [[ "$(cut -f3 "$d/log.tsv")" == 77 ]]; then pass "inuse の先頭の 0 を落として記録する"; else ng "inuse の正規化: $(cat "$d/log.tsv" 2>/dev/null)"; fi

# 1b. claude の数と tmux のクライアント数を一緒に記録する。数えられなければ "-" で、inuse は記録する
for c in "ok|||3	2" "psfail|fail||-	2" "tmuxfail||fail|3	-" "tmux0||none|3	0"; do
  IFS='|' read -r name ps tm want <<<"$c"
  d="$TMP_DIR/t1b-$name"
  if FAKE_PS=$ps FAKE_TMUX=$tm FAKE_INUSE=77 run "$d" >/dev/null && [[ "$(cut -f3- "$d/log.tsv")" == "77	$want" ]]; then
    pass "$name: inuse と一緒に claude / tmux の数を記録する ($want)"
  else
    ng "$name: 記録内容が違う: [$(cut -f3- "$d/log.tsv" 2>/dev/null)] want=[77	$want]"
  fi
done

# 2. 7 日より古い行だけを落とす
d="$TMP_DIR/t2"; mkdir -p "$d"
printf '%s\told8d\t1\n%s\tedge\t2\n%s\tover\t3\n%s\tnew6d\t4\nbroken line\n\n' \
  $((NOW - 8 * DAY)) $((NOW - 7 * DAY)) $((NOW - 7 * DAY - 1)) $((NOW - 6 * DAY)) > "$d/log.tsv"
FAKE_INUSE=5 run "$d" >/dev/null
got=$(cut -f2 "$d/log.tsv" | tr '\n' '|')
want="edge|new6d|broken line||$(date -r "$NOW" '+%Y-%m-%d %H:%M:%S')|"
if [[ "$got" == "$want" ]]; then
  pass "7 日より古い行だけを落とす (境界ちょうど・読めない行・空行は残す)"
else
  ng "古い行の除去が違う: got=[$got] want=[$want]"
fi

# 3. zprint の失敗・書式の変化では記録しない
for mode in fail noinuse; do
  d="$TMP_DIR/t3-$mode"; mkdir -p "$d"
  printf '%s\tkeep\t9\n' "$NOW" > "$d/log.tsv"
  before=$(cat "$d/log.tsv")
  if FAKE_MODE=$mode run "$d" >/dev/null 2>"$d/err"; then
    ng "zprint の $mode で rc=0 を返した"
  elif [[ "$(cat "$d/log.tsv")" != "$before" ]]; then
    ng "zprint の $mode で記録が変わった"
  elif compgen -G "$d/.log.tsv.*" >/dev/null; then
    ng "zprint の $mode で一時ファイルが残った"
  else
    pass "zprint の $mode では記録せずに失敗する ($(head -1 "$d/err"))"
  fi
done

# 3b. ログを読めない・現在時刻が数字でないときは、既存の記録を残したまま失敗する
for mode in unreadable badnow; do
  d="$TMP_DIR/t3b-$mode"; mkdir -p "$d"
  printf '%s\tkeep1\t9\n%s\tkeep2\t10\n' $((NOW - 60)) "$NOW" > "$d/log.tsv"
  before=$(cat "$d/log.tsv")
  now=$NOW
  # badnow は算術評価でコマンドが走る形 (定義済みの変数の subscript。未定義だと set -u で先に止まる。数字の検査が無いと $((now - …)) が touch を実行する)
  case $mode in unreadable) chmod 000 "$d/log.tsv" ;; badnow) now="PPID[\$(touch $d/pwned)]" ;; esac
  rc=0; KERNEL_ALLOC_WATCH_DIR="$d" KERNEL_ALLOC_WATCH_ZPRINT="$TMP_DIR/zprint" KERNEL_ALLOC_WATCH_PS="$TMP_DIR/ps" KERNEL_ALLOC_WATCH_TMUX="$TMP_DIR/tmux" \
    KERNEL_ALLOC_WATCH_NOW="$now" "$BIN" >/dev/null 2>"$d/err" || rc=$?
  chmod 600 "$d/log.tsv"
  if [[ $rc -eq 0 ]]; then
    ng "$mode で rc=0 を返した"
  elif [[ "$(cat "$d/log.tsv")" != "$before" ]]; then
    ng "$mode で既存の記録が変わった: $(cat "$d/log.tsv")"
  elif compgen -G "$d/.log.tsv.*" >/dev/null; then
    ng "$mode で一時ファイルが残った"
  elif [[ $rc -ne 1 ]]; then
    ng "$mode の rc が 1 でない (${rc}。2 は使い方の誤りと区別できない)"
  elif [[ -e $d/pwned ]]; then
    ng "$mode で現在時刻の値がコマンドとして実行された"
  else
    pass "$mode では既存の記録を残して失敗する"
  fi
done

# 3c. ロックの中の段 (__record_locked) を計測の値なしで直接呼んでも、記録しない
d="$TMP_DIR/t3c"; mkdir -p "$d"
printf '%s\tkeep\t9\n' "$NOW" > "$d/log.tsv"
before=$(cat "$d/log.tsv")
if env -u KW_INUSE -u KW_CLAUDE -u KW_TMUX KERNEL_ALLOC_WATCH_DIR="$d" KERNEL_ALLOC_WATCH_NOW="$NOW" "$BIN" __record_locked >/dev/null 2>&1; then
  ng "計測の値なしの __record_locked が rc=0 を返した"
elif [[ "$(cat "$d/log.tsv")" != "$before" ]]; then
  ng "計測の値なしの __record_locked で記録が変わった"
else
  pass "計測の値なしの __record_locked は記録せずに失敗する"
fi

# 4. list: 記録が無ければ失敗、あれば前の行との差を出す
d="$TMP_DIR/t4"
if run "$d" list >/dev/null 2>&1; then ng "記録が無いのに list が rc=0"; else pass "記録が無ければ list は失敗する"; fi
FAKE_INUSE=100 run "$d" >/dev/null; FAKE_INUSE=250 run "$d" >/dev/null
out=$(run "$d" list)
last=$(tail -1 <<<"$out")
if [[ "$(awk '{print $3, $5, $6, $7}' <<<"$last")" == "250 +150 3 2" ]]; then
  pass "list が inuse と前の行との差と claude / tmux の数を出す"
else
  ng "list の出力が違う: [$last]"
fi
# 3 列しかない古い行は claude / tmux を "-" で出す
printf '%s\told\t5\n' "$NOW" > "$d/log.tsv"
last=$(run "$d" list | tail -1)
if [[ "$(awk '{print $2, $5, $6}' <<<"$last")" == "5 - -" ]]; then
  pass "3 列の古い行は claude / tmux を - で出す"
else
  ng "古い行の list が違う: [$last]"
fi

# 5. 並行した record の行を落とさない
d="$TMP_DIR/t5"
pids=()
for i in $(seq 20); do FAKE_INUSE=$i run "$d" >/dev/null & pids+=($!); done
rcs=0; for p in "${pids[@]}"; do wait "$p" || rcs=$((rcs + 1)); done
n=$(wc -l < "$d/log.tsv" | tr -d ' ')
if [[ $rcs -eq 0 && $n -eq 20 ]]; then
  pass "並行した record 20 本の行が全部残る"
else
  ng "並行した record: 失敗 $rcs 本 / 行 $n (期待 20)"
fi

# 7. snapshot の判定。記録を並べてから、今の在庫で snapshot を撮る。起動は 3 時間前 (7 日前の行を使うケースは 7 日前)
#    列: 名前 | 起動 (何秒前) | 置く行 ("何秒前:inuse" を空白区切り。inuse に abc などの壊れた値も置ける) | 今の inuse
#        | 期待 (verdict rate_per_hour rate_basis base_inuse)
jfield() { python3 -c 'import json,sys; d=json.loads(sys.stdin.read()); print(" ".join("null" if d[k] is None else str(d[k]) for k in sys.argv[1:]))' "$@"; }
while IFS='|' read -r name bootago rows cur want; do
  d="$TMP_DIR/t7-$name"; mkdir -p "$d"
  : > "$d/log.tsv"
  for r in $rows; do printf '%s\trow\t%s\n' $((NOW - ${r%%:*})) "${r#*:}" >> "$d/log.tsv"; done
  out=$(BOOT=$((NOW - bootago)) FAKE_INUSE=$cur run "$d" snapshot) || { ng "$name: snapshot が失敗した"; continue; }
  got=$(jfield verdict rate_per_hour rate_basis base_inuse <<<"$out" 2>&1)
  if [[ "$got" == "$want" ]]; then pass "snapshot の判定 $name: $got"; else ng "snapshot の判定 $name: got=[$got] want=[$want] ($out)"; fi
done <<'CASES'
fresh-ok|10800||1000|ok null null null
short-span|10800|599:30000|90000|watch null null null
span-edge|10800|600:30000|30100|watch 600 recent 30000
watch-slow|10800|3600:25000|26000|watch 1000 recent 25000
suspect|10800|3600:30000|40000|suspect 10000 recent 30000
suspect-edge|10800|3600:30000|35000|suspect 5000 recent 30000
below-rate|10800|3600:30000|34999|watch 4999 recent 30000
healthy-burst|10800|3600:1000|7000|ok 6000 recent 1000
leaking|10800||100000|leaking null null null
critical|10800|3600:14000000|15000000|critical 1000000 recent 14000000
ignore-preboot|10800|10860:5000000 3600:1000|1200|ok 200 recent 1000
unsorted|10800|1800:20000 3600:10000|30000|suspect 20000 recent 10000
ignore-future|10800|-600:1 3600:30000|40000|suspect 10000 recent 30000
ignore-broken|10800|7200:abc 3600:30000|40000|suspect 10000 recent 30000
recent-not-diluted|604800|518400:1200 3600:50000|60000|suspect 10000 recent 50000
before-window|604800|518400:1200|60000|watch 408 before_window 1200
gap-then-burst|604800|518400:1000 21660:1000 300:39000|40000|suspect 6482 before_window 1000
decrease|10800|3600:30000|29999|watch -1 recent 30000
CASES
BOOT=1

# 7b. check (人の形) は判定と、再起動の目安までの時間を日本語で出す
d="$TMP_DIR/t7b"; mkdir -p "$d"
printf '%s\tfirst\t30000\n' $((NOW - 3600)) > "$d/log.tsv"
out=$(BOOT=$((NOW - 7200)) FAKE_INUSE=40000 run "$d")
if [[ "$out" == *"判定: 漏れの疑い"* && "$out" == *"1 時間あたり +10,000 個 (直近 6 時間の最初の記録 30,000 個から、1.0 時間の平均)"* \
   && "$out" == *"再起動の目安 (15,000,000 個) まで: 約 62.3 日"* && "$out" != *"注意"* ]]; then
  pass "check が判定・増え方・再起動の目安を出す"
else
  ng "check の出力が違う: $out"
fi
# 残り 48 時間未満は時間で出す。正常なときは目安の行を出さない
printf '%s\tfirst\t14000000\n' $((NOW - 3600)) > "$d/log.tsv"
out=$(BOOT=$((NOW - 7200)) FAKE_INUSE=14500000 run "$d")
if [[ "$out" == *"まで: 約 1.0 時間"* ]]; then pass "残り 48 時間未満は時間で出す"; else ng "残りの時間の表示が違う: $out"; fi
printf '%s\tfirst\t1000\n' $((NOW - 3600)) > "$d/log.tsv"
out=$(BOOT=$((NOW - 7200)) FAKE_INUSE=1100 run "$d")
if [[ "$out" != *"再起動の目安"* ]]; then pass "正常なときは再起動の目安を出さない"; else ng "正常なのに再起動の目安を出した: $out"; fi
# 減ったときは符号と 3 桁区切りをそのまま出す (+- にしない)
printf '%s\tfirst\t300000\n' $((NOW - 3600)) > "$d/log.tsv"
out=$(BOOT=$((NOW - 7200)) FAKE_INUSE=50000 run "$d")
if [[ "$out" == *"1 時間あたり -250,000 個"* ]]; then pass "check は減った増え方を -250,000 と出す"; else ng "減ったときの表示が違う: $out"; fi
# 起動時刻が読めないときは、起動より前の記録も混ざる旨を出し、JSON の boot_epoch は null
out=$(FAKE_BOOT=fail FAKE_INUSE=50000 KERNEL_ALLOC_WATCH_DIR="$d" KERNEL_ALLOC_WATCH_ZPRINT="$TMP_DIR/zprint" KERNEL_ALLOC_WATCH_PS="$TMP_DIR/ps" \
  KERNEL_ALLOC_WATCH_TMUX="$TMP_DIR/tmux" KERNEL_ALLOC_WATCH_SYSCTL="$TMP_DIR/sysctl" KERNEL_ALLOC_WATCH_NOW="$NOW" "$BIN")
js=$(FAKE_BOOT=fail FAKE_INUSE=50000 KERNEL_ALLOC_WATCH_DIR="$d" KERNEL_ALLOC_WATCH_ZPRINT="$TMP_DIR/zprint" KERNEL_ALLOC_WATCH_PS="$TMP_DIR/ps" \
  KERNEL_ALLOC_WATCH_TMUX="$TMP_DIR/tmux" KERNEL_ALLOC_WATCH_SYSCTL="$TMP_DIR/sysctl" KERNEL_ALLOC_WATCH_NOW="$NOW" "$BIN" snapshot | jfield boot_epoch)
if [[ "$out" == *"注意: 起動時刻を読めなかった"* && "$js" == null ]]; then
  pass "起動時刻が読めないときは注意を出し、boot_epoch は null"
else
  ng "起動時刻が読めないときの出力が違う: [$js] $out"
fi

# 7c2. sysctl が値を出してから rc≠0 で終わっても、起動時刻は 1 つに読めて snapshot は壊れない
d="$TMP_DIR/t7c2"; mkdir -p "$d"
printf '%s\trow\t30000\n' $((NOW - 3600)) > "$d/log.tsv"
out=$(FAKE_BOOT_RC=1 BOOT=$((NOW - 7200)) FAKE_INUSE=40000 run "$d" snapshot 2>&1) && got=$(jfield boot_epoch verdict <<<"$out" 2>&1) || got="rc≠0: $out"
if [[ "$got" == "$((NOW - 7200)) suspect" ]]; then pass "sysctl が値の後に失敗しても起動時刻を 1 つに読む"; else ng "sysctl が値の後に失敗したとき: $got"; fi

# 7d. 終了コードはヘルプの記述どおり: ロックを開けない・記録を読めない list・読める行が無い list はどれも 1
d="$TMP_DIR/t7d"; mkdir -p "$d"; : > "$d/.lock"; chmod 000 "$d/.lock"
rc=0; run "$d" >/dev/null 2>&1 || rc=$?
chmod 600 "$d/.lock"
if [[ $rc -eq 1 ]]; then pass "ロックを開けないときは rc=1"; else ng "ロックを開けないときの rc=$rc"; fi
printf 'x\n' > "$d/log.tsv"
rc=0; run "$d" list >/dev/null 2>&1 || rc=$?
if [[ $rc -eq 1 ]]; then pass "読める行が無い list は rc=1"; else ng "読める行が無い list の rc=$rc"; fi
chmod 000 "$d/log.tsv"
rc=0; run "$d" list >/dev/null 2>&1 || rc=$?
chmod 600 "$d/log.tsv"
if [[ $rc -eq 1 ]]; then pass "記録を読めない list は rc=1"; else ng "記録を読めない list の rc=$rc"; fi

# 7c. --help は rc=0 で標準出力に、使い方の誤りは rc=2
if grep -q 'snapshot' <<<"$("$BIN" --help)"; then pass "--help が snapshot を案内する"; else ng "--help に snapshot が無い"; fi
rc=0; "$BIN" nosuch >/dev/null 2>&1 || rc=$?
if [[ $rc -eq 2 ]]; then pass "不明なサブコマンドは rc=2"; else ng "不明なサブコマンドの rc=$rc"; fi

# 9. destroy-all-logs: --yes 無しは何も消さない。--yes で log.tsv と一時ファイルだけを消し、ロックの印・ほかのファイル・dir は残す
d="$TMP_DIR/t9"; mkdir -p "$d"
printf '%s\trow\t1\n%s\trow\t2\n' $((NOW - 60)) "$NOW" > "$d/log.tsv"; : > "$d/.log.tsv.abc123"; : > "$d/.lock"; : > "$d/keep.txt"
rc=0; out=$(KERNEL_ALLOC_WATCH_DIR="$d" "$BIN" destroy-all-logs 2>&1) || rc=$?
if [[ $rc -eq 1 && -f "$d/log.tsv" && -f "$d/.log.tsv.abc123" && "$out" == *"log.tsv  (2 行)"* && "$out" == *".log.tsv.abc123"* ]]; then
  pass "destroy-all-logs は --yes 無しなら消すものを出すだけで rc=1"
else
  ng "destroy-all-logs (--yes 無し): rc=$rc out=[$out] $(ls -A "$d" | tr '\n' ' ')"
fi
rc=0; KERNEL_ALLOC_WATCH_DIR="$d" "$BIN" destroy-all-logs --yes >/dev/null 2>&1 || rc=$?
if [[ $rc -eq 0 && ! -e "$d/log.tsv" && ! -e "$d/.log.tsv.abc123" && -e "$d/.lock" && -e "$d/keep.txt" ]]; then
  pass "destroy-all-logs --yes は log.tsv と一時ファイルだけを消し、ロックの印とほかのファイルは残す"
else
  ng "destroy-all-logs --yes: rc=$rc 残り=[$(ls -A "$d" | tr '\n' ' ')]"
fi
rc=0; out=$(KERNEL_ALLOC_WATCH_DIR="$d" "$BIN" destroy-all-logs --yes 2>&1) || rc=$?
if [[ $rc -eq 0 && "$out" == *"消す記録は無い"* ]]; then pass "消す記録が無ければ rc=0"; else ng "消す記録が無いとき: rc=$rc [$out]"; fi
rc=0; KERNEL_ALLOC_WATCH_DIR="$d" "$BIN" destroy-all-logs --force >/dev/null 2>&1 || rc=$?
if [[ $rc -eq 2 ]]; then pass "destroy-all-logs に --yes 以外を渡すと rc=2"; else ng "destroy-all-logs --force の rc=$rc"; fi
# ロックを取ってから消す段は、対象が 0 件 (先に別の destroy が消した) でも成功を返す
rc=0; KW_DESTROY=1 KERNEL_ALLOC_WATCH_DIR="$d" "$BIN" __destroy_locked >/dev/null 2>&1 || rc=$?
if [[ $rc -eq 0 ]]; then pass "消す段は対象が 0 件でも rc=0"; else ng "消す段の対象が 0 件のときの rc=$rc"; fi
# 消す段を印なしで直接呼んでも、確認もロックも素通りしない
printf '%s\trow\t1\n' "$NOW" > "$d/log.tsv"
rc=0; env -u KW_DESTROY KERNEL_ALLOC_WATCH_DIR="$d" "$BIN" __destroy_locked >/dev/null 2>&1 || rc=$?
if [[ $rc -ne 0 && -f "$d/log.tsv" ]]; then pass "消す段は印なしでは動かない"; else ng "消す段を印なしで呼んだ: rc=$rc log=$([[ -f $d/log.tsv ]] && echo 残 || echo 消)"; fi
rm -f "$d/log.tsv"
# 改行を含む名前の一時ファイルがあっても、記録先の外 (cwd) のファイルを消さない
mkdir -p "$TMP_DIR/t9cwd"; : > "$TMP_DIR/t9cwd/important"; : > "$d/log.tsv"; : > "$d/.log.tsv.a
important"
rc=0; (cd "$TMP_DIR/t9cwd" && KERNEL_ALLOC_WATCH_DIR="$d" "$BIN" destroy-all-logs --yes >/dev/null 2>&1) || rc=$?
if [[ $rc -eq 0 && -f "$TMP_DIR/t9cwd/important" && ! -e "$d/log.tsv" && ! -e "$d/.log.tsv.a
important" ]]; then
  pass "改行を含む名前も 1 つのパスとして消し、記録先の外は消さない"
else
  ng "改行を含む名前: rc=$rc cwd=[$(ls -A "$TMP_DIR/t9cwd")] d=[$(ls -A "$d" | tr '\n' '|')]"
fi
# 同じ名前の dir は消さずに残し、ファイルだけを消して rc=0。`-` で始まる相対パスの記録先でも動く
mkdir -p "$TMP_DIR/t9rel/-q/.log.tsv.x"; : > "$TMP_DIR/t9rel/-q/log.tsv"; : > "$TMP_DIR/t9rel/-q/.log.tsv.y"
rc=0; (cd "$TMP_DIR/t9rel" && KERNEL_ALLOC_WATCH_DIR="-q" "$BIN" destroy-all-logs --yes >/dev/null 2>&1) || rc=$?
if [[ $rc -eq 0 && -d "$TMP_DIR/t9rel/-q/.log.tsv.x" && ! -e "$TMP_DIR/t9rel/-q/log.tsv" && ! -e "$TMP_DIR/t9rel/-q/.log.tsv.y" ]]; then
  pass "同じ名前の dir は残してファイルだけ消す (記録先が - で始まる相対パスでも)"
else
  ng "dir / - で始まる記録先: rc=$rc 残り=[$(ls -A "$TMP_DIR/t9rel/-q" 2>&1 | tr '\n' ' ')]"
fi
rc=0; (cd "$TMP_DIR/t9rel" && KERNEL_ALLOC_WATCH_DIR="-q" KERNEL_ALLOC_WATCH_ZPRINT="$TMP_DIR/zprint" KERNEL_ALLOC_WATCH_PS="$TMP_DIR/ps" \
  KERNEL_ALLOC_WATCH_TMUX="$TMP_DIR/tmux" KERNEL_ALLOC_WATCH_SYSCTL="$TMP_DIR/sysctl" KERNEL_ALLOC_WATCH_NOW="$NOW" "$BIN" >/dev/null 2>&1) || rc=$?
if [[ $rc -eq 0 && -s "$TMP_DIR/t9rel/-q/log.tsv" ]]; then pass "- で始まる相対パスの記録先にも記録できる"; else ng "- で始まる記録先への記録: rc=$rc"; fi
# 消すものが無ければ --yes 無しでも rc=0
rc=0; KERNEL_ALLOC_WATCH_DIR="$d" "$BIN" destroy-all-logs >/dev/null 2>&1 || rc=$?
if [[ $rc -eq 0 ]]; then pass "消すものが無ければ --yes 無しでも rc=0"; else ng "消すものが無いときの --yes 無しの rc=$rc"; fi
# ほかの記録がロックを握っている間は消さない (rc=75)
printf '%s\trow\t1\n' "$NOW" > "$d/log.tsv"
/usr/bin/lockf -k "$d/.lock" sleep 30 & holder=$!
for _ in $(seq 100); do /usr/bin/lockf -t 0 "$d/.lock" true 2>/dev/null || break; sleep 0.05; done
rc=0; KERNEL_ALLOC_WATCH_DIR="$d" "$BIN" destroy-all-logs --yes >/dev/null 2>&1 || rc=$?
pkill -P "$holder" 2>/dev/null || true; kill "$holder" 2>/dev/null || true; wait "$holder" 2>/dev/null || true   # lockf の子 (sleep) も止める
if [[ $rc -eq 75 && -f "$d/log.tsv" ]]; then pass "ロックを握られている間は消さずに rc=75"; else ng "ロックを握られている間: rc=$rc log=$([[ -f $d/log.tsv ]] && echo 残 || echo 消)"; fi

# 6. tmux が固まっても、ほかの record はロックで待たされずに記録できる (計測をロックの外で済ませている)
d="$TMP_DIR/t6"; mark="$TMP_DIR/t6.hang"
FAKE_TMUX=hang FAKE_MARK=$mark FAKE_INUSE=1 run "$d" >/dev/null 2>&1 & hung=$!
for _ in $(seq 200); do [[ -s $mark ]] && break; sleep 0.05; done
if [[ ! -s $mark ]]; then
  ng "固まる tmux が 10 秒たっても呼ばれない (判定できない)"
else
  rc=0; FAKE_INUSE=2 run "$d" >/dev/null 2>"$d.err" || rc=$?
  if [[ $rc -eq 0 && "$(cut -f3 "$d/log.tsv" 2>/dev/null)" == 2 ]]; then
    pass "tmux が固まっている間も、ほかの record は記録できる"
  else
    ng "tmux が固まっている間に record が失敗した: rc=$rc $(head -1 "$d.err")"
  fi
fi
[[ -s $mark ]] && kill "$(cat "$mark")" 2>/dev/null || true
wait "$hung" 2>/dev/null || true

exit "$fail"
