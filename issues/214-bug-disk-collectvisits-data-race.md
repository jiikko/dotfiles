# 214 bug: テスト専用カウンタ `collectVisits` が並行走査で競合し、`-race` が落ちる

起票日: 2026-09-03
出典: issue 213 の着手時に `TestUpdateKeysYieldToDoctorDelete` が赤かったので切り分けた
重要度: P2 (**CI が赤い**。production の実害は「テスト専用カウンタがずれる」だけ)
関連: `src/doctor/disk/guard.go` の `collectVisits` / `collectBundleIDsSeen` /
`src/doctor/disk/scan.go` の `guards.do` / issue 148 (disk 診断)

## 症状

`go test -race ./...` (glogx) が `TestUpdateKeysYieldToDoctorDelete` で落ちる。
落ちる理由は assert ではなく **データ競合**:

```
WARNING: DATA RACE
Read at 0x000104f25340 by goroutine 104:
  doctor/disk.collectBundleIDsSeen()  guard.go:260
  doctor/disk.collectBundleIDs()      guard.go:209
  doctor/disk.installedBundleIDs()    guard.go:195
  doctor/disk.scanEntry.func7()       scan.go:226
Previous write at 0x000104f25340 by goroutine 52:
  (同じ経路)
```

`0x…340` は `guard.go:215` の **package 変数** `var collectVisits int`。

## なぜ起きるか (コメントの前提が崩れている)

```go
// collectVisits は collectBundleIDs が実際に降りたディレクトリ数 (テスト専用の計測点)。
// 走査は単一 goroutine から呼ばれる (installedBundleIDs → collectBundleIDs) ので素の int でよい。
var collectVisits int
```

`guards.do` は **key ごとの `sync.Once`** なので、**1 回の走査の中では**確かに 1 度しか走らない。
崩れるのは**走査が 2 つ同時に走るとき**で、`guards` は走査ごとに別インスタンスなので
`sync.Once` は共有されない。package 変数だけが共有される。

並行走査が起きる経路:

- テスト: 1 つのテスト関数が複数の `browseModel` / `doctorView` を作り、それぞれが走査を始める
  (`TestUpdateKeysYieldToDoctorDelete` は状態 × キーの直積で 8 サブテストを回す)
- production: `r` (rescan) の前世代と新世代が一瞬重なる。issue 211 で
  `start()` の冒頭に `v.stop()` を入れたので前世代は cancel されるが、**cancel は
  goroutine の即死ではない**ので重なる窓は残る

## 直し方

`collectVisits` を `atomic.Int64` にする (テスト専用の計測点なので意味は変わらない)。
「単一 goroutine 前提」のコメントは**誤りなので消す**。

⚠️ カウンタを消して `seen` map の長さを返す形にもできるが、`collectBundleIDsSeen` の
シグネチャが変わり呼び出し側 2 箇所に波及する。計測点としての価値は同じなので atomic に留める。

## テスト観点

- `scan_test.go` の `collectVisits = 0` / 参照を atomic の API に合わせる
- **並行走査で `-race` が落ちないこと**を回帰テストにする (2 つの `Scan` を同時に回す)
- 変異: atomic を素の int に戻すと `-race` で落ちること

## レビュー状態

反証レビュー未実施。競合の事実は `-race` の出力で確認済み (上記)。
