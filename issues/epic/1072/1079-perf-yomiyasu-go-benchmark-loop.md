# 1079 (perf): yomiyasu の Go 実装をベンチマークで改善する (最大 10 周)

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-13〜）

起票日: 2026-11-13

1073〜1078 が一通り終わったら、ベンチマークに使う文章を集めて計測し、Go のソースを改善するループを最大 10 周回す (ユーザー指示 2026-11-13)。本家と同じ答えを出すことは保つ。本文は着手のときに書き、反証レビューに通す。

前半 (ベンチマークの文章を集め、本家の Python の所要を測る下ごしらえ) は [1082](done/1082-perf-ja-writing-bench-corpus-baseline.md) に切り出した (dotfiles-24 が担当。2026-11-13)。この issue に残るのは、Go の実装 (lint・diff・check) ができた後に、1082 の文章で Go を測り、改善するループ (最大 10 周)。

## 1082 からの申し送り (dotfiles-24、2026-11-13)

道具は `src/ja-writing/bench/` (`gen.py` が入力を組み、`pybench.sh` が本家を測り、`summarize.py` が集計する。使い方は `bench/README.md`)。本家の所要は 9 周を 2 回測り、中央値は 2 回の間で 5% 以内に揃った (数字は 1082)。

1. 本家と Go は同じ run の中で交互に流す。別の時間帯の run どうしでは 1〜2 割ずれた
2. 比べるのは中央値か最小値にする。並行するセッションの負荷で、最大値に 7.8 秒のような外れが出る
3. 処理の時間として出している「`--version` を引いた値」には、入力に依らない準備 (lint の語の一覧の読み込みなど) も入る
4. rc が 0 でない run か、周で出力の大きさが変わるケースがあれば、`summarize.py` は rc 1 で落ちる

## 計画 (2026-11-26。着手のときに書き、反証レビュー (sonnet、読むだけ) の 11 件を受けて直した)

### 測り方

- 道具は `src/ja-writing/bench/pybench.sh -g` (`-g <名前>=<バイナリ>` を何本でも渡せる) と `bench/compare.py`。
  本家と全部の Go を同じ run の中で、run ごとの種で決めたかき混ぜの順に流す (別の時間帯の run どうしは 1〜2 割ずれる。1082 の申し送り)
- 毎回の run に 4 つの Go を入れる: `orig` (1079 を始める前の Go)・`same` (orig の写し。同じコードでの揺れを見る)・`before` (前の周に採った Go)・`after` (この周の候補)。
  1 周目は before が orig と同じなので 3 つ
- 採否は起動を引かない中央値の比 (after / before) で決める。起動を引いた値は対象を探す目安にだけ使う
  (準備を起動の側へ移すと、全体は変わらないのに速く見える。1082 の申し送り 3)
- 本家と同じ答え: `summarize.py` が、Go の各ケースの stdout・stderr の SHA-256 が本家の同名のケースと 1 周でも違えば rc 1 で落ちる。
  正本は `make -C src/ja-writing test` の記録の比べで、毎周これも緑にする

### 1 周の手順 (最大 10 周)

1. 前の周の run で、本家より遅いか処理の時間の長いケースを対象に選ぶ
2. 対象を in-process のベンチマーク (`go test -bench BenchmarkLintFile -cpuprofile / -memprofile` など。lint の分は `lint/lint_bench_test.go`) で測り、
   時間か割り当てを食っている関数を見つけてから直す (測らずに直さない)
3. `make -C src/ja-writing test` と lint を緑にする
4. 5 周の run を流し、`compare.py runs.tsv after before same orig` で比べる。採るのは、対象のケースが「速い」(比が揺れの帯の外で、過半数の周で速い) で、
   ほかのケースに「遅い」が無いときだけ。採らない直しは revert する。`compare.py runs.tsv after orig same orig` で最初からの累積も見る
5. 採った直しは、実際の文書 (`bench/docbench.py time`。repo の Markdown) でも本家と Go の所要を測り、合成の入力だけを速くする近道でないことを見る
6. 周の結果 (対象・プロファイルで見た関数・直し方・前後の中央値の比・揺れ・採否・測った条件) をこの issue の進捗に書く

### 終わり方

- lint の長い入力 (len-064k・256k・1024k・ai・rep) で、Go の中央値が本家以下になり、CPU と割り当てのプロファイルで全体の 1 割を超える関数が無くなったとき
- 10 周を回したとき、または採れる直しが 2 周続けて無いとき (「候補が無い」と「直しが外れた」は分けて書く)
- Claude の weekly の枠が 80% に届いたとき (dotfiles-53 からの引き継ぎで伝えられたユーザー指示、2026-11-25)。周の始めに `ratelimit` で見て、届いていたら周の終わりで止めて報告する。
  `subagent-model-tiering.md` の「weekly 枠は判断材料にしない」より、この明示の指示を優先する

### 起点 (2026-11-25、5 周の中央値。本家と今の Go を同じ run で流した。`tmp/1079-bench-1125/base5`)

起動を引いた処理の時間の比 (Go / 本家): lint-len-064k 1.61・ai 1.30・rep 1.27・256k 1.22・1024k 1.14 で Go が遅い。
diff と check は 0.15〜0.84 で Go が速い。絶対の時間が長いのは check-pair-256k (Go 1.53 秒)・diff-pair-256k (1.27 秒)。

## 進捗

- 計画は反証レビュー (sonnet、読むだけ。2026-11-26) の 11 件を受けて直した。採ったもの: 揺れをケースごとに同じバイナリの組で測る・最初の Go (orig) を毎回の run に入れる・
  本家との答えを stdout と stderr の SHA-256 で比べる・run ごとのかき混ぜの種・採否を `compare.py` で出す・起動を引かない中央値で決める・
  in-process のベンチマークで測る・実際の文書でも測る・終わりの目標・weekly の目安の出典。道具は commit「feat(ja-writing/bench): 本家との答えの比べ・…」
- 1 周目 (採らない): lint の割り当てを減らした (`nonSpaceCount` が rune の並びを作らない・`pyunicode.Runes` の容量を字数で取る。1 回の lint の割り当て 100 MB → 71 MB)。
  5 周の run (`tmp/1079-bench-1125/it1`) で、本家と答えは全部一致したが、orig と比べてほぼ全ケースで 2〜8% 遅く (字数を数える 1 回の走査が、割り当てを減らした分より高い)、revert した
  - `GOGC` を 100・200・400・off に変えても、起動込みの所要の差は 4% 未満。in-process のベンチマークで CPU の 3 割を占めた `runtime.madvise` は、
    同じ処理を何度も回した計測の側の形で、CLI の所要の主因ではない
- 2 周目 (直し方の判断待ちだった): 1 回目の実行だけを 30 プロセス分集めたプロファイル (len-064k) で、lint の起動込みの所要の約 3 割 (1 プロセス約 33 ms) が
  regexp2 の compile の最適化 (`syntax.mayOverlapByEnumeration`。ループを atomic にできるかを、隣の集合と重なるかを 1 字ずつ回して調べる) だった。
  pyre は否定の集合を補集合の範囲で書く (regexp2 の後戻りの誤りを避けるため。`internal/pyre/emit.go` の `emitSet`) ので、回す字が 100 万を超える
  (`labelSeparated` の compile だけで 18.8 ms)
  - 案 A (regexp2 の写しで回す字に上限 4,096。超えたら「重なりうる」として最適化を 1 つ諦める): 5 周の run (`tmp/1079-bench-1125/rx-run`) で、答えは全部一致し、
    orig と比べて全ケースが速い。lint-len-064k 0.42 倍 (0.110 → 0.046 秒)・len-004k 0.65 倍・256k 0.69 倍・1024k 0.88 倍、diff-pair-small 0.62 倍・check-pair-small 0.74 倍
  - 案 B (pyre の側で 4,096 字を超える集合に、集合に無い字 1 つの引き算を足す): lint は案 A と同じだけ縮んだが、diff-pair-256k が orig の 1.066 倍・
    check-pair-256k が 1.037 倍に遅くなった (引き算のある集合は当てるときに遅い道を通る) ので採らない
  - ユーザーの答え (2026-11-26): 案 A を「写して直す」(src/ja-writing/third_party/regexp2 に v2.5.2 を写し、go.mod の replace で使う)。
    上流への PR は出さない (聞いた選択肢: 写して直す / 上流に PR を出して待つ / 写して上流にも出す / 採らない)
