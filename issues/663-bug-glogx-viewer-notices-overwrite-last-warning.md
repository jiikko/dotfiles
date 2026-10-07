# 663 (bug): glogx の viewer の通知が断りの文やコピー失敗まで lastWarning を上書きし、直前のエラーを `w` でコピーできなくする

> 🚨 **担当中: claude (glogx 監査の修正セッション)**（2026-10-08〜）

起票日: 2026-10-07

## 概要

viewer (issues / status) の通知口 `setNotice(text, ok bool)` は 2 値しか持たず、`browseModel.deliverNotice` は
`ok=false` をすべて `showWarning` へ流す。`showWarning` の doc (tui.go) は 3 分類を定めており、
「操作を拒否した理由の案内」と「クリップボード操作そのものの失敗」は `showWarning` を**通さない**
(lastWarning を上書きすると直前のエラーがコピー不能になるため)。viewer 経由の通知だけがこの約束から外れている。

出典: glogx issues viewer 監査 (668) の ui-components U2 / duplication D2。

## 詳細

- 該当: `issuesView.setNotice` / `statusView.setNotice` / `browseModel.deliverNotice` / `browseModel.showWarning` (tui.go)
- `setNotice(…, false)` の呼び出しは 30 件 (issues_view.go 16 + issues_linkjump.go 4 + status 10)。分類は
  断りの文 16 / クリップボード失敗 2 / エラー詳細 12 (監査体と反証レビューの手分類が一致)
- viewer の通知 (`takeNotice`) を受けるのは `deliverNotice` の 3 箇所だけで、別経路で `toast.Show` する道は無い (反証レビューで確認)
- issues viewer のコピー (`issuesView.copyText` / `copyLines`) は共有の `browseModel.copyWithToast` を通らず手組みで、
  失敗文言「コピーに失敗しました」が tui.go と別に 2 箇所ある。`copyWithToast` の doc は「失敗文言の複製を一本化した」
  と書いており、issues 側だけ取り残されている

### 発火条件 (コード経路で確認。実機の再現は未実施)

1. pull 失敗などで lastWarning に「pull に失敗: …」が入っている
2. issues viewer で末尾の issue に居て `J` (「これが最後の issue です」) を押す、または合成の親行で `v` / `e` / `p` / `Y` / `N`
   (`actionKey` の断りの分岐。親行の `y` は group 名のコピーに回るので断りにならない)、またはコピーが失敗する
3. viewer を閉じて `w` を押すと、元の pull の警告ではなく断りの文がコピーされる

silent に壊れる (compile も test も通る)。

## 対応方針

- `setNotice(text string, kind noticeKind)` (`noticeOK` / `noticeRefusal` / `noticeError`) にし、`deliverNotice` は
  `noticeError` だけを `showWarning` へ流す。exhaustive lint で kind の取りこぼしを止める
- issues viewer のコピーは「貼る内容と表示の文言」を返すだけにし、コピー自体は `copyWithToast` を通す
- `docs/glogx-ui-guide.md` §9 の通知の項 (「失敗は `w` でコピーできるよう控える」とだけ書いている) を同じ変更で 3 分類に合わせる
- 回帰テスト: lastWarning を入れた状態で viewer の断りの文を出し、lastWarning が変わらないことを assert

## 関連ファイル

- `src/glogx/tui.go` (`deliverNotice` / `showWarning` / `copyWithToast`)
- `src/glogx/issues_view.go` / `src/glogx/status_view.go` (`setNotice` / `copyText` / `copyLines`)
- `docs/glogx-ui-guide.md` §9

## 進捗

- [x] 通知の kind を 3 値にした (`noticeKind`: `noticeOK` / `noticeRefused` / `noticeError`。tui.go)。`deliverNotice` は exhaustive な switch で、
  `showWarning` へ流すのは `noticeError` だけ。分類は断り 21 / エラー 9 (計 30)
  - 🚨 Msg 経路で置く 2 件 (「開いていた issue が見つかりません」「見出しが変わりました」) は `noticeError`。次の打鍵が q だと
    トーストを読む前に終わるので、理由を `lastWarning` に残す契約 (issue 059、`TestIssuesScanMsgDeliversRebindNotice`) を守る
- [x] コピー失敗の文言は `clipboardFailText` を `copyWithToast` と viewer で共有し、種類は `noticeRefused`。
  viewer から `copyWithToast` を直接呼ぶ形にしなかったのは、viewer が browseModel を持たないため (意味の不具合は kind で直る)
- [x] ui-guide §9 を 3 分類に合わせた
- [x] 回帰テスト: `TestIssuesViewerCopyFailureToast` を「直前の警告を上書きしない」に書き換えた (旧版は 2026-07-31 17:56 の commit で入り、
  43 分後に `showWarning` の doc が 3 分類を定めた時点で取り残されていた) / `TestIssuesViewerRefusalKeepsLastWarning` を新設
- 変異: `deliverNotice` を旧来の形 (`noticeRefused` も `showWarning`) に戻す → 上の 2 本が red、059 のテストは緑のまま (意図どおり)
- `make -C src/glogx lint` 0 issues / `make -C src/glogx test` rc=0 (2026-10-08)
- [x] 敵対的レビュー (2026-10-08、663〜667 をまとめて 2 体を直列。①壊す・回帰 ②素通り)。①は P1/P2 の退行 0 件
  (横断キーの 3 打鍵 64 通りで全画面の 2 枚同時は 0 件 / setNotice 38 件で旧来 lastWarning に残ったエラー詳細の消失 0 件 /
  watchChain・groupExpansion・ContainerDir・OrderLess・IsStatusDir・URL ピッカーの窓はどれも旧実装と等価)。②は新しい検査を 3 つ壊した (下記)。
  直した差分は判定ロジックを新設せず、各修正を変異で直接確かめたので、3 周目は回さずに閉じた (adversarial-review §7 の例外)
  - ②P2: 通知の種類の検査が 30 件中 2 経路しか無く、「本文を読めませんでした」を noticeRefused にしても緑だった →
    `TestSetNoticeKindsFollowWarningClassification` (setNotice の全呼び出しを構文木で読み、エラー詳細を含む = noticeError / クリップボード失敗 = noticeRefused /
    それ以外 = noticeRefused (Msg 経路の 2 件だけ例外) を検査。3 種類の件数の canary 付き)。🚨 ソースを読む検査なので overlay の変異は効かない —
    作業ツリーに実際に当てて、レビューの変異 5 本 (末尾の J / 複数コピーの失敗 / 本文読み / 移動 / 破棄) が全部 red になることを確かめた
  - ①P3 (据え置き): パスを含むがエラー文を含まない 3 件 (「実体が見つかりません」「ファイルが見つかりません」「確認中に … が変わったため中止」) は
    noticeRefused のまま。`w` でコピーする価値のあるエラー詳細を持たないため
