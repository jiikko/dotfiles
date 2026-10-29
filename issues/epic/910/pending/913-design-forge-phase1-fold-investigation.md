# 913 (design): forge Phase 1 (事前調査) の専門家 fan-out を main の単一ループへ畳む

起票日: 2026-10-25

> **保留** (2026-10-29。ユーザー決定: 911 と一緒に保留)。前提の eval (911) を保留にしたので、A / B / C を比べる手段が無い。forge の出番が 09-16 以降無い (911 の「0 の結果」)。
> **再開の条件**: 911 と同じ (audit で forge を選ぶ回が戻る、または forge を単独で使う場面が増える)

epic [910](../910-design-fold-subagents-into-main-loop.md)。前提: [911](911-test-task-completion-evals-for-forge-audit.md) の eval。

## 概要

forge の実装モードは、Phase 1 で専門家を並行に調査させ (`forge.js` の `kind: investigate`)、Phase 1.1 で別の専門家にその findings をクロスレビューさせ、
統合エージェントがまとめたものを main が Phase 1.5 の設計書の材料にする (`_claude/skills/forge/phases-0-1.md`)。
ロスターは専門領域ごとで (`forge.js` の `resolveRoster` / `baseRoster`。Swift の Standard は必須 6 体で、investigate のときだけ research-assistant を足して 7 体)、
Maximum は全員 (`_common/modes.md`)。

記事に照らすと、この段は畳む候補の条件に重なる:

- 分割の軸が問題の種類 (専門領域) で、全員が同じ対象コードを読む
- 出力が次の段の入力になる連鎖 (専門家 → クロスレビュー → 統合 → main の設計) で、受け渡しのたびに要約される
- 設計を書く main 自身は対象コードを読まないまま設計する形になりうる

一方、Phase 4 (レビュー) と Phase 4.3 (Ultra) は「書いたものを外から確かめる」検証で、記事も残す側に置いている。**この issue は Phase 1 / 1.1 だけを対象にする**。

ロスターの体数 (6+1 / 全員) を決めた根拠は `_common/modes.md` に見当たらない (2026-10-25 に読んだ範囲)。

## タスク

- [ ] 911 の eval に forge 実装モードのケースを 1〜2 本足す (仕込んだ欠陥を直すタスク。正解表は「設計書が触るべきファイルと、避けるべき罠」)。
      Phase 1.5 の設計書の「変更ファイル一覧」を正解表とスクリプトで照合できる形にする (911 の「照合はスクリプト」に合わせる)。
      「避けるべき罠」に触れたかは LLM の採点になるので、911 の 2 回採点・一致率の手順を使う
- [ ] 案 A (現状): investigate の fan-out + 1.1 クロスレビュー
- [ ] 案 B (畳む): Phase 1 を main が自分で読む形にし、専門家の観点は `_claude/agents/<name>.md` の本文を reference として必要なときに Read する
      (比べるときは今の `_claude/agents/<name>.md` をそのまま Read する。skill へ移すかどうかの置き場所は、この issue の結論の後に 914 で決める)。類似コードの探索だけ Explore に委ねてよい
- [ ] 案 C (中間): investigate を 1〜2 体に絞り、クロスレビューと統合をやめて main が raw を読む (Minimum の形を Standard にも使う)
- [ ] **B / C には、設計段の別視点を残す段を必ず付ける**: `~/.claude/CLAUDE.md`「レビュー方針」は設計もレビューに通すことを求めていて、今の forge で main の外から
      設計の材料を見ているのは 1.1 だけ (1.5 は人の承認)。B / C では 1.5 の設計書を、観点を分けた read-only のサブエージェント (直列) に反証させる段を置き、その費用も比べる数字に入れる
- [ ] A / B / C を runs 回ずつ回し (判定規則は 911 に先に書いたもの。判別できなければ A のまま)、設計書の採点・費用・所要を比べる。**最終スコアだけでなく、どのケースで結果が反転したか**を見る (記事の手順)
- [ ] 結果に応じて `forge.js` / `phases-0-1.md` / `_common/modes.md` / `_common/cross-review.md` を同時に直す
      (forge SKILL.md の「仕様を変えたら forge.js も同期更新」)。Phase 4 / 4.3 は変えない
- [ ] 変えたら skill の整合チェック 3 回を回し、911 の driver で変更後の版を同じケースに回し直す (`bin/skill-eval forge` は forge のケースが無く「判定できない」で終わる。2026-10-25 に `--dry-run` で 0 件を確認)

## 受け入れ条件

- Phase 1 の形 (A / B / C) が eval の数字で選ばれ、選んだ理由が `phases-0-1.md` に書かれている
- 案 A を残すなら、残す理由 (どのケースで B / C が劣ったか) が書かれている

## 未確認

- `forge.js` は、`agentType: item.name` で起動した専門家が `_claude/agents/<name>.md` の本文をシステムプロンプトとして読み込む**前提**で書かれている
  (`_common/agents.md` 冒頭の注記: プロンプトのテンプレートは agentType のシステムプロンプトに委ね、forge.js は簡潔版だけを持つ)。実際にそう動くかは**未実測**。
  読み込まないなら、今の fan-out は「専門家」ではなく、観点の 1 行 (lens) が違うだけの同じモデルを並べていることになる。最初に 1 本で確かめる
  (ロスターに入る `Explore` は組み込みの agent で `_claude/agents/` に本文が無いので、確かめる対象から外す)
  - 2026-10-29 (911 の 0): frontmatter は効いている。10-03 の forge の run で、`dependency-analyzer` は定義の `model: sonnet` どおり sonnet、他の専門家は定義どおり opus で走った。
    本文が system prompt に入ったかは transcript に残らないので直接は見ていない (同じ定義ファイルが `agentType` で読まれていることまでは確か)

## 関連ファイル

- `_claude/workflows/forge.js` (`resolveRoster` / `investigatePrompt` / cross-review の pipeline)
- `_claude/skills/forge/phases-0-1.md` / `_common/modes.md` / `_common/agents.md` / `_common/cross-review.md`

## 進捗

- 2026-10-29: 本文を今のコードと突き合わせた。`resolveRoster` / `baseRoster` / `investigatePrompt`、`_common/agents.md` の注記、Standard の 6+1、forge の skill eval が 0 本は本文どおり。
  modes.md の「根拠」の表はスコアからモードを選ぶ表で、体数の根拠ではない (本文の「見当たらない」は今もそのとおり)。911 と一緒に保留にした
