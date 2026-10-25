# 916 (docs): rules / CLAUDE.md の古いモデル向け指示と手順型の節を棚卸しする

起票日: 2026-10-25

epic [910](910-design-fold-subagents-into-main-loop.md)。他の子と独立。

## 概要

記事は、各チームがシステムプロンプトに 1 文ずつ足していくと規則が衝突し、一貫性が失われる形を「Frankenprompt」と呼ぶ。
対策として、手順は skill (必要なときに読む) へ、絶対に守らせる制約はツールへ移し、古いモデル向けに書かれた指示は棚卸しする
(記事が挙げる道具は `/claude-api prompt-audit`)。

dotfiles の常時読み込みは、retro のたびに 1 項目ずつ足される形で育っている。2026-10-25 の量:
`_claude/rules/*.md` が 35 ファイルで合計 239,035 byte (`wc -c`。`paths:` で条件ロードされるものを含む。日本語なので文字数はこれより少ない)、
`_claude/CLAUDE.md` が 29,423 byte。既にやっている対策: なぜ・起源を `rules-rationale/` へ、長い手順を `rules-checklists/` へ切り出す /
`paths:` の条件ロード / 強制できるものを hook・スクリプトへ移す (tmux の shim と hook、`scripts/issue_claim.sh` / `issue_done.sh`) /
retro からの新規ルールを入口で絞る (issue-rules の retro 節)。

## タスク

- [ ] **古いモデル向けの指示を探す**: 「現行モデルは〜しがち」のような、特定のモデルの癖を前提にした指示を rules / CLAUDE.md から列挙する
      (例: `~/.claude/CLAUDE.md`「応答・成果物の長さとスコープ」の Opus 5 / Opus 5.5 / Fable 5.1 の記述、`subagent-model-tiering.md` の「現行モデルは指示なしでも積極的に委譲する」)。
      今のモデルで前提が成り立つかを、headless のセッションで指示を外した A-B で確かめられるものから確かめる。
      `claude-api` skill に `prompt-audit` があるかは未確認 (あれば使う。無ければ手で列挙する)
- [ ] **ユーザーの決定を分ける**: 「ユーザー指示 YYYY-MM-DD」「〜決定」のように日付つきのユーザーの決定から来た項目は、「提案」の列に分け、直す・消すかはユーザーに諮る
      (`~/.claude/CLAUDE.md`「レビュー方針」: 既存のユーザー決定は、ユーザーが変更を指示するまで維持する)。A-B で確かめるときも、runs と判定規則を先に決める (1 回ずつの A-B では差を判別できない)
- [ ] **手順型の節を列挙する**: 規範ではなく手順 (コマンドの並び・heredoc の書き方・worktree の作り方) が本文に入っている節を挙げ、
      ①スクリプト / hook で強制できる → 道具へ ②特定の作業のときだけ要る → skill か `rules-checklists/` へ ③常に要る → 残す、に仕分ける
- [ ] **衝突を探す**: 同じ状況に別々の rule が違う指示を出している組を挙げる (例: 一時ファイルの置き場所はハーネスの scratchpad と `~/.claude/CLAUDE.md` の「本体 checkout の tmp/」が逆を言っていて、
      CLAUDE.md 側が優先を明記している。こういう明記の無い組を探す)
- [ ] 仕分けの結果を表にしてこの issue に残し、移す・消すものは 1 本ずつ別の commit にする (一括改変はしない。`_claude/` を触るときの dotfiles の CLAUDE.md の規約)
- [ ] 移した後、`wc -c` で常時読み込みの量を測り直して記録する

## 受け入れ条件

- 古いモデル向けの指示の一覧と、それぞれ「確かめて残す / 直す / 消す」の判断が書かれている
- 手順型の節の仕分けの表があり、①②に仕分けたものの移動先が決まっている

## 関連ファイル

- `_claude/rules/` / `_claude/rules-rationale/` / `_claude/rules-checklists/` / `_claude/CLAUDE.md`
- `_claude/issue-rules.md` (retro 節: 新規ルールを入口で絞る)

## 進捗

起票のみ。
