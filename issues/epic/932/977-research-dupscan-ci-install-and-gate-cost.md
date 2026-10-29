# 977 (research): CI の dupscan 導入コストと gate 実行コストを分けて改善を判断する

起票日: 2026-10-29
優先度: 中

## 観測

[Obaket1301の72run評価](https://github.com/jiikko/obaket/blob/f3e77fa65/docs/research/dupscan-evaluation-2026-10-29.md)で、recordのある自然push69件のCI step時間は、
build中央値54秒 (42〜117)、gate中央値44秒 (36〜108)。秒刻みのubuntu-slim実績。
build stepにはgit clone/checkoutとgo buildが含まれる。pin3版・入力/設定の変化も混ざり、
同じ条件のベンチではない。2つの中央値を足してjob中央値ともしない。

測定の最多pinは588ec472 (61run)。現行dotfilesのCLIが常に44秒かかる、メモリ確保が原因、
という証拠ではない。[924](done/924-perf-dupscan-exploration-latency.md)のwarm探索や
[951](done/951-perf-dupscan-allocation-and-resource-review.md)のallocationとは別の導入コストの課題。

## 調査・改善案

1. tool/input/config/runnerを固定し、clone/checkout・依存取得・compile・gateの時間を分ける。cold/warmを複数回測る。
2. 毎回固定版を再buildする費用に対し、Go build/module cacheまたは検証済みbinaryの再利用を比較する。
   新しい配布方式を最初から採用せず、既存の導入recipeで減らせるかを先に測る。
3. cacheを使うならtool SHA、OS/arch、Go/toolchainとcgo/ABI条件を検討し、別版・破損を無音で使わない。
   利用側CIとupstreamの導入ドキュメントが主対象。原因が確認できた範囲だけCLI最適化へ進む。

これはObaketで既に使うLinux CIの観測であり、dotfilesの公式macOS専用方針を変更する提案ではない。
検証環境は利用側で別途扱い、Linux対応拡大をこのissueの受け入れ条件にしない。

## 受け入れ条件

- [ ] 条件と標本数を揃え、cold/warmの各工程と実job全体を測定する。
- [ ] 採用/見送りの判断を、実際に減った時間と追加の保守・配布費用で説明する。
- [ ] cache有無で判定と出力の意味が同じで、版違い/破損/取得失敗が偽の成功にならない。
  time/run ID等の変動欄は比較から明示して分ける。

## 既存課題との関係

[948](948-research-dupscan-meets-its-aims.md)には利用価値の費用として結果を渡す。
一般的なtoken削減や開発時間の因果効果を、この工程時間だけで証明したとは扱わない。
