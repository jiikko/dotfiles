# test: WaitDelay 規律ゲートが実質 1 箇所しか検査しておらず、canary も退行を検出できない

起票日: 2026-09-06
カテゴリ: test
優先度: 中（現状の実害は 0 件。効くのは「次に誰かが書いたとき」）
出典: /audit resource-leaks 2026-09-06（forge Minimum+）。2 エージェントが**対立する修正案**を出しているので、
軸の選択はユーザー判断（下の「方針が割れている」節）

対象: `src/glogx/waitdelay_discipline_test.go:TestEveryCommandContextSetsWaitDelay`

## 何が起きているか

ゲートの検出パターンは **`exec.CommandContext(` の 1 本だけ**（`waitdelay_discipline_test.go:44`）。

### ① `exec.Command(` を 1 件も検査していない

`src/glogx` の非テストコードで `exec.Command(`（ctx 無し）は **7 箇所**ある:

```
open_workspace.go:41, open_workspace.go:54, tui.go:3078,
gitlog.go:68, external_commands.go:285, external_commands.go:374, external_commands.go:379
```

いずれも**現状は安全**（前景の対話実行 / 即 detach する `open` / 起動時の同期経路）なので
**実害は 0 件**。しかし誰かが `exec.Command("gh", ...).Output()` を書いた瞬間、
ゲートは**緑のまま通す**。issue 105（13 箇所中 1 箇所が静かに抜けた）と同じ形が再生産される。

### ② canary の下限が実件数と乖離している

canary は `checked == 0` で落ちる形（:70）だが、`subproc/` と `tools/` を除いた
非テストの `exec.CommandContext` は **2 件**しかなく、うち 1 件
（`external_commands.go:293`）は `subproc: no-waitdelay` で `continue` する。
**実質検査しているのは 1 件**。下限 1 の canary は「検査が消えた」をほぼ検出しない
（[`verify-execution-not-just-exit-code.md`](../_claude/rules/verify-execution-not-just-exit-code.md)
の「canary の下限は実件数の近くへ置く」）。

### ③ 検査本体と無関係な自作 `itoa` がある

`waitdelay_discipline_test.go:78-90` の自作 `itoa` は `strconv.Itoa` で足りる。
ゲート本体の主張を薄めている（軽微）。

## 🚨 方針が割れている（ユーザー判断が要る）

| 案 | 内容 | 根拠 |
|---|---|---|
| **A: 字句パターンを広げる** | 検出を `exec.Command(` へ（`exec.CommandContext(` は前方一致で含まれる）。`doctor` にも同じ検査を置くか、置かない理由を `runner.go` に書き残す | 変更が小さく、今のゲートの延長。パイプを作らない `Run()` は既存の `subproc: no-waitdelay` 注記でそのまま除外できる |
| **B: import 境界の検査へ移す** | 「`src/glogx` の非テストコードで `os/exec` を import してよいのは `subproc` / `gitlog`（Cmd を受け取って張る側）/ `tea.ExecProcess` へ渡すだけの前景実行に限る」 | 字句ゲートは迂回が無限に出る（[`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md) §8）。import なら書き換えで迂回できず、母集合が「import しているファイル一覧」という数えられる形になる |

**B 側の付随判断**: `doctor` は既に**型**で守れている（`runner.Runner` を `Options.Run` に注入。
非テストの `exec.Command` は `runner/runner.go:24` の 1 箇所だけ）ので、同種テストを置いても
検査対象は 1 箇所でほぼ何も守らない。`runner.go` に「この module の外部実行はここだけ」と
1 行残せば足りる。

## 🚨 A を採る場合の注意（行内注記が嘘になる）

`tui.go:3078` の `exec.Command("nvim", "-R", ...)` は `cmd.Stdin = strings.NewReader(...)` を
使っており、**`*os.File` でないので os/exec が `os.Pipe` と copy goroutine を作る**
（= WaitDelay が想定する構図そのもの）。ここが安全なのは「パイプが無いから」ではなく
**「前景の対話エディタで ctx も無い」**から。この理由のまま `subproc: no-waitdelay` を付けると、
**行内注記が嘘の不変条件を固定する**。

## 受け入れ条件

- [ ] 軸（A / B）を決め、選ばなかった側を却下理由つきでこの issue に残す
- [ ] canary の下限を実件数の近くへ置く
- [ ] **変異検証**: 検査対象のファイルから守りを外して red になることを、選んだ軸で確認する
- [ ] `itoa` を `strconv.Itoa` へ

## 関連

- issue 105（この規律が生まれた経緯: 13 箇所中 1 箇所が静かに抜けた）
- research issue 308（本監査の記録。WaitDelay が保証するのは `Wait()` が返ることだけで、
  子孫の回収は保証しないという指摘を含む）
