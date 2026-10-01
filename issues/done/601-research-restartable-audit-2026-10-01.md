# 601 (research): restartable の audit (codex のコードスキャン、2026-10-01)

起票日: 2026-10-01

## 範囲とやり方

- 対象: `src/restartable` (22 ファイル: Go 17 = 本番 9・テスト 8、その他) と、それを使う obaket の `macOS/bin/dev-fg-loop`・`dev-restart`・`dev-fg-loop-test` (3 本・728 行)。
  obaket 側の所見は obaket の issue に書いた (obaket 1009〜1011)
- 監査タイプ 19: security / resource-leaks / broken-code / error-handling / false-green / dead-code / performance / design / responsibility / duplication / encapsulation /
  leaky-abstraction / polymorphism / ui-components / test-cleanup / test-helpers / ux / dependency / general
- codex (gpt-6-luna、max、read-only) を 6 本 (タイプを束ねた lens)。ユーザーの指示で最初は直列、途中から最大 4 本並列。所見は Claude がコードで裏を取ってから採った
- 既知・記録済みの制約 (README の「検出しない形 / 注意」、[586](586-feat-restartable-foreground-restart-runner.md) / [588](588-feat-restartable-transition-dialog-and-ready-cmd.md) の記録) は再提出の対象外にした
- 反証レビュー (issue の起票ルールの codex) は通していない (所見そのものが codex の監査で、Claude が主要な主張をコードで確かめた。枠が近いとのユーザーの指示もあり省略)

## P2 (別の issue に起こした)

- [597](597-bug-restartable-control-lock-fd-inherited-by-children.md) — control の lock の fd が子に引き継がれ、runner が死んだ後もアプリが lock を握る (resource-leaks)
- [598](598-test-restartable-cli-exit-codes-untested.md) — status / restart の終了コードをテストしていない (false-green F5)
- [599](599-perf-restartable-process-history-grows-unbounded.md) — 終わったプロセスの記録が増え続ける (performance / resource-leaks / general の 3 本が独立に指摘)
- [600](600-refactor-restartable-control-requests-channel-exposed.md) — control.Server.Requests の露出 (encapsulation E4)

## P3 (ここで扱う。必要になったら個別に起こす)

- [x] **「起動」の段が描かれない** (broken-code): `actor.go` の `startRun` は段を「起動」に進めるが、その時点で描画せず、子の起動後に「起動の確認」へ進むか板を閉じてから描く。画面では段がビルドから確認へ飛ぶ。
  発火条件: 起動・再起動のたびに。推奨: 子を起動する前に描画する。コードの追跡で確認 (実画面では未確認)
- [x] **未使用のイベント・欄** (dead-code): `model.go` の `ControlStatusEvent` / `ControlStatusEffect` は model にあるが actor から送られない (status は `actor.handleControl` が直接応答する)。
  `Event.ExitCode` も読み書きが見当たらない (codex の報告。Claude は ControlStatus* の参照を grep で確認)。推奨: 消すか、実際の経路に寄せる
- [x] **ログの上限で詰め直しが重い** (performance): `output.go` の `OutputBuffer.AddLine` は、上限 2,000 行に達した後、落とす行ごとに残りを `copy` で詰め直す。大量ログのバーストで余分な CPU。推奨: リングバッファ。未計測
- [x] **重複** (duplication): build-failed からの再ビルドで、状態・メッセージ・段のリセットが `updateKey` と `updateControlRestart` の 2 か所にある / 出力の書き込み失敗で書くのをやめて 1 回だけ知らせる処理が
  `output.go` の `logSink.writeOutput` と `presenter.go` の `discardWriteErrors.Write` の 2 か所にある。発火条件: 片方だけ直すと TTY と非 TTY で挙動がずれる。推奨: 共通の関数へ
- [x] **狭い端末の確認ダイアログ** (ui-components): 板を閉じた後 (起動の確認が済んだ後) の確認ダイアログは `confirm.Dialog` を通り、`layout.Panel` が最小 10 桁にするので、幅 1〜9 桁の端末ではみ出す
  (板の間の確認は 588 で 1 行表示にした)。推奨: 幅に応じた描画を 1 か所に寄せる
- [x] **狭い端末の確認が英語** (ux): 幅 10 桁未満の 1 行表示は `quit? y/n` / `rst? y/n`。周りの案内は日本語。推奨: その幅に収まる短い日本語
- [x] **ターミナルでない起動で、子の出力を無害化せずに端末へ流す** (security): stdin をリダイレクトし stdout を端末につないで起動すると非 TTY のモードになり、子の出力を素通しする
  (TTY の UI では termsafe を通す)。子の出力に外から来た文字列 (クラウドのファイル名など) が載ると、端末の制御列を流しうる。codex は P2 としたが、置き換える前の bash の dev-fg-loop も
  アプリの出力をそのまま端末へ流していたので悪化ではない (status quo)。推奨: UI の有無と出力先が端末かを別々に判定し、端末へ出すなら無害化する
- [x] **--control に他人が書けるディレクトリを渡すと socket を差し替えられる** (security): 明示した `--control` の親ディレクトリの所有者・権限を検査しない。既定の置き場所 (自分だけの 0700 の dir) を使う
  obaket は該当しない。推奨: 親ディレクトリを検査するか、README に条件を書く
- [x] **TestCloseWhileRequestsArePendingDoesNotPanic が送信待ちに着いたかを確かめていない** (test-cleanup): Close が先に進むと競合を通らない。直す前は -count=20 で panic を出したので効いてはいるが、
  確率に頼っている。推奨: serveConn が送信待ちに着いたことを同期してから Close する
- [x] **一時ディレクトリの準備の重複** (test-helpers): `integration_test.go` で `os.MkdirTemp("/tmp", prefix)` と削除の登録が 12 か所。推奨: prefix を取る helper に

## 0 件だったタイプと攻めた範囲 (次の監査の起点)

- responsibility: actor / model / control / UI の境界と、obaket の bash の変更理由・状態の依存を見た。actor は依存を束ねるが、状態遷移・socket・描画は分かれていて責務集中とは判定しなかった
- leaky-abstraction: `runner.Presenter` と bubbletea の実装の境界、control の要求・応答と JSON、obaket 側の CLI と health の照合。`Start` / `Println` は能力を確かめてから呼ぶ
- polymorphism: `Model.Update`・`actor.handleControl`・板の種類と段の分岐。同じ種別の振る舞いを複数箇所で実装している候補は無かった
- dependency: go.mod の 19 件はすべて版を固定、直接依存はすべて参照されている。`golang.org/x/sys` の公開 advisory (GO-2026-5024) は Windows 向けで対象版より新しい v0.46.0 を使っている。govulncheck は未実行
- false-green の F1〜F4: 0 件 (F5 の 1 件は 598)

## 判断 (2026-10-01)

P2 の 4 件は個別の issue で直す (597 が最優先: runner が死んだ後に起動し直せなくなる)。P3 は、restartable を次に触るとき (再設計の [596](../pending/596-design-restartable-redesign-candidates.md) を含む) にまとめて扱う。

## 進捗 (2026-10-01、P3 の 10 項目)

commit: 3b477d29 (テストの helper と同期) / 184e3556 (未使用と再ビルドの重複) / 75c43c13 (無害化・起動の段・狭い端末・ログの上限・書き込み失敗の共通化) /
faa62202 (再ビルドの板のテスト) と、この記録の commit。push はしていない (597→601 の順に取り込む)。

### 項目ごとの決定

- **起動の段**: 直した。`actor.startRun` が `LaunchStartedEvent` の直後、`startProcess` (exec の成否まで待つ) の前に描画する。
  テスト `TestLaunchStageReachesPresenterBeforeChildStarts` (build あり / なし)。🚨 描画を presenter に渡すまでで、画面に出る保証ではない
  (`Presenter.Render` は `program.Send` するだけで、exec がすぐ済めば次のフレームの前に段が進む。codex の設計レビューの P3。exec が待つとき
  — 作り直した実行ファイルの初回検査など — には見える)。実画面では未確認
- **未使用**: 消した (`ControlStatusEvent` / `ControlStatusEffect` / `Event.ExitCode` と model_test.go の参照 2 箇所)
- **ログの上限**: 直した。先頭行を別持ちし、tail の先頭を切り落とす (copy で詰めない)。`BenchmarkOutputBufferAddLineAtCapacity` (上限 2,000 行に
  達した後の 1 行): 353 / 352 / 353 ns/op → 10.2 / 9.3 / 9.3 ns/op (M3 Max、-count 3、他の 4 体が並行して走っている中)。同じ処理の死に分岐 (firstSeen) も消した。
  byte の上限と Drain 後の再利用は `TestOutputBufferByteBoundKeepsLongFirstLineAndIsReusableAfterDrain` で固定
- **重複**: 2 つとも寄せた。再ビルドは `model.rebuildAfterFailure`、書き込み失敗は `runner.FailOnceWriter` (logSink と ui の `discardWriteErrors` が使う。
  `Close` は元の writer を閉じない: stdout のため。codex の設計レビューの P3)。板を開き直す部分は寄せる前から無検査だった (下の変異) ので
  `TestRebuildFromBuildFailedIsTheSameForKeyAndControl` を足した
- **狭い端末の確認ダイアログ** / **英語**: 直した。幅 10 桁未満では板の外の確認も 1 行 (`compactConfirmLine`)。確認と段の 1 行を日本語にし、
  入らない幅は ASCII を切り詰める (`fitFirst`)。描いた行 (テストで固定): 幅 9 `終了y/n` / `再起動y/n`、幅 6 `y/n` (再起動は 8 まで)、幅 1 `y`。
  段は `終了` / `ビルド` / `起動` / `確認` (幅 1 は `s` / `b` / `l` / `r`)
- **非 TTY の無害化**: 直した。`Config.StdoutIsTerminal` (main.go が渡す) で、判定は `logSink.CopyFrom` の 1 か所。端末でない出力先だけ byte のまま、
  端末へ行くものは TTY と同じ行分割と `safeLine` を通し、UI なしでは 1 行ずつすぐ書く (上限で落とさない)。改行の無い末尾は改行か EOF まで出ない
  (README に書いた)。テスト `TestHeadlessOutputIsSanitizedOnlyWhenStdoutIsTerminal` (端末 / パイプ)
- **--control の親ディレクトリ**: README の「検出しない形 / 注意」に条件を書いた (検査は足していない。597 が `control.Listen` を触っているため)
- **TestCloseWhileRequestsArePendingDoesNotPanic**: 直した。goroutine の stack から、この Server の `serveConn` 8 本が select で止まったのを見てから
  Close する (production に差し込み口を足さない。600 が Requests の型を変えるので、テストは Requests に触れない)。反復 50 → 3
- **一時ディレクトリの準備**: 12 箇所を `shortTempDir(t, prefix)` に寄せた

### 変異 (mutate-verify-list、11 本)

rc 0 (想定のテストだけが red): 起動の段の描画を外す / 端末でも素通しする / 狭い端末で Dialog に戻す / 確認を英語に戻す / 段を英語に戻す /
acceptLoop で Requests を閉じる (`panic: send on closed channel`) / byte の上限を外す / `FailOnceWriter` の failed を立てない (runner と ui の
両方のテストが red = 寄せた効果) / 再ビルドで板を開かない / 再ビルドで結果を消さない。
rc 6 (緑) が 1 本: 再ビルドで板を開かない変異が、寄せた直後は既存のどのテストでも緑だった → テストを足して rc 0。
**未検査のまま**: main.go が `StdoutIsTerminal` を渡す配線 (渡さない変異を当てるテストが無い。598 の CLI テストに寄せられるか、取り込み時に見る)

### codex

- 設計の反証 (gpt-6-luna、read-only): P3 2 件、両方採用 (起動の段は描画の保証にならない → 記録だけ / `Close` を委譲しない → no-op を保った)
- 実装レビュー 通常 (base 951051bb..HEAD): 指摘なし
- 敵対的 (effort high): 再現する破壊なし。未確認リスク 1 件 (確認の 1 行の期待値を一部の幅しか固定していない) を採用し、幅 1〜9 を全部固定した。
  codex は read-only の sandbox で `go test` を動かせていない (静的な確認のみ)

### 検証

`go test -race -count=1 ./...` rc=0 (4 package ok)。`scripts/golangci_lint.sh v2.5.0 run --max-same-issues 0 --max-issues-per-linter 0 ./...` rc=0、0 issues。
- 取り込み (dotfiles-58): 597 → 600 の後に cherry-pick。control_test.go で 597 のテストと 601 の補助関数 (waitForBlockedServeConns) が同じ場所に足されて衝突したので両方残した。go test -race と lint (上限なし) は 0 issues。
- 残り (601 の担当の報告): main.go が StdoutIsTerminal を渡すことを守るテストは無い (外す変異が緑)。CopyFrom の headless 引数は sink.headless と常に同じで冗長 (触っていない)
