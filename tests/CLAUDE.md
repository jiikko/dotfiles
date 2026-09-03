# tests/

`make test` の実行対象。規約の正本は Makefile (`run_tests`) と `scripts/lint_test_scripts.sh`。
ここには「読まないと踏む」ものだけ書く。

## 発見規約 (登録不要 = 死蔵しない)

- `tests/**/test_*.sh` (名前に `helper` を含まない) を置くだけで `make test` と CI の対象になる。Makefile に登録しない (手動列挙は「書いたのに走らない」テストを生む)
- ヘルパーは `lib/` か `test_` で始まらない名前に置く。`.bats` も自動発見 (bats 未導入環境は skip を表示して通る)
- lint は shebang で方言判別する: zsh shebang → `zsh -n`、それ以外 → `shellcheck -S warning`。shebang を忘れると zsh 構文のまま shellcheck に回って落ちる
- 単体実行は `make test-dir DIR=tests/<dir>`、変更に紐づく分だけは `make test-changed PATHS="..."` (写像は `scripts/test_changed.sh --help`)

## 所要時間 (実測 2026-09-03 / 開発機・Go キャッシュ有効)

**`make test` は約 7 分** (414 秒)。コミット前の既定はこれを通すこと。2 分程度で打ち切ると
**一度も完走しない** (下の内訳のとおり `tests/zshrc` だけで 3 分を超える)。

| 内訳 | 秒 | 支配的なもの |
|---|---|---|
| `test-lint` | 20 | shellcheck + 発見式の check_*.sh |
| `test-syntax` | 1 | |
| `test-discovered` | 326 | `tests/zshrc` 187 / `tests/tmux` 56 / `tests/bin` 45 / 他 10 ディレクトリで 33 |
| `test-bats` | 14 | |
| `test-src` | 53 | Go 6 プロジェクトの lint + `go test -race` |

`tests/zshrc` の中は **`av1ify` 124 秒 + `concat` 56 秒 = 96%**。どちらも実 ffmpeg を回す
動画系で、待ちの実体はエンコードなので**分割しても総量は減らない**。

- **判断基準**: 触ったパスを `make test-changed PATHS="<触ったファイル>"` に渡す。これで足りるのは
  「そのパスの写像先だけで壊れうる変更」のとき。**時間が無くて全体を省いたら、その事実を報告に書く**
  (「docs だけだから省いた」を前例として積まない。issue 185 項目 4 / issue 188)
- ⚠️ `test-changed` は写像先しか回さない。`test-lint` の発見式ゲート (`check_*.sh`) は
  shell / workflow / json 等を触ったときだけ入るので、**Makefile や CI の構造を変えたら
  `make test-lint` を明示的に回す**

## 「0 件」「skip」「沈黙」の扱い

- 発見 0 件は失敗 (`run_tests` が落とす。issue 063)。find の失敗やディレクトリ改名を「未実行なのに緑」にしない
- 環境依存で走らせないときは**テスト自身の stdout に理由を出す** (`skipped: ...`)。子プロセスのログに書くだけでは、失敗時のみ表示される設計だと沈黙と区別がつかない
- **ファイルを丸ごと skip するときは `exit 77`** (automake の慣例)。0 で抜けると runner が合格と同じ `[ok]` を出し、**何も検査していないことが緑に埋もれる**。実害: 2026-08-29 に `test_deny_bare_tmux_kill.sh` が `timeout(1)` 不在で丸ごと skip し、60 件の assert が消えたのに `[ok]` と集計されていた (issue 139)。runner は 77 を `[skip]` として出し、直列側は件数と一覧も出す。**skip は失敗ではない**ので `make test` は緑のまま。増えていたら理由を確かめる
- 一部の assert だけ落とす「部分 skip」は従来どおり 0 で抜けてよい (77 はファイル全体を検査しなかったときだけ)。
  **強制手段**: `make test-skip-exit-code` (`scripts/check_skip_exit_code.sh`) が「skip を告げた直後の
  `exit 0`」を落とす。部分 skip なら行内に `partial-skip: allow <理由>` を書く (issue 139)
- `~/.claude` や `$HOME` の状態を見るテスト (tests/claude/test_dangling_symlinks.sh / test_claude_links_complete.sh) は CI では対象が無い。「対象 0 件 = skip」を明示して pass する設計で、CI で検査できていないことを隠さない

## pipefail の罠 (落ちた理由が消える)

- `set -euo pipefail` 下の `var=$(… | grep …)` は無マッチの時点で代入ごと死に、直後の FAIL メッセージへ到達しない。抽出は `|| true` で受けて、空を明示的に FAIL にする
- `… | grep -q` は一致していても偽になりうる (grep が先に抜けて producer が SIGPIPE → pipefail が拾う)。`grep -q PAT <<< "$(cmd)"` にする。`make test-pipefail-grep-q` (scripts/check_pipefail_grep_q.sh) が落とす。例外は行内 `pipefail-grep-q: allow` + 理由

## platform (macOS のみ)

対象は macOS だけ (issue 133)。**CI も全 workflow が macos-15 runner**なので、手元と CI の
userland は同じ。かつて「手元 BSD / CI GNU」の差を潰すために持っていた道具
(`make test-gnu` / `scripts/with_gnu_grep.sh` / `scripts/check_platform_dialect.sh`) は、
**対象が macOS だけになった時点で「正しい macOS の書き方」を弾く側に回った**ので外した
(例: 素の `stat -f %m` は macOS では正しいのに、あの検査は GNU フォールバックを要求していた)。

- BSD の書き方で構わない。「GNU でも動くように」だけを理由に分岐を足さない
- ⚠️ **残っているのは「版」の差**。CI の `/bin/bash` は 3.2、開発機は Homebrew の 5 系。
  workflow が brew の bash を PATH 先頭に出して揃えている (下の節)

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
