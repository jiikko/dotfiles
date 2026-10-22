# 859 (bug): speedtest が途中で切れた測定を成功の値として返す

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

- [ ] 1 の修正とテスト
- [ ] 2 の修正
