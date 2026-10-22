# 864 (test): lan-manager 監査 (2026-10-22) のテスト・CI

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-22〜）

起票日: 2026-10-22

## 概要

lan-manager の全体監査 (865) のうち、テスト (test-cleanup / test-helpers)・CI・lint の発見。テストは削除していない (報告だけ)。

## 詳細

### A1 (P2) flaky: `setup/setup_test.go:TestRemoteCommandRunsDetached` (テストだけの競合)

- 基準の `go test -count=1 ./...` で 1 回、`TempDir RemoveAll cleanup: unlinkat .../001/state: directory not empty` で落ちた。単独 3 回は緑、`-count=20` で 1/20 再現
- 原因: テストは `-setdhcp` がログに出た時点で戻るが、`Script()` の EXIT trap の `finish()` は戻し (`networksetup -setdhcp`) の**後**に
  `drop` (`mv staticip.lock → .gone.$$` と `rm -rf`) を走らせる。`drop` の mv が `t.TempDir` の RemoveAll の途中に入ると落ちる
  (`-setdhcp` が見えた瞬間に state/ を見ると、30 回中 13 回で lock か `.gone.<pid>` が残っていた)
- このテストだけが背景のシェルを待たない (偽の `sudo -b` が `"$@" & exit 0`)。他のテストはスクリプトのプロセス自体を `Wait()` する
- production 側は安全: 「終わった」の合図の後に走るのは lock の解放だけで、その数 ms に同じ相手で次の固定が始まっても `alive()` が「走っている」で断る
- `-race` では検出されない (プロセス間のファイルシステムの競合)
- 方向: テストで lock と `.gone.*` が消えるまで `waitFor` してから戻る (または背景の pid を控えて終了を待つ)

### A2 (P2) CI の lanmgr のテストが約 73 秒から約 136 秒に伸びた (ci C4)

- CI の lanmgr の所要: 72.96s (run 39006462614) → 83.62s (39135250672) → 136.44s (39380638031) → 124.57s (39750796070)。setup も 38〜50s
- 手元の `go test -race -count=1 -json ./lanmgr ./setup` で lanmgr 82.6s のうち上位 4 本で約 58s: `TestEveryScreenFitsHeight` 21.1s・
  `TestFilterCaretExact` 20.1s・`TestSetupViewFits` 10.5s・`TestListWindowDoesNotDependOnSelectedRow` 6.3s
- 幅を回すテストがループのたびに `newHarness` を作り直す (網の読み込みの goroutine も毎回起動)。`TestFilterCaretExact` は約 660 個、`TestEveryScreenFitsHeight` は 900 個。lanmgr と setup に `t.Parallel()` は 0 本
- 方向: 状態ごとに harness を 1 つ作ってループの中は `Resize` だけにする (監査の掃引のテストはこの形で 4 状態 × 140 幅 × 12 高さが 31 秒)。掃引系に `t.Parallel()`

### A3 (P3) `ledger.FileStore.Update` の「鍵は変えさせない」を守るテストが無い

- `mutate-verify` で `hosts[i].Host = k` を消しても `./ledger` の全テストが緑 (rc=6)。今の呼び出し元は Host を書き換えないので潜在
- lanmgr の偽物 `memStore.Update` もこの規則を真似ていない
- 方向: Update の f の中で Host を書き換え、鍵が戻ることを assert するテストを 1 本。memStore にも同じ規則

### A4 (P3) 準備の書き込みの失敗を捨てている

- `setup/setup_test.go` の、同時に 2 本走らせて変更が 1 回になることを見るテストの stale の 2 周目以降 (`_ = os.MkdirAll(lockPath)` / `_ = os.WriteFile(.../pid)`) と、
  生きている持ち主の lock を断るテスト (`_ = os.WriteFile(lockPath+"/pid")`)
- 書けないと、前者は引き取りの窓を検査しなくなり (件数は 1 のまま緑)、後者は空の pid を「生きている」と読んで起動時刻の照合を通らないまま緑になる
- 方向: `t.Fatal` で受ける

### A5 (P3) 「条件が成り立つまで Advance を回す」待ちが 8 通り (test-helpers)

- `model_test.go` の `advanceUntil` (5s・Gosched)・`awaitWired` (5s)・無名の待ち 3 か所、`reach_test.go:awaitReach` (10s・Sleep 1ms)、`speed_test.go:awaitSpeed` (10s・Gosched)、別 package の `setup/setup_test.go:waitFor`
- `len(h.m.bg) > 0` の無名の待ちは上限で Fatal せず黙って先へ進む (後の assert が別の文面で落ち、原因が読めない)
- 方向: `pollUntil(t, what, limit, step, cond)` 1 本に寄せる

### A6 (P3) Scanner と Store の偽物がパッケージごとに写され、本物と規約が違う (test-helpers)

- `lanmgr/model_test.go` の `fakeScan` / `memStore`、root の `dump_test.go` の `dumpScan` / `brokenStore`、`speed_test.go` の `speedLedger`、glogx の `fakeLanScan` (2 か所に二重写し)
- 本物の `discover.Scanner.Close` は `Changed` を閉じ、glogx の偽物は真似るが、lanmgr と dump の偽物は `Changed()` が nil。`dump.go:settle` の `<-m.Changed()` の枝と `main.go:watch` はテストから通らない
- 方向: テスト用の package に本物の規約 (Close で Changed を閉じる・ErrNotFound・鍵の固定) を持つ偽物を 1 つ置く

### A7 (P3) lint: 台帳の古い行での Upsert を止める検査が無い (lint-from-done)

- done の 824 の 7 (古い行での Upsert) の再発を止めるものが無い。lanmgr から `Ledger.Upsert` を直に呼ぶ形が 2 か所 (862 の 6)
- 方向: 862 の 6 の `Merge` を入れた後、`.golangci.yml` の forbidigo で lanmgr 配下の `.Upsert(` を禁止する (ledger とテストは除く)

### A8 (P3) `.golangci.yml` に treefiler から写した除外とコメントが残る

- depguard の `!**/width_test.go` と `!**/tools/width-probe/main.go` は、この module に存在しない。コメントがこの module に無い
  `width_test.go:TestNoSecondWidthEngine` を指す。`uniq-by-line` のコメントが外した ruleguard の不変条件を根拠にしている。検査の結果は変わらず、読む人を誤導するだけ
- 方向: 除外とコメントを消す

### 記録だけ (直さない判断でよい)

- CI の setup-go のキャッシュは go.sum の hash でしか鍵を変えず、build cache が古いまま固定される (run 39750796070 の lint と test の両方で
  `Cache hit occurred on the primary key … not saving cache`)。test の step で最初の package が ok を出すまで約 6 秒で、124 秒のうちの小さな部分

## 変異の記録 (監査で当てたもの)

| 変異 | 結果 |
|---|---|
| space の登録で空の欄だけ埋めるのをやめる | red (TestSpaceRegisterKeepsRowWrittenMeanwhile) |
| setup の台帳の手順の `if r.managed` を無効にする | red (TestSetupLedgerDoesNotResurrectRemovedMac) |
| `speedLine` の `!r.managed` を外す | red (TestSpeedOnlyManaged) |
| ledger の Update の鍵の固定を消す | **green** (A3) |
| `trimLog` の短いファイルの guard を消す | red (TestFileLogTrimRaceAndPerm。panic) |

## 関連ファイル

項目ごとに上に書いた。

## 進捗

- [x] A1 flaky (dotfiles-0e。commit「test(mac-lan-manager): TestRemoteCommandRunsDetached が背景の lock の後始末を待つ」)
  - `staticip.lock*` が消えるまで待ってから戻る。直した後 `-count=60` で 0 件 (直す前は `-count=20` で 1 件。直す前の割合でも 60 回で 0 件になる確率は 5% 弱。負荷下は未確認)
  - 偽の `sudo -b` で起こした背景を待たない形は、他のテストには無かった (敵対的レビューで確認。他は `cmd.Wait()` で背景の終わりまで待つ)
- [ ] A2 テストの所要 (dotfiles-b9)
- [x] A3 Update の鍵の固定のテスト (dotfiles-0e。862 の 6 と同じ commit)。edit の `hosts[i].Host = k` を消す変異 → TestUpdateOnlyExisting が red。memStore も鍵の固定と空のホスト名の拒否を本物に合わせた
- [ ] A4〜A6 (dotfiles-b9)
- [x] A7 (dotfiles-0e。862 の 6 と同じ commit)。forbidigo ではなく、`Upsert` を `ledger.Store` から外して compiler が止める形にした (lanmgr は Store しか持たないので、丸ごと置き換える口を呼べない)
- [x] A8 (dotfiles-0e。862 の 5 と同じ commit)。width_test.go・tools/width-probe の除外と、dispWidth・ruleguard を指すコメントを消した。`make lint` は 0 issues のまま
