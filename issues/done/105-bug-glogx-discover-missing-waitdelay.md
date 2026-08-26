# 105 bug: glogx の RepoRoot だけ WaitDelay が張られておらず、起動が無音で固まりうる

起票日: 2026-08-25 / priority: medium

## 事実

`src/glogx/issues/discover.go:49-51` の `git rev-parse --show-toplevel` は、
**repo 内で唯一 `WaitDelay` を張っていない `.Output()` 呼び出し**。

```go
ctx, cancel := context.WithTimeout(context.Background(), repoRootTimeout)  // 30s
cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
cmd.Dir = cwd
out, err := cmd.Output()      // ← cmd.WaitDelay が未設定
```

全数確認 (2026-08-25): `.Output()` / `.CombinedOutput()` は 9 箇所あり、
`issues/discover.go:51` 以外の 8 箇所はすべて `WaitDelay` を張っている
(`external_commands.go:81` / `:84` は `noPromptGitCmd` が `:37` で張る)。

## なぜ問題か

`usage/usage.go:62-72` (`SubprocessWaitDelay` の doc) が理由の正本:
`exec.CommandContext` の deadline が kill するのは**直接の子だけ**で、子が残した孫が
親のパイプを握っていると `Wait()` は戻らない。`.Output()` は stdout を `bytes.Buffer` に
取るために **os.Pipe と copy goroutine を作る**ので、この形にまさに該当する
(パイプを持たない `cmd.Run()` は /dev/null 直結なので事情が違う)。

しかも同じファイルの `:40-41` は
「stdin 待ちでハングした git が goroutine ごと残り続けるのを防ぐ」ために timeout を張った、と
書いている。**目的は正しいが手段が片方欠けている** (timeout だけでは戻らないケースがある)。

`RepoRoot` は issues 探索の起点なので、固まると **TUI が出る前の起動経路**で無音で止まる。

## 直し方

`cmd.WaitDelay` を張る。ただし `SubprocessWaitDelay` は `usage` パッケージにあり、
`issues` から素直に使うと `issues → usage` という妙な依存辺ができる
(同じ file が `repoRootTimeout` を「import できないので値だけ揃える」とコメントして
30s を写しているのも同根)。置き場は [issue 106](106-refactor-glogx-shared-discipline-copied.md) と併せて決める。

## 発火頻度について (正直な見積もり)

`git rev-parse --show-toplevel` が孫を残す経路は通常ない (hook もページャも走らない)。
**実害を実測で再現できてはいない**。それでも直す価値があるのは、
規律 (「`.Output()` には必ず WaitDelay」) が 9 箇所中 8 箇所で守られていて、
1 箇所だけ黙って外れている状態そのものが次の複製元になるため。

---

## 対応 (2026-08-26)

「1 行足す」ではなく**忘れられない形**にした:

- `src/glogx/subproc/` を新設し、`WaitDelay` と `GitOpTimeout` の正本をそこへ置いた。
  README の基準「サブパッケージを切るのは実在する第二消費者があるとき」に合致する
  (`issues` が実際に消費者で、消費できないから値を写していた)
- `subproc.CommandContext(ctx, name, args...)` を用意し、`issues/discover.go` はこれを使う。
  WaitDelay を張るのが「書く人が覚えているか」に依存しない
- `issues/discover.go` の `repoRootTimeout = 30s` の写しは削除し、`subproc.GitOpTimeout` を参照
- `usage.SubprocessWaitDelay` / `gitlog.go` の `gitOpTimeout` は別名として残した
  (既存の 12 箇所と、値を参照している既存コメントをそのまま保つため)

### 再発を止める

`src/glogx/waitdelay_discipline_test.go` を新設。ソースを走査して
「`exec.CommandContext` を呼んだら 8 行以内に `WaitDelay` が要る」を強制する。
例外は行内に `subproc: no-waitdelay` と理由を書く (現在 1 件: `xdg-open` の `Run()` は
stdout/stderr が /dev/null 直結でパイプを作らないため不要)。

**この検査が本当に効くことを、修正を revert して確認した**: `issues/discover.go` を素の
`exec.CommandContext` に戻すと、その行を名指しで落ちる。走査対象 0 件も fail にしてある
(パス変更や WalkDir 失敗で検査が静かに消えるのを防ぐ)。
