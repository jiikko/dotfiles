# 910 (design): サブエージェントの fan-out を単一ループ + skill へ畳む余地を測って決める (epic)

起票日: 2026-10-25

## 目的

記事「エージェント構築: サブエージェントをメインのエージェントループに畳み込むべきとき」
(https://claude.com/ja/resources/articles/building-agents-when-to-fold-subagents-into-your-main-agent-loop) の判断基準を、
dotfiles の forge / audit / agents / codex-drive / rules に当てる。進め方は記事のとおり
**「eval を先に作る → 1 つずつ畳む → 結果が反転したタスクを複数回の実行で見る → 劣るものだけサブエージェントのまま残す」**。

記事の判断基準 (子 issue が共通で引く):

- サブエージェントに残すもの: 他の情報をほとんど必要とせずコンパクトな結果を返す作業 (独立した調査経路) / ブラックボックスとしての検証 /
  コンテキストの肥大化が測定できるほど効いている場合
- 畳む候補: 問題の種類 (専門領域・組織) ごとの分割 / 出力が次の段の入力になる連鎖 / 要約の受け渡しで情報が落ちる段 / 入れ子のループによる遅延
- 手順は skill (必要なときに読む)、絶対に守らせる制約はツール (hook・スクリプト) に置く

## 子 issue (着手順)

1. [911](pending/911-test-task-completion-evals-for-forge-audit.md) — forge / audit のタスク完了 eval を作る (**912 / 913 の前提**)。
   最初の一手は 911 の「0. 前提の確認」(専門家が agent の本文を読むか / forge の利用回数 / 1 run の実費)。結果次第で 912 / 913 を続けるかを決める
2. [912](pending/912-design-audit-default-direct-execution.md) — audit の forge 経路を直接実行に寄せる
3. [913](pending/913-design-forge-phase1-fold-investigation.md) — forge Phase 1 の専門家 fan-out を畳む
4. [914](914-refactor-agents-inventory-to-skills.md) — agents/ の棚卸しと skill への移行
5. [915](915-refactor-codex-drive-progressive-disclosure.md) — codex-drive の SKILL.md をフェーズのファイルに分ける (911 と独立。先に着手してよい)
6. [916](916-docs-rules-prompt-audit.md) — rules / CLAUDE.md の棚卸し (独立)

## 状態

- 2026-10-29: 911〜913 は保留 (ユーザー決定)。forge の出番が 09-16 以降無く、eval を作る価値が今は低い (911 の「0 の結果」)。912 は eval の要らない 2 つだけ済ませた。914〜916 を進める
