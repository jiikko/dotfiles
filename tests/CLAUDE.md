# tests/

`make test` の実行対象。規約の正本は Makefile (`run_tests`) と `scripts/lint_test_scripts.sh`。
ここには「読まないと踏む」ものだけ書く。

## 発見規約 (登録不要 = 死蔵しない)

- `tests/**/test_*.sh` (名前に `helper` を含まない) を置くだけで `make test` と CI の対象になる。Makefile に登録しない (手動列挙は「書いたのに走らない」テストを生む)
- ヘルパーは `lib/` か `test_` で始まらない名前に置く。`.bats` も自動発見 (bats 未導入環境は skip を表示して通る)
- lint は shebang で方言判別する: zsh shebang → `zsh -n`、それ以外 → `shellcheck -S warning`。shebang を忘れると zsh 構文のまま shellcheck に回って落ちる
- 単体実行は `make test-dir DIR=tests/<dir>`、変更に紐づく分だけは `make test-changed PATHS="..."` (写像は `scripts/test_changed.sh --help`)

## 「0 件」「skip」「沈黙」の扱い

- 発見 0 件は失敗 (`run_tests` が落とす。issue 063)。find の失敗やディレクトリ改名を「未実行なのに緑」にしない
- 環境依存で走らせないときは**テスト自身の stdout に理由を出す** (`skipped: ...`)。子プロセスのログに書くだけでは、失敗時のみ表示される設計だと沈黙と区別がつかない
- `~/.claude` や `$HOME` の状態を見るテスト (tests/claude/test_dangling_symlinks.sh / test_claude_links_complete.sh) は CI では対象が無い。「対象 0 件 = skip」を明示して pass する設計で、CI で検査できていないことを隠さない

## pipefail の罠 (落ちた理由が消える)

- `set -euo pipefail` 下の `var=$(… | grep …)` は無マッチの時点で代入ごと死に、直後の FAIL メッセージへ到達しない。抽出は `|| true` で受けて、空を明示的に FAIL にする
- `… | grep -q` は一致していても偽になりうる (grep が先に抜けて producer が SIGPIPE → pipefail が拾う)。`grep -q PAT <<< "$(cmd)"` にする。`make test-pipefail-grep-q` (scripts/check_pipefail_grep_q.sh) が落とす。例外は行内 `pipefail-grep-q: allow` + 理由

## BSD (手元) / GNU (CI) の方言差

手元 macOS は BSD、CI は GNU。この差は**手元だけ緑**になるので、目視では捕まらない。

- **grep** (`\t` の解釈等) は実行で見る: パターンや観測ログの assert を触ったら `make test-gnu` (GNU grep を `grep` として見せて tests/ 全体を回す)
- **stat / date** は静的に落とす: `make test-platform-dialect` (`scripts/check_platform_dialect.sh`)。
  空白区切りの `stat -f %X` を `stat -c` より先に置く形と、フォールバックの無い `date -r <epoch>` を落とす。
  例外は行内に `platform-dialect: allow` + 理由。実測と直し方の正本はスクリプト冒頭
- 使い分け: **grep は挙動が入力次第なので実行で、stat / date は書き方で決まるので静的検査で**見る
- mtime を取る shell コードは `scripts/lib/tmux_resurrect_guards.sh` の `tt_mtime_of` を使う (自前で `stat` を呼ばない)

## tmux を触るテスト

- 冒頭で `unset TMUX TMUX_PANE` し、`-L <一意名>` か `TMUX_TMPDIR` で socket を隔離する。`$TMUX` が生きていると `TMUX_TMPDIR` は無視されて本番サーバへ向く (2026-07-07 に `make test` が本番を kill した。tests/tmux/test_fork_scratch.sh 冒頭が正本)
- `tests/tmux/lib/isolate_env.sh` は HOME / XDG / ロケールの隔離**のみ**で、socket は対象外
- scripts/ の unit テストは stub 方式 (PATH 先頭に偽 tmux / gum / fzf を置いて呼び出しを記録。共有アサートは tests/tmux/lib/stub_assert_helper.sh)。実サーバが要るテストだけ Darwin skip
- shim を PATH 先頭に置くときは実体を絶対パスで解決してから exec する (相対名は自分自身に解決して無音で無限再帰。`~/.claude/rules/path-shim-must-resolve-real-binary.md`)

## 並列実行と CI の分割

- `run_tests_parallel` (heavy 群 = `CI_HEAVY_TEST_DIRS`) に入れられるのは tempdir 独立で共有資源に触らないテストだけ。tmux / nvim 系は直列のまま
- CI は heavy / rest の 2 job。分割の整合 (パッケージ依存・prune) は `make test-ci-group-deps` が検査する

## bench

- `tests/<area>/bench_*.sh` は `metric=<name> ms=<value>` 行を出し、同ディレクトリの `bench_budgets.ci` と `tests/check_bench_budgets.sh` で照合する (CI の bench.yml が tests/run_bench.sh で複数回実行 → min 集約)。`make test` の対象ではない。予算値の決め方は各 `bench_budgets.ci` のコメントが一次情報

## 新しいテストを書いたら

- 壊す変更を 1 つ当てて red を見るまで commit しない (`~/.claude/rules/mutation-verify-new-tests.md`)。変異は使い捨て worktree で当てる (共有 tree では並行セッションの変異と混ざる)
