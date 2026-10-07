# 667 (test): issues viewer のテストに変異を当てても緑のままの assert があり、fixture の手組みのずれがそれを生んでいる

> 🚨 **担当中: claude (glogx 監査の修正セッション)**（2026-10-08〜）

起票日: 2026-10-07

## 概要

issues viewer のテストに `go test -overlay` で変異を当てたところ、名指ししている不変条件を壊しても緑のままの assert が 6 本あった。
うち 2 本 (#1 / #2) は issues 系のテストを全部回しても緑で、守りが無い。原因の一部は、実ファイルの issue を作って本文を開く
fixture が 6 つのヘルパーと 26 箇所の手組みに散り、手組みどうしがずれていること。

出典: glogx issues viewer 監査 (668) の test-cleanup / test-helpers。変異体は監査時の一時領域にあり残っていないので、各項に変異の中身を書く。

## 詳細: 守っていない assert (test-cleanup)

ベースラインは対象 8 本すべて PASS (監査体の測定)。

1. **`TestIssuesNumberFilterTypingSwallowsListKeys` の `"a"` と `"tab"` が空振り** (issues_number_filter_test.go)
   - 変異: `issues_view.go` の `if v.numFilter.typing {` を `if v.numFilter.typing && key != "a" && key != "tab" {` にし、入力中の a / tab を一覧側へ素通しさせる
   - 結果: `go test -run TestIssues` の 179 本すべて PASS (**main が再実行して確認**。監査体は issues 系 490 本で緑と報告)
   - 理由: 番号フィルタはタブと状態を無視するので rows も cursor も変わらない。変わるのは `v.filter` / `v.tabIdx` で、Esc で解いた後に別の行集合へ戻る実害がある
   - 直し方: 各キーの後で `v.filter` と `v.tabIdx` が変わらないことを assert する
2. **`TestIssuesViewRescanRebindsOpenBody` の 2 つ目の assert (本文ヘッダーの状態) が空振り** (issues_view_test.go)
   - Enter の後に `drawer.finish()` が無く引き出しの幅が 0 のままで、本文が描かれていない。`"pending"` に一致していたのは一覧の空表示の案内 `(a: pending も表示)`
   - 変異: 本文ヘッダーの `status := v.open.StatusLabel()` を `""` にする → issues 系全体が緑 (監査体の測定)
   - 直し方: `drawer.finish()` を足し、`bodyHeadLines` の行から状態を読む
3. **`TestIssuesViewKeepsSelectionAcrossRescan` が「錨をパスで張り替える」を区別できない**: 再スキャンの fixture が同じ順で錨も 0 行目。
   `anchorMark` を index で保つ形に置き換えても緑 (守りは `TestIssuesViewMoveReanchorsCursorAndMarkToReturnedPaths` 1 本だけ)。
   直し方: 2 回目の受信で錨より前に issue を挿入した集合を渡す
4. **`TestIssuesViewGroupParentActionsAreNoopWithNotice` の `N` が空振り、かつ実クリップボードを書き換えうる** (issues_group_view_test.go)
   - 親行のガードから `N` だけ外しても緑 (`copyNextNumber` も通知を出すので `notice != ""` で通る)。このテストはクリップボードを差し替えていないので、退行すると実際に pbcopy が走る
   - 直し方: クリップボードの stub を入れ `copied == ""` を assert する。そもそも親行で `N` を禁じる必要があるか (次の番号は対象行に依存しない) を決め、不要なら一覧から外す
5. **`TestIssuesWatchIgnoresUnstableFile` が名前の「読まない」を検出しない** (issues_watch_test.go): 安定待ちの分岐を無効化して毎回すぐ読む形にしても緑
   (`TestIssuesWatchReloadsAfterExternalEdit` は赤になるので穴は塞がっている)。直し方: ループ内で `!v.scanning` と `v.watch.pending != ""` を assert
6. **`TestIssuesViewRowShowsNumberBadgeCategoryTitle` がカテゴリをタブ行の文字で満たしている**: 行からカテゴリ列を消しても緑 (タブ行のチップに `feat` が出る)。
   Title は検査していない。直し方: カーソル行だけを見てタイトルも見る。直さないなら名前から Title を外す

重複 (このテストだけが赤になる変異を作れなかった。統合・削除の候補。削除するなら失うものを本 issue に書く):

7. `TestIssuesViewBodyHintAdvertisedEditorKeyWorks` — 本文モードの `e` の変異で `TestIssuesViewBodyHintKeysAllRespond` / `TestIssuesViewActionKeysWorkInBothModes` も赤。固有なのは案内文 `"e: 編集"` の pin だけ
8. `TestIssuesViewDrawerKeepsTickAlive` — `TestIssuesDrawerAnimatesThroughModelTicks` が包含。故障箇所を絞る目的なら残してよい (低)

軽微 (未変異): `TestIssuesViewMultiSelectYank` の `handleKey("shift+up")` は直後の直接代入で上書きされ効いていない / `TestIssuesViewRescanReturnsCmd` は `msg != nil` のみ

## 詳細: ヘルパー (test-helpers)

1. **実ファイルで本文を開く fixture の一本化** (最優先): 同じ処理のヘルパーが 6 つ (`realIssue` / `realDoneIssue` / `path2issue` / `realIssuesView` / `newBodyKeyEnv` / `newJumpEnv`)、
   手組みの `&issues.Issue{Path: …}` が issues_view_test.go に 20〜28 + 他ファイルに 3〜6 (grep の書き方で揺れる。監査体と反証レビューで数えが食い違った。
   母集合の issues 系 main パッケージの `os.WriteFile` は 52 箇所で両者一致)。
   既にずれている: `drawer.finish()` を呼ぶのは issues_view_test.go で 7 箇所、issues 系全体で 10 箇所だけ (抜けた 1 箇所が上の #2)。root の `EvalSymlinks` は `newJumpEnv` だけ。
   → `openRealBody(t, body, opts) *issuesView` に寄せ、着地と描画の確定まで必ず行う
2. **外部作用 (クリップボード / エディタ / ブラウザ) の既定 stub**: `loadedView` / `newTestIssuesView` は差し替えず、各テストが自分で差し替える (35 箇所)。上の #4 が漏れの実例。
   → viewer を作るヘルパーが `t` を受け取り、既定で「呼ばれたら t.Fatal」の stub を入れる
3. 時計の手組み 5 箇所 (issues_drawer_test.go 3 / issues_view_test.go 2) が既存の `stubClock(t)` (tui_helpers_test.go) を使わず、片付けが `defer` と `t.Cleanup` で混在
4. 閉じる演出つき browse の 4 行 (`newTestBrowse` → `closeAnimOff=false` → `toggle` → `finishAnim`) が issues_close_anim_test.go に 4 回
5. `fmt.Sprintf("%03d")` のループが issues_view_test.go に 5 箇所、`manyIssues` / `numStr` (issues_cursor_glide_test.go) と重複
6. `issues/` パッケージ: `writeFile` / `writeFileContent` / `writeIssue` / `mkFiles` が同形、`parse_test.go` の `contains` / `indexOf` は `strings.Contains` の書き直し

## 反証レビュー (2026-10-07)

#1 / #2 / #4 / #5 / #6 / 軽微は反証レビューでも成り立った (#2 は glogx パッケージ全体で緑、#5 の assert `v.watch.seen == ""` はループ中常に非空で空振り)。
#3 / #7 / #8 とヘルパー #2〜#6 は監査体の測定のみで、独立には確かめていない。

## 対応方針

ヘルパー #1 / #2 を先に入れ、その上で test-cleanup #1〜#6 を直す (ヘルパーが着地と stub を保証すれば #2 / #4 の形は再発しない)。
各修正は名指しの変異を当てて red を確認し、commit message に変異を書く。

## 関連ファイル

- `src/glogx/issues_*_test.go` / `src/glogx/tui_helpers_test.go` / `src/glogx/issues/*_test.go`

## 進捗

- [ ] ヘルパー #1 `openRealBody` / #2 既定 stub
- [ ] test-cleanup #1〜#6 (各変異で red 確認)
- [ ] #7 / #8 の統合判断
- [ ] ヘルパー #3〜#6
