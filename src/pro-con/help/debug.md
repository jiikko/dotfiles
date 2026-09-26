# デバッグ (pro-con help debug)

## まず見るもの (どれも読むだけ)

- `pro-con ps`: pro-con が起動したプロセスを役ごとに (pid・経過・状態・カード)。カードと session の食い違いは状態の列に「食い違い: 」
- `pro-con log --since 30m`: dispatcher の出来事。`--card <カード>` で絞る。落ちた = kind `crash`、起動時の復旧 = `recover`、
  除けた依頼 = `reject`、入れ替え = `upgrade`、起こし直し = `supervisor`、書けない・取れない = `error`
- `pro-con card show <カード>`: 履歴・質問・今の待ち・進捗・worktree のパス。PG が何をしたかは `pro-con card log <カード>`
- `pro-con du`: ディスクの使用量 (数秒かかる)。`pro-con screen`: 人の画面に今出ているもの

## 状態の置き場 (`~/.local/state/pro-con/live/`)

🚨 手で書かない・消さない。書き手は dispatcher だけ (画面と `pro-con card` は `inbox/` に置くだけ)。読むのは構わない。

| 置き場 | 中身 |
|---|---|
| `cards.json` | カードの記録 (正本)。片付けたものは `cards-archive.jsonl`、書庫からも消した印は `cards-purged.jsonl` |
| `inbox/` | 受付の箱。適用待ちの依頼。`rejected/` は除けた依頼、`files/` は添付の受け渡し |
| `events.jsonl` | 出来事の記録 (`pro-con log` が読む。1 MiB で `events.1.jsonl` へ回す) |
| `dispatcher.log` / `stop.log` | dispatcher (と supervisor) の標準出力 / 画面の quit で止めた処理の出力。落ちた理由はまずここ |
| `sessions.json` / `sessions-retired.json` | pro-con が起動した session の記録 / 再開で入れ替わった前の session |
| `pm.json` / `integrator.json` | PM / 取り込みの係の session と起動中の印 |
| `dispatcher-state.json` / `seen.json` | ゲージに出す様子 (役の様子・PG の枠) / dispatcher が取った session の一覧と出力の末尾 |
| `doing.json` / `progress.json` / `conflicts.json` / `diffs/` | PG が今走らせているもの / 進捗と worktree / 取り込みの衝突 / 差分の本文 |
| `runs/` | テストの係の実行のログ (`<カード>-<時刻>-<印>.log`) |
| `attachments/` | カードの添付 |
| `settings.json` | `pro-con config set` の値 (PG の枠・PM の数) |
| `*.lock` / `screens/` / `relay/` / `dispatcher.sock` | 2 つ起動しない印 (dispatcher / supervisor / monitor) / 開いている画面の印 / 画面の中継 / 起こす口 |
| `dispatcher-held` / `stop-result` | 人が止めた印 (`dispatcher --stop`) / 最後に止めた結果 |
| `resume-*.json` | ライブアップグレードで引き継ぐ画面の状態 (正常に終われば消える) |

## 止まっている・動かない

- ヘッダーに「dispatcher が動いていない」: 持ち主の画面を開けば起こす。「止めてある」なら人が止めた印なので画面の `c` か `pro-con dispatcher`
- supervisor が起こし直しを諦めた (10 分に 5 回を超えて落ちた) ときも人が止めた印が付く。`dispatcher.log` で落ちた理由を直してから起こす
- 依頼が適用されない: dispatcher が居るか (`pro-con ps`) と `pro-con log` の `reject` を見る
- PG が進まない: `pro-con card show <カード>` の「今の待ち」(利用枠・順番・テストの係・人の番) を読む
- 画面が止まった (シェルに `suspended` が出た。ctrl+z・外からの SIGTSTP・端末の前面を外された SIGTTIN): その pane で `fg`。画面は端末を入れ直して描き直す。
  止まっている間も dispatcher と PG は別のプロセスで動き続け、止まっている持ち主の画面も持ち主として数える (閉じた扱いにならない)。
  `fg` でも戻らなければ、**先に別の pane で持ち主の画面を開いてから**止まった pane を閉じる (持ち主の画面が 0 のまま 1 分たつと PG を止める)。
  pane を閉じずに止まった画面だけ終わらせた (`kill %1` 等) なら、その pane で `reset` (端末の設定が画面のまま残る)。止まった・前面を取り戻したことは `pro-con log` の kind `screen` に残る (issue 518)

## クラッシュ・再起動の後

- 何も打たなくてよい。dispatcher は起動したとき 1 度だけ、マシンの再起動で消えた session を調べて再開する (`pro-con log` の `recover`、
  ゲージに「起動時: 復旧 N / 判定できない M」)。判定できないものは、いつもの自動の再開 (1 分待ってから) に任せる
- テストの係の実行の途中で落ちた実行は 1 度だけ頼み直す (また中断したら rc=-1 で PG に返す)

## ライブアップグレード

- 画面: ソースを直すと裏でビルドされ、ヘッダーに「新版あり ctrl+r」。ctrl+r で PID のまま新版へ入れ替わる (状態は引き継ぐ)。
  ビルドの失敗は `src/pro-con/.autobuild.log`。新版が異常終了したら `resume-*.json` が残り、`PRO_CON_RESUME=<パス> bin/pro-con` で戻る
- dispatcher: 合図なしに、区切り (テストの係の実行中などでない) で自分を新版へ入れ替える。PG は止まらない。
  新版が今の記録を読めなければ旧版のまま続ける。結果は `pro-con log` の `upgrade` とゲージ
- 仕組みの詳細は `src/pro-con/README.md` の「ライブアップグレード」
