# ratelimit

Claude Code / codex の利用枠 (5h / weekly) を取得・整形する module と、単独コマンド `bin/ratelimit`。使い方・境界と制約の正本は README.md、フラグの詳細は `main.go` 冒頭。

## ファイルの地図

- `main.go` — `bin/ratelimit` のエントリポイント。フラグ解析・キャッシュ読み書き・閾値判定 (`-check`)
- `usage/usage.go` — Claude 側の取得の本体 (`fetchClaude` / `FetchVersion`)。usage パッケージは glogx / bubbletea に依存しない (tuikit / termsafe / subproc / atomicfile / doctor/cachedir には依存する)
- `usage/shared.go` — Claude 取得の全プロセス共有ゲート (`Fetch`。最後の結果の共有・5 分の間引き・429 時の 30 分停止。理由は README の「境界と制約」)
- `usage/codex.go` — codex 側の取得 (`FetchCodex`。`codex app-server` の JSON-RPC 経路。選定理由はファイル冒頭) と両方をまとめる `FetchAll`
- `usage/pace.go` — ペースゲージの計算とゲージの読み方 (`_claude/statusline-command.sh` の `pace_row` と二重実装。乖離は `usage/pace_drift_test.go` が突き合わせる)
- `usage/render.go` — 1 行・表形式の整形 (lint で I/O 禁止・stdout 直書き禁止)
- `usage/banner.go` — ブロック文字 AA (大見出し・盤中央の使用率)
- `usage/dial.go` — 全画面ダッシュボードのアナログ盤描画
- `usage/braille.go` — `dial.go` 専用の点描キャンバス (2x4 ドット)
- `gorules/` — ruleguard のカスタム lint 規則 (build tag で通常ビルドから外れる。規則と理由の正本は `src/glogx/gorules/rules.go`)
- `exec_boundary_test.go` — 横断検査。非テストの `.go` が `os/exec` を直接 import しないこと (`subproc.CommandContext` 経由を強制) を固定する

## 入口

- `bin/ratelimit` (`main.go`)。Claude Code の UserPromptSubmit hook (`_claude/hooks/ratelimit-warn.sh`) が Claude 枠を、codex 系 skill が codex 枠を見る
- `usage.FetchAll` などの取得と `usage.RenderDashboard` — glogx の利用枠オーバーレイ・ダッシュボードが replace で取り込む

## ビルド・テスト

- `make -C src/ratelimit lint` (`vet-gorules` を含む) / `test`

## 詳しくは

- README.md の「境界と制約」(外部プロセス起動の一本化・キャッシュの分離理由・termsafe 通過・pace 二重実装の突き合わせ)
