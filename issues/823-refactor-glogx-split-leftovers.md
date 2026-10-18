# 823 (refactor): glogx の分割 (742 / 817) の残タスク

起票日: 2026-10-18

## 概要

分割 (742 / 817) の後に「設計の悪化・main は本当に薄いか・ビルドキャッシュ・カバレッジ」を測り直し、設計レビュー (opus 1 体。読み取りのみ・
悪化の反証を指示) の指摘を実コードで裏取りした。すぐ直せるものはこの issue で直し、大きいものを残タスクとして持つ。

## 測定 (2026-10-18。基準は分割の直前 96d70ecc^、比較は 1e23d1f4)

- main の非テストのコード: 62 ファイル 23,567 行 → 19 ファイル 8,178 行 (−65%)。main のテスト: 37,396 → 18,748 行
- ただし中心は減っていない: tui.go 4,272 → 4,227 行・browseModel のメソッド 150 → 145・フィールド 68 → 68。減ったのは画面の本体を internal へ移した分
- ビルドキャッシュ (空の GOCACHE で `go test -race -count=1 -run '^$' ./...` を作り、ファイルの 2 行目に 1 行足して作り直した増分。GOFLAGS=-trimpath):
  main (tui.go) 27.4 → 13.0 MB・doctor 画面 27.4 → 21.2 MB・土台 (internal/term / internal/app) は 42.8〜43.8 MB (分割前のどの編集より大きい)。
  分割前 3 か月の変更の置き場 (main 39% / 画面と gitdata 44% / 土台 11%) で重み付けすると 1 回あたり約 27.4 → 20 MB (約 −27%。画面は doctor の値で代用した見積もり)。
  🚨 ファイルの**末尾**に足す測り方は行の位置を動かさず下流の作り直しを起こさないので、土台が 0.6 MB に見える (使わない)
- カバレッジ (`-coverpkg=./...`): 88.6% → 88.8%。テスト名は 1,241 → 1,254 で消えたものは 0。関数ごとに照合して率が下がったのは
  newBrowseModel と routeKeyToLan の 2 つだけで、どちらも未到達の文の数は同じ (通っていた文が 1 つ減って分母が動いた)

## この issue で直したもの

- [x] usageview の公開フィールド (`UsageOverlay.Visible / Snap / Err / StaleErr`) を非公開にした。本番は読み取りのメソッド (`Visible()` / `Snapshot()` / `Err()` / `StaleErr()`)、
  起動時の表示は `NewUsageOverlay()`、main のテストは Fixture の setter (`SetVisible` など) を使う
- [x] 画面どうしの import を止めた: ratelimitview → usageview (`FetchNote`) を、main が注記を組んで `RatelimitRenderOpts.Note` で渡す形にした。
  `structure_ratchet_test.go` の `TestNoViewImportsAnotherView` が本番のコードの view → view の import を止める (import を戻す変異で red)。
  main が Note を渡すことは `TestRatelimitOptsPassesFetchNote` が見る (渡す行を消す変異で red)
- [x] 移設の残骸: 中身が `package main` だけの `issues_cursor_glide_test.go` を消した・`issues_rows_setter_test.go` の中身 (`recvTypeName`) を使う側の
  `structure_ratchet_test.go` へ移した・main のコメントに残っていた旧い非公開の型の名前 (diffOverlay・usageOverlay・issuesView など 13 か所) を今の型へ直した
- [x] lanview の TestMain の前処理 (今は効いていない保険) を残す理由を `internal/lanview/main_test.go` に書いた
- [x] Fixture がポインタを返す (commitview・statusview) のは受容し、golden が数えるのは本番の公開面だけだと `src/glogx/CLAUDE.md` に書いた

## 却下したもの

- `gitdata.GHErrorKind` を非公開にする: main のテストが `gitdata.GHError{Kind: gitdata.GHOther}` を組む。非公開にするとテストのためだけの口が要る
- 一括取得の「取れなかった SHA を埋める」を 1 つの口へ寄せる: 判断は既に `gitdata.FillUnknownFetched` の 1 か所にあり、2 つの呼び出し元が渡す範囲
  (静的経路は全部 / TUI はチャンクの分) の違いは意図したもの (tui.go の ciResultMsg の注記)。取得の関数の中へ入れると返り値の契約 (Statuses に無い SHA は
  取れなかったもの) とそのテストを変える

## 残タスク (未着手)

- [ ] テストの helper の写し: `internal/*/shared_helpers_test.go` が 8 パッケージで約 680 行、中身が一致する写しを持つ (`assertSanitizedAndVisible` 4 か所、
  `stubWorktreeStatus` / `newTestStatusView` / `applyStatusLoad` 3 か所)。写しどうしのずれを見る検査は無い。共有のテスト部品のパッケージへ寄せる
- [ ] 全画面の enum とキーの対応 (`app.CrossTarget` / `app.FullScreen*`) が最下層の internal/app にあり、画面を 1 つ足すたびに app と、app に依存する全画面が作り直される
  (上のキャッシュの「土台」の値)。trigger: 次に画面を足すとき
- [ ] no-real-commands の shim (`tests/glogx/test_no_real_commands_in_tests.sh`) が見るのは brew xcrun pgrep launchctl du docker の 6 つだけで、
  ssh・dscacheutil・dns-sd・networksetup・pbcopy を見ていない (issue 747 の進捗と retro 814 に「未対応」として記録されていたもの)
- [ ] tui.go / browseModel そのものは分割で減っていない (上の測定)。filer と lan の振り分け (`routeKeyTo*` / `drain*Notices` / `restore*Screen`) はほぼ同じコードの 2 本

## 進捗

- [x] 2026-10-18 起票と上の「直したもの」。glogx の go test -race (14 パッケージ)・make lint・check_comment_provenance・go_packages_index・
  no-real-commands・到達不能の検査が緑。テスト名は 1,254 → 1,255 (TestNoViewImportsAnotherView。TestRatelimitOptsPassesFetchNote を足して 1,256)
- [x] 2026-10-18 分割でずれたパス・古くなった文書を直した: 移したファイルをパス付きで指していた 11 ファイル (docs の 2 仕様書・open の issue 666・
  doctor / pro-con のコメント・hook のコメント・rules と rationale の実例) を今の置き場へ。docs のファイル名だけの参照 4 か所にパスを付け、
  docs に残っていた旧い型の名前 2 か所 (`usageOverlay.dismiss` / `diffOverlay`) を直した。lint と CI の設定は `**/` の glob で当たり続けていた。
  残したもの: docs/status-viewer-spec.md の画面の見本の図の中のファイル名 (例の git status の行で、参照ではない)・done の issue (書いた時点の記録)

## 反証レビュー

本文の事実は、起票の前に設計レビュー (opus) の指摘を実コードで裏取りしたもの (commands は上の各項)。別のレビューには通していない
