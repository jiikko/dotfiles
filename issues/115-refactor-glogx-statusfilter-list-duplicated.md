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
