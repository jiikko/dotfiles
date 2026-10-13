# 751 (test): glogx の issues viewer のテストで、変異を当てても緑のままのものと、寄せられる手組み

起票日: 2026-10-13

## 概要

2026-10-13 の監査 (記録は 754) の test-cleanup / test-helpers。変異は監査体が scratchpad の複製で当てた (diff の確認とビルドの成功を見たうえで。baseline は `-run 'TestIssues|TestBrowse'` の 303 本が PASS)。
main は 4 の境界の不足 (7 桁のケースが無い / BiDi のテストが 4 文字だけ) と、5 の分岐の位置をコードで確かめた。変異の結果そのものは監査体の報告。

## 詳細

1. **`issues_view_test.go` の `TestIssuesLayoutAgreesBetweenKeysAndRender` の「本文 (引き出し)」のサブテストが空振り**
   - `headWidth` から本文の分岐 (`if v.open != nil { return v.bodyWidth(total) }`) を消しても緑。`bodyHeadLines` は幅によらず常に 3 行なので、期待値と実装が同じ定数を引くだけになる
   - 直し方: サブテストを消すか、下限のクランプのサブテスト (これは効いている) に畳む。`headWidth` の本文の分岐が今は行数に効いていないことも併せて判断する (`bodyHeadLines` が幅で行数を変えるまで、死んだ分岐の疑い。未確認)
2. **`issues_drawer_test.go` の `TestDrawerTargetWidth` が値を固定していない**
   - `issuesDrawerRatio` 0.8→0.7・`issuesDrawerExtra` 10→14・`issuesDrawerMaxPeek` 18→30 の 3 つの変異がすべて緑。期待値を定数そのものから作っていて、定数どうしの関係しか見ていない。
     ユーザーの要望で決めた寸法 (80% + 10 桁、覗き見 18 桁) を固定するテストが glogx 側に無い
   - 直し方: リテラルの表 (total と期待幅の組) を 1 つ足す。関係を見るループは残す
3. **引き出しの開閉のテスト 3 本が tuikit の anim のテストと同じ性質を見ている** (読解のみ。変異なし)
   - `TestDrawerCloseIsReverseOfOpen` / `TestDrawerCloseFromPartialOpen` / `TestDrawerFinishLandsImmediately` と、`tuikit/anim` の `TestTransition*`。glogx 側は所要時間の配線だけを見る価値があるので、消さずに 1 本に統合する案 (配線の変異は未検証)
4. **698 の回帰テストの境界の取りこぼし** (`src/glogx/issues/audit698_test.go`)
   - `TestURLsStopAtBidiControls` は 4 文字 (U+202E / 2066 / 2028 / 2029) だけを回す。`urlRe` の範囲を `\x{2066}-\x{2066}` に狭めても緑。範囲の端 (U+202A〜202D・2067〜2069) が空振り
   - `TestNextNumberIgnoresDateLikeNames` は 6 桁と 8 桁以上だけ。上限の比較を `maxNumberDigits+1` にずらしても緑 (7 桁のケースが無い)
   - 直し方: BiDi は範囲の全 11 文字をループで回す (列挙の出典は termsafe の `isBidiControl` と揃える)。桁は 7 桁を 1 件足す
5. **`issues/move.go` の `MoveToSubdir` で、バナーを書けなかったときの rename の戻しが検査されていない**
   - done などから next へ rename で運び、`writeClaimBanner` が失敗したときの `os.Rename(dest, iss.Path)` を消しても `issues` パッケージ全体が緑。同じファイルの他の戻し 2 本 (`os.Remove(NextLinkPath)` と `clearClaimBanner` の失敗) は変異で赤になる
   - 直し方: 書き込めないファイル (chmod 0o444) でバナーの失敗を起こすテストを足す (この再現方法は未確認)。748 の 1 を直すときに一緒に書くとよい

### test-helpers

- 実ファイルと `issues.Issue` の手組み (`t.TempDir()` → `filepath.Join` → `os.WriteFile` → `&issues.Issue{...}`) が、issues 系の main 側テスト 13 ファイルに残っている。
  `os.WriteFile` は 50 か所、`&issues.Issue{Path: ` は 33 か所 (監査体の grep)。うち約 16 か所は 667 で作った `writeRealIssue` (`tui_helpers_test.go`) でそのまま置き換えられる見込み
  (`writeRealIssue` は Slug も付けるが、対象のテストは Slug を見ていない。要確認)。667 の方針どおり、触るテストから順に寄せる

## 却下 (再提出しない)

- assert を持たない Test 関数: `TestMain` と `TestIssuesRowsGoThroughSetter` だけ。後者は canary 付きの静的な gate
- `issues_epic_status_dirs_test.go`: 期待値を第 2 の列挙で持たない設計 (コメントどおり)
- `time.Date(2026, 7, 31, ...)` の共通化・`XDG_CACHE_HOME` の `t.Setenv` 4 か所: 効果が小さい

## 関連ファイル

- `src/glogx/issues_view_test.go`・`src/glogx/issues_drawer_test.go`・`src/glogx/issues/audit698_test.go`・`src/glogx/issues/move_test.go`・`src/glogx/tui_helpers_test.go`
- 前回: 667。監査の記録: 754

## 進捗

- [ ] 1〜5 と test-helpers は未着手
