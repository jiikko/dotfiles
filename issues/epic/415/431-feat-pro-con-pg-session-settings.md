# 431 (feat): pro-con の PG / 係の session 用の設定 (hook と規約を絞る)

起票日: 2026-09-24

親: [415](415-design-claude-pm-worker-orchestration.md) / 実測の出典: [425](done/425-research-claude-bg-remaining-measurements.md) の結果 6

## 概要

`claude --bg` で起こした session にも、ユーザーの hook と規約 (`_claude/rules/` など) が全部効く。425 の実測では:

- 1 語を返すだけの session で、起動時の context が約 13 万 token (cache_creation 107,001 + cache_read 26,079)。PG・係を 1 体起こすたびにかかる
- Stop hook (`_claude/hooks/issue-progress-check.sh`) が、その session の触っていない issue を「更新漏れの疑い」と言い、
  Read → Edit → EnterWorktree → Edit まで進ませた (権限の確認で止まり、書き込みは起きていない)。どの commit を「関わった」と数えたかは未確認

役割 (426 の決定 6: PG / テストの係の要約役 / 調べる係 / レビューの係) ごとに、要る hook と規約だけを載せた設定で起動する。

## 対応方針 (叩き台)

- `claude --bg --settings <file>` で役割ごとの設定を渡す。どこまで絞れるか (ユーザーの `~/.claude/` の規約・hook を外せるか、足すだけか) を先に実測する
- 役割ごとに要るものを決める: PG は worktree・commit・テストの規約は要る / 調べる係とレビューの係は読み取りだけなので編集系の規約は要らない
- 絞った設定で、起動時の context の token を 425 と同じ方法で測り直す (前後を比べる)
- Stop hook の issue 進捗チェックは、PG では外すか、PG が触った issue だけを見る形にする

## 下調べ (2026-09-24。`claude --help` を読んだだけで未計測。Claude Code 2.1.281)

- `--bare`: hook・auto-memory・CLAUDE.md の自動の読み込みを飛ばす最小のモード。🚨 **認証は ANTHROPIC_API_KEY か apiKeyHelper だけで、
  サブスクリプションの OAuth / keychain を読まない**。今の使い方 (サブスクリプション) では PG に使えない
- `--setting-sources <user,project,local>`: 読む設定の出どころを選ぶ。`project,local` にすれば、ユーザーの設定 (`~/.claude/settings.json` =
  `_claude/settings.json`。hook はここ) を外せる見込み。**規約 (`~/.claude/CLAUDE.md` と `~/.claude/rules/`) がこれで外れるかは未確認** (設定と別の経路の可能性)
- `--restricted`: ユーザー・project・local の設定を無視し、Bash などコードを動かすツールと WebFetch を外す (`--tools` で戻せる)。
  読むだけの係 (調べる係・レビューの係) の候補
- `--exclude-dynamic-system-prompt-sections`: 機械ごとの部分 (cwd・git status 等) を最初の発言へ移し、session をまたいで prompt のキャッシュを使い回しやすくする。PG を何体も起こすときの候補
- `--system-prompt` / `--append-system-prompt`: PG の規律 (AskUserQuestion を使わない、pro-con のコマンドで質問する 等。426 の決定 2) を渡す口

計測の手順 (週の利用枠がリセットされてから。2026-09-24 の時点で 96% 使用済み。1 回 約 13 万 token): `claude -p --output-format json --model haiku "OK とだけ答えて"` を、
何も付けない / `--setting-sources project,local` / それに `--exclude-dynamic-system-prompt-sections` を足す、の 3 通りで回し、
usage (input / cache_creation / cache_read) と、Stop hook が走ったか (transcript の hook のレコード) を比べる

## 計測 (2026-09-24 / Claude Code 2.1.281 / haiku / `~/dotfiles` から `claude -p --output-format json "OK とだけ答えて"`)

| 起動のしかた | 起動時の token (cache_creation + cache_read) | hook | 規約 (instructions) |
|---|---|---|---|
| 何も付けない | 107,403 + 14,053 = 121,456 | SessionStart が走る | 載る |
| `--setting-sources project,local` | 95,696 + 17,890 = 113,586 (約 1 割減) | 走らない | 載る |
| それに `--exclude-dynamic-system-prompt-sections` | 96,583 + 16,985 = 113,568 | 走らない | 載る |
| PG 用の `CLAUDE_CONFIG_DIR` (空のディレクトリ) | 起動しない (`Not logged in · Please run /login`、rc=1。モデルは呼ばない) | — | — |

- **一番大きいのは規約の注入** (transcript の `instructions` の添付、約 14 万文字): `_claude/rules/` の全ルール (`~/.claude/rules/` の link) と、
  `~/dotfiles/CLAUDE.md` / `.claude/rules/` (project のもの)。ユーザーの分は設定ディレクトリ (`~/.claude/`) から読まれ、`--setting-sources` では外れない
- `--setting-sources project,local` で hook (SessionStart・Stop) は外れる (hook の添付が消えた)。Stop hook の問題 (425 結果 6) はこれで止まる見込み
- 次の手: **PG 用の設定ディレクトリ** (`CLAUDE_CONFIG_DIR`) を作り、PG に要るルールだけを link して起動する。ログインが要るので、
  人の操作を [433](433-human-login-pro-con-pg-config-dir.md) に切り出した。ログインの後に 4 行目を測り直す

## 🚨 前提 (2026-09-25 の監査 460 で分かったこと)

- PG 用の `CLAUDE_CONFIG_DIR` を渡すと、PG の transcript は `$CLAUDE_CONFIG_DIR/projects` に書かれるが、pro-con は `~/.claude/projects` に決め打ちで読む
  (`src/pro-con/main.go` の 2 か所・`live.New`)。そのままでは見張り (`dispatcher.watch`)・落ちた回数 (`restartsSince`)・カードの表示・428 の attach の記録が**黙って止まる**。
  `CLAUDE_CONFIG_DIR` を使うなら、同じ変更で projects の置き場を合わせ、見つからないことを出来事に出す

## 受け入れ条件

- [ ] 役割ごとの設定ファイルがあり、daemon がそれを渡して起動する
- [ ] 起動時の context の token を、絞る前 (約 13 万) と比べた実測値がある
- [ ] PG が、自分の触っていない issue を Stop hook に促されて書き換えにいかない

## 関連ファイル

- `_claude/settings.json` (ユーザーの設定。ここを PG 用に写さず、PG 用は別ファイルにする)
- `_claude/hooks/issue-progress-check.sh`

## 進捗

- [x] 下調べと計測 (2026-09-24。上の 2 節)。`--setting-sources project,local` で hook は外れる / 規約は外れない
- [ ] PG 用の設定ディレクトリでのログイン待ち (433)。その後: PG に要るルールを選んで link し、起動時の token を測り直す
