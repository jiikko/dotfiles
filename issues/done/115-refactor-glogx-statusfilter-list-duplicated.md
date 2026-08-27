# 115 refactor: `ParseStatusFilter` の段階一覧が手書きスライスで二重管理

起票日: 2026-08-27 / 出典: leaky-abstraction 監査 (L5 暗黙契約) / priority: low

## 事実

`src/glogx/issues/parse.go:ParseStatusFilter` は段階を手書きで列挙している:

```go
for _, f := range []StatusFilter{FilterOpen, FilterPending, FilterAll} {
```

対になる `String()` は `default` の無い switch なので `exhaustive` linter が新 case を強制するが、
**このスライスリテラルは誰も強制しない**。

## 発火条件と壊れ方

4 つ目のフィルタ段階を足したとき。`String()` は lint で直させられるが、スライスへの追加を
忘れると、保存された新段階名が `ok=false` で既定 (open) へ落ちる。

**silent**。「開き直したら伏せていたはずの段階に戻っている」という、まさに `String()` の doc が
序数保存について警告しているのと同じ絵になる。

## 反証の試み

`String()` の doc に「名前なら未知の段階は既定へ倒せる」とあり、**壊れ方の緩さは設計どおり**。
ただし「段階の一覧が二重管理になっている」ことへの言及は無く、往復を pin するテストも無い。

## 対応

`for f := FilterOpen; f <= FilterAll; f++` に変える。上限 `FilterAll` は `Next()` が既に
使っているので出典が 1 本化する。

---

## 対応 (2026-08-27)

`for f := FilterOpen; f <= FilterAll; f++` に変えた。上限は `Next()` が既に使っている
`FilterAll` と同じ出典になる。

### 検証: 「4 つ目の段階を足したときに検出できるか」で測った

一般的な変異ではなく、**この修正が防ぎたい事故そのもの**を実験で作った
(`FilterBlocked` を `String()` にだけ足し、引く側は触らない):

| | 結果 |
|---|---|
| 修正後 (範囲で走る) | 新しい段階が**引ける** ✓ |
| 修正前 (手書きスライス) | 引けず、テストが名指しで red — `段階 2 ("blocked") を引けない (String() には在るが引く側に無い)` |

### 変異検証

上限を `<=` から `<` へ (off-by-one) / 開始を `FilterPending` へ / `String()` の名前を変える
(保存形式の破壊) — いずれも red。

### テストを 3 本足した

- `TestStatusFilterNameRoundTrip` — 名前を **literal で固定** (production の `String()` から
  作ると自己言及になる)
- `TestStatusFilterEveryStageIsParseable` — 全段階が引けること。**走査 0 件も fail** にした
  (範囲の書き方が壊れたら赤にする)
- `TestStatusFilterUnknownNameFallsBackToOpen` — 未知の名前は既定へ倒す契約

### 敵対的レビューは回していない (判断)

純関数の 1 行変更で、状態も並行も外部 I/O も動かない。しかも「防ぎたい事故」を実験で直接
再現して修正の前後を比較できたので、追加の視点で得られるものが小さいと判断した。
