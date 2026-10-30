# 1013 (bug): go_autobuild の再ビルドの判定が go:embed で焼き込んだファイルを見ない

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-31〜）

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
  - pro-con の入れ替え (`src/pro-con/upgrade/upgrade.go` の Spawn → `go_autobuild_spawn_if_stale`。画面の ctrl+r と dispatcher の自動の入れ替え) も
    同じ入力集合で判定するので、md だけの変更では dispatcher が入れ替わらず、新しく起こす PM / 取り込みの係に古い `pm-guide.md` /
    `integrator-guide.md` を渡し続ける (`dispatchercmd.go` が渡す)
- pro-con は md を実行時にディスクから読まない (`pmGuide` / `integratorGuide` / `helpFS.ReadFile` は embed)。他に救う経路も無い
  (glogx の `autobuildStaleBinary` は glogx 自身のバイナリだけを見る。反証レビューで確認)
- zundamon-kaisetsu はこの穴を避けるために go:embed を使っていない (`src/zundamon-kaisetsu/internal/build/build.go` のテンプレートの
  コメント)。go_autobuild.zsh の `_go_autobuild_inputs` のコメントにも「go:embed を使うようになったら足すこと」とあるが、pro-con は足していない
- 未実測: pro-con で md だけを直したときに実際に古い案内が出たか (上は入力集合からの推論)

## 対応方針

ビルドに成功したときに、go 自身に embed の pattern を出させて記録し、起動のたびの指紋ではその pattern を展開したファイルを入力に足す。
directive の構文解析 (引用符・`all:`・build tag・実際にリンクされるパッケージに限る) は go が解くので、こちらで書かない。

- 記録: ビルドの後に `go list -deps -f '{{.Dir}} {{.EmbedPatterns}}' <pkg>` (module の外の標準ライブラリなどは捨てる) を
  `.autobuild.built` の隣に書く。pattern は .go を変えない限り変わらず、.go を変えればビルドし直しになって記録も取り直される
- 展開 (起動のたび): `all:` を外して pattern を package のディレクトリから glob で展開する。dot ファイルも拾う (余分に拾うのは
  再ビルドが増えるだけで無害、拾い損ねると黙って古いまま)。ディレクトリに当たったらその下を再帰で全部
- 🚨 展開して 0 件の pattern は stale として扱う (go build は通ったのに展開が 0 件 = こちらの展開の誤り。黙って取りこぼさない)
- 初回 (記録が無い) は、記録が無いこと自体を入力の違いとして扱う (記録が書かれた後の指紋と食い違って 1 回ビルドし直す形を許すか、
  ビルドの段で記録を書いてから指紋を取る)。実装でビルドの流れを読んで決める
- `_go_autobuild_record_rev` の `git status -- '*.go' go.mod go.sum` も入力集合の定義の 1 つ (診断用の `.autobuild.rev`)。同じ規則に揃えるか、
  揃えない理由を書く
- glogx 側 (`autobuildSourceFile`): glogx と replace 先の doctor には今 embed が無いので、直しても動きは変わらない。直す代わりに
  「glogx の木に go:embed が入ったら落ちる」テストで非対称を守るか、同じ記録を読むかを実装で決める
- 起動のたびに走るので、指紋の時間を前後で測る (今は 0.75ms)
- 採らない案:
  - モジュールの下の全ファイルを入力にする: バイナリと `.autobuild.*` が src_dir 直下にあり、README の編集でも再ビルドが走る
  - directive を zsh と Go で自前で構文解析する: `all:`・引用符・dot ファイルの扱いを取り違えると、go build は通ったまま黙って 0 件になる
  - ビルド時に解決済みのファイルの一覧 (`EmbedFiles`) だけを記録する: embed したディレクトリへファイルを足しただけ (.go を触らない) の変化を拾えない
- 入れたら、zundamon-kaisetsu の「go:embed で焼き込まない」のコメントと go_autobuild.zsh の `_go_autobuild_inputs` の注記を今の事実に合わせて直す

## 受け入れ条件

- [x] embed したファイルだけを変えると指紋が変わる (tests/bin/test_go_autobuild.sh)
- [x] embed したディレクトリにファイルを足す・embed したファイルを消すと指紋が変わる
- [x] `all:` の pattern・dot ファイル・replace 先の module の embed の fixture
- [x] 展開して 0 件の pattern を黙らせない (方針を変えた: stale にし続けると backoff が効かず pro-con の入れ替えが毎周「新版あり」になるので、起動のたびに stderr で知らせる)
- [x] pro-con の入れ替えの経路 (`go_autobuild_spawn_if_stale`) も、md だけの変更で入れ替えると答える
- [x] glogx 側の扱いを決めて、その決めたことをテストで守る (glogx と replace 先に embed が無いことをテストで守り、入ったら直す場所を出す)
- [x] 起動の経路の指紋の時間を前後で測って書く (下の進捗)

## 関連ファイル

- `bin/lib/go_autobuild.zsh` (`_go_autobuild_inputs` / `_go_autobuild_fingerprint`)
- `src/glogx/autobuild.go` (`autobuildSourcesNewer` / `autobuildSourceFile`)
- `src/pro-con/cardcmd.go` / `src/pro-con/helpcmd.go`
- `src/zundamon-kaisetsu/internal/build/build.go`

## 進捗

- 2026-10-31 起票 (mac-lan-manager のラベルの二重管理を直したときのぼやきから。ユーザーの指示で issue 化して着手する)
- 2026-10-31 反証レビュー (opus、読み取り): P1 なし。中心の主張 (embed は pro-con だけ・入力 515 件で md 0 件・md を実行時に読まない) は反証できず。
  採った指摘: 入れ替えの経路の影響 / 0 件を無視すると黙って取りこぼす / go list で pattern を記録する案 / `_go_autobuild_record_rev` も定義の 1 つ /
  glogx 側は今は動きが変わらない / 受け入れ条件の抜け (消す・all:・replace 先)。
  受け入れ条件から外した指摘: glogx の mtime の順序の判定は cp -p / rsync -a で足したファイルや消したファイルを拾えない
  (既存の性質。glogx は今 embed を持たないので、この issue では扱わない)
- 2026-10-31 実装と 5 周の敵対的レビュー (opus)。commit (subject):
  - fix(go_autobuild): //go:embed で焼き込んだファイルも再ビルドの判定の入力に数える
  - fix(go_autobuild): go list を HUP に巻き込まない・落ちたらビルドの失敗にする・( ) を含む pattern を字どおりに展開する
  - fix(go_autobuild): 開始時点の記録を go list の前に戻し、否定のクラスと \ のエスケープを Go と同じに展開する
  - fix(go_autobuild): embed の pattern を 1 字ずつ zsh の pattern に写し、\ のエスケープも Go と同じにする
  - fix(go_autobuild): embed の pattern の写しをクラスの中と外で分け、呼び出し側の option と locale に左右されないようにする
  - fix(go_autobuild): ^ を上端にした範囲を Go と同じに展開し、replace 先の embed を守る
  - 形: ビルドの前に `go list -deps -f '{{.Dir}} {{.EmbedPatterns}}'` で pattern を `.autobuild.embeds` に記録 (module と replace 先の外は捨てる)、
    起動のたびにその pattern を Go の path.Match と同じ意味で zsh に写して展開し、指紋に足す。記録の有無も入力に数える (前の版のバイナリは 1 回だけ作り直す)。
    go list は trap '' HUP の届く foreground で走らせ、落ちたらビルドの失敗にする
  - 実測 (src/pro-con の指紋、50 回の平均を 3 回): 前 24.8〜25.3ms / 後 25.2〜26.6ms、入力は 515 → 522 件
  - pattern の展開を go1.26.0 の go list と突き合わせ: 18 種の pattern × 既定 / LC_ALL=C / shwordsplit / extendedglob で 83 件一致
  - 敵対的レビューで実験で再現して直したもの: go list がコマンド置換で HUP に死ぬ (P1) / ( ) の誤展開 / 開始時点の記録の位置 / 否定のクラス /
    \ のエスケープ / クラスの中の \! \- / 字としての \[ / 呼び出し側の locale・option / ^ を上端にした範囲
  - 直さない (コード `_go_autobuild_go_glob` の上に書いた。どれも黙らない): [\]-^] は 0 件で知らせる / NFD の字と (b) が quote する字が同じ階層の名前に並ぶと
    0 件で知らせる / **/x は Go より多く拾う / go list と指紋の間に .go を保存して新しい embed を足すと、次に .go を触るまでその embed 先を見逃す
    (窓は go list の所要時間、バイナリは正しい)
  - 未実測: 本物の pro-con で md だけを直して pull したときに作り直されること (この変更が master に入った後の最初の pull で確かめられる)
