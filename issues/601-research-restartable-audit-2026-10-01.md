# 601 (research): restartable の audit (codex のコードスキャン、2026-10-01)

起票日: 2026-10-01

## 範囲とやり方

- 対象: `src/restartable` (22 ファイル: Go 17 = 本番 9・テスト 8、その他) と、それを使う obaket の `macOS/bin/dev-fg-loop`・`dev-restart`・`dev-fg-loop-test` (3 本・728 行)。
  obaket 側の所見は obaket の issue に書いた (obaket 1009〜1011)
- 監査タイプ 19: security / resource-leaks / broken-code / error-handling / false-green / dead-code / performance / design / responsibility / duplication / encapsulation /
  leaky-abstraction / polymorphism / ui-components / test-cleanup / test-helpers / ux / dependency / general
- codex (gpt-6-luna、max、read-only) を 6 本 (タイプを束ねた lens)。ユーザーの指示で最初は直列、途中から最大 4 本並列。所見は Claude がコードで裏を取ってから採った
- 既知・記録済みの制約 (README の「検出しない形 / 注意」、[586](done/586-feat-restartable-foreground-restart-runner.md) / [588](done/588-feat-restartable-transition-dialog-and-ready-cmd.md) の記録) は再提出の対象外にした
- 反証レビュー (issue の起票ルールの codex) は通していない (所見そのものが codex の監査で、Claude が主要な主張をコードで確かめた。枠が近いとのユーザーの指示もあり省略)

## P2 (別の issue に起こした)

- [597](597-bug-restartable-control-lock-fd-inherited-by-children.md) — control の lock の fd が子に引き継がれ、runner が死んだ後もアプリが lock を握る (resource-leaks)
- [598](598-test-restartable-cli-exit-codes-untested.md) — status / restart の終了コードをテストしていない (false-green F5)
- [599](599-perf-restartable-process-history-grows-unbounded.md) — 終わったプロセスの記録が増え続ける (performance / resource-leaks / general の 3 本が独立に指摘)
- [600](600-refactor-restartable-control-requests-channel-exposed.md) — control.Server.Requests の露出 (encapsulation E4)

## P3 (ここで扱う。必要になったら個別に起こす)

- [ ] **「起動」の段が描かれない** (broken-code): `actor.go` の `startRun` は段を「起動」に進めるが、その時点で描画せず、子の起動後に「起動の確認」へ進むか板を閉じてから描く。画面では段がビルドから確認へ飛ぶ。
  発火条件: 起動・再起動のたびに。推奨: 子を起動する前に描画する。コードの追跡で確認 (実画面では未確認)
- [ ] **未使用のイベント・欄** (dead-code): `model.go` の `ControlStatusEvent` / `ControlStatusEffect` は model にあるが actor から送られない (status は `actor.handleControl` が直接応答する)。
  `Event.ExitCode` も読み書きが見当たらない (codex の報告。Claude は ControlStatus* の参照を grep で確認)。推奨: 消すか、実際の経路に寄せる
- [ ] **ログの上限で詰め直しが重い** (performance): `output.go` の `OutputBuffer.AddLine` は、上限 2,000 行に達した後、落とす行ごとに残りを `copy` で詰め直す。大量ログのバーストで余分な CPU。推奨: リングバッファ。未計測
- [ ] **重複** (duplication): build-failed からの再ビルドで、状態・メッセージ・段のリセットが `updateKey` と `updateControlRestart` の 2 か所にある / 出力の書き込み失敗で書くのをやめて 1 回だけ知らせる処理が
  `output.go` の `logSink.writeOutput` と `presenter.go` の `discardWriteErrors.Write` の 2 か所にある。発火条件: 片方だけ直すと TTY と非 TTY で挙動がずれる。推奨: 共通の関数へ
- [ ] **狭い端末の確認ダイアログ** (ui-components): 板を閉じた後 (起動の確認が済んだ後) の確認ダイアログは `confirm.Dialog` を通り、`layout.Panel` が最小 10 桁にするので、幅 1〜9 桁の端末ではみ出す
  (板の間の確認は 588 で 1 行表示にした)。推奨: 幅に応じた描画を 1 か所に寄せる
- [ ] **狭い端末の確認が英語** (ux): 幅 10 桁未満の 1 行表示は `quit? y/n` / `rst? y/n`。周りの案内は日本語。推奨: その幅に収まる短い日本語
- [ ] **ターミナルでない起動で、子の出力を無害化せずに端末へ流す** (security): stdin をリダイレクトし stdout を端末につないで起動すると非 TTY のモードになり、子の出力を素通しする
  (TTY の UI では termsafe を通す)。子の出力に外から来た文字列 (クラウドのファイル名など) が載ると、端末の制御列を流しうる。codex は P2 としたが、置き換える前の bash の dev-fg-loop も
  アプリの出力をそのまま端末へ流していたので悪化ではない (status quo)。推奨: UI の有無と出力先が端末かを別々に判定し、端末へ出すなら無害化する
- [ ] **--control に他人が書けるディレクトリを渡すと socket を差し替えられる** (security): 明示した `--control` の親ディレクトリの所有者・権限を検査しない。既定の置き場所 (自分だけの 0700 の dir) を使う
  obaket は該当しない。推奨: 親ディレクトリを検査するか、README に条件を書く
- [ ] **TestCloseWhileRequestsArePendingDoesNotPanic が送信待ちに着いたかを確かめていない** (test-cleanup): Close が先に進むと競合を通らない。直す前は -count=20 で panic を出したので効いてはいるが、
  確率に頼っている。推奨: serveConn が送信待ちに着いたことを同期してから Close する
- [ ] **一時ディレクトリの準備の重複** (test-helpers): `integration_test.go` で `os.MkdirTemp("/tmp", prefix)` と削除の登録が 12 か所。推奨: prefix を取る helper に

## 0 件だったタイプと攻めた範囲 (次の監査の起点)

- responsibility: actor / model / control / UI の境界と、obaket の bash の変更理由・状態の依存を見た。actor は依存を束ねるが、状態遷移・socket・描画は分かれていて責務集中とは判定しなかった
- leaky-abstraction: `runner.Presenter` と bubbletea の実装の境界、control の要求・応答と JSON、obaket 側の CLI と health の照合。`Start` / `Println` は能力を確かめてから呼ぶ
- polymorphism: `Model.Update`・`actor.handleControl`・板の種類と段の分岐。同じ種別の振る舞いを複数箇所で実装している候補は無かった
- dependency: go.mod の 19 件はすべて版を固定、直接依存はすべて参照されている。`golang.org/x/sys` の公開 advisory (GO-2026-5024) は Windows 向けで対象版より新しい v0.46.0 を使っている。govulncheck は未実行
- false-green の F1〜F4: 0 件 (F5 の 1 件は 598)

## 判断 (2026-10-01)

P2 の 4 件は個別の issue で直す (597 が最優先: runner が死んだ後に起動し直せなくなる)。P3 は、restartable を次に触るとき (再設計の [596](pending/596-design-restartable-redesign-candidates.md) を含む) にまとめて扱う。
