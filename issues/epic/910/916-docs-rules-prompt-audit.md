# 916 (docs): rules / CLAUDE.md の古いモデル向け指示と手順型の節を棚卸しする

> 🚨 **担当中: air5 (glogx)**（2026-10-29〜）

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
      🚨 指示を外すために共有の `~/.claude/CLAUDE.md` や `_claude/rules/` を書き換えない (並行するすべてのセッションに効く)。設定を写した別の場所から起動する方法を先に確かめる (未確認)。
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

## 結果 (2026-10-29)

### 本文の前提の訂正

- `claude-api` skill の prompt-audit は**ある**。2026-09-28 に dotfiles の全プロンプト面へ回し、指摘を全部適用している ([575](../../done/575-docs-prompt-audit-2026-09-28.md))。
  この issue は 575 の後に起票したが 575 を踏まえていなかったので、575 で決着したものは出し直さず、575 が見ていない所だけを見た
- 量は起票時より増えていた: `_claude/rules/*.md` 36 本・245,686 byte (起票時 35 本・239,035)、`_claude/CLAUDE.md` 31,579 byte (同 29,423)。`paths:` で条件ロードするものは 6 本

### 古いモデル向けの指示

| 場所 | 文 | 判断 | 理由 |
|---|---|---|---|
| `_claude/CLAUDE.md`「応答・成果物の長さとスコープ」 | Opus 5.5 / Fable 5.1 の寄り方 | 残す | 575 の L4 で決着済み (今のモデルについて正しく、出典と「乖離に気づいたら同じ commit で直す」が付いている)。今のモデルも Opus 5.5 / Fable 5.1 で変わっていない |
| `subagent-model-tiering.md`「そもそも委譲するか」 | 「現行モデルは指示なしでも積極的に委譲する」 | 残す (A-B は未実施) | 抑える向きの指示で、外して誤ったときの損 (委譲の過多 = 費用と時間。1 体の固定分の実測は rationale の retro 687) が、残して誤ったときの損より大きい。このマシンの 08-22 以降でも general-purpose 212 回・Explore 59 回の起動がある |
| 同上 | 「現行モデルは指示なしでも自己修正する」 | 残す (A-B は未実施) | CLAUDE.md の最終項を一次情報として指しているだけで、独自の主張ではない |
| 同上「モデル選択の目安」・上位モデルへの節 | sonnet / opus の使い分け | 残す | 575 の keep list 1 (別名で書かれ、根拠の実測がある) |

A-B (指示を外した headless の比較) は回していない。共有の `~/.claude/CLAUDE.md` と `_claude/rules/` を書き換えずに指示だけ外して起動する方法が未確立で、本文の 🚨 がそれを禁じている。
**再評価の trigger**: 次のモデル (Opus 5.5 / Fable 5.1 の後継) が既定になった日に、上の 4 行を見直す。

### ユーザーの決定から来た項目

日付つきのユーザーの決定は `_claude/CLAUDE.md` の 2 件 (どちらも 2026-10-21: 一時ファイルの置き場・skill を直したら観点を変えて 3 回点検)。rules には 0 件。どちらもモデルの癖の前提ではなく、変える対象にしない。

### 手順型の節

コードブロックがある rule は 8 本で、中身は全部合わせて約 3.3 KB (rules 全体の 1.3%)。見出しが「手順」の節も、ほとんどは判断の順序を書いた規範で、コマンドの並びではなかった。

| rule の節 | 仕分け | 理由 |
|---|---|---|
| `mutation-verify-new-tests.md`「復元の作法」(1.8 KB) | ② `rules-checklists/` へ**移した** | 手で変異を当てるときだけ要る。本文自身が「`bin/mutate-verify` を使えば構造的に起きない」と書く |
| `commit-with-pathspec.md` の heredoc の例 | ③ 残す | commit のたびに要る (100 byte) |
| `avoid-wall-clock-assertions.md` の待ちの関数の例 | ③ 残す | dotfiles では `tests/lib/wait_until.sh` と `make test-lint` が強制するが、rule は全 repo に効く |
| `parallel-write-agents-need-worktree-isolation.md` の worktree の作り方 | ③ 残す | 本文が「repo に worktree を作るコマンドがあればそちら」と道具を優先している |
| `pending-issue-rationale-in-code.md` のコメントの例 | ③ 残す | 書き方の見本で、短い |
| `claude-md-layer-prompt.md` の問いかけの形式 | ③ 残す | 発動したその場で要る (460 byte) |
| `path-shim-must-resolve-real-binary.md` の形 | ③ 残す | 発動点 (shim を書く瞬間) で要る |
| `escalate-to-forge-after-failed-tries.md` の forge 起動のテンプレ | ③ 残す | 発動点で要る (317 byte)。forge の出番が減っているので、forge skill へ寄せるかは 911 の再開と一緒に見る |
| 「手順」の見出しの節 (survey-receiver・instrument-before・list-masked・check-other-platform など) | ③ 残す | 判断の順序で、道具で強制できない |

① (スクリプト / hook で強制できる) に当たる新しいものは無かった。強制できるものは既に道具がある (`bin/mutate-verify`・`scripts/issue_claim.sh` / `issue_done.sh`・tmux の shim と hook・`make test-lint` の sleep 検査)。

### 衝突

sonnet 1 体 (読み取りのみ) に、発動点が同じ指示を rule をまたいで並べて探させた。全 36 本を通しで精読したのではなく、並行・本数・上限 / git の操作 / 失敗後の対応 / 待ちと時間 /
出力の扱い / commit message の渡し方、の観点で絞って読んだ範囲で、**優先の明記が無い衝突は 0 件**。弱い候補 2 件は原文で確かめて、どちらも衝突ではなかった:
- 性能の gate: `avoid-wall-clock-assertions.md` は「絶対値を CI の gate にしない」と書き、その場で `perf-claims-need-measurement.md` の「比・伸び率で測る」を名指ししている
- forge への escalate と 5h 枠: `subagent-model-tiering.md` の「閾値を超えたら大きな作業の前に諮る」が大きな作業に forge を挙げていて、escalate の場面にもかかる

### 量

移した後: `_claude/rules/*.md` 244,271 byte (−1,415)、`_claude/CLAUDE.md` 31,579 byte (変更なし)

## 進捗

- 2026-10-29: 本文を今のコードと突き合わせ、575 を踏まえて棚卸しした (上の「結果」)。「復元の作法」を checklist へ移した
  (commit: rules(mutation-verify): 手で変異を当てるときの「復元の作法」を checklist へ移す (916))。受け入れ条件 2 つ (古いモデル向けの指示の判断・手順型の節の仕分け) を満たしたので done へ移す
