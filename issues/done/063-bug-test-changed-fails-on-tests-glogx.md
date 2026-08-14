# make test-changed が tests/glogx/ の変更で必ず失敗する (テスト 0 件のディレクトリへ振っている)

起票日: 2026-08-14
種別: bug
優先度: **P3** (`make test` と CI は無改変。壊れているのはローカルの検証導線だけ)

## 何が起きるか

`tests/glogx/` 配下のファイルを触って CLAUDE.md 記載の手順を踏むと、**変更内容に関係なく
必ず失敗する** (実行して採取):

```
$ make test-changed PATHS="tests/glogx/bench_glogx.sh tests/glogx/bench_budgets.ci"
[test-changed] targets: test-lint-tests / tests: tests/glogx
[lint-tests] zsh -n 44 本 + shellcheck 40 本 OK
✗ tests/glogx 配下にテストが見つかりません (find 失敗 or 0 件)
make[1]: *** [test-dir] Error 1
make: *** [test-changed] Error 1
$ echo $?
2
```

機構:

- 写像は `tests/*/*` を **`make test-dir DIR=tests/<dir>`** に振る (`scripts/test_changed.sh:120-121`)
- `run_tests` は `find <dir> -name 'test_*.sh'` が 0 件なら **意図的に fail** する
  (`Makefile:80-84`。「未実行なのに成功」を弾くための正しい設計)
- `tests/glogx/` に入っているのは `bench_glogx.sh` と `bench_budgets.ci` だけで、
  **`test_*.sh` が 0 件**。tests/ 配下で 0 件なのはここだけ:

| ディレクトリ | test_*.sh |
|---|---|
| tests/bin, tests/scripts, tests/setup, tests/theme | 各 1 |
| tests/claude | 4 / tests/nvim 6 / tests/tmux 21 / tests/zshrc 34 |
| **tests/glogx** | **0** |

つまり「0 件を fail にする」ガード自体は正しく、**写像がテストを持たないディレクトリを
テストランナーへ渡している**のが誤り。

## なぜ重要か

CLAUDE.md は「変更したファイルだけ検証する (推奨)」として `make test-changed` を第一手に
挙げている。bench スクリプトや予算を触った人は**自分の変更と無関係な赤**を踏み、
「写像に無いパスは fail するので `make test` で全体を回す」という書かれた回避策も
(パスは写像に**ある**ので) 素直に当てはまらない。

051 の対応時に実際に踏み、個別ターゲット (`scripts/lint_test_scripts.sh` /
`tests/test_check_bench_budgets.sh` / `tests/test_bench_stats.sh` / `make -C src/glogx test`)
を手で並べて検証する羽目になった。

## 対応方針 (2 案。b を推す)

**(a) 写像側を直す**: `tests/glogx/*` は `test-lint-tests` + `go: src/glogx` だけに振り、
`add_test_dir` しない。最小の修正だが「tests/ 配下なのにテストが無い」状態は残る。

**(b) `tests/glogx/test_bench_glogx_metrics.sh` を新設する** (推奨):
bench の**配管**を回帰テストにする。051 では手で確認しただけで、テストは 1 本も無い:

- `bench_glogx.sh` を短い `-benchtime` で 1 回走らせ、`bench_budgets.ci` に載っている
  metric が**全部出ること**と値が数値であることを検査する (予算と emit の食い違いを
  ローカルで検出できる。今は CI まで行かないと分からない)
- 列ずれガード (`$4 == "ns/op" && $6 == "B/op"`) に合成入力を通し、**emit しないこと**を固定する
  (051 で実測した false green: `b.ReportMetric` を持つ benchmark が混ざると
  ガード無しでは `ms=0.001` が予算内で通る)

⚠️ (b) は Go の build + 数秒を `test-runtime` に足す。`-benchtime` を短くして安くする場合、
**`go test` の同一プロセスでテストも走る形にすると `TestFrameAllocBudget` の
`frameAllocBytes` が落ちる** (`testing.Benchmark` が ambient の `-benchtime` を拾い、
反復不足で Fatal。051 で実測済み)。`bench_glogx.sh` 自身は `-run '^$'` でテストを
除外しているので現状は無害 — 新しい呼び出しを足すときに踏む罠。

## 未確認

- ~~(b) の所要時間~~ → 実測 2.6 秒 (build cache 温、`GLOGX_BENCHTIME=1x`)
- 他に「tests/ 配下だがテストを持たないディレクトリ」を将来足す運用があるか
  (あるなら (a) の一般形 = 写像に「テストを持たない tests/ 配下」の概念を入れる)

## 対応記録 (2026-08-14)

案 (b) で解決:

- `bench_glogx.sh` にテスト用 seam を追加 (`GLOGX_BENCHTIME` = benchtime 上書き /
  `GLOGX_BENCH_INPUT` = go test を走らせず合成入力を awk に流す)。既定値は従来どおりで
  CI/運用経路は無改変
- `tests/glogx/test_bench_glogx_metrics.sh` 新設 (4 assertions):
  予算の全 17 metric が数値付きで emit される / 想定列の ms・alloc_kb 換算 /
  列ずれ入力 (`$4=ms/op`) は emit しない + stderr 警告 / 較正器は時間のみ
- 変異検証: 列ずれガードを `if (0)` に潰すと「列ずれ入力なのに emit された」で red を実測
- `make test-changed PATHS="tests/glogx/..."` が green (issue の再現手順が解消)

## 関連

- `issues/done/060-bug-test-changed-misses-tests-claude.md` (同型: 写像が実在するテストへ
  到達しない。あちらは「拾わない」、本 issue は「テストが無い所へ振る」)
- `issues/done/051-perf-glogx-bench-gates-time-only.md` (踏んだ経緯と、手で回した検証の一覧)
- `scripts/test_changed.sh` / `Makefile` の `run_tests`
