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

## 決着 (2026-09-03)

**修正済み**: `collectVisits` を `atomic.Int64` に (commit `bad49d3c` / `1c7f076f`)。
「単一 goroutine 前提」のコメントは atomic が要る理由に書き換えた。

検証:

- baseline: `go test -race -run 'TestConcurrentScansDoNotRaceOnCollectVisits|TestCollect' ./disk/` が green
- 元症状: glogx の `TestUpdateKeysYieldToDoctorDelete` が `-race` で green (rc=0)
- **変異**: 使い捨て worktree で `atomic.Int64` → 素の `int` (`.Add(1)` → `++`、未使用 import 削除、
  テスト側も素の int に) に戻し、**ビルドが通ることと diff が意図した 3 箇所だけであることを確認**
  してから実行 → `WARNING: DATA RACE` × 4 + FAIL。落ちたのは `got <= before` の判定不能ガードでは
  なく race detector で、予測どおり
- CI 配線: `-race` は `src/*/Makefile` の `test:` 全 6 プロジェクトと
  `.github/workflows/doctor.yml` の両方に入っている。退行は止まる

## 敵対的レビュー (opus, red team) の結果

**修正そのものは壊せなかった** (「壊せなかった」と明記させた)。空振りだった攻め口:

- 同型バグの取り残し (doctor 側): 非テスト全ファイルで package 変数への代入を走査。可変な
  package 状態は `collectVisits` の他に `destructiveHook` (TestMain が 1 度書く) と
  `runningUnderTest` (init) だけ。`catalog` / 正規表現 / `brewSharedVarDirs` は read-only。
  `svc` / `brewledger` / `runner` に可変 package 状態は無い。glogx 側の counter は
  `probeSeq` / `bytesRead` が既に atomic
- `guards.do` の sync.Once の帰属: 本文の主張は正しい (`g := &guards{opt: opt}` は `Scan` 内の
  per-scan instance。`installedBundleIDs` の呼び出しは `g.do("apps", …)` の 1 箇所だけ)
- production の `r` 重なりでの誤集計・二重削除: 世代は `gen` で全 Msg が捨てられ、ch は世代ごとに
  buffered、`Reuse` closure は snapshot のコピーを返すだけで共有 map への書き込みが無い
- 回帰テストの検知力: 非 atomic への差し替えで単独実行でも FAIL。判定不能ガードもあり vacuous でない

**切り出した指摘** (このコミットでは直さない):

- 216 (P2): glogx の unit test が doctor の実走査 goroutine を join せずに漏らす。
  214 のデータ競合の**上流の発火源**で、`collectVisits` は「たまたま跨いでいた唯一の package 変数」
  だった。修正先の `src/glogx/doctor_view.go` はユーザー指示で凍結中 (別セッションが全体見直し)
- 217 (P3): `pullCleanup` が `sync.WaitGroup` のまま (発火経路は未確認) +
  `collectVisits` の assert が絶対値を見ている件の記録
