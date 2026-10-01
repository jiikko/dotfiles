#!/usr/bin/env bash
# scripts/run_make_targets_parallel.sh (test-lint の並列ランナー) の契約テスト。
#
# なぜ: 逐次版 run_all_targets が守っている契約 (issue 109 / 130) —「途中で失敗しても全部
# 走らせる」「失敗をまとめて返す」「対象 0 件は失敗」— は、並列化で最も壊れやすい。
# 壊れても**緑になる**方向 (失敗を取りこぼす / 出力を落とす) なので、ここで固定する。
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=tests/lib/wait_until.sh
. "$ROOT_DIR/tests/lib/wait_until.sh"
RUNNER="$ROOT_DIR/scripts/run_make_targets_parallel.sh"
[ -x "$RUNNER" ] || { printf '✗ ランナーが無い / 実行権限が無い: %s\n' "$RUNNER"; exit 1; }

# 配線: test-lint が実際にこのランナーを使っているか (使っていなければテストは何も守らない)
if grep -qE '^\t@\+?scripts/run_make_targets_parallel\.sh ' "$ROOT_DIR/Makefile"; then
  :
else
  printf '✗ Makefile の test-lint がこのランナーを使っていない (配線が外れている)\n'; exit 1
fi

fail=0
ok()  { printf '  ✓ %s\n' "$1"; }
bad() { printf '  ✗ %s\n' "$1"; fail=1; }

FIX=$(mktemp -d)
trap 'rm -rf "$FIX"' EXIT
# slow-ok は fast-fail が終わるまで終わらない (秒数ではなく順序で「失敗の後にまだ走っている」を作る)。
# 🚨 この順序を外さない: 最初の失敗で残りを止める (fail-fast の) 退行は、失敗より後に終わる target でしか見えない (issue 613 の敵対レビュー)
cat > "$FIX/Makefile" <<'MK'
slow-ok:
	@i=0; until [ -f fast-fail.done ] || [ $$i -ge 200 ]; do sleep 0.05; i=$$((i+1)); done; [ -f fast-fail.done ]; echo "OUT slow-ok"
fast-fail:
	@echo "OUT fast-fail"; echo "ERR fast-fail" >&2; touch fast-fail.done; exit 3
mid-ok:
	@echo "OUT mid-ok"
# 完了順が引数順と食い違うことを秒数ではなく順序で作る (出力順の検査を vacuous にしないため。issue 613):
# ord-late は ord-early が書き終えるまで終わらない。逐次に走ると ord-late が上限 (10 秒) まで待って落ちる
ord-late:
	@i=0; until [ -f ord-early.done ] || [ $$i -ge 200 ]; do sleep 0.05; i=$$((i+1)); done; [ -f ord-early.done ]; echo "OUT ord-late"
ord-early:
	@echo "OUT ord-early"; touch ord-early.done
also-fail:
	@echo "OUT also-fail"; exit 1
# ランデブー: 互いの開始マーカーを待つ。逐次実行だと先に走った方が相手を待ち続けて
# 上限で失敗する = 「並列に走っている」ことを壁時計でなく順序で pin する
rv-a:
	@touch rv-a.started; i=0; until [ -f rv-b.started ] || [ $$i -ge 200 ]; do sleep 0.05; i=$$((i+1)); done; [ -f rv-b.started ]
rv-b:
	@touch rv-b.started; i=0; until [ -f rv-a.started ] || [ $$i -ge 200 ]; do sleep 0.05; i=$$((i+1)); done; [ -f rv-a.started ]
MK

run() { ( cd "$FIX" && "$RUNNER" "$@" > "$FIX/out" 2> "$FIX/err" ); echo $?; }

printf 'Test 1: 先頭が落ちても後続を全部走らせる\n'
rm -f "$FIX/fast-fail.done"
rc=$(run fast-fail slow-ok mid-ok)
[ "$rc" = 1 ] && ok '非 0 で返る' || bad "非 0 で返るべきだが rc=$rc"
for t in slow-ok mid-ok; do
  grep -q "OUT $t" "$FIX/out" && ok "$t は走った" || bad "$t が走っていない (最初の失敗で隠れた)"
done

printf 'Test 2: 失敗したターゲットを全部まとめて報告する\n'
rm -f "$FIX/fast-fail.done"
rc=$(run fast-fail slow-ok also-fail)
[ "$rc" = 1 ] && ok '非 0 で返る' || bad "非 0 で返るべきだが rc=$rc"
line=$(grep '✗ 失敗したターゲット' "$FIX/err" || true)
case "$line" in
  *fast-fail*also-fail*) ok "失敗 2 件を両方報告 ($line)" ;;
  *) bad "失敗の集約が欠けている: '${line:-（行が無い）}'" ;;
esac

printf 'Test 3: 失敗の stdout と stderr を両方出す\n'
# 🚨 片方だけ出すと「err は空だから通った」と誤読する
#    (_claude/rules/verify-execution-not-just-exit-code.md の実例)
grep -q 'OUT fast-fail' "$FIX/out" && ok '失敗ターゲットの stdout が出ている' || bad '失敗ターゲットの stdout が捨てられている'
run fast-fail > /dev/null
grep -q 'ERR fast-fail' "$FIX/err" && ok '失敗ターゲットの stderr が出ている' || bad '失敗ターゲットの stderr が捨てられている'

printf 'Test 4: 出力は完了順でなく引数順\n'
rm -f "$FIX/ord-early.done"
run ord-late ord-early > /dev/null   # 完了は ord-early が先 (ゲート)、引数は ord-late が先
order=$(grep -o 'OUT [a-z-]*' "$FIX/out" | tr '\n' ' ')
[ "$order" = "OUT ord-late OUT ord-early " ] && ok "引数順で出ている ($order)" || bad "出力順が引数順でない: '$order'"

printf 'Test 4b: ターゲットは実際に並列に走る (逐次化の退行を検知)\n'
# 🚨 ここが無いと「集約の契約」は守れていても、& / wait を外して逐次に戻す退行が緑で通る
#    (敵対レビュー 2026-09-05 P3-1: 完全逐次化の変異で当時の 6 ケースが全部緑だった)
rm -f "$FIX/rv-a.started" "$FIX/rv-b.started"
rc=$(run rv-a rv-b)
[ "$rc" = 0 ] && ok '2 ターゲットが互いの開始を観測した (並列)' || bad "並列に走っていない (逐次化されている): rc=$rc"

printf 'Test 5: 対象 0 件は失敗\n'
rc=$( ( cd "$FIX" && "$RUNNER" > "$FIX/out" 2> "$FIX/err" ); echo $? )
[ "$rc" != 0 ] && ok "0 件で非 0 ($rc)" || bad '0 件が緑になっている (検査ゼロを成功にしている)'

printf 'Test 6: 終了コードを回収できないターゲットは失敗\n'
# make 相当を「自分の親 (バッファ用サブシェル) ごと殺す」偽物に差し替えて rc ファイルを消す。
# 判定不能を成功へ丸めると、kill された検査が緑で通る
cat > "$FIX/fake-make" <<'FM'
#!/bin/sh
kill -9 "$PPID" 2>/dev/null
sleep 5
FM
chmod +x "$FIX/fake-make"
rc=$( ( cd "$FIX" && MAKE="$FIX/fake-make" "$RUNNER" slow-ok > "$FIX/out" 2> "$FIX/err" ); echo $? )
[ "$rc" != 0 ] && ok "rc 欠落で非 0 ($rc)" || bad 'rc を回収できないのに緑になっている'
grep -q '終了コードを回収できなかった' "$FIX/err" && ok '理由を出している' || bad '理由が出ていない (沈黙で失敗している)'

printf 'Test 7: 中断 (SIGTERM) で子孫を残さない\n'
# 🚨 直接の子 (バッファ用サブシェル) だけを kill しても孫 (make -> shellcheck / go 等) は残り、
#    既に消えた outdir へ書き続ける。実測 2026-09-06: 修正前は偽 make 2 + 孫 4 = 6 プロセスが孤児。
#    判定は rc ではなく**残存プロセス数**で、上限つきポーリングで待つ
#    (_claude/rules/avoid-wall-clock-assertions.md)。
cat > "$FIX/fake-make-tree" <<'FMT'
#!/bin/sh
sh -c 'sleep 120' &      # 孫を起こす (make -> 子ツール の構図)
sleep 120
FMT
chmod +x "$FIX/fake-make-tree"
alive() { pgrep -f "$FIX/fake-make-tree" 2>/dev/null | wc -l | tr -d ' '; }
gone()  { [ "$(alive)" = 0 ]; }
# 🚨 exec で起こす: `( ... ) &` だとサブシェルの pid が返り、kill -TERM がランナー本体へ
#    届かない (このテストを書くときに実際に踏んだ)
( cd "$FIX" && MAKE="$FIX/fake-make-tree" exec "$RUNNER" a b >/dev/null 2>&1 ) &
runner_pid=$!
alive_ge2() { [ "$(alive)" -ge 2 ]; }
TT_WAIT_TICKS=200 TT_WAIT_TICK=0.05 tt_wait_until alive_ge2 || :
if [ "$(alive)" -lt 2 ]; then
  bad "偽 make が起動しない (前提が崩れている)"
else
  kill -TERM "$runner_pid" 2>/dev/null
  wait "$runner_pid" 2>/dev/null
  TT_WAIT_TICKS=200 TT_WAIT_TICK=0.05 tt_wait_until gone || :
  if gone; then ok '中断後に子孫が 0 (pgid ごと看取っている)'
  else bad "中断後に $(alive) プロセスが孤児として残った (issue 301)"; pkill -f "$FIX/fake-make-tree" 2>/dev/null; fi
fi

[ "$fail" -eq 0 ] || { printf '✗ 失敗あり\n'; exit 1; }
printf '✓ すべて成功\n'
