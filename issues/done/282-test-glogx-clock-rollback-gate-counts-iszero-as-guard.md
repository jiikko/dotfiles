# test: 時計巻き戻しゲートが `IsZero()` を「ガード」に数えるため、最も自然な新規コードを素通しする

起票日: 2026-09-06
カテゴリ: test
優先度: 中（ゲートが在るのに、新しい鮮度判定の既定の書き方が無審査で通る）

## 何が起きているか

`clock_rollback_test.go:TestFreshnessChecksGuardAgainstClockRollback` の `guarded` は、
同一関数内に次のいずれかがあれば「ガードあり」と判定する:

```go
for _, tok := range []string{"age < 0", "age >= 0", ".After(now)", ".After(timeNow())", "IsZero()"} {
```

🚨 **`IsZero()` は巻き戻しガードではない**。「まだ一度も取得していない」の判定であって、
「取得時刻が未来」の判定ではない。しかも `if x.IsZero() { return false }` は
**新しい鮮度判定を書くときの最も自然な書き方**なので、この抜け道は偶然ではなく**既定経路**。

## 実測（probe）

巻き戻しガードを**一切持たない**新しい鮮度判定を 1 本足した:

```go
func zzProbeFresh(e zzProbeEntry, now time.Time) bool {
	if e.FetchedAt.IsZero() { return false }
	return now.Sub(e.FetchedAt) < zzProbeTTL
}
```

→ 走査件数は **11 → 12 に増え**（= ちゃんと見えている）、テストは **PASS**。

## 既存コードの状況（probe で全数確認）

11 件中 10 件は `age < 0` / `age >= 0` / `.After(now)` の**実ガード**で通っている。
**`doctor_cache.go:carryFresh` だけが `IsZero()` のみで通過**している。

`carryFresh` は doc で「未来の時刻は**引き継ぐ**」と意図的に例外を選んでいるが、
検査が用意している除外マーカー `// clock: elapsed-only` を使っていないため、
**「意図的な例外」と「偶然通っただけ」を機械が区別できない**。

## 発火条件

- 新しい鮮度判定を足したとき、`IsZero()` を書いていれば無審査で通る
- **silent に壊れる**: `IsZero()` は無関係な理由（旧いキャッシュの検出）で書かれており、
  書いた人は自分がゲートを満たしたことに気づかない

## 推奨対応（順序が重要）

1. **先に** `carryFresh` に `// clock: elapsed-only` と理由を付ける
2. **次に** `guarded` の token 集合から `IsZero()` を落とす
   （逆順だとゲートが赤くなる）
3. 🚨 除外は `checked++` の**前**に return するので、**件数は 11 → 10 に減る**。
   減ることを期待値として確認すること
4. probe 関数を再度当てて、今度は **FAIL する**ことを確認する
   （`~/.claude/rules/verify-execution-not-just-exit-code.md` の canary）

## 偽陽性の見込み

**ほぼ無い**。`IsZero()` に依存しているのは 1 関数だけで、それは除外マーカーが正しい対象。
`issues/done/198` も「`IsZero()` は now が epoch 近傍のときの別の不変条件を守るもの」と書いており、
巻き戻しガードとは別物であることが repo 内で既に言語化されている。

## 反証の試み

201 本文は token 集合を「同一関数内に `< 0` / `>= 0` / `.After(` / `IsZero()` のいずれかを要求」と
**設計として**書いているので、これは見落としではなく**意図的な選択への反証**。
上の probe が「その選択がゲートの目的を達成できていない」ことを実験で示している。

## 関連

- `issues/done/201`（このゲートの出典）/ `issues/done/198`（`IsZero()` の別の役割）
