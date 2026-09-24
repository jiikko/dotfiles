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

## 受け入れ条件

- [ ] 役割ごとの設定ファイルがあり、daemon がそれを渡して起動する
- [ ] 起動時の context の token を、絞る前 (約 13 万) と比べた実測値がある
- [ ] PG が、自分の触っていない issue を Stop hook に促されて書き換えにいかない

## 関連ファイル

- `_claude/settings.json` (ユーザーの設定。ここを PG 用に写さず、PG 用は別ファイルにする)
- `_claude/hooks/issue-progress-check.sh`

## 進捗

- [ ] 未着手 (427 の前にやる)
