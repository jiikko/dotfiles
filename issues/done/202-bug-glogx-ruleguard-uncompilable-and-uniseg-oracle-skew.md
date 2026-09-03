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

## 発見 2 (❌ **却下: false positive**): 幅テストのオラクルの版差

監査は「`TestAcceptedSymbolsNeverCombineWithEachOther` が uniseg (Unicode 15) をオラクルに
しているので、Unicode 16 でだけ結合する記号を `acceptedSymbols` に足すとテストが green のまま
通る」と報告した。**main agent が実測して否定した (2026-09-03)。**

### 版差そのものは実在する (実測)

`"a" + U+0897` などで測ると:

| rune | `uniseg.GraphemeClusterCount` | `ansi.StringWidth` |
|---|---|---|
| U+0897 | **2** (結合しない) | **1** (結合する) |
| U+1ACF | 2 | 1 |
| U+1ADD | 2 | 1 |
| U+113B8 | 2 | 1 |

uniseg v0.4.7 (Unicode 15) と x/ansi v0.11.7 (16) の判断は確かに割れている。

### しかしテストは素通りしない

このテストは uniseg の後に **2 つの assert** を持つ。同じ入力で 3 つの assert を順に評価した実測:

```
U+0897: [assert1 uniseg==2] true / [assert2 受理] ok=false / [assert3 fast==ansi] 0 vs 1 → 不一致
U+1ACF: [assert1 uniseg==2] true / [assert2 受理] ok=false / [assert3 fast==ansi] 0 vs 1 → 不一致
```

uniseg のオラクル (assert1) は確かに通してしまうが、**`fastDispWidth` が受理しない
(assert2 の `t.Fatalf("受理されなかった")`) ので red になる**。仮に受理されたとしても、
assert3 (`fast == ansi.StringWidth`) が 0 vs 1 で落ちる。

**守っている本体は「fast-path の逐次加算 == ansi.StringWidth の総和」の方**で、uniseg は
補助的な前段チェックにすぎない。監査は assert1 だけを見て「オラクルが本番と違う」と判断したが、
**後続の assert が本番のライブラリで裏を取っている**ことを見落としていた。

### 対応

**何もしない。** ただし、この却下理由を残しておかないと次の監査が同じ指摘を再生成するので、
`width_fast_test.go` の uniseg を使う箇所に「このオラクルは前段で、本番との一致は下の
`ansi.StringWidth` 比較が担う」という 1 行を足す
([`pending-issue-rationale-in-code.md`](../_claude/rules/pending-issue-rationale-in-code.md))。

## 受け入れ条件

- [x] 発見 1: `gorules/rules.go` へ移し、`make lint` が `go vet -tags ruleguard ./gorules` を
      回す形にした (`397fded2`)。変異 (`m.NoSuchMethodZZ`) で red を確認
- [x] 発見 2: **却下 (false positive)**。版差は実在するが、テストは後続の assert で red になる
      ことを実測で確認した。却下理由をコード直近にコメントで残す

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
