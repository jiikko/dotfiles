# 854 (design): tmpclean sweep の「Swift のツール」の拾い方が広く、Xcode の中の make が動いている間は消えない

起票日: 2026-10-21

## 概要

`tmpclean sweep` (851) は、$TMPDIR の空の `TemporaryDirectory.*` のうち「動いている Swift のツールのうち一番古いものの起動より前に作られた」
ものだけを消す。Swift のツールかどうかを決める `swiftToolCommand` (`src/tmpclean/sweep.go`) が Xcode.app の中の全部 (`/Xcode[^/]*\.app/`)・
`/Toolchains/`・`/CommandLineTools/` の配下を拾うので、ArgsResolver を使わない make や git が長く動いていると、その起動から後のものが消えない。
拾う範囲を絞るか (絞るならどこまでか) を決める。

## 実測 (2026-10-21 23:09、hook の入口から本物の環境で 1 回)

- removed=62,599・kept 42,281 (理由は全部「動いている Swift のツールの起動より後に作られた」)・failed=0・rc=0
- 範囲を決めたのは `pid 78459 (2026-10-10 00:03:14 から): /Applications/Xcode.app/Contents/Developer/usr/bin/make dev-fg-loop`
  (`~/.cache/dotfiles-tmpdir-sweep/last.json` の `limit`)
- 敵対レビュー (851 の実装時) も P3 で同じ形を挙げていた (Xcode の make・CommandLineTools の git も拾う)
- この make は obaket の `make dev-fg-loop` (cwd `~/src/my-products/apps/obaket/macOS`。子は `restartable ... --build make ... dev-fg-build`)。
  待っている間の make 自身は dir を作らず、片付けの範囲を止めているだけ。再起動のたびのビルドは作り手になりうる
  (xcodebuild でパッケージを読む段は 1 回 2 件残した。851 の実測。dev-fg-build 自体は測っていない)

## 選択肢

| 案 | 何が起きるか | 今の挙動から失うもの |
|---|---|---|
| A. 今のまま | make が止まるか再起動すれば残りも消える | なし。長く動く make / git がある間は、その起動から後の分が溜まり続ける |
| B. ArgsResolver を使うものだけに絞る (Xcode 本体 `Contents/MacOS/Xcode`・xcodebuild・SWBBuildService・XCBBuildService・SourceKitService・sourcekit-lsp・`swift*`) | make や git が動いていても消える | Xcode.app / Toolchains / CommandLineTools の中にある、列挙していない名前で SwiftPM を使うプロセスの守り (例: Interface Builder の `Contents/Developer/usr/bin/ibtoold`。2026-10-21 に動いていた。ArgsResolver を使うかは未確認。3 日より古いことと lsof の守りは残る) |

B にするなら、変える前に「`/Xcode[^/]*\.app/` 等の広い一致が副次的に守っていたもの」を列挙する (`list-masked-failure-modes-before-removing-guard.md`)。
例えば Xcode.app の中で SwiftPM を自分の中に持つプロセスの名前を、`ps` の実物と Xcode.app の中のバイナリ (`otool -L` で SwiftPM / SwiftDriver の
framework を読むもの) から集める。テスト (`TestSwiftToolCommand`) の拾う / 拾わないの表と、変異・敵対レビューまで要る (安全機構を緩める変更)。

## 状態

- 2026-10-21: ユーザーが make (dev-fg-loop) を一度止める予定。止めた後の sweep (次の日の SessionStart) で残りが消えるかを見る
- 2026-10-22: 同じ形が再発した。09:30 の sweep は removed=6,426・kept 15,174 (「動いている Swift のツールの起動より後に作られた」14,853 /
  「中身がある」321)・rc=0。範囲を決めたのは `pid 71058 (2026-10-15 22:34:50 から): /Applications/Xcode.app/Contents/Developer/usr/bin/make dev-fg-loop`
  (10/10 の pid 78459 とは別。止めた後に再び起動されていた)。同じ日に cleanup-tmp-dir skill の `tmpclean apply` が、3 日より古い
  `TemporaryDirectory.*` 8,225 件を含む 23,362 件をゴミ箱へ移した (plan eb4171aba07c4648)。11:40 ごろユーザーが obaket を止め、
  pid 71058 が居ないことを `ps -p 71058` (rc=1) で確かめた。次の日の sweep で残りが消えるかを見る。dev ループは止めてもまた起動されるので、
  案 A のままだと同じ形が繰り返す
- 決めるのはユーザー (起票の時点では「よく分かっていない。とりあえず issue に」)

## 関連ファイル

- `src/tmpclean/sweep.go` (`swiftToolCommand`・`collectSwiftProcs`)・`src/tmpclean/sweep_test.go` (`TestSwiftToolCommand`)
- `src/tmpclean/README.md` の sweep の節
- `~/.cache/dotfiles-tmpdir-sweep/last.json` の `limit` (どのプロセスが範囲を決めたか)

## 進捗

- [x] 起票と実測 (2026-10-21)
- [ ] make を止めた後の sweep で残りが消えたかを `sweep.log` / `last.json` で確かめる
- [ ] A か B かを決める (ユーザー)
