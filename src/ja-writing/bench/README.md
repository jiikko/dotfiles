# ベンチマーク (src/ja-writing/bench)

本家 yomiyasu (Python) と Go の実装の所要を、同じ入力で比べるための入力と道具。設計と測った数字は issue 1082、改善のループは 1079。

## 入力 (`gen.py`)

`python3 -I bench/gen.py <出力先の dir>` が、commit 済みの `testdata/corpus/` から決まった手順で組み立てる。乱数と時刻を使わないので、同じ commit なら同じバイト列になる。
組み立てた文章は commit しない (元の文章を何倍にも複製して repo を太らせないため)。各ファイルの大きさと SHA-256 は出力の `manifest.tsv` に出る。

| ファイル | 伸び方の軸 | 元の文章 |
|---|---|---|
| `len-004k` 〜 `len-1024k` (5 本、4 倍ずつ) | 長さ | `testdata/corpus/dotfiles/` と `references/` の段落を順に並べる (約 570 KB を超える分は 2 周目) |
| `rep-064k` | 同じ段落の繰り返し | 上の段落のうち、最初の 300 バイト以上のもの |
| `table-064k` | 表と、太字のラベル・入れ子の箇条書き | 本家の `raw_ai` の文を 20 行の表と 10 項目の箇条書きに入れる |
| `ai-064k` | AI が書きがちな語の多さ | 本家の `raw_ai` と `blacklist_ai` |
| `line-032k` | 改行の無い長い 1 行 | 本家の `raw_ai` の文をつなげる |
| `pair-small` / `pair-064k` / `pair-256k` (`-before` と `-after`) | diff と check の、直す前と後の組 | 本家の `raw_ai` と、同じ名前の `yomiyasu_rewritten` を同じ順に並べる (small は 1 組) |

出典と再配布: dotfiles の文書はこの repo のもの。本家の `tests/corpus/` と `references/` は MIT (`../LICENSE`)。

## 本家の所要を測る (`pybench.sh`)

```sh
python3 -I bench/gen.py <入力の dir>
bench/pybench.sh [-n <周の数>] <入力の dir> <結果の dir>   # 周の数を指定しないときは 5
```

- 本家の動かし方は `oracle` の Runner と同じ (環境変数を空から作る・`python3 -P -s -S -B -X utf8`・SHA-256 が合う本家のファイルだけを写した木)。
  本家のファイルは `make golden` が取る置き場 (`~/.cache/yomiyasu-oracle/<commit>/`) から読む
- 各周で全ケースを 1 回ずつ流し、周ごとにケースの順を 1 つずつずらす。`runs.tsv` に run ごとの rc と秒数、`summary.tsv` にケースごとの中央値・最小・最大と、
  同じ道具の `--version` の中央値を引いた処理の時間が出る。`env.tsv` に Python のバージョン・本家の commit・CPU・OS・開始と終了の時刻が出る
- 測るのは python3 の起動から終わりまでの壁時計の時間。使い捨ての cwd の用意と入力の写しは区間の外
- 入力の dir が今の `gen.py` の出力と違うと止まる (別の commit で作った入力を測らない)

## 読むときの注意

- 同じコードを続けて 2 回測っても、2 回目が 10〜21% 遅かった (issue 1082)。前後の比較は、別の時間帯の run どうしでせず、同じ run の中で交互に流す
- `--version` を引いた処理の時間は、import の中で入力に依らない準備をしている分を起動の側に数える。小さい入力では起動 (0.08〜0.13 秒) が所要の大半になる
- check は中で lint と diff を子プロセスで起こすので、その起動も処理の時間に入る
