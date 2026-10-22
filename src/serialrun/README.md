# serialrun

重いコマンド (`make test` / `go test` / `xcodebuild` / `mutate-verify` 等) を**マシン全体で順番待ち (FIFO)** にし、同時には 1 本だけ走らせる CLI。
並行して動く Claude Code の session がそれぞれの worktree で重いコマンドを打っても、マシンが重くならないようにする。入口は `bin/serial-run`。
設計の正本は issue 869。

```sh
serial-run [-l <lock名>] [--max-wait <秒>] -- <コマンド> [引数…]   # 順番を待ってから実行し、子の rc を返す
serial-run status [-l <lock名>]                                     # 先頭と待ち行列を出す
serial-run hook                                                     # Claude Code の PreToolUse(Bash) hook の本体
```

- rc: 子の rc (シグナルで死んだら 128+n) / `125` 道具の失敗 (誤用・置き場・待ちの上限・入口のビルドの失敗) / `126` 起動できない / `127` 見つからない
- 待ちの上限は既定 1 時間 (`--max-wait 0` で上限なし)。超えたら子を起こさずに 125
- 待っている間は stderr に順番と先頭の持ち主を出す (順番が変わったときと 30 秒ごと)。先頭が 30 分を超えて持っていたら `🚨 長く持っている` と pid・状態を出す
- 状態の置き場: `~/.local/state/serial-run/<lock名>/` (`XDG_STATE_HOME` / テストは `SERIAL_RUN_STATE_DIR`)。`history.log` に 1 本 1 行 (待ち・所要・rc)

## Claude Code からの使われ方

`_claude/hooks/serial-run-wrap.sh` (PreToolUse(Bash)) が、重いコマンドの直前に `serial-run -- ` を差し込む
(`cd a && make test && git push` → `cd a && serial-run -- make test && git push`)。対象と字句の規則は `hook.go` の `isHeavy` / `wrapCommand`。

- 🚨 Claude Code の許可ルールは**書き換えた後のコマンド**で判定される (2.1.308 で実測)。コマンド全体を `zsh -c` で包むと他の部分への deny が
  効かなくなるので、重い単純コマンドの前にだけ差し込む。許可ルールを書くなら `Bash(serial-run -- make test:*)` の形
- 引用符・コマンド置換の中、heredoc・`case`・算術を含むコマンドは書き換えない (文字列の中身を変えない。読み切れない形は元のまま通す)
- 字面で見えない重い処理 (スクリプトの中の `go test`) は包まれない。必要ならその道具の側で `serial-run` を呼ぶ (入れ子は素通しになる)
- 前に誰かいるときは、差し込むと同時に LLM へ「順番待ちに入る・前に N 本・先頭の持ち主・`serial-run status`」を `additionalContext` で知らせる
  (待っている間は Bash の出力が返らないので、固まったと読ませない)。誰もいなければ何も添えない
- 止め方: Claude Code の環境に `SERIAL_RUN_HOOK_DISABLE=1`

## 排他と順番 (崩すと 2 本同時に走る。変更時はテストの変異まで戻ること)

- 生死は `flock(2)` だけで決め、pid を見ない (札は持ち主が開いている間 flock を持ち、プロセスが死ねばカーネルが外す)
- 採番 (`seq`) から札の rename までを seq の flock の中で行う (離すと、札がまだ無い間に後から来た人が先頭だと読む)
- `seq` は固定長で同じ位置に上書きする (truncate・tmp + rename にしない)。壊れていたら残っている札の最大 + 1 から続ける
- 札は `.tmp-<id>` で作って flock してから rename する (src/pro-con/flockmark の Create と同じ理由)
- 待ちはイベント駆動: 自分の 1 つ前の札に blocking の flock をかけて眠る。保険として 2 分ごとに列挙し直す (blocking の待ちが起こされなかったとき)
- 実行権は `run.lock` の flock。札の順番は公平さ、run.lock は排他を受け持つ
- 子は serial-run と同じプロセスグループに置く (別に置くと、グループごとの SIGKILL で serial-run だけが死に、子が lock の外で走り続ける)。
  lock の fd は子へ渡さない (O_CLOEXEC)。子には `SERIAL_RUN_HELD=<置き場>:<番号>` を渡し、中からの同じ lock の呼び出しは待たずに通す
- 置き場は消さない・作り直さない (走っている持ち主は古い run.lock の inode を持っているので、作り直すと排他が破れる)
