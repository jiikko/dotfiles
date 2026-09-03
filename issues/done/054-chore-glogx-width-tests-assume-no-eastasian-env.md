# glogx: `RUNEWIDTH_EASTASIAN` が真の環境で幅系テストが 22 本落ちる (→ 支持しないと決定)

起票日: 2026-08-14
種別: chore
優先度: **P3** (その env を使う人が現れるまでは実害なし)

## 観測した事実

`RUNEWIDTH_EASTASIAN=1` (または `true`) を設定して `go test ./...` を回すと、
**幅 1 を期待値に焼いている既存テストが 22 本 fail する**。例:

```
width_test.go:22: dispWidth("→") = 2; want 1
```

x/ansi は `method.go` の `init` でこの env を読み、East Asian Ambiguous
(罫線・矢印・`…`・`·` 等) を幅 2 として数える。したがって `dispWidth` も 2 を返すのが
**正しい** が、テストは 1 を焼いている。

🚨 **これは 046 の前から同じ**。旧 `dispWidth` も非 ASCII は `ansi.StringWidth` に
落としていたので同じ 2 を返していた。046 が持ち込んだものではない
(046 のレビューで判明し、実測で確認済み)。

## 何が未決か

**glogx がこの env を支持するかどうかの設計判断**が無い。選択肢:

1. **支持する** — テストの期待値を `ansi.StringWidth` から取る形に直す (22 本)。
   幅モデルがライブラリ由来である以上、こちらが筋は通る
2. **支持しない** — テストの冒頭で env が立っていたら skip するか、
   `width.go` の doc に「この env は未対応」と明記する
3. **無視する** — 現状維持 (誰も踏んでいない)

046 で `dispWidth` の fast-path は**幅をライブラリから引く**形にしたので、
**実装側はこの env で正しく動く** (`fillRight` / `truncateDispLeft` の算術も一致する。
`RUNEWIDTH_EASTASIAN=1` の子プロセスを起こす回帰テストを置いてある)。
残っているのは**テストの期待値だけ**。

> 🚨 **この段落は誤り** (2026-08-15 に着手して実測で判明)。正しくは「**幅計算の層**は
> この env で正しく動くが、**描画は壊れる**」。詳細は下記「着手時の実測」。

## 実測の裏取り (並行セッションの独立確認、2026-08-14)

base `997d078` の worktree で `RUNEWIDTH_EASTASIAN=1 go test ./...` の FAIL 行を
突き合わせた結果、失敗集合は **base 26 件 / HEAD 27 件**で、差分は次の 3 つだけだった:

- HEAD にだけ出る 2 件 = 2026-08-14 に新設したテスト (`TestFrameAllocBudget` /
  `TestWrapWindowFrameGeometry`)
- 修正済み 1 件 (`TestIssuesViewerReloadsAfterEditorCloses`)

既存分は同一集合 = 「046 の前から同じ」は裏が取れている。

🚨 **新しく落ちる 2 件のうち `TestFrameAllocBudget` は 047 で新設した「確保の予算」ガード**
= **安全機構そのものがこの env で落ちる**。この env を支持する判断をするなら、
安全機構が先に動かなくなる点を織り込むこと (issue 051 の確保ゲートとも絡む)。

## なぜ今決めないか

この env を使う人が現れていない (再現条件を実験で作れていない)。
`width.go` の doc が「East Asian Ambiguous は ansi では幅 1 で locale に依存しない」と
書いているが、これは `LANG` については正しく `RUNEWIDTH_EASTASIAN` については誤り —
**doc の方は 046 で訂正済み**。

## 着手条件 (trigger) — 発火済み

- ~~`RUNEWIDTH_EASTASIAN` を設定している環境で glogx を使う人が出たとき~~
- ~~または CI にその env が混入して赤くなったとき~~

→ trigger を待たずユーザー判断で着手 (2026-08-15)。着手して初めて「テストの期待値の話ではない」と
分かったので、待っていた場合は誤った前提のまま作業を始めていた。

## 着手時の実測 (2026-08-15)

`RUNEWIDTH_EASTASIAN=1 go test -count=1 ./...` の失敗は **28 本**。うち複数は期待値の
焼き付けではなく**不変条件そのものの違反**だった:

```
tui_nav_test.go:331: 幅超過 (158 > 80): "▖▁▁▁…▗▒"        ← 80 桁のパネルが 158 桁
tui_nav_test.go:331: 幅超過 (98 > 80):  "┌ CI jobs: …┐"
markdown_test.go:52: width=20: 行 25 が幅を超えた (w=38): "───────┼───────────"
status_view_test.go:1185: width=3 の行が幅を超えた (w=4): "  │"
```

原因は**「グリフ数 = 表示幅」を前提にした埋め方**が複数箇所に残っていること:

- `box.go:275` (`strings.Repeat(b.h, fw-2-dispWidth(title))`) / `box.go:310` (`▁` の底辺)
- `render.go:193` (`─` の区切り線) / `usage_overlay.go:178`
- `issues/markdown.go:350` (水平線) / `:470` (表の桁)

罫線・ブロック要素が幅 2 になると、これらは要求幅の 2 倍近くを出力する。加えて
`clusterWidth` が `uniseg.StringWidth` (この env を見ない) なので、幅モデルが二重化している。

つまり「支持する」は 28 本の期待値修正では済まず、埋め方の設計変更 (奇数幅の端数処理を含む)
と幅モデルの一本化を伴う。

## 対応記録 (2026-08-15)

**選択肢 2「支持しない」を採用** (ユーザー判断)。全角の枠線グリフで組んだ UI は、それらが
幅 2 で描かれる端末では原理的に妥協 (奇数幅を空白で埋める等) が入るため、支持すると決めても
見た目の品質は戻らない。代わりに**黙って壊れない**ことに寄せた:

- `widthenv` パッケージを新設し、env の検出 (`EastAsianAmbiguous`) と文言 (`Message`) を
  1 箇所に集約。main と issues の両方 (とテスト) から参照する。真偽の解釈は x/ansi の init と
  同じ `strconv.ParseBool` に揃えた
- **実行時**: `run()` の頭で env が真なら stderr に警告を 1 回出して続行する。
  🚨 TUI は alt screen に入るので、対話モードでは終了後に見える形になる。主な想定発火先
  (CI・非 TTY 実行) では stderr にそのまま残る
- **テスト**: 幅に依存する 4 パッケージ (main / issues / usage / widthenv) の TestMain が
  `widthenv.ExitIfUnsupported()` を呼び、env が真なら理由を出して停止する。28 本の意味不明な赤が
  **パッケージごと 1 行の説明付き失敗**になる。子プロセス (`TestDispWidthAgreesUnderEastAsianEnv`
  が「幅**計算**の層はライブラリと一致し続ける」という別の主張のために env をわざと立てて起こす)
  だけ除外し、除外条件は「マーカー env **かつ** 親が渡す `-test.run` に当該テスト名が含まれる」に
  縛った
- `width.go` の doc を実測に合わせて訂正 (「Ambiguous は幅 1 で locale 非依存」は
  `RUNEWIDTH_EASTASIAN` については誤りだった)。`width_fast_test.go` の子プロセステストにも
  「これは支持の主張ではない」と明記

検証:

- `RUNEWIDTH_EASTASIAN=1 go test -count=1 ./...` = 28 本の赤 → main / issues で各 1 行の説明。
  termsafe / usage はこの env でも本当に green (幅に依存していない) ことを実測で確認
- 通常の `go test ./...` / `make lint` / repo root の `make test` は green
- 実行時警告は実バイナリで実測 (`RUNEWIDTH_EASTASIAN=1 glogx -n 2` の stderr に出る / env 無しでは出ない)
- 変異検証: `EastAsianAmbiguous` の判定を反転させると `widthenv` のテストが red になり、
  同時に幅系テストのガードも外れる (= ゲートが黙って無効化されたら気づける)。
  除外条件を「マーカーだけで通す」形に戻すと `TestEastAsianChildExemptionIsScoped` が red

### 敵対的レビューで塞いだ穴 (新設したゲートなので自己レビューで閉じない)

- **P1: ゲートを作った当の `widthenv` 自身が、自分のガードを呼んでいなかった**。env 下で
  `TestAmbiguousIsNarrowByDefault` が「`ansi.StringWidth("─")=2, want 1`」の生ログを 6 行吐き、
  この仕組みが消そうとしている混乱が本人から漏れていた (実測で再現)。共通ヘルパー
  `ExitIfUnsupported` に切り出し、幅に依存する全パッケージから呼ぶ形にした
- **P2: `GLOGX_EAW_CHILD=1` を環境に export したまま suite を回すとガードが全無効化された**
  (`RUNEWIDTH_EASTASIAN=1 GLOGX_EAW_CHILD=1 go test .` で 20 本以上の生ログが復活するのを実測)。
  除外を `-test.run` の内容まで見る形に絞り、回帰テストを追加
- **P3: `usage` パッケージだけガードが無かった**。今は幅を主張する assert が無く env 下でも
  green だが、`render.go` は `ansi.StringWidth` で幅を測るので、幅のテストを足した瞬間に同じ
  生ログ問題になる。先回りでガードを入れた (`termsafe` は幅計算に依存しない純粋な文字列処理
  なので対象外。env 下でも green であることを実測で確認)

## 関連

- `issues/done/046-perf-glogx-dispwidth-fastpath-dead.md` (「未確認リスク」節が一次情報)
- `src/glogx/width_fast_test.go` の `TestDispWidthAgreesUnderEastAsianEnv`
- `src/glogx/widthenv/widthenv.go` (支持しない判断の一次情報)
- `_claude/rules/adversarial-review-own-safeguards.md` — 新設したゲートなので敵対的レビューを通した
