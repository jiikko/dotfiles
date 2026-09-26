# 539 (feat): issue の受け入れ条件の印は PG が、done への移動は取り込みの係が行う

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

外から動かす Claude の確認 (カード C-093) に、ユーザーが「PG が印・取り込みの係が done」と答えた。

- 今の指示: PG への指示 (`src/pro-con/dispatcher/dispatcher.go` の `Prompt`) は「関わる issue: …」を渡すだけ。取り込みの係 (`src/pro-con/integrator-guide.md` の役目 3) は
  push の後に `card close --issue` するまで。PM (pm-guide) は issue を書いて分けるまで
- issue の受け入れ条件の印と done への移動は、どの指示書にも無い。2026-09-27 だけで 17 件を issue-sync で手で移した。511 は実装済みでも受け入れ条件の印が 4 つ付いていなかった

## 期待する動作

- **PG**: review に出す前に、関わる issue の本文に、確かめた受け入れ条件の印 (`- [x]`) と進捗 (commit の subject・実測・残り) を書き、その commit も自分の worktree に入れる (`_claude/issue-rules.md` の「commit のたびに本文へ進捗を追記する」と同じ)。確かめていない条件には印を付けない
- **取り込みの係**: push が通った後に、関わる issue の受け入れ条件を見る
  - 全部に印があれば `scripts/issue_done.sh <NNN>` で done へ移し、その commit を push してから `card close --issue` する
  - 残りがあれば、残り (未着手 / スコープ外 / 未検証) を本文に 1 行書いて open のまま close する
  - 印を鵜呑みにしない: diff とテストの結果で裏を取れない印は外すか、差し戻す (415 論点 4 と同じ)
- 指示の文は 1 か所から出す (PG への指示は `Prompt`、取り込みの係は `integrator-guide.md`)。実装の選択は PG が決めてよい

## 関連ファイル

- `src/pro-con/dispatcher/dispatcher.go` の `Prompt` / `src/pro-con/integrator-guide.md` の役目 2・3 / `scripts/issue_done.sh` / `_claude/issue-rules.md`

## 関連

- 487 (取り込みの係) / 511 (印が付いていなかった実例) / 538 (取り込みの係のテスト。同じ integrator-guide の役目 2 を触る)
