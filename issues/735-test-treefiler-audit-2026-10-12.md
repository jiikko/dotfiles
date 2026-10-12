# 735 (test): treefiler のテストの穴と共通化 (2026-10-12 の監査)

起票日: 2026-10-12

## 概要

監査 (737) のテストの発見。調査体がテスト 39 ファイル・233 関数に変異 37 本 (有効 34) を当てた。消してよいと言い切れたテストは 0 件。上ほど優先。

## 詳細

1. **(false-green) filer の砂場の配線を確かめるテストが無い** — `filer/main_test.go` の `fsGuard = sb.Guard` を「常に許す関数」に替えても、ファイル操作のテストが全部緑
   (変異で確認)。差し替え忘れの検査 (未登録のパスで `fsGuard` が拒否し、Violations に載る) を足す。safefs の `TestGuardRunsBeforeTheOperation` は safefs の中だけを見る
2. **(test-helpers) 設定の置き場をテストごとに分けずに Model を作るテストがある** — 少なくとも `background_test.go` の `TestGitNestedRepos`・`TestGitRepoInsideRepo`、
   `regress691_test.go` の `TestRootSlashFindsNodes`・`TestRerootUpToSlash` (他の全数は数えていない)。🚨 本物の設定は触らない (TestMain が置き場を一時ディレクトリへ
   向けている)。起きうるのはテストどうしが一時の置き場を共有すること (`.` 等で保存するテストの後に読むと結果が順序に依存する) だけ。隔離の手書き
   (`t.Setenv("TREEFILER_CONFIG_DIR", t.TempDir())`) は 9 ファイル 20 か所 → `isolateConfig(t)` に寄せ、`newAt` が必ず通る形にする (効果はテストの独立)
3. **(false-green) `TestFrameAllocBudget` の上限が緩んだまま** — `filer/alloc_budget_test.go`。今の確保は 256 回で上限 276 (issue 724 で 274 から下がったのに据え置き)。
   約 8% の退行を素通しする。258 前後へ締め、上限のコメント (「274 回 (上限 276)」。issue 711 の記述のまま) も今の値へ直す
4. **(未確認) 仮想化の仕組みのいくつかがテストで固定されていない** — `virt.go` の `windowMargin` を 4 倍 → 1 倍にしても全部緑で、doc が根拠に挙げる seed 118 / 214 / 241
   (`TREEFILER_VIRT_SEEDS=300`) も 1 倍で再現しない (0 倍なら 118 と 214 が赤)。`isFresh` を常に false・`placeVirtual` の `v.fresh` / `spKid` の put を外す変異も緑
   (SEEDS=300 でも)。等価 (届かない) かを判定してから、届かないなら削る・届くなら固定する場面を足す。doc の seed の記述も直す
5. **(test-cleanup・弱い重複) 畳める 2 本** — `settings_test.go` の `TestPanelOwnsKeys` (`layers_test.go` の網羅テストと重なる。層テストの押すキーを j・J の両方にして畳む) /
   `tileview_test.go` の `TestDiffRefetchesOnEachToggle` (`TestDiffToggleShowsRefetchedContent` の下位。回数の assert を寄せて畳む)
6. **(test-helpers)** — 親を作って書く `mkfile` (4 か所: `filer_test` の fixture・`column_test`・`background_test` の `mk`・`virt_test` の `mustWriteAll`) /
   走査の結果を待つ `waitWalked(t, m, &now, dir)` (`leak_test.go` に 3 回) / `background_test.go` の `gitIn(... "init", "-b")` が `initRepo` を使っていない 1 か所
7. **(coverage) `sortByLabel` が `TestChoiceSettingsHaveTables` の外** — `settings.go` の `sortByChoices` に足して label を忘れると、`S` の知らせが「並び: 」だけになる
8. 記録: `TestPaperGroundsShareColors` は空でないことだけを見る (色の値は固定しない)

## 関連

- 737 (この監査)・695 (前回のテストの課題)

## 進捗

- 2026-10-12: 起票 (監査 737)。反証レビュー (sonnet) で 2 を false-green から test-helpers へ下げた (TestMain が既に隔離している)・3 にコメントの更新を足した
