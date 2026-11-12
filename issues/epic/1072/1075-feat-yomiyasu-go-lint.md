# 1075 (feat): yomiyasu の lint (31 規則) を Go で作り直す

起票日: 2026-11-12

## 概要

本家の `yomiyasu_lint.py` (6,398 行) を `yomiyasu lint` として作り直す。AI が書いた日本語の型と Markdown の書式の型を、直す候補として出す道具で、
否定対比・前置き・比喩動詞・直訳語・体言止めの連続・装飾記号など、2026-11-12 の照合で dotfiles に入っていなかった型の多くがここにある。

## 本家が持つもの (v3.1.0)

- 規則の ID は 31 (`ai_phrase` `bold_label_list` `bold_not_rendered` `calque` `cleft_list_ending` `dialogue_period` `emoji_prohibited` `excess_bold`
  `excess_list` `fragment_run` `heading_colon` `instruction_residue` `katakana_term` `meta_filler` `metaphor_label` `metaphor_verb` `narrative_density`
  `negative_parallelism` `negative_parallelism_density` `pause_comma` `project_term` `redundant_bracket` `renyo_noun` `sentence_end_repetition`
  `short_mochiron` `short_summary_heading` `slide_comma` `slop_vocabulary` `thin_evaluation` `trailing_colon` `translated_term`)。
  重さは error・warn・info に分かれる (info は `--strict` と `--fail-on` に入れない)
- オプション: `--json` `--strict` `--fail-on` `--doc-type` `--reader` `--words` `--no-words` `--version`
- 文書の種類 (`--doc-type`) と読み手 (`--reader`) で外す規則・重さを変える。語の一覧 (`allow.txt` で外す・`flag.txt` と `flag-patterns.txt` で足す) を読む
- 出力は直す候補と件数と「意味の保持と分かりやすさは判定していない」の注意。点数 (`rule_penalty_score`) と、v1.1.2 までの出力の形との互換も持つ
- `katakana_term`・`thin_evaluation`・`renyo_noun` の語は、スクリプトの隣の `../references/lists/<名前>.txt` を読み、読めなければ内蔵の一覧を使う。
  check はこのファイルの SHA-256 を記録に入れて照合する
- diff が絵文字の数え方 (`_emoji_matches`) をこの module から借りる

## 対応方針

- 規則を 1 つずつ写す。規則ごとに、本家の関数・語の一覧・正規表現と Go の対応を表にして README に置く (本家の更新を追うときに引く)
- 6,398 行を 1 回で写さず、規則の群ごと (語の一覧で当てる規則 / 文の構造を見る規則 / Markdown の書式の規則 / 文書全体の密度の規則) に分けて commit する。
  各 commit は、その群の規則について 1073 の記録と一致する状態で入れる
- 語の一覧のファイルは Go の module に写して埋め込み (`go:embed`)、本家と同じ中身・同じ SHA-256 にする。写したファイルの出典と
  本家の commit を README に書く
- 速さ: 本家は 3.0.1 で正規表現を初めて使うときに組む形にして起動を速くした。Go では compile を package の初期化に置くか遅らせるかを、
  hook (1078) の時間の上限から決める。dotfiles の文書 (Markdown 約 1,400 本) を通した所要を本家と Go で測り、数字を README に書く
- regexp2 は後戻りする照合器なので、行が長い入力での最悪の所要を測る (本家の `tests/test_lint_performance_regressions.py` の入力を使う)

## 受け入れ条件

- [ ] 31 規則すべてが入力の集まりで本家と同じ候補 (行・列・規則・重さ・文) を出す。`--json` の出力も字まで同じ
- [ ] rc が本家と同じ (`--strict`・`--fail-on` の組み合わせを含む)
- [ ] 語の一覧の読み込みと、`--doc-type` / `--reader` の切り替えが本家と同じ結果になる
- [ ] dotfiles の文書を通した所要を、本家と Go の両方で測った数字がある

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/yomiyasu_lint.py`・`skills/yomiyasu/references/lists/*.txt` (lint が読む語の一覧)・`docs/tools.md`
- 1074 (markdown_visibility を使う)・1076 (diff が絵文字の数え方を借りる)

## 進捗

- (未着手)
