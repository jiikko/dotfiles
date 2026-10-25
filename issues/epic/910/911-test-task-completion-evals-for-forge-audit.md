# 911 (test): forge / audit のタスク完了 eval を作る (今は「発火するか」の eval しかない)

起票日: 2026-10-25

epic [910](910-design-fold-subagents-into-main-loop.md) の前提。912 / 913 / 914 は、この eval で変更の前後を比べてから決める。

## 概要

`_claude/skill-evals/` にあるケースは 11 skill 分で、どれも「skill が発火するか / 選択肢を出すか」しか見ていない
(2026-10-25 時点: `audit/invoked` は `offers-ci` / `offers-concurrency` の grader、`codex-drive/fires` は `fires` の grader。forge はケースが無い)。
fan-out を畳む・残すの判断には「仕込んだ欠陥を見つけられたか」のような**タスク完了**の eval が要る。
記事の実例では、統合で壊れた挙動が **2 回に 1 回しか出なかった**ので、各ケースを複数回回す。

## タスク

- [ ] **fixture の repo を作る**: 小さな Go か shell の repo に、既知の欠陥を 5〜10 件仕込む。監査タイプ (concurrency の K1 check-then-act・false-green の F2 対象 0 件が合格・
      encapsulation の E2 など) と forge のレビューで見つかるべきものに対応させ、仕込んだ欠陥の一覧 (正解表) をケースの外に置く。
      fixture は `_claude/skill-evals/<skill>/<case>/` の下で、skill の link に入らない場所に置く (`bin/skill-eval` の冒頭の注記)
- [ ] **audit のケース**: 監査タイプ 2〜3 本 (concurrency / false-green / encapsulation) を、「直接実行」と「forge Minimum+ / Standard」で回せる形にする。
      grader は「正解表の欠陥を何件見つけたか」「正解表に無い指摘のうち発火条件の無いものの件数」を数える
- [ ] **forge のケース**: レビューモード (`/forge <path>`) で、仕込んだ欠陥の検出数を数えるケース。実装モードは対話 (AskUserQuestion) を挟むので、
      モードを明示して Phase -1 を飛ばせる形 (呼び出し元がモードを明示したときの省略。forge SKILL.md の Phase -1) で作れるかを先に確かめる
- [ ] **費用と所要も記録する**: `claude plugin eval` の出力から 1 回あたりの費用・ターン数・所要を取り、検出数と同じ表に残す
      (記事の比較軸は品質・コスト・遅延の 3 つ)
- [ ] **runs を 3 以上**にし、ケースごとのばらつき (検出数の最小・最大) を出す
- [ ] 1 回の比較にかかる費用を見積もり、`--max-cost-usd` の既定 (1 USD) で足りるかを書く。足りなければ上限をケースごとに決める

## 受け入れ条件

- forge (レビューモード) と audit (2 タイプ以上) について、検出数・偽陽性数・費用・所要を runs 回ぶん出せる
- 正解表を変えた fixture で grader が実際に数を変えること (欠陥を 1 件消すと検出数が減ること) を 1 回確かめる (grader 自体の空振りを除く)

## 未確認

- forge の fan-out は Workflow ツールで起動する。`claude plugin eval` の sandbox で Workflow が動くかは未確認 (最初に 1 本で確かめる)
- 直近 90 日の transcript では、forge.js の専門家 (`agentType` に agent 名が入る形) の起動が `~/.claude/projects` の jsonl から見つからなかった
  (2026-10-25 に `"agentType":"…"` を数えて general-purpose 33 / Explore 7 のみ)。workflow の agent が transcript にどう残るかを確かめていないので、
  「forge は使われていない」とは言えない

## 関連ファイル

- `bin/skill-eval` / `_claude/skill-evals/`
- `_claude/skills/forge/SKILL.md` / `_claude/workflows/forge.js`
- `_claude/skills/audit/SKILL.md`

## 進捗

起票のみ。
