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

## 受け入れ条件

- [ ] 役割ごとの設定ファイルがあり、daemon がそれを渡して起動する
- [ ] 起動時の context の token を、絞る前 (約 13 万) と比べた実測値がある
- [ ] PG が、自分の触っていない issue を Stop hook に促されて書き換えにいかない

## 関連ファイル

- `_claude/settings.json` (ユーザーの設定。ここを PG 用に写さず、PG 用は別ファイルにする)
- `_claude/hooks/issue-progress-check.sh`

## 進捗

- [ ] 未着手 (427 の前にやる)。下調べだけ済み (2026-09-24)。計測は週の利用枠のリセット後
