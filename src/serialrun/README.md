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
  - 🚨 「マシン全体」で 1 本になるのは、置き場の解決に使う環境 (`SERIAL_RUN_STATE_DIR` / 絶対パスの `XDG_STATE_HOME` / `HOME`) が同じ session どうしだけ。
    HOME を差し替えたテスト環境などから呼ばれたものは別の待ち列に並ぶ。相対の `XDG_STATE_HOME` は使わない (cwd ごとに別の待ち列になる)
- lock 名 (`-l`) は小文字・数字・`.` `_` `-` だけで、先頭の `.` は通さない (APFS は大小文字を区別しないので、`Default` と `default` が同じ置き場を別名で指す)

## Claude Code からの使われ方

`_claude/hooks/serial-run-wrap.sh` (PreToolUse(Bash)) が、重いコマンドの直前に `serial-run -- ` を差し込む
(`cd a && make test && git push` → `cd a && serial-run -- make test && git push`)。対象と字句の規則は `hook.go` の `isHeavy` / `wrapCommand`。

- 🚨 Claude Code の許可ルールは**書き換えた後のコマンド**で判定される (2.1.308 で実測)。コマンド全体を `zsh -c` で包むと他の部分への deny が
  効かなくなるので、重い単純コマンドの前にだけ差し込む。許可ルールを書くなら `Bash(serial-run -- make test:*)` の形
- 引用符・コマンド置換の中、heredoc・`case`・算術・`$'…'` を含むコマンドは書き換えない (文字列の中身を変えない。読み切れない形は元のまま通す)
- 1 つのパイプラインで包むのは最初の重い段 1 つだけ。🚨 **パイプを含むコマンドでは、差し込みは 1 か所まで** (2 か所になるなら全体を見送る)。
  1 つのパイプラインに serial-run が 2 本入ると互いの札を待って止まるので、字句で段の境目を読み違えてもこの形にならないための歯止め。
  手で書いた `serial-run` を含むパイプライン、複合コマンドの境目が `|` と隣り合う形、`|` の後で改行する形も見送る
- PATH を書き換えるコマンド (前置きの `PATH=` / `export PATH=…` / `unset PATH` / `eval`) は見送る (差し込んだ serial-run が見つからなくなる)
- make の dry-run 等 (`-n` `-q` `-p` `-t` `--help`)・go / swift / mutate-verify の help・xcodebuild の情報表示は待たせない
- hook の判断 (差し込んだ / 重いのに見送った理由) は置き場の `hook.log` に残り、`serial-run status` が最後の 5 行を出す
- 重いコマンドの頭に許可ルール (deny / ask) を書くなら、`serial-run -- ` を付けた形も書く (Claude Code は書き換えた後のコマンドで判定する)
- 置き場を作れない・採番できないときは、🚨 を出して順番を待たずに走らせる (包んだコマンドを走らせないより害が小さい)
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
- `ps` (子の開始時刻・状態・親) は `LC_ALL=C TZ=UTC` と時間の上限 2 秒で呼ぶ (`proc.go`)。「その pid は居ない」と「ps が使えない (判定不能)」を分け、
  判定不能の札は消さずに 5 秒ごとに見直す
- 持ち主の serial-run だけが死んで子が残った札 (孤児) は、子が生きている間は生きている扱い。子の同一性は開始時刻で確かめる (開始時刻を書けなかった札は、
  その pid が札を出した後に起きたかで見る)。子が停止 (stat T) したままなら、後ろの待ち手が子孫ごと止めて札を消す (持ち主が子孫を止める途中で殺された形)。
  ただし自分の祖先は止めない
- テストの差し込み口 (`SERIAL_RUN_TEST_*`) は `-tags serialrun_testseam` でビルドしたときだけ読む (`seam_on.go`)。本番のバイナリは読まない
- 子は serial-run と同じプロセスグループに置く (別に置くと、グループごとの SIGKILL で serial-run だけが死に、子が lock の外で走り続ける)。
  lock の fd は子へ渡さない (O_CLOEXEC)。子には `SERIAL_RUN_HELD=<置き場>:<番号>:<nonce>` を渡し、中からの同じ lock の呼び出しは待たずに通す (置き場は symlink を解決して比べる)。
  環境変数が途中で落とされても、先頭の札の持ち主 (または孤児の子) が自分の祖先なら素通しする
- 置き場は消さない・作り直さない (走っている持ち主は古い run.lock の inode を持っているので、作り直すと排他が破れる)
