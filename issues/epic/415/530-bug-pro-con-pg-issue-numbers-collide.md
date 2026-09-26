# 530 (bug): PG が起票した issue の番号が master と衝突する (PG は push しないので採番が見えない)

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

外から動かす Claude の確認 (カード C-083) に、ユーザーが issue にすると答えた。PG は push しない (521) ので、PG が worktree で採番した issue 番号は、
取り込まれるまで master 側から見えない。その間に master で同じ番号が使われると衝突する。

- 実例: 監査 C-071 (513) は起票した issue を **6 回付け直した** (516・517 → 518・519 → 521・522 → 523・524 → 525・526 → 527 / 528・529。git log の「C-071」の改番の commit 6 本)
- issue の規約 (`_claude/issue-rules.md`) は「採番したら即 commit & push」で衝突を防ぐ前提で、push しない PG には効かない

## 期待する動作

- PG が起票するときに、master と重ならない番号が取れること (取り込みの係が改番して回らなくてよいこと)
- 実装の選択は PG が決めてよい。候補: dispatcher (か PM) が番号を払い出す口を作る (`pro-con card issue-number` のような。払い出した番号を状態の置き場に記録し、二重に出さない) /
  PG の起票を仮の名前 (番号なし) にして、取り込みの係が取り込むときに採番する
- 取り込みの係の指示書 (`integrator-guide.md`) と PG への指示 (`Prompt`) に、起票のしかたを 1 か所から書く

## 関連

- 521 (PG は push しない) / 513 (C-071。改番の実例) / `_claude/issue-rules.md` の採番

## 順番の見積もり (PM, 2026-09-27。C-095 を C-093 の後に積んだ)

- 触る場所: PG への指示 (`dispatcher.go` の `Prompt`) と `integrator-guide.md` の、PG が issue を書く手順
- 変える判断: PG が issue をどう起こし・書き戻すか
- 順番の理由: 539 (C-093) が同じ `Prompt` と `integrator-guide.md` に「PG は受け入れ条件に印を付ける・取り込みの係が done へ移す」を足している最中。PG の issue の書き方の指示を 2 枚で別々に書かないよう、539 の後にする
- 経緯: 530 は C-083 に付けて積んだが、C-083 は 530 を実装しないまま閉じられた (進捗も実装の commit も無い)。C-095 で改めて積んだ
