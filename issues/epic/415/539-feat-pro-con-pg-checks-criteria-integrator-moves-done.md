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

## 進捗

- [x] PG: `Prompt` に、関わる issue があるカードだけ「review の前に確かめた受け入れ条件の印 (`- [x]`) と進捗を書いて commit する。確かめていない条件に印を付けない。done へは移さない」の行を足した (「終えたら review」の行の前)。
  テスト `TestPromptAsksIssueCheckmarksOnlyWithIssues` (issue の無いカードには書かない・review 行より前)
- [x] 取り込みの係: `integrator-guide.md` の役目 3 に、push の後に受け入れ条件を見て、全部に印があれば `scripts/issue_done.sh` で done へ移して push、残りがあれば本文に 1 行書いて open のまま、印は diff とテストで裏を取る、を足した
- [x] 既存の `TestPromptClaudeUnchanged` は 514 の前の文と丸ごと比べていて PG の規律を足すと落ちるので、テストのコメントの指示どおり「claude のとき codex の行が無く、担い手の欄で変わらない」を見る形へ直した (commit「pro-con: claude の PG への指示のテストを 514 の前の文との丸ごと比較から codex の行が無いことを見る形へ」)。claude でも codex の行を出すように壊すと落ちることを確かめた
- 実測: `make -C src/pro-con lint test` rc=0 (37s)
- 残り: 指示の文が効くのは次に起動する PG / 取り込みの係から (動いている session の指示は変わらない)。実運用での観測は未検証
