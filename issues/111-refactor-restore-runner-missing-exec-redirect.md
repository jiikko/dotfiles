# `tmux_restore_runner.sh` だけ `exec </dev/null >/dev/null 2>&1` を持たない

起票日: 2026-08-26
種別: refactor
優先度: **P3** (現状で実害は確認できていない。非対称の理由が不明なのが問題)

## 確認できた事実 (2026-08-26)

`grep -L 'exec </dev/null'` で 3 本を比較した結果、**`scripts/tmux_restore_runner.sh` だけ**が
このリダイレクトを持たない:

| スクリプト | `exec </dev/null >/dev/null 2>&1` |
|---|---|
| `scripts/tmux_periodic_save.sh` | あり |
| `scripts/tmux_server_watchdog.sh` | あり |
| `scripts/tmux_restore_runner.sh` | **なし** |

3 本とも tmux の `run-shell` 文脈から起動される点は同じで、**無音契約 (出力を tmux へ返さない)
は 3 本とも同じはず**。`run-shell` はコマンドの出力があると view-mode を開いてしまうため、
これは体感に出る種類の契約 (`tmux_log_kill_command.sh` のテストにも「run-shell エラーの
view-mode 化防止」の assert がある)。

## どちらかが要る

1. **揃える** — restore_runner にも同じリダイレクトを入れる。ただし**挙動変更**なので、
   現在この経路の出力に依存している何か (デバッグ出力・エラーの伝播) が無いか確認してから
2. **理由をコメントで残す** — 意図的に持たせていない (例: 復元中の失敗を tmux 側に見せたい)
   なら、その理由をコード直近に書く (`pending-issue-rationale-in-code.md`)

**どちらが正しいかはこの調査では判定できなかった**ため、着手時に `run-shell` 経由で
restore_runner が出力を出すケースを実際に作って確認すること。

## 出典

2026-08-25 の issue 078 / 079 の作業中に並行セッションが気づいたもの。
コード上の非対称は本 issue の起票時に独立に再確認した。

## trigger

復元経路 (`tmux_restore_runner.sh` / `_tmux.conf` の restore hook) を次に触るとき。
issue 104 (復元経路の目視確認) と同時に見ると効率が良い。
