# 1076 (feat): yomiyasu の diff (推敲の前後の比較) を Go で作り直す

起票日: 2026-11-12

## 概要

本家の `yomiyasu_diff.py` (11,577 行。本家で最も大きい) を `yomiyasu diff` として作り直す。直す前と後の文を比べ、数値・否定・伝聞・動作主・
慎重表現・言い切りの強さなど、意味が変わった疑いのある所を出す道具。2026-11-12 の照合で「直す前後の比較」は dotfiles に原則の文しか無く、
機械で比べる手段が無かった。

## 本家が持つもの (v3.1.0)

- オプション: `--stance` `--json` `--fail-on` `--doc-type` `--reader` `--words` `--no-words` `--material` `--endings` `--version`
- 照合は `difflib.SequenceMatcher` を土台にしている (`SequenceMatcher` 21 か所・`difflib.Match` 26 か所)。本家は 2.0.1 と 2.2.2 で
  「difflib と同じ答えのまま」照合を速くした (同じ段落を何十回も繰り返す文書で遅かったため)。比の計算に `fractions.Fraction` を使う
- 正規表現の後読み 265 か所・先読み 256 か所 (本家で最も多い)
- 絵文字の数え方は lint の `_emoji_matches` を読み込んで使い、読めなければ簡単な数え方に切り替える

## 対応方針

- `difflib.SequenceMatcher` (Python 3.15 の `Lib/difflib.py`。autojunk の規則を含む) を `internal/difflib` に写し、本家の速い照合と
  答えが同じことを、本家の試験の入力と記録で確かめる。本家の速い照合は「difflib と同じ答え」を前提にしているので、Go でも 2 つを突き合わせる試験を残す
- `Fraction` は `math/big.Rat` で持ち、出力に出る値の書き方 (小数・分数) を本家と揃える
- 11,577 行を検出の群ごとに分けて commit する。分け方は本家のコードを読んでから決め、群の一覧をこの issue に書く
- 所要: 同じ段落を繰り返す長い文書 (本家の `tests/test_v202_diff_bold_performance.py` などの入力) を本家と Go で測る

## 受け入れ条件

- [ ] 入力の集まり (直す前と後の組) で、本家と同じ出力 (stdout・`--json`・rc) を出す
- [ ] `internal/difflib` が Python の `difflib.SequenceMatcher` と同じ `get_matching_blocks` / `get_opcodes` / `ratio` を返す (記録との突き合わせ)
- [ ] 本家の速い照合を写したものが、`internal/difflib` と同じ答えを返す試験がある
- [ ] 長い文書の所要を本家と Go の両方で測った数字がある

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/yomiyasu_diff.py`・`docs/tools.md`
- 1074 (markdown_visibility)・1075 (lint の絵文字の数え方)・1077 (check が diff を読む)

## 進捗

- (未着手)
