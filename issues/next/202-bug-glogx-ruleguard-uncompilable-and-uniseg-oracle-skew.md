# 202 bug: ruleguard のルールが型検査されない / 幅テストのオラクルが本番と別の Unicode 版

起票日: 2026-09-03
出典: `/audit` の dependency (direct、2026-09-03。src/glogx 限定)
重要度: **P2** (どちらも「検査が静かに効かなくなる」形)

**2 件とも main agent が独立に再現した。**

## 発見 1: `ruleguard.rules.go` はどのビルド構成でもコンパイルできない

`go.mod` の direct require にある `github.com/quasilyte/go-ruleguard/dsl v0.3.23` は
`src/glogx/ruleguard.rules.go` からのみ import される。しかしこのファイルは
`//go:build ruleguard` + `package gorules` で、同ディレクトリの他は全部 `package main`。

**実測 (2026-09-03)**:

```
$ go vet -tags ruleguard ./
found packages main (action_modal.go) and gorules (ruleguard.rules.go) in /Users/koji/dotfiles/src/glogx
```

tag 無しでは build 対象外、tag を立てるとパッケージ名衝突でロード不能。`.golangci.yml` に
`build-tags` の指定は無く、CI にも tag を渡す箇所は無い。ルール本文は golangci-lint の gocritic が
`rules: ruleguard.rules.go` (`.golangci.yml:108`) で**データとして読む**だけで、型検査は
golangci-lint 同梱の go-ruleguard が行う。

### 発火条件

- **`ruleguard.rules.go` に型エラー・API 誤用を書いても、`make test` / `make lint` の Go
  コンパイルは一切通らない**。気づけるのは gocritic が「ルールのロードに失敗した」と言うときだけ。
  `.golangci.yml:157-158` のコメントによれば**過去にルールが無言で消えた前科がある**
- **`go.mod` の `dsl v0.3.23` を上げ下げしても lint 結果は一切変わらない**。実際に効くのは
  golangci-lint v2.5.0 (`Makefile:6`) が抱える版。バージョンを上げて「DSL を更新した」と
  思い込む経路がここ

### 対応案

- `dsl` を direct require から外し、`ruleguard.rules.go` に「この require は装飾で、実効版は
  golangci-lint 同梱」と書く。ただし **import があるので `go mod tidy` が戻す可能性**があり、
  実際の挙動を確かめてから決める
- または `ruleguard.rules.go` を**別ディレクトリ** (`gorules/` 等) へ移し、パッケージ名衝突を
  解消して `go vet -tags ruleguard ./gorules` が通る形にする。`.golangci.yml` の `rules:` の
  パスも合わせる。**こちらなら型エラーを CI で捕まえられる**
- どちらを採るにせよ、**ルールが無言で消えていないこと**を検査する方法 (意図的に違反コードを
  置いて lint が落ちるか) を同じ変更で用意する

## 発見 2: 幅テストのオラクル (`uniseg`) が本番の分割器と Unicode 版が違う

`github.com/rivo/uniseg v0.4.7` は direct require だが、**本体 (TUI) からは 1 箇所も import
されていない**。生きている参照は 2 つだけ:

- `tools/width-probe/main.go` (README で「本体から参照しない調査ツール」と明示、depguard も除外)
- **`termwidth/width_fast_test.go:76` — `uniseg.GraphemeClusterCount(s)`**

2 つ目が問題。`fastDispWidth` の受理集合が「互いに結合しない」ことを保証する
`TestAcceptedSymbolsNeverCombineWithEachOther` は、**uniseg のクラスタ境界を正解とみなしている**。
一方、本番の分割器は `termwidth/termwidth.go:225` の
`ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)`。

**この repo 自身が版差を記録している** (`termwidth/width_fast_test.go:296-299`):

> uniseg v0.4.7 が Unicode 15、x/ansi v0.11.7 が 16 という版差の産物で、uniseg が 16 に
> 上がれば 0 件になり

過去に 744 件 / 93 rune の不一致が実測されている。

### 発火条件

`acceptedSymbols` に記号を 1 つ足したとき、その記号が **Unicode 16 では後続と結合するが
Unicode 15 では結合しない**ケースだと:

1. uniseg (オラクル) は「2 クラスタ」と答え、テストは **green のまま通る**
2. 本番の `ansi.FirstGraphemeCluster` は 1 クラスタに結合する
3. `fastDispWidth` の逐次加算が `ansi.StringWidth` と食い違い、fast-path が幅を誤る
4. `TruncateLeft` / `dropToColumn` の桁がずれる

**「幅は 1 本に統一した」という設計 (`.golangci.yml:69-75` の depguard 2 ルール +
`width_test.go:TestNoSecondWidthEngine`) が、その統一を検証する側で破れている。**
depguard の除外 (`width_fast_test.go`) は width-probe と同じ扱いになっており、
この版差リスクには触れていない。

### 対応案

- オラクルを `ansi.FirstGraphemeCluster` に揃える (本番と同じ分割器で「結合しない」を確かめる)。
  ⚠️ ただし**本番と同じライブラリをオラクルにすると自己言及になる**
  ([`mutation-verify-new-tests.md`](../_claude/rules/mutation-verify-new-tests.md) の「自己言及」)。
  この検査が守りたいのは「fastDispWidth の逐次加算 == ansi.StringWidth の総和」なので、
  **オラクルを uniseg から `ansi.StringWidth` との突き合わせへ変える**方が筋が良い可能性がある
- または uniseg を Unicode 16 対応版へ上げる (上げれば版差は消えるが、**次の Unicode 版でまた開く**)
- どちらにせよ、**版差が開いていることを検知する検査**を足すのが本筋
  (`uniseg` と `x/ansi` のクラスタ境界が食い違う rune が 0 件であることを範囲総当たりで見る、等)

## 受け入れ条件

- [ ] 発見 1: `ruleguard.rules.go` の型エラーが CI で落ちる形にする (または「型検査されない」ことを
      ファイル冒頭に明記し、`dsl` の require の意味も書く)
- [ ] 発見 2: オラクルの版差が塞がるか、版差が開いたことを検知できる形にする
- [ ] 各対応は**変異で red を見る**まで確認する (ルールに型エラーを入れる / 版差のある rune を
      `acceptedSymbols` に足す)

## 報告に留めたもの (issue 化しない。発火条件が弱いか、今は動かせない)

- **端末ライブラリが 2 本**: `golang.org/x/term` (direct、`main.go` の 2 箇所) と
  `charmbracelet/x/term` (indirect、bubbletea 経由)。実害は「TUI 起動の要否判断と TUI 内部の
  サイズが別ソース」だけで、幅と違って単一情報源の規律も無い。**未確認リスク**として記録
- **`replace doctor => ../doctor` と go.sum**: `go.sum` に doctor のエントリが無く、
  doctor 側の依存 (`x/sys` / `howett.net/plist`) が glogx の go.mod へ手動転記されている。
  doctor が新しい依存を足すと glogx の CI 側で「missing go.sum entry」が出る。
  ただし**CI は dotfiles 全体を checkout する前提で成立しており、今のところ壊れていない**
- **chroma の `formatters` が html/svg を引き込む**: `formatters.Get("terminal256")` しか
  使わないのに `formatters/html` と `formatters/svg` が `go list -deps` に入る。
  **バイナリサイズは未計測**なので「肥大化する」とは書かない
  ([`perf-claims-need-measurement.md`](../_claude/rules/perf-claims-need-measurement.md))
- **`ultraviolet` が擬似バージョン**: bubbletea v2.0.8 がタグの無いコミットに依存している。
  glogx の幅モデルは「描画エンジンの既定が WcWidth」という ultraviolet の実装詳細に乗って
  辻褄を合わせている (`termwidth/termwidth.go:19-36` が実測表つきで記録)。bubbletea を上げると
  切替条件が予告なく変わりうるが、**上流都合で今は動かせない**。bubbletea を上げるときの
  観測ポイントとして記録する
