# 1075 (feat): yomiyasu の lint (40 規則) を Go で作り直す

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-12〜）

起票日: 2026-11-12

## 概要

本家の `yomiyasu_lint.py` (6,398 行) を `ja-writing lint` として作り直す。AI が書いた日本語の型と Markdown の書式の型を、直す候補として出す道具。
否定対比・前置き・比喩動詞・直訳語・体言止めの連続・装飾記号など、2026-11-12 の照合で dotfiles に入っていなかった型の多くがここにある。

## 本家が持つもの (v3.1.0)

- 規則の ID は 40 ある。どれも出力の `"rule"` の値になる
  - 名前で書かれた 31: `ai_phrase` `bold_label_list` `bold_not_rendered` `calque` `cleft_list_ending` `dialogue_period` `emoji_prohibited` `excess_bold`
    `excess_list` `fragment_run` `heading_colon` `instruction_residue` `katakana_term` `meta_filler` `metaphor_label` `metaphor_verb` `narrative_density`
    `negative_parallelism` `negative_parallelism_density` `pause_comma` `project_term` `redundant_bracket` `renyo_noun` `sentence_end_repetition`
    `short_mochiron` `short_summary_heading` `slide_comma` `slop_vocabulary` `thin_evaluation` `trailing_colon` `translated_term`
  - 表から登録される 9: `dash_decoration` `dash_insertion` `dash_list_ending` (`_dash_finding`)、`say_hedge` `copy_comma_noun` `bare_verb_chain`
    (`_SHAPE_RULE_MESSAGES`)、`summary_restatement` `meta_intro` `count_declaration` (`INFO_SENTENCE_PATTERNS`)
  - 重さは error・warn・info に分かれる。info は `--strict` と `--fail-on` に入れない
- オプション: `--json` `--strict` `--fail-on` `--doc-type` `--reader` `--words` `--no-words` `--version`
- 文書の種類 (`--doc-type`) と読み手 (`--reader`) で、外す規則と重さを変える
- 語の一覧のうち、`allow.txt` で候補を外し、`flag.txt` と `flag-patterns.txt` で候補を足す
- 出力は直す候補と件数と「意味の保持と分かりやすさは判定していない」の注意。`--json` は `LINT_FORMAT_VERSION = 2` の形を `indent=2` で出す。
  点数 (`score`) と `is_clean` は出さない
- `katakana_term`・`thin_evaluation`・`renyo_noun` の語は、スクリプトの隣の `../references/lists/<名前>.txt` を読む。読めなければ内蔵の一覧を使う
- 絵文字の判定は Unicode 18.0 の emoji-data などから作った表を持つ (実行時にファイルを読まない)。diff がこの数え方 (`_emoji_matches`) を借りる

## 対応方針

- 規則を 1 つずつ写す。規則ごとに、本家の関数・語の一覧・正規表現と Go の対応を表にして README に置く (本家の更新を追うときに引く)
- 6,398 行を 1 回で写さず、規則の群ごとに分けて commit する。群は、語の一覧で当てる規則 / 文の構造を見る規則 / Markdown の書式の規則 / 文書全体の密度の規則の 4 つ。
  各 commit は、その群の規則について 1073 の記録と一致する状態で入れる
- 規則ごとに似た形の関数が並ぶと dupscan の gate (節の下限 12) に当たるので、形の同じ規則は、本家の表 (`INFO_SENTENCE_PATTERNS` など) と同じく表で駆動する
- 語の一覧のファイルは 1073 で埋め込んだものを使う
- diff が使う絵文字の数え方は、公開の関数にして、1073 の関数の単位の記録で本家と比べる
- 速さ: 本家は 3.0.1 で正規表現を初めて使うときに組む形にして起動を速くした。Go では compile を package の初期化に置くか遅らせるかを、
  hook (1078) の時間の上限から決める。dotfiles の文書 (Markdown 約 1,400 本) を通した所要を本家と Go で測る。
  起動の時間と処理の時間を分け、同じ入力を交互に複数回測る。数字は README に書く
- 本家は語の一覧の set から選択肢を組むので、パターンの文字列と選択肢の並びが `PYTHONHASHSEED` で変わる。lint を写すときは選択肢の並びを決まった順にし、本家の答えが seed に依らないことを複数の seed で確かめる
- regexp2 は後戻りする照合器なので、行が長い入力での最悪の所要を測る。入力は本家の `tests/test_lint_performance_regressions.py` の入力のほかに、
  後戻りが増える形 (入れ子の繰り返し・長い同じ字の並び) を本家の全パターンの記録から選んで作る。テストの合否には時間を使わず、試す数か結果で判定する

## 受け入れ条件

- [ ] 40 規則すべてが入力の集まりで本家と同じ候補 (行・列・規則・重さ・文) を出す。`--json` の出力も字まで同じ
- [ ] 規則 ID ごとに、当たる入力と当たらない入力が入力の集まりにあり、その件数を記録から数える検査が通る (1073 の空振りを止める検査)
- [ ] rc が本家と同じ (`--strict`・`--fail-on` の組み合わせを含む)
- [ ] 語の一覧の読み込みと、`--doc-type` / `--reader` の切り替えが本家と同じ結果になる
- [ ] 絵文字の数え方の公開の関数が、本家の `_emoji_matches` と同じ値を返す
- [ ] dotfiles の文書を通した所要を、本家と Go の両方で測った数字がある
- [ ] 規則を 1 本ずつ外したとき (群ごとに最低 1 本)、記録との突き合わせが落ちることを確かめた (`mutate-verify`)
- [ ] `make test-dupscan-gate` が通る

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/yomiyasu_lint.py`・`skills/yomiyasu/references/lists/*.txt`・`docs/tools.md`
- 1074 (markdown_visibility を使う)・1076 (diff が絵文字の数え方を借りる)

## 進捗

- (未着手)
