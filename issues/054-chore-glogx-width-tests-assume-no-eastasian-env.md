# glogx: `RUNEWIDTH_EASTASIAN` が真の環境で幅系テストが 22 本落ちる (支持するかが未決)

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

⚠️ **これは 046 の前から同じ**。旧 `dispWidth` も非 ASCII は `ansi.StringWidth` に
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

## 実測の裏取り (並行セッションの独立確認、2026-08-14)

base `997d078` の worktree で `RUNEWIDTH_EASTASIAN=1 go test ./...` の FAIL 行を
突き合わせた結果、失敗集合は **base 26 件 / HEAD 27 件**で、差分は次の 3 つだけだった:

- HEAD にだけ出る 2 件 = 2026-08-14 に新設したテスト (`TestFrameAllocBudget` /
  `TestWrapWindowFrameGeometry`)
- 修正済み 1 件 (`TestIssuesViewerReloadsAfterEditorCloses`)

既存分は同一集合 = 「046 の前から同じ」は裏が取れている。

⚠️ **新しく落ちる 2 件のうち `TestFrameAllocBudget` は 047 で新設した「確保の予算」ガード**
= **安全機構そのものがこの env で落ちる**。この env を支持する判断をするなら、
安全機構が先に動かなくなる点を織り込むこと (issue 051 の確保ゲートとも絡む)。

## なぜ今決めないか

この env を使う人が現れていない (再現条件を実験で作れていない)。
`width.go` の doc が「East Asian Ambiguous は ansi では幅 1 で locale に依存しない」と
書いているが、これは `LANG` については正しく `RUNEWIDTH_EASTASIAN` については誤り —
**doc の方は 046 で訂正済み**。

## 着手条件 (trigger)

- `RUNEWIDTH_EASTASIAN` を設定している環境で glogx を使う人が出たとき
- または CI にその env が混入して赤くなったとき

## 関連

- `issues/done/046-perf-glogx-dispwidth-fastpath-dead.md` (「未確認リスク」節が一次情報)
- `src/glogx/width_fast_test.go` の `TestDispWidthAgreesUnderEastAsianEnv`
