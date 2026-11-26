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
- 各周で全ケースを 1 回ずつ流し、周ごとにケースの順を、run ごとの種 (`env.tsv` の `seed`) と周の番号でかき混ぜる。
  `runs.tsv` には run ごとの rc・秒数・1 分のロードアベレージを書く。`summary.tsv` には、ケースごとの中央値・最小・最大と、同じ道具の `--version` の中央値を引いた処理の時間が出る。`env.tsv` に Python のバージョン・本家の commit・CPU・OS・開始と終了の時刻が出る
- 測るのは `env -i` の起動から python3 の終わりまでの壁時計の時間。使い捨ての cwd の用意と入力の写しは区間の外。`startup-python` (`python3 -c pass`) がこの測り方の下限になる
- rc が 0 でない run と、周によって出力の大きさが変わるケースのどちらかがあれば、`summarize.py` が rc 1 で落ち、`summary.tsv` を出さない
- 入力の dir が今の `gen.py` の出力と違うと止まる (別の commit で作った入力を測らない)

## Go と比べる (`pybench.sh -g`)

```sh
git worktree add --detach <wt> <直す前の commit> && (cd <wt>/src/ja-writing && go build -o <dir>/jw-before .)  # 直した後も commit から同じ形で作る
bench/pybench.sh -n 5 -g before=<dir>/jw-before -g after=<dir>/jw-after <入力の dir> <結果の dir>
```

- `-g [<名前>=]<バイナリ>` を渡すと、同じケースを Go の `ja-writing lint|diff|check` でも流す。Go のケースは `<ケース>@<名前>` (名前を省くと `@go`)。
  本家と全部の Go を同じ周の中で、同じかき混ぜの順に混ぜて流す。`-g` は何度でも渡せるので、直す前と後を同じ run で比べられる
- `runs.tsv` には stdout と stderr の SHA-256 も残す。Go のケースの stdout・stderr が本家の同名のケースと 1 周でも違うと、`summarize.py` が rc 1 で落ちる。
  答えの一致の正本は `make test` の記録の比べで、これは計測の入力で答えがずれていないことの確かめ
- 同じバイナリを 2 つの名前で渡すと、同じコードでの揺れが分かる。揺れはケースごとに違う (1 周で 2〜13%) ので、比べる run に毎回入れる
- この揺れは、別々にビルドしたバイナリどうしの差 (同じコードでも 1〜4%) を含まない。前の周と比べるときは、別のビルドも対照に入れる。
  対象の経路のコードが前の周と同じビルド (戻した直しのバイナリなど) で、その組の比も並べて読む (1079 の 6 周目)
- バイナリは作業ツリーではなく commit から作る。作業ツリーで作ると、当てたままのパッチが混ざる
- 比べるのは `python3 -I bench/compare.py <runs.tsv> <after> <before> [<揺れ a> <揺れ b>]`。ケースごとに中央値の比・after が速かった周の数・揺れの比を出し、
  比が揺れの帯の外で過半数の周で同じ向きなら「速い」「遅い」と出す。起動を引いた値は使わない (準備を起動の側へ移すと、全体は変わらないのに速く見える)
- pprof で lint の内訳を見るときは `lint/lint_bench_test.go` の `BenchmarkLintFile` (`JA_WRITING_BENCH_FILE` に入力を渡す)。
  繰り返す in-process の計測では、プロセスごとに 1 回だけ払う準備 (正規表現の compile) が償却されて見えない。
  そこで 1 回目だけのプロファイルを、プロセスを分けて集める (`-test.benchtime 1x` を何回も打ち、`go tool pprof` に全部を渡す)
- diff と check の内訳は `diff/diff_bench_test.go` の `BenchmarkDiffPair` (`JA_WRITING_BENCH_BEFORE` と `JA_WRITING_BENCH_AFTER` に組の入力を渡す)。
  check は diff と lint を子のプロセスとして起こすので、check を in-process で測っても子の分は見えない。check の所要の大半は diff なので、こちらで見る

## 実際の文書で答えの一致と所要を比べる (`docbench.py`)

`gen.py` の入力は組み立てた文章。repo の Markdown のような実際の文書の集まりを、本家と Go の lint に通すには `docbench.py` を使う。
一覧のファイルは根の dir からの相対パスを 1 行に 1 つ書く (`git ls-files` の出力など)。根の dir は、tracked の文書を別の場所に写したものにする (`.yomiyasu-words` を探させないため。あれば止まる)。

```sh
go build -o <dir>/ja-writing .
python3 -I bench/docbench.py check <本家の scripts の dir> <dir>/ja-writing <根の dir> <一覧>   # --json・text・--strict の stdout・stderr・rc が同じか
python3 -I bench/docbench.py time  <本家の scripts の dir> <dir>/ja-writing <根の dir> <一覧> -n 7   # 1 ファイルずつ (起動込み) の合計と、全部をつないだ 1 文書の所要
```

本家の scripts の dir は `make golden` が取る置き場の `~/.cache/yomiyasu-oracle/<commit>/skills/yomiyasu/scripts`。`time` は周ごとに本家と Go の順を入れ替え、`pybench.sh` と同じ固定の環境で動かす。

## 読むときの注意

- 同じコードを続けて測っても、run ごとに 1〜2 割ずれることがある (issue 1082)。前後の比較は、別の時間帯の run どうしでせず、同じ run の中で交互に流す
- `--version` を引いた処理の時間には、入力に依らない準備 (lint が語の一覧を読むなど) も入る。import の中で済ませる準備は逆に起動の側に入る
- check は中で lint と diff を子プロセスで起こす。子は `sys.executable -B` だけで起こされ site を import するので、親の `-S` の条件と違い、その起動も処理の時間に入る
- 測った形は oracle の Runner と同じ固定の環境で、skill から ふだん使う形 (`-S` を付けない・環境変数を引き継ぐ) とは違う
