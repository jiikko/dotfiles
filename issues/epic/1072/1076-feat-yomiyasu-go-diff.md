# 1076 (feat): yomiyasu の diff (推敲の前後の比較) を Go で作り直す

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-12〜）

起票日: 2026-11-12

## 概要

本家の `yomiyasu_diff.py` (11,577 行。本家で最も大きい) を `ja-writing diff` として作り直す。直す前と後の文を比べ、意味が変わった疑いのある所を出す道具。
見るのは数値・否定・伝聞・動作主・慎重表現・言い切りの強さなど。2026-11-12 の照合で「直す前後の比較」は、dotfiles に原則の文しか無く、
機械で比べる手段が無かった。

## 本家が持つもの (v3.1.0)

- オプション: `--stance` `--json` `--fail-on` `--doc-type` `--reader` `--words` `--no-words` `--material` `--endings` `--version`。`--json` は `indent=1`
- 照合は `difflib.SequenceMatcher` を土台にしている (21 か所)。どれも `autojunk=False` で、`isjunk` は渡さない
  - 使う API: `get_matching_blocks`・`get_opcodes`・`ratio`・`quick_ratio`・`real_quick_ratio`・`set_seq1` / `set_seq2`・`find_longest_match`
  - 字の列のほか、リストどうしの照合もある
  - 本家は 2.0.1 と 2.2.2 で「difflib と同じ答えのまま」照合を速くした (同じ段落を何十回も繰り返す文書で遅かったため)
- `fractions.Fraction` は 6 回使うが、読点の増え方の閾値の比較の 1 か所だけ。出力の数は `{:.2f}` と `round(x, 2)` の float
- 正規表現の後読み 265 か所・先読み 256 か所 (本家で最も多い)
- 語の一覧のうち 4 本 (nominalization・intensifiers・feelings・hedges) を読む
- 絵文字の数え方は lint の `_emoji_matches` を読み込んで使い、読めなければ簡単な数え方に切り替える

## 対応方針

- `internal/difflib` (Python の `difflib.SequenceMatcher` と、本家の速い照合 `_ExactSequenceMatcher` の写し。本家の `yomiyasu_diff.py` の 1306〜1832 行) は、
  [1081](1081-feat-yomiyasu-go-difflib.md) に切り出した (dotfiles-bb が担当。2026-11-13)。下の 2 項のうち difflib と速い照合の写しは 1081 の範囲で、この issue はそれを使う側を作る
  1081 の API (2026-11-13 に master に入った): `difflib.New[T](a, b []T)` (= `SequenceMatcher(None, a, b, autojunk=False)`) と `difflib.NewExact(a, b []rune)` (= 本家の `_ExactSequenceMatcher`)。
  メソッドは `SetSeq1` `SetSeq2` `SetSeqs` `FindLongestMatch` `GetMatchingBlocks` `GetOpcodes` `Ratio` `QuickRatio` `RealQuickRatio`。文字列は `pyunicode.Runes` で `[]rune` にして渡す。
  本家の照合の呼び出しは ast で 15 か所 (`SequenceMatcher` 11・`_ExactSequenceMatcher` 4)。記録は `testdata/golden/difflib.json`
- 本家の diff は太字の判定 (`yomiyasu_diff.py` の 1923〜2685 行) を lint と同じ中身で自分でも持つ (dotfiles-24 が AST で比べ、答えを変える違いは無かった)。
  Go では lint と共有の `internal/bold` ([1083](1083-feat-ja-writing-lint-bold-not-rendered.md)) を使い、写さない。1083 が本家の lint 側と diff 側の両方の関数を記録に当てて、同じ答えになることを確かめる
- `difflib.SequenceMatcher` (Python 3.15 の `Lib/difflib.py`) を `internal/difflib` に写す。本家が使う API だけでよい。
  autojunk は本家が使わない。引数の形は Python と同じにし、本家と同じく常に `autojunk=False` で呼ぶ
- 本家の速い照合も写す。「difflib と同じ答え」が本家の前提なので、Go でも 2 つを突き合わせる試験を残す
- `Fraction` は閾値の比較の 1 か所だけなので `math/big.Rat` で持つ。出力の数は 1073 の `internal/pyjson` と同じ規則 (Python の `round` と `repr`、`format` の `.2f`) で書く
- 語の一覧は 1073 で埋め込んだものを使う
- 11,577 行を検出の群ごとに分けて commit する。分け方は本家のコードを読んでから決め、群の一覧をこの issue に書く
- 所要: 同じ段落を繰り返す長い文書 (本家の `tests/test_v202_diff_bold_performance.py` などの入力) を本家と Go で測る。
  起動の時間と処理の時間を分け、同じ入力を交互に複数回測る。テストの合否には時間を使わない

## 受け入れ条件

- [ ] 入力の集まり (直す前と後の組) で、本家と同じ出力 (stdout・`--json`・rc) を出す
- [ ] 検出の種類ごとに、当たる組と当たらない組が入力の集まりにある (1073 の空振りを止める検査)
- [ ] `internal/difflib` が、本家の使う全 API で Python の `difflib.SequenceMatcher` と同じ値を返す (1073 の関数の単位の記録との突き合わせ)
- [ ] 本家の速い照合を写したものが、`internal/difflib` と同じ答えを返す試験がある
- [ ] 長い文書の所要を本家と Go の両方で測った数字がある
- [ ] 検出の群ごとに最低 1 つ、実装を外して落ちることを確かめた (`mutate-verify`)
- [ ] `make test-dupscan-gate` が通る

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/yomiyasu_diff.py`・`docs/tools.md`
- 1074 (markdown_visibility)・1075 (lint の絵文字の数え方)・1077 (check が diff を読む)

## 進捗

- (未着手)
