# test: 無害化テスト 5 本に陽性対照が無く、箱が空になると vacuous に緑を返す

起票日: 2026-09-06
カテゴリ: test
優先度: 中（セキュリティ境界の検査が、描かれなくなった瞬間に何も守らなくなる）

## 何が起きているか

`untrusted_display_test.go` の 5 本:

- `TestPRStatusBoxSanitizesBranchNames`
- `TestDiscardBoxSanitizesPath`
- `TestMarkNextBoxSanitizesFilename`
- `TestUsageBoxSanitizesVersion`
- `TestToastAndWarningAreSanitized`

いずれも

```go
for _, line := range v.someBox(80, false) {
    if hasTerminalControl(line) { t.Errorf(...) }
    if strings.Contains(line, "PWNED") { t.Errorf(...) }
}
```

の形で、**細工した値が実際に描かれたことを主張する assert が無い**。
箱が空を返せば assert は 1 つも実行されず PASS する。

🚨 **同じファイルの `TestDoctorLiveDockerScanIsSanitized` は末尾に `sawItem` を置き**、
コメントで「候補の行が**実際に描かれている**ことを確かめる（描かれていなければ、上の assert は
1 つも sink を守っていない）」と書いている。**著者自身がこの罠を特定して 1 本だけに適用し、
5 本に横展開していない**。

## 実測（変異検証）

1. `issues_view.go:issuesView.markNextBox` の先頭に `if true { return nil }`
   （diff 1 行、`go build` OK）→ `TestMarkNextBoxSanitizesFilename` は **PASS**
2. sanitize 呼び出しを `what = "1 件"` に置換（細工値を描かない形）→ 同じく **PASS**

## 発火条件

- 箱が空を返す / 細工値が描かれなくなる退行が入ったとき、検査が素通しする
- **silent に壊れる**。CI もテストも止めない
- 🚨 `src/glogx/CLAUDE.md` は逆に「doctor の live 経路が 1 度も通っていなかった = issue 228」という
  **同型の実害**を記録している

## 推奨対応

`TestDoctorLiveDockerScanIsSanitized` の `sawItem` パターンを 5 本に横展開する
（無害化後の可視文字列が箱に在ることを 1 行 assert）。
理想は共有ヘルパー `assertSanitizedAndVisible(t, lines, wantVisible string)`。

## 反証の試み

`issues/done/` の 228 / 216 / 170 / 082 に、この 5 本を意図的に陽性対照なしにした記述は無い。

## 関連

- `issues/done/228`（同型の実害。live 経路が 1 度も通っていなかった）
- `untrusted_display_test.go:TestDoctorLiveDockerScanIsSanitized`（正しい形の前例）
