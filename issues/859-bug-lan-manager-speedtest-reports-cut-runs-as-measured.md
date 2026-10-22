# 859 (bug): speedtest が途中で切れた測定を成功の値として返す

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-22〜）

起票日: 2026-10-22

## 概要

lan-manager の全体監査 (865) の P2。下りの測定が 5 秒の終わりより前に EOF になると、ssh の失敗 (`waitErr`) を捨てて `Err == nil` の速度を返す。
CLI の `speedtest` は rc=0 で `↓ 0.00 Mbps` (か切れるまでの短い区間の値) を「測れた」と出し、画面も成功の値として覚える。
同じ「止めた合図の見方」の取り違えが CLI の `runSpeed` にもある (下の 2)。

## 詳細

### 1. `speedtest/speedtest.go:measureDown` (P2・実行で確認)

- 終わりの判定が `case !stopped && !errors.Is(rerr, io.EOF)` なので、`stopped` の前の EOF は成功に落ちる。相手の `head -c 8e9` が
  5 秒以内に正常に終わることはないので、`stopped` より前の EOF はどれも「途中で切れた」はず
- 発火条件: 測定中に ssh が切れる (有線を抜く・相手が眠る)、または相手の起動ファイルが stdout に書いた後で `head` が失敗する
- 再現 (監査の検証テスト。`go test -overlay` で作業ツリーには書いていない): 1 回だけ出力して EOF になり `exit status 127` で終わる
  偽の相手で 3 回回し、3 回とも `Result = {UpBps:0.088… DownBps:0 Err:<nil>}`

### 2. `speed.go:runSpeed` の `stopped := ctx.Err() != nil` (P3・未確認)

- ping と `dump --peer` は「Ctrl-C が先に子に届く」競合を、`stop` を結果より先に直接見る形で直してある (`ping.go:runPing` のコメント)。
  runSpeed だけが `ctx.Err()` で見ている
- subproc は子を別の process group に置かないので、ssh が先に SIGINT で死ぬと ✗ の行を出して rc=1 になり、最後の相手で止めても 130 にならない
- 退行を当てたら red になるテストがあるかは未確認

## 対応方針

- 1: `!stopped` で抜けたら EOF でも失敗にする (`途中で切れた: %w` に `waitErr` を含める)。上の偽の相手をテストに足す
- 2: `runPing` と同じく `stop` を直接見る。runSpeed と runPing の骨組みの重複は 863 の P10

## 関連ファイル

- `src/mac-lan-manager/speedtest/speedtest.go` (`measureDown`)
- `src/mac-lan-manager/speed.go` (`runSpeed`)、`ping.go` (`runPing`)

## 進捗

- [x] 1 の修正とテスト: `measureDown` は時間より前に抜けたら EOF でも失敗にする (`途中で切れた: <ssh の失敗>`。ssh が 0 で終わったなら
  「相手が時間より前に出し終えた」)。`TestMeasureDownCutBeforeTimeIsError` (下りが 2 塊で EOF・ssh が 127 / 0 で終わる 2 通り)
- [x] 2 の修正: 止める合図の見方を `untilStop` (`dump.go`。結果より先に stop を直接見る) に寄せ、`runSpeed` と `runPing` の両方から使う
  (863 の P10 のうち「stop を受けて 1 台ずつ走らせる」部分。「相手を選ぶ」部分は残る)。`TestUntilStopSeesStopBeforeCancel`・
  `TestSpeedtestStopWins` は `GOMAXPROCS(1)` で、合図を受ける goroutine が走る前に戻る並びを毎回作る
- 変異 (`mutate-verify`。どれも想定のテストが red):
  - `case !stopped:` を旧の `!stopped && !errors.Is(rerr, io.EOF)` に戻す → `TestMeasureDownCutBeforeTimeIsError`
  - `untilStop` の判定を旧の `ctx.Err() != nil` に戻す → `TestUntilStopSeesStopBeforeCancel`
  - `runSpeed` に旧のコード (goroutine + `ctx.Err()`) を貼り戻す → `TestSpeedtestStopWins`
  - 判定不能だった 1 本: `runSpeed` が合図をまったく見ない変異は、既存の `TestSpeedtestStopCancelsMeasurement` が `<-ctx.Done()` で止まり 600 秒の上限に当たった (起こりうる退行の形ではないので当て直した)
- [x] `live/speed_test.go` の `TestSpeedStartMeasuresThroughPipes` は「下りは相手が流し終えても値を出す」を固定していた (偽の相手が 3 MB で終わる)。
  偽の相手を止められるまで流す (`cat /dev/zero`) 形に直した。なのでこのテストは EOF の判定を見ない (見るのは `TestMeasureDownCutBeforeTimeIsError` だけ)
- [x] 敵対的レビュー (opus、読み取りのみ): P1・P2 なし。`-race -count=5` で緑、cat・wc の残りなし。P3 の 3 件:
  - テストのコメントの位置ずれ (`TestMeasureDownCancelReapsProcess` のコメントが新しいテストの頭に付いた) → 直した
  - 未確認リスク (直さない): 取り消しと、別の理由で死んだ ssh の EOF が同時に用意できると、select が EOF を選んで
    「途中で切れた」と返しうる (`errors.Is(context.Canceled)` が偽)。`pctx` は `ctx` の子なので、取り消しで殺された ssh の EOF は
    取り消しより後にしか用意できず、テストで作れなかった (50 回回すテストを書いたが、`ctx.Err()` を先に見る直しを外す変異が緑のままだった)。
    旧の形はこの並びで値を成功として返していたので、今より悪くはない。lanmgr は Err の種類を見ないので、変わるのは表示の文だけ。
    再提起するなら、この並びを作れる再現 (テスト) を添える
  - 受容: 相手が時間の中で `downMax` (8e9 B) を出し切ると (約 12.8 Gbps 超)、正しい測定でも「相手が時間より前に出し終えた」になる。ssh の暗号化の速さでは届かない
