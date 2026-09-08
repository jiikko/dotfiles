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
[`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) 節 2 が
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
[`mutation-verify-new-tests.md`](../../_claude/rules/mutation-verify-new-tests.md) の
「文字列を部分一致で pin していないか」に自ら寄る（文言を変えるとテストが壊れる／
文言が同じなら別の理由でも通る）。

**採るべき形**: `sandboxFatalPanic{err error}` に変え、recover 側は
**`errors.Is(p.err, errNotInSandbox)` の sentinel 比較**で拒否理由を区別する。
`sandboxAllowable` は既に `error` を返すので、文言比較へ落とす必要がない。
`sandboxAllowRejects` は `(rejected bool, reason error)` へ。

## 受け入れ条件

- [x] `sandboxFatalPanic` が `error` を持ち、recover 側が sentinel で理由を区別する
- [x] cleanup が panic 経路でも走る（`defer` の構造を直す）
- [x] **変異検証**: 「`sandboxAllow` の中で無関係な Fatal を起こす」変異を当て、
      **現状は緑 → 修正後は red** を確認する（これが本 issue の合否そのもの）
- [x] 変異を当てる前に baseline が緑であることを測る

## 進捗（commit: fix(314): サンドボックス自己テストの拒否理由を sentinel で区別する）

対象は `src/doctor/disk/main_test.go` の 1 ファイルのみ。

- `errNotInSandbox` sentinel を新設し、`sandboxAllowable` が `%w` で wrap する
- `sandboxFatalPanic{msg string}` → `{err error}`。`sandboxRecorder.Fatal` は
  引数が error 1 個ならそのまま運ぶ（`fatalReason`）ので wrap 鎖が切れない
- `sandboxAllowRejects` は `(rejected bool, reason error)` の **3 値**へ。
  `errors.Is(p.err, errNotInSandbox)` のときだけ `rejected = true`。
  無関係な Fatal は **判定不能**として拒否にも許可にも畳まない
- cleanup の `defer` を recover の `defer` より**先に**積み（LIFO なので後に走る）、
  panic 経路でも走るようにした
- その配線を固定するための seam `runSandboxAllow(func(*sandboxRecorder))` と
  `TestSandboxAllowRunnerClassifiesAndCleansUp`（3 ケース）を追加

## 結果（変異検証の実測 / 2026-09-09）

判定は `go test -race -v ./disk/...` の**ケース名ごとの PASS/FAIL** で行った。
変異のビルド可否は `go vet ./disk/...` と `go test -c -o /dev/null ./disk/` で別に確認
（🚨 `go build ./...` は `_test.go` を**コンパイルしない**ので、この検証には使えない）。

| # | 変異 | 修正前 | 修正後 |
|---|---|---|---|
| baseline | なし | rc=0 緑 | rc=0 緑 |
| M1 | `sandboxAllow` の `t.Fatal(err)` → `t.Fatal("MUTANT1: 無関係な理由で落ちた")`（拒否理由の差し替え） | **rc=0 緑** | **rc=1 red** |
| M2 | `sandboxAllow` の先頭に `if strings.Contains(root, "Documents") { t.Fatal("MUTANT2: 無関係な Fatal") }`（無関係な Fatal 経路の追加） | **rc=0 緑** | **rc=1 red** |
| M3 | cleanup の `defer` を修正前の構造（recover が nil のときだけ走る形）へ戻す | （テストが存在しない） | **rc=1 red** |

- M1 修正後に落ちたのは `TestSandboxAllowRejectsPathsOutsideTempDir` の
  「判定不能」分岐 ×3（`/Users/koji` / `/` / `/Users/koji/Documents`）
- M2 修正後に落ちたのは **`Documents` のケースだけ**（他 2 ケースは緑のまま）。
  ケース単位で識別できていることの確認
- M3 で落ちたのは `TestSandboxAllowRunnerClassifiesAndCleansUp` の
  **panic する 2 サブケースだけ**（`cleanup が走った回数 = 0 (want 1)`）。
  非 panic ケースは緑のまま = 変異が当たった場所と一致
- 修正後・変異なしの緑も別に測った（`%w` を `%v` に書き損じると正規の拒否まで
  判定不能へ落ち、M1/M2 が**誤った理由で** red になるため）

## 残タスク

- なし（スコープ内）。`gofmt -l .` / `go vet ./disk/...` / `go test -race ./disk/...` すべて通過
- スコープ外: `sandboxAllow` に「無関係な Fatal」が**実際に**増えたわけではない
  （M1/M2 は変異であって現状のコードではない）。今回入れたのは「増えたときに気づく」側

## 関連

- [`sandbox-real-destructive-test-apis.md`](../../_claude/rules/sandbox-real-destructive-test-apis.md)
  （このサンドボックス機構そのものの正本）
- issue 234（doctor のテストサンドボックスの穴。同じ機構の過去の指摘）
