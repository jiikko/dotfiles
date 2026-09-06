# bug: doctor のサンドボックス自己テストが Fatal の理由を捨て、「判定不能」を合格に畳んでいる

起票日: 2026-09-07
カテゴリ: bug
優先度: 高（破壊的操作のガードを検査する側が、**何で落ちたか**を見ていない）
出典: /audit dead-code 2026-09-06。2 エージェントが独立に実測追認

対象: `src/doctor/disk/main_test.go:sandboxAllowRejects` / `sandboxFatalPanic`

## ① 拒否の理由を見ていない

```go
func sandboxAllowRejects(root string) (rejected bool) {
	rec := &sandboxRecorder{}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(sandboxFatalPanic); ok {
				rejected = true       // ← 「何で Fatal したか」を捨てている
				return
			}
			panic(r)
		}
		for _, f := range rec.cleanups { f() }
	}()
	sandboxAllow(rec, root)
	return false
}

type sandboxFatalPanic struct{ msg string }
```

`sandboxAllow` の本体には **`sandboxAllowable` 以外の Fatal 経路**がある。
つまり「サンドボックス外だから拒否した」も「引数が壊れていて落ちた」も、
どちらも `rejected = true` になる。

**これは「判定不能を合格に畳む」形**そのもので、
[`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md) 節 2 が
禁じているもの。しかも畳んでいるのが**破壊的操作のガードを検査するテスト**なので、
ガードが別の理由で壊れても緑になる。

## ② cleanup が非 panic 分岐にしかない

上の `defer` を読むと、`for _, f := range rec.cleanups { f() }` は
**`recover()` が nil のときしか走らない**。拒否された（= panic した）経路では
登録された cleanup が実行されない。

現状は「拒否されたなら登録も残っていない」ので実害が出ていないが、
**① の修正（理由を持たせる）で分岐が増えると、「登録が残る」経路が開く**。
①と同じ commit で閉じること。

## 🚨 修正案の注意（監査の一次案は採らない）

一次案は「判定文言を定数へ切り出して**文字列比較**する」だったが、これは
[`mutation-verify-new-tests.md`](../_claude/rules/mutation-verify-new-tests.md) の
「文字列を部分一致で pin していないか」に自ら寄る（文言を変えるとテストが壊れる／
文言が同じなら別の理由でも通る）。

**採るべき形**: `sandboxFatalPanic{err error}` に変え、recover 側は
**`errors.Is(p.err, errNotInSandbox)` の sentinel 比較**で拒否理由を区別する。
`sandboxAllowable` は既に `error` を返すので、文言比較へ落とす必要がない。
`sandboxAllowRejects` は `(rejected bool, reason error)` へ。

## 受け入れ条件

- [ ] `sandboxFatalPanic` が `error` を持ち、recover 側が sentinel で理由を区別する
- [ ] cleanup が panic 経路でも走る（`defer` の構造を直す）
- [ ] **変異検証**: 「`sandboxAllow` の中で無関係な Fatal を起こす」変異を当て、
      **現状は緑 → 修正後は red** を確認する（これが本 issue の合否そのもの）
- [ ] 変異を当てる前に baseline が緑であることを測る

## 関連

- [`sandbox-real-destructive-test-apis.md`](../_claude/rules/sandbox-real-destructive-test-apis.md)
  （このサンドボックス機構そのものの正本）
- issue 234（doctor のテストサンドボックスの穴。同じ機構の過去の指摘）
