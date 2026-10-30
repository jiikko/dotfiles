# 1013 (bug): go_autobuild の再ビルドの判定が go:embed で焼き込んだファイルを見ない

起票日: 2026-10-31

## 概要

`bin/lib/go_autobuild.zsh` の入力集合 `_go_autobuild_inputs` (指紋 `_go_autobuild_fingerprint` の元) と、glogx の
`src/glogx/autobuild.go` の `autobuildSourceFile` は、`.go` (`_test.go` を除く) と go.mod / go.sum だけを入力に数える。
`//go:embed` で焼き込んだファイルだけを直すと、指紋が変わらないので古いバイナリが古い中身のまま起動する。

## 詳細

- 今 `go:embed` を使っているのは pro-con だけ (`src/pro-con/cardcmd.go` の `pm-guide.md` / `integrator-guide.md`、
  `src/pro-con/helpcmd.go` の `help/*.md`)。`bin/pro-con` は `go_autobuild_exec` で起動する
- 実測 (2026-10-31): `_go_autobuild_inputs src/pro-con` は 515 件 (`.go` 485 件 + go.mod / go.sum と replace 先)。`.md` は 0 件
- 効く場面: pm-guide.md や help の文だけを直した commit を pull しても、`pro-con` は直す前の案内を出し続ける。
  `.go` を 1 つでも触る commit が来るまで直らない
- zundamon-kaisetsu はこの穴を避けるために go:embed を使っていない (`src/zundamon-kaisetsu/internal/build/build.go` のテンプレートの
  コメント)。go_autobuild.zsh の `_go_autobuild_inputs` のコメントにも「go:embed を使うようになったら足すこと」とあるが、pro-con は足していない
- 未実測: pro-con で md だけを直したときに実際に古い案内が出たか (上は入力集合からの推論)

## 対応方針

入力集合に「go:embed が指すファイル」を足す。zsh と glogx の 2 つの定義を同じ規則で直す (どちらかだけ直すと、
shim は再ビルドしないのに glogx は古いと言う、またはその逆になる。`autobuildSourcesNewer` のコメント)。

- 規則: 入力の `.go` (`_test.go` を除く) の `//go:embed <pattern>...` の行を読み、各 pattern をその `.go` のディレクトリから展開する。
  ディレクトリに当たったらその下を再帰で全部、ファイルに当たったらそのファイル。展開して 0 件の pattern は無視する (go build が落とすので、
  ここで拾わなくても気づける)
- 起動のたびに走るので、速さを測る (今の指紋は 0.75ms。directive の行は .go を読んでいる段で拾えるか、grep 1 回で済むか)
- 採らない案:
  - モジュールの下の全ファイルを入力にする: バイナリと `.autobuild.*` が src_dir 直下にあり、README の編集でも再ビルドが走る
  - ビルド時に `go list -f '{{.EmbedFiles}}'` で一覧を記録する: 正確だが、embed したディレクトリへファイルを足しただけ
    (.go を触らない) の変化を拾えない。起動のたびに go list を回すのは遅い
- 入れたら、zundamon-kaisetsu の「go:embed で焼き込まない」のコメントと go_autobuild.zsh の注記を、今の事実に合わせて直す

## 受け入れ条件

- [ ] embed したファイルだけを変えると、zsh の指紋が変わる (tests/bin/test_go_autobuild.sh)
- [ ] 同じ変更で glogx の `autobuildSourcesNewer` も新しいと答える (src/glogx/autobuild_test.go)
- [ ] embed したディレクトリにファイルを足すと、両方とも新しいと答える
- [ ] 起動の経路の時間を前後で測って書く
- [ ] 2 つの定義が同じ集合を返すことを、同じ fixture で確かめる

## 関連ファイル

- `bin/lib/go_autobuild.zsh` (`_go_autobuild_inputs` / `_go_autobuild_fingerprint`)
- `src/glogx/autobuild.go` (`autobuildSourcesNewer` / `autobuildSourceFile`)
- `src/pro-con/cardcmd.go` / `src/pro-con/helpcmd.go`
- `src/zundamon-kaisetsu/internal/build/build.go`

## 進捗

- 2026-10-31 起票 (mac-lan-manager のラベルの二重管理を直したときのぼやきから。ユーザーの指示で issue 化して着手する)
