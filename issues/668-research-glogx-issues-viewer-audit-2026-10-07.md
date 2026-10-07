# 668 (research): glogx issues viewer の監査 (設計 7 タイプ + テスト 2 タイプ) の記録

起票日: 2026-10-07

## 概要

`/audit` を直接実行で、glogx の issues viewer (`src/glogx/issues_*.go` と `src/glogx/issues/`、テスト込みで約 1.6 万行) に回した。
タイプ: design / responsibility / duplication / polymorphism / leaky-abstraction / encapsulation / ui-components / test-cleanup / test-helpers。
read-only の調査体 3 体を直列に回し (構造系 5 / 重複・UI 2 / テスト 2)、main が主要な主張を裏取りしてから起票した。

## 結論 (起票先)

| 起票先 | 中身 | 今壊れているか |
|---|---|---|
| 663 | 通知が断りの文・コピー失敗まで lastWarning を上書き (U2 / D2) | 壊れている (main がコード経路で確認) |
| 664 | 横断キーの手書き列挙。`D` がどの viewer からも効かない (U1) | 壊れている (`D` の不在を main が grep で確認) |
| 665 | `issues/` の規則の写し: waiting のバッジ欠け (A)・空の状態ディレクトリの見張り漏れ (B) / C・G・H・I は潜在 | A・B は壊れている (main がコードで確認、監査体が overlay で再現) |
| 666 | 状態更新の散在: E・D・F・D1・D3・U3・J | 潜在 |
| 667 | 変異で緑のままの assert 6 本 + fixture の散在 | #1 は main が変異を再実行して確認 |

全数勘定: 採用 32 項目 = 構造系 10 (A〜J) + 重複・UI 6 (D1〜D3 / U1〜U3) + テスト 16 (cleanup 8 / helpers 6 / 軽微 2。すべて 667)。却下は下節の 25 項目。

## 却下した指摘と理由 (再提起の前に読む)

- **`issues_view.go` (2891 行 / 127 関数) を行数で割る / サブパッケージに切る**: 3 問 (責務分離・探索コスト・参照範囲) のどれも良くならない。flat な package main は第二消費者が出るまで意図的 (src/glogx/CLAUDE.md)。本文 pager だけは 662 が第二消費者になりうるので 666 の D に trigger を置いた
- **browseModel との結合 (`want*` / `takeNotice`)**: `issues_view.go` 冒頭で契約として明文化。ただし `want*` の列挙の穴は 664
- **`dirWatcher` の seam**: CI 検査のための意図的な差し替え口 (issue 087)
- **`displayRow` の種別判定 27 か所をポリモーフィズムへ**: 種別は 2 + groupHead で、操作ごとに通知文と例外が違い、寄せても判断の数は減らない
- **exhaustive lint が守る switch (`Status.String` / `Badge` / `shows` / hint の段階)**: 守られている
- **`issues/` が画面の語彙 (`StatusFilter.Next` / `VisibleBadges` / `HumanTab`) を持つ (L2)**: CLAUDE.md が `issues/` の責務に「表示整形」を含めている
- **`ResolveRepoRoot` / `WorktreeRoots` が `issues/` にある**: `discover.go` のコメントと issue 320 で意図が明文化
- **`ClaimBanner` に `(glogx)` が入る**: 署名の文字列で害なし
- **`rows` と `displayRows` の世代の同期**: `issues_rows_setter_test` が強制
- **`pendingCursorPath` / `pendingMarkPath` / `pendingMoveStale`**: set / clear のヘルパーがあり、`receive` の部分消費は意図された分岐
- **`GroupKey` を生の絶対パス文字列で持つ**: 永続化と prune の理由が書かれている
- **予約外のサブディレクトリを見張らない**: spec で受容済み (P3-2)。665 B はこれとは別 (予約名の空ディレクトリ)
- **issues と status の開閉スライドの骨格**: issues/done/085 の付随 `071-two-slide-state-machines` で「削減が小さい」と判断済み。新しい事実は `animProgress` が定数以外逐語同一・`close` の二重呼び出しガードが status に無いこと (status は `handleKey` 前に畳まれるので今は発火しない)。再提起はこの差が効く形が出たとき
- **`isMarkdownPath` (.md/.markdown) と `issues.isMarkdown` (.md のみ)**: 本文 pager で開く判定と走査で拾う判定で、集合が違うのは意図的 (後者は doc に実測 405/405 件の根拠)
- **`isDigitKey` と `isASCIIDigit`**: 数行で変更理由も同じ。効果なし
- **本文 pager のスクロール / hint / y/N 確認 / トースト経路**: 既に共有 (`pagerScrollKey` / `fitHintItems` / `confirm.Dialog` / `deliverNotice`)。`IsYes` と `IsYesStrict` の差は issue 071/123 で意図的
- **タブ行 (issues の `scrollTabs` と doctor の `tabBarLine`)**: あふれ方の要件が違う (可変数の横スクロール vs 5 枚固定の略称詰め)
- **`emptyMessage`**: 画面ごとの状態の語彙そのもの
- **開くスライドの見た目 (`layout.SlideIn` と `slideLeftWindow`)**: 意図的に別 (085、ユーザー選定)
- **確認の板 (`markNextBox` / `discardBox`)**: 既に `confirm.Dialog`
- **実 fsnotify 版の watch テスト (`TestIssuesWatchReAddsRecreatedDir` 等) と seam 版の重複**: ファイル冒頭で「実 fsnotify との結合を見る版として残す」と明記
- **`TestBrowseCloseAnimSwallowsEditorKeyOnly`**: e の飲み込みを外す変異で赤。効いている
- **`TestBrowseCloseAnimStillPassesQuitKey` と `TestIssuesCloseLandsOnKeyWithoutSwallowing`**: 入口が違う (`i` キーと `close()` 直呼び)
- **`issues/parse_test.go` の `TestScanRealRepoIssuesDirIfPresent`**: 実データの smoke で安価
- **sleep の待ち**: issues 系テストに `time.Sleep` は無い。`liveEventChains` の 300ms 静止窓は ack 同期への移行先として受容済み、seam の `EventPath` 150ms は否定の待ちで正当、`waitMsg` は上限つき条件待ち

範囲外で 1 行だけ記録: `doctor_rowcursor.go` の `rowCursor.window` が `listnav.WindowOffset` と同じ式。

## 攻めたが見つからなかった範囲

- 循環依存 (`issues/` は main を import できない)
- `issues/` パッケージの banner / body / move / nextlink / filelink の重複 (テストは変異を当てていない。fixture の重複のみ)
- drawer は `anim.Transition` + `layout.ComposeDrawer`、枠は `overlayCenteredBox`、入力は `lineedit` を通している
- 一時ディレクトリはすべて `t.TempDir`、`XDG_CACHE_HOME` は `TestMain` が隔離、`issues_rows_setter_test.go` の静的 gate は canary 付き
- linkjump / group / state のテストの残りは通読のみ (変異なし)

## 監査の注記

- 主張のうち main が直接裏取りしたもの: A (`Badges` のコード) / B (`issuesWatchDirs` のコード) / U2 (`deliverNotice` と `showWarning` の doc) /
  U1 の `D` (want フラグの grep と ui-guide §2) / 667 #1 (変異の再実行で 179 本 PASS)。それ以外の件数・経路は監査体の報告で、各 issue に「監査体の」と書いた

## 反証レビュー (2026-10-07、read-only サブエージェント 1 体)

起票の取り下げは 0 件。誤動作の主張 (663 / 664 の `D` / 665 A・B / 667 #2・#4) はすべて現コードで成り立った。訂正 8 点を別 commit で反映した:
664 から「status の pager で i/s/R が捨てられる」を取り下げ (ownsKeys のモードでは横断を通さないのが既存の設計判断) し doctor の穴を追加 /
663 の発火条件の「親行で y」を「v/e/p/Y/N」に訂正 / 665 I の「黙って」を issue 277 の見出し照合の防御を前提に限定 / 665 G の「逐語同一」を訂正 /
666 E・D の数え (make 5 か所・5 関数) / 666 D3 に古いコメントを追加 / 667 の手組み件数の揺れを明記。
