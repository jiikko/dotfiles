# 995 (perf): treefiler のカーソル 1 つの移動がもっさりする

起票日: 2026-10-30

## 概要

bin/treefiler でカーソルを 1 つ動かすと若干もっさりする (ユーザー報告 2026-10-30)。実物のバイナリでの所要をログに出す仕組みと、
Claude が隔離 tmux から打鍵して同じログを取る台本を作り、それで測って直す。

## 詳細

- 測り方: `make -C src/treefiler perf` (tools/tfperf)。実物の bin/treefiler を run ごとの `-L` の tmux で起こし、打鍵を 1 つずつ送って、
  `TREEFILER_PERFLOG` のログ (perflog。Update・View・端末への書き込みの 1 行ずつ) と送った時刻を組にする。測り方と指標の読み方は
  `src/treefiler/CLAUDE.md` の「打鍵の所要を実物のバイナリで測る」
- 実測 (2026-10-30、200x50、`j*20 k*20`、700ms 間隔。直す前のバイナリ):
  - キーの Update は 0.02〜0.08ms、1 フレームの View は 0.2〜0.6ms、キーが届いてから最初の書き込みまで p50 9ms / p95 17ms。treefiler の中は速い
  - 送ってからキーが届くまで p50 8ms / p95 24ms (tmux と pty。treefiler の外)
  - **ホーム (`ROOT=~`) では、キーの後の動きの次のフレームが p95 49〜75ms / 最大 80ms 待たされる**。src/ では出ない
- 原因: main.go の tick は bool の single-flight で、周期を張った時点に決める。ホームでは裏の走査がずっと続き (`Busy()`)、80ms の
  遅い tick が張ってある。そこへ打鍵で動きが始まっても `ticking` が立っているので 16ms の tick を張らず、動きの 2 フレーム目が遅い tick
  (最大 80ms 後) まで止まる。perf.log で、打鍵の直前に 80ms 間隔の tick が並び、キーの後の最初の tick が前の tick のちょうど 80ms 後に
  来ることを確かめた
- 同じ作りが glogx の `maybeTick` (browse_anim.go。80ms のスピナーの tick が張ってある間に filer・スライド・toast の動きが始まると待たされる) にある

## 対応方針

- tick を 1 本に束ねる判断を tuikit の部品 `tickchain` に置く: 張ってある tick の届く時刻を覚え、それより早く要るときだけ張り直す。
  追い越された古い tick は seq で見分け、描き直しには使うが張り直しの判断は `Want` に任せる (チェーンが増えない)
- treefiler の main.go と glogx の maybeTick の両方をこれに寄せる

## 進捗

- [x] 打鍵の所要のログ (perflog・`TREEFILER_PERFLOG`) と、隔離した tmux から打鍵して測る道具 (tools/tfperf・`make perf`)
- [x] tuikit/tickchain と treefiler の main.go への配線。テスト `TestKeyDuringSlowTickArmsFrameTick` と tickchain のテスト
  - 変異で red を確認 (bin/mutate-verify): main.go を「張ってあれば張らない」に戻す → TestKeyDuringSlowTickArmsFrameTick
    (「次の tick は 80ms 後」) / tickchain の比較を `if c.armed` に戻す → TestFasterNeedRearmsOverPendingSlowTick
- [x] 直す前後の比較 (`ROOT=~`・200x50・`j*20 k*20`・700ms、base と fix を交互に 2 回ずつ)。next_frame_ms (キーから動きの次のフレーム):

  | | p50 | p95 | max |
  |---|---|---|---|
  | base 1 | 16.3 | 49.2 | 68.9 |
  | fix 1 | 16.5 | 17.0 | 17.0 |
  | base 2 | 16.5 | 75.2 | 80.4 |
  | fix 2 (load average 26) | 16.8 | 18.8 | 21.6 |

- [x] glogx の maybeTick を tickchain に寄せる → issue 998 に分けた
- [x] 敵対的レビュー (opus、2 周)
  - 1 周目 P2 6 件・P3 5 件。直した: `!` / `s` の子の stdout が perflog の包んだ writer になる (Cmd.Stdout に端末を先に入れる)・
    TREEFILER_PERFLOG が子へ漏れる (読んだらすぐ消す)・main の Fired の配線が無検査 (時計を注入してテストで見る)・draw が前のフレームの
    書き込みを数える・Ctrl-C で tmux が残る / socket が消えない (`-S <out>/tmux.sock` と signal)・打鍵とログを名前で照合していない・
    「実端末」の言い過ぎ (文書を直し default-terminal を本番に揃える)・ログの書き出しが event loop に乗る (最初の view と Close だけ)・
    旧い置き場の設定を読まない
  - 2 周目 P2 1 件・P3 5 件。直した: 古い tick の検査が偽の緑 (seq で見る)・前のフレームとの境目をキーの後の View の終わりに・
    tmux のキー名の写し間違い (Delete / Insert / Shift+Tab / `S-` / `C-[` / `C-m` / `C-i` / 小文字の修飾)・Ctrl-C 2 度で抜ける・相対の -out
  - 直さず記録: q で閉じない / Ctrl-C で止めた run の perf.log には最初の描画より後が残らない (CLAUDE.md に測れないものとして書いた)。
    -out が 104 byte を超えると tmux の socket を作れず起動で止まる (止まる側。make 経由は約 56 byte)。tools/tfprobe の settle のコメントは
    main.go の tick の張り方を模していない (ヘッドレスで影響は小さい)
  - 変異で red を確認 (bin/mutate-verify): main を single-flight に戻す・Fired の行を消す・tick の Cmd が seq を運ばない → TestKeyDuringSlowTickArmsFrameTick /
    tickchain の Fired が seq を見ない → 同じテスト / tickchain の比較を `if c.armed` に戻す → TestFasterNeedRearmsOverPendingSlowTick /
    tfperf の名前の照合を外す → TestAnalyzeRejectsShiftedKeys / 前のフレームの書き込みを数える → TestAnalyzeDrawIgnoresWriteStartedBeforeKeyView
- [x] 直した後の実物での確認 (`ROOT=~`・`j*15 Down*5 k*10`・500ms): next_frame_ms p50 16.5 / p95 17.0 / max 17.0。tmux の socket とプロセスは残らない

## 残タスク (未着手・スコープ外)

- 1 行の移動はビーズの移動がセルの粒度で途中に 1 回しか画面に出ない (目に見えて着くのはキーの約 70ms 後)。動きの長さ (moveDur 0.50・
  beadDur 0.35 秒。ease-out quint) は spec §0.1 のユーザー決定 (2026-10-08) なので、このまま「もっさり」が残るなら長さの見直しを諮る
- ホームを開いている間、裏の走査が終わるまで 16ms ごとの書き直しが続く打鍵がある (700ms 窓で 42 フレーム・45KB)。原因は未調査
