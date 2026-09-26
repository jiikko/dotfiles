# procsup — 子プロセスを 1 つ起こして見張る (ワンショットの supervisor の部品)

foreman / supervisord の 1 本ぶん。**呼んだプロセスが生きている間だけ**見張る (launchd などへの常駐の登録はしない)。
依存はゼロ (標準ライブラリのみ)。仕様と失敗モードの一次情報は `procsup.go` の doc コメント。

使っているところ: `src/pro-con` の supervisor (`supervise.go`。dispatcher を子に持つ。issue 506) と見張り (`monitorsup.go`。issue 475)。
取り込むには go.mod に `require procsup v0.0.0` と `replace procsup => ../procsup`。

## すること / しないこと

| procsup が持つ | 呼ぶ側が `Spec` の関数で決める |
|---|---|
| 子を起こす・抜けたら `RestartWait` 空けて起こし直す | 子の終わり方の意味 (`Classify`: `Crash` 数えて起こし直す / `Retry` 数えずに起こし直す / `Done` 見張りを終える) |
| `CrashWindow` に `CrashLimit` を超えて落ちたら諦める | 起こし直す前に続けてよいか (`Continue`。初回の起動の前は呼ばない) |
| 止める合図 (ctx の取り消し) で止める: 生命線を閉じる → `StopSignal` → `StopWait` 後に SIGKILL | 出来事の言葉 (`OnEvent`) |
| 生命線 (`Lifeline`): 子の stdin を親だけが書く側を持つパイプにする (親が kill -9 で死んでも子の stdin が EOF になる) | 子が stdin の EOF で抜けること (生命線は子が読んで初めて効く) |

```go
res := procsup.Run(ctx, procsup.Spec{
	Command:  func() (*exec.Cmd, error) { return exec.Command("worker", "--until-stdin-closes"), nil },
	Lifeline: true,
	Classify: func(err error) procsup.Outcome {
		if procsup.ExitCode(err) == 0 {
			return procsup.Done
		}
		return procsup.Crash
	},
	RestartWait: 10 * time.Second, CrashLimit: 3, CrashWindow: 10 * time.Minute, StopWait: 5 * time.Second,
})
// res.Reason: ReasonStopped / ReasonDone / ReasonHalted / ReasonGaveUp / ReasonStartFailed
```

裏で回すなら `stop := procsup.Start(ctx, spec)` (返した関数で止めて結果を待つ。何度呼んでもよい)。

## 🚨 気をつけること

- 止める信号は**子のプロセスだけ**に送る (プロセスグループには送らない)。孫は子が止める
- 親のファイル (lock 等) を子へ渡さない: Go の開くファイルは CLOEXEC なので、`ExtraFiles` に入れなければ渡らない。
  生命線の書く側が子へ漏れると、親が死んでも EOF にならない (`TestLifelineSurvivesParentKill` が検査する)
- `CrashLimit` は「落ちてよい回数」。0 なら 1 回落ちたら諦める
