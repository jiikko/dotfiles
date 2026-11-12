# 1073 (feat): yomiyasu の Go 実装の土台 (module・Python の re を写す層・本家の出力を正解にする検査)

起票日: 2026-11-12

## 概要

1074〜1078 が共通に使う土台を作る。`src/yomiyasu` の module と CLI の入口、Python の標準ライブラリの振る舞い (`re`・`json`・`str`・`unicodedata`) を
Go で写す内部の package、本家の Python の実装を正解 (oracle) として出力を記録し Go の実装と突き合わせる仕組み。

## 測った事実 (2026-11-12、本家 v3.1.0 = commit db78b9bc)

- Python のコードは全部で 25,987 行 (`wc -l`): markdown_visibility 1,429・lint 6,398・diff 11,577・check 5,639・hook 944
- 読み込みの依存: markdown_visibility ← lint ← diff (絵文字の数え方 `_emoji_matches` を lint から借りる) ← check (lint と diff を読む) ← hook (check を読む)
- 正規表現の後読み `(?<=` `(?<!` が 443 か所 (lint 174・diff 265・markdown_visibility 4)、先読み `(?=` `(?!` が 390 か所。
  Go の標準の `regexp` (RE2) はどちらも書けないので、[regexp2](https://github.com/dlclark/regexp2) (v1.12.0。.NET の構文で、先読み・後読みを持つ) を使う
- regexp2 で手元で試した結果 (`tmp/yomiyasu-gap-2026-11-12/probe-regexp2`): 先読み・後読みは通る。位置は文字 (rune) 単位で返り、Python の str の添字と同じ単位。
  `(?P<名前>` は構文エラー (`(?<名前>` に変える)。`\s` は `\x1f` に当たらない (Python の `\s` は `\x1c`〜`\x1f` にも当たる)
- 所有格の量指定子・アトミックグループ・`re.X` (verbose)・`re.A` は使っていない。`(?P<` は 47 か所、`\Z` は 1 か所、`re.I` は 9 か所、行内のフラグは `(?m)` 5 か所と `(?i)` 1 か所
- Python 3.15.0 の `unicodedata` は Unicode 17.0.0、Go 1.26 の `unicode` は 15.0.0。15.0 より後に割り当てられた字の分類 (`unicodedata.category`) と正規化が食い違いうる
- 出力の JSON は `ensure_ascii=True`・`indent=1`・`sort_keys`・`separators=(",", ":")` の組み合わせを使い分けている (check と hook)。
  Go の `encoding/json` とは、非 ASCII の書き方・HTML の字の escape・小数の書き方・鍵の並びが違う

## 対応方針

### 置き場所と CLI

- `src/yomiyasu` (module `yomiyasu`)。CLI は 1 本の `yomiyasu` に subcommand (`lint` / `diff` / `check` / `hook`) を持たせ、各 subcommand の
  オプションは本家の argparse の定義と同じにする。入口は `bin/yomiyasu` (`go_autobuild_exec`。zsh なら root Makefile の `ZSH_SYNTAX_FILES` に登録)
- `src/README.md` の 3 点セット (Makefile の `lint` / `test`・`go.mod` と `go.sum`・`.github/workflows/src_yomiyasu.yml`) と package の doc comment を揃え、
  `src/README.md` の一覧と `src/PACKAGES.md` (生成) に足す
- 本家のライセンス (MIT) を `src/yomiyasu/LICENSE-yomiyasu` に置く。本家の `UNICODE-LICENSE.txt` が何のデータに掛かるかを確かめ、写すなら一緒に置く

### Python の標準ライブラリを写す内部の package

- `internal/pyre`: Python の `re` のパターンを regexp2 の構文に直して compile する。直すもの: `(?P<名前>` / `(?P=名前)`・`\Z`・`\s` `\S` (`\x1c-\x1f` を足す)・
  `\w` `\W` `\b` `\B` (Python の str の定義に合わせる)・行内のフラグ。`search` / `match` / `fullmatch` / `finditer` / `findall` / `sub` / `split` を Python の意味で持つ
  (空の一致の扱い: Python 3.7 以降は `re.findall("a*|b", "b")` が `['', 'b', '']`、`re.sub("x*", "-", "abxd")` が `-a-b--d-`。手元の 3.15.0 で確かめた)。
  置換の書式 (`\1`・`\g<名前>`) も Python のもの
- `internal/pyjson`: `json.dumps` と同じ字を出す (`ensure_ascii`・`indent`・`separators`・`sort_keys`・小数の `repr`・挿入順の鍵)
- `internal/pystr`: Python の str の振る舞い (`splitlines` が区切る字・`isspace` / `isalnum` などの分類・`strip` の空白)
- 字の分類と正規化: Go の表 (Unicode 15.0.0) と Python の表 (17.0.0) の差は、oracle で差のある字の一覧を生成して上書きの表として持つか、
  差のある字を「本家と揃わない入力」として記録するかを、差の件数を数えてから決める

### 本家の出力を正解にする検査

- `src/yomiyasu/oracle/`: 本家のファイルを固定の commit (db78b9bc) から取ってきて sha256 で照合し、`${XDG_CACHE_HOME:-~/.cache}/yomiyasu-oracle/<commit>/` に置く
  (git clone はしない)。Python は `python3 -I -B` で動かし、取ってきたファイルに手を入れない
- `make golden` (手で打つ): 入力の集まり (`testdata/corpus/`) を本家に通し、stdout・stderr・rc を `testdata/golden/` に記録する。記録には Python と
  `unicodedata` のバージョンを入れる。Go のテストは記録と突き合わせるだけで、CI では Python を動かさない
- 正規表現の単位の突き合わせ: Go の実装が compile したパターン (パターンとフラグ) を全部書き出し、本家と同じ Python の `re` で同じ入力の行に当てた結果と
  比べる。道具の出力の突き合わせより前に、`re` を写す層の誤りを 1 パターン単位で見つける
- 入力の集まりの元: 本家の references の例文・本家のテストの入力・dotfiles の文書・手で作った端の入力 (CRLF・NFD・`\x1c`〜`\x1f`・絵文字・
  BMP の外の字・空のファイル)。本家のテストの入力を集める方法 (テストを動かして入力を記録するか、ファイルから抜き出すか) は、手元に pytest が要るかを見て決める
- 揃わないことを認める範囲: argparse の usage とエラーの文面 (rc は揃える)・本家のコマンド名 (`yomiyasu_lint.py`) を含む案内の文。
  どちらも記録と比べる前に決めた表で置き換え、表はこの issue と `src/yomiyasu/README.md` に書く

## 受け入れ条件

- [ ] `make -C src/yomiyasu lint test` と root の `make test-lint` が通り、CI の `src/yomiyasu` が緑
- [ ] `internal/pyre` が本家の全パターンで Python の `re` と同じ一致 (位置・長さ・グループ) を返す (正規表現の単位の突き合わせ)
- [ ] `internal/pyjson` が check と hook の使う組み合わせで `json.dumps` と同じ字を出す
- [ ] `make golden` が本家を動かして記録を作り直せる。取ってきたファイルの sha256 が合わなければ止まる
- [ ] 新しく書いたテストに、実装をわざと壊して落ちることを確かめた記録がある (`mutate-verify`)

## 関連ファイル

- 本家: `skills/yomiyasu/scripts/*.py` (手元の写しは `tmp/yomiyasu-gap-2026-11-12/src/`。tmp は消えるので、正は oracle が取ってくるもの)
- `src/README.md` (新しいプロジェクトの決まり)・`bin/lib/go_autobuild.zsh`

## 進捗

- (未着手)
