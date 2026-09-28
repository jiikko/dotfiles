# pro-con

PM (producer) と PG (consumer) を分けて Claude Code を並列に回すための TUI。設計の正本は issue 415 (epic)。用語・使い方・デバッグは `pro-con help <話題>` (本文は `help/*.md`) が正本で、ここには実装側の事情とファイルの場所だけを書く。外から動かす Claude の入口は skill `pro-con` (`_claude/skills/pro-con/SKILL.md`)。

## ファイルの地図 (package)

- `card/` — ドメイン (依頼カードの状態・不変条件)。UI にも backend にも依存しない
- `backend/` — UI と「状態を持つ側」の境界。UI は `Snapshot` を読み `Command` を送るだけ
- `live/` — 本番 backend。記録 (store) からカードを読み、pro-con が起動した session の様子を足す
- `fake/` — claude を起動しない模擬 backend (`--mock` 用。壁時計でなく刻みで進む)
- `store/` — 本物モードのカードの記録 (cards.json)。書き手は dispatcher だけ。受付の箱への Submit と Apply
- `dispatcher/` — 本物モードの中心 (Tick)。箱の適用・PG/PM の起動再開・close・orders・btw・review・upgrade 等をサブファイルごとに持つ
- `agents/` — `claude agents --json` で今動いている session を一覧する
- `eventlog/` — dispatcher の出来事の記録 (events.jsonl)
- `metrics/` — 閉じたカードの所要の記録 (metrics.jsonl)。`stats` コマンドが読む
- `diskuse/` — pro-con が作った物のディスク使用量の測定 (`du` コマンド)
- `monitor/` — 見張り (`pro-con monitor`)。取り込みの衝突・テスト順番の長さを読むだけで判定
- `wtclean/` — 閉じたカードの PG の worktree/branch/session の片付け判定 (`Judge`) と実行
- `presence/` — 開いている画面の数え上げ (flock ベース)
- `relay/` — 画面が描いた最新の 1 枚を `pro-con screen` へ渡す中継
- `wake/` — dispatcher を即時に起こす Unix socket
- `schedule/` — dispatcher が決まった時刻に回す予定表 (`Jobs`)
- `upgrade/` — 動いている pro-con 自身のライブアップグレード (ctrl+r で syscall.Exec)
- `foreground/` — 画面が端末前面のプロセスグループを持っているかの確認・取り戻し
- `gitx/` — pro-con が git を呼ぶ共通の口 (`-C` の落とし穴に注意。GIT_DIR 等の継承)
- `config/` — `~/.config/pro-con/config.toml` の読み込みと repo 列挙
- `ui/` — TUI 本体。状態は backend が持ち、ここは描画と Command 送信のみ
- `help/` — `pro-con help` が出す話題ごとの本文 (`debug.md` / `flow.md` / `terms.md` / `usage.md`)
- `pm-guide.md` / `integrator-guide.md` — PM / 取り込みの係の session に渡す指示書 (`card guide` / `card guide --integrator` が出す)
- `samples/` — 過去の issue 番号ごとの動作確認・回帰サンプル

## 入口 (CLI サブコマンド、main.go が dispatch)

`bin/pro-con` (本物) / `--mock` (模擬) / `--view` (見るだけ) と、`main.go` が振り分けるサブコマンド (`card` / `dispatcher` / `monitor` など)。各サブコマンドの実装は同名の `*cmd.go` (例: `card` → `cardcmd.go`)。一覧とフラグは README.md 冒頭の「起動のしかた」表と `pro-con help`

## ビルド・テスト

- `make -C src/pro-con lint` / `test`

## 詳しくは

- README.md 全体 (起動のしかた・画面と dispatcher のつながり・即時割り振り・複数画面・終了の規則など)
- `pro-con help usage` / `help flow` / `help terms` / `help debug` (状態の置き場・デバッグ手順の正本)
