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

| C. 広い一致は残し、Swift のライブラリを持たないと分かっている実行ファイル (make・gnumake・git・git-*) だけを外す | make や git が動いていても消える。make が起こす xcodebuild・swift、git を起こす swift-package は自分の名前で拾われる | 外した名前のプロセスの守りだけ (CommandLineTools の make・git は `otool -L` で libSystem・CoreFoundation などにしかリンクしていない。2026-10-22 に確認)。別の長く動くツールが原因になったら、名前を足す必要がある |

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
- 2026-10-22: ユーザーが案 C に決めた (B より、失うものの列挙がほぼ自明なため)

## 関連ファイル

- `src/tmpclean/sweep.go` (`swiftToolCommand`・`collectSwiftProcs`)・`src/tmpclean/sweep_test.go` (`TestSwiftToolCommand`)
- `src/tmpclean/README.md` の sweep の節
- `~/.cache/dotfiles-tmpdir-sweep/last.json` の `limit` (どのプロセスが範囲を決めたか)

## 進捗

- [x] 起票と実測 (2026-10-21)
- [x] make を止めた後の sweep で残りが消えたかを `sweep.log` / `last.json` で確かめる
  - 2026-10-29 の観測: `$TMPDIR` の `TemporaryDirectory.*` は 0 件 (`ib8TCh`・`hYWpyC` とも無い)。`sweep.log` は
    `10-23 14:50 removed=0` → `10-24 19:19 removed=1` (`ib8TCh` が 3 日を越えた 10-23 21:33 の後の最初の回) →
    `10-25 19:41 removed=1` (`hYWpyC` が 3 日を越えた 10-25 19:23 の直後の回) → `10-26 20:04 removed=10`。ゴミ箱に `TemporaryDirectory` は無い
  - 確かめきれなかったこと: `last.json` は毎回上書きされ (残っているのは 10-28 の空の回)、10-24・10-25 に消えた 1 件が
    どの dir かを名前で突き合わせられない。その時刻に長く動く make / git が居たかも分からないので、「make が居ても範囲を決めない」の本番での観測は取れていない
    (修正そのものは `TestSweepIgnoresLongRunningMake` と変異 ① が守る)。再開の trigger: obaket の dev ループが動いている日の sweep で
    `last.json` の `limit` が make を指していたら open に戻す
  - 2026-10-22 19:50 の観測 (dotfiles-0e。読むだけ): 最後の sweep は 11:06 (`removed=0`、`kept: 新しい 1`) で、修正 (2ce152af、17:42) より前の版。
    修正の後の sweep はまだ走っていない (hook は 1 日 1 回。`stamp` は 11:06)。$TMPDIR の `TemporaryDirectory.*` は 2 件:
    `ib8TCh` (10-20 21:33) と `hYWpyC` (10-22 19:23)。どちらも `-days 3` に届かないので、今 sweep しても「新しい」で残り、確かめにならない
  - 確かめ方: 10-23 21:33 より後に走った sweep の `last.json` で、`ib8TCh` が `removed` に入るか (入らなければ `kept` の理由と `limit` を見る)。
    この時刻に CommandLineTools の make (他のセッションの make test) が動いていても範囲を決めていないことが、修正の効き目の観測になる
- [x] 案を決める (ユーザー。2026-10-22 に案 C)
- [x] 案 C の実装 (`src/tmpclean/sweep.go` の `notSwiftTool`・`isSwiftTool`)。判定は実行ファイル (コマンド行の先頭の語) だけを見る。
  先頭の語に空白を含むパス (`/Applications/Xcode 16.app/...`) では当たらず、今までどおり守る側に倒れる
- [x] テスト: `TestSwiftToolCommand` に拾う / 拾わない例を足した (引数に `/git` を含む xcodebuild は拾う)。
  `TestSweepIgnoresLongRunningMake` で sweep の入口から通した (40 日動いている make は範囲を決めず、範囲を決めるのは次に古い `swift build`)
- [x] 変異 (`mutate-verify`。3 本とも想定のテストが red): ①呼び出しを旧の `swiftToolCommand.MatchString` に戻す → `TestSweepIgnoresLongRunningMake`
  ②除外の `^([^ ]*/)?` の固定を外す → `TestSwiftToolCommand` ③除外を外す → 両方
- [x] 敵対的レビュー (opus、読み取りのみ): 守りの穴は作れなかった (P1・P2 なし)。CommandLineTools の git*・make・`libexec/git-core` の 181 本に
  `otool -L` を当てて Swift へのリンクは 0 本 (同じ判定で `swift-build` は当たる)。子 (xcodebuild・swift・xcrun 経由) は自分の実行ファイルの名前で拾われる。
  P3 (今回の変更が作った穴ではない): 別名の symlink (`sb -> swift-build`) や SwiftPM を組み込んだ自作の道具は前から拾えない。
  これまでは長く動く make が偶然守っていた。README の「検出しないもの」の範囲 (3 日と lsof だけが守る) なので、対応しない。
  Xcode.app の中の make・git は、この機械に Xcode.app が無いので `otool -L` が未確認
