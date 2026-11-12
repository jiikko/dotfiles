# ベンチマーク (src/ja-writing/bench)

本家 yomiyasu (Python) と Go の実装の所要を、同じ入力で比べるための入力と道具。設計と測った数字は issue 1082、改善のループは 1079。

## 入力 (`gen.py`)

`python3 -I bench/gen.py <出力先の dir>` が、commit 済みの `testdata/corpus/` から決まった手順で組み立てる。git が追跡しているファイルだけを読み、乱数と時刻を使わないので、同じ commit なら同じバイト列になる (追跡しているファイルを手元で書き換えていれば、その中身で組み立てる)。
組み立てた文章は commit しない (元の文章を何倍にも複製して repo を太らせないため)。各ファイルの大きさと SHA-256 は出力の `manifest.tsv` に出る。

| ファイル | 伸び方の軸 | 元の文章 |
|---|---|---|
| `len-004k` 〜 `len-1024k` (5 本、4 倍ずつ) | 長さ | `testdata/corpus/dotfiles/` と `references/` の段落を順に並べる (約 555 KB を超える分は 2 周目) |
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
- 各周で全ケースを 1 回ずつ流し、周ごとにケースの順を周の番号を種にしてかき混ぜる。
  `runs.tsv` には run ごとの rc・秒数・1 分のロードアベレージを書く。`summary.tsv` には、ケースごとの中央値・最小・最大と、同じ道具の `--version` の中央値を引いた処理の時間が出る。`env.tsv` に Python のバージョン・本家の commit・CPU・OS・開始と終了の時刻が出る
- 測るのは `env -i` の起動から python3 の終わりまでの壁時計の時間。使い捨ての cwd の用意と入力の写しは区間の外。`startup-python` (`python3 -c pass`) がこの測り方の下限になる
- rc が 0 でない run と、周によって出力の大きさが変わるケースのどちらかがあれば、`summarize.py` が rc 1 で落ち、`summary.tsv` を出さない
- 入力の dir が今の `gen.py` の出力と違うと止まる (別の commit で作った入力を測らない)

## 読むときの注意

- 同じコードを続けて測っても、run ごとに 1〜2 割ずれることがある (issue 1082)。前後の比較は、別の時間帯の run どうしでせず、同じ run の中で交互に流す
- `--version` を引いた処理の時間には、入力に依らない準備 (lint が語の一覧を読むなど) も入る。import の中で済ませる準備は逆に起動の側に入る
- check は中で lint と diff を子プロセスで起こす。子は `sys.executable -B` だけで起こされ site を import するので、親の `-S` の条件と違い、その起動も処理の時間に入る
- 測った形は oracle の Runner と同じ固定の環境で、skill から ふだん使う形 (`-S` を付けない・環境変数を引き継ぐ) とは違う
