# tests/

`make test` の実行対象。規約の正本は Makefile (`run_tests`) と `scripts/lint_test_scripts.sh`。
ここには「読まないと踏む」ものだけ書く。

## 発見規約 (登録不要 = 死蔵しない)

- `tests/**/test_*.sh` (名前に `helper` を含まない) を置くだけで `make test` と CI の対象になる。Makefile に登録しない (手動列挙は「書いたのに走らない」テストを生む)
- ヘルパーは `lib/` か `test_` で始まらない名前に置く。`.bats` も自動発見 (bats 未導入環境は skip を表示して通る)
- lint は shebang で方言判別する: zsh shebang → `zsh -n`、それ以外 → `shellcheck -S warning`。shebang を忘れると zsh 構文のまま shellcheck に回って落ちる
- 単体実行は `make test-dir DIR=tests/<dir>`、変更に紐づく分だけは `make test-changed PATHS="..."` (写像は `scripts/test_changed.sh --help`)
- 🚨 **`bash tests/.../test_x.sh` の直接実行はしない**。zsh のテストを bash で回すと
  `source "${0:A:h}/test_helper.sh"` が空パスに潰れて helper が 1 行も走らず、`cd` の失敗を
  見ていない経路が **repo root に fixture を書く** (実測 2026-09-03。残骸 3 件 / issue 204)。
  数字も無効になる (rc=127 で即落ちしたものを「速い」と読む)。ランナー経由なら shebang が尊重される

## 所要時間 (実測 2026-09-03 / 開発機・macOS)

**`make test` は 433 秒 (約 7 分)** — これは**通しで 1 回測った値** (`rc=0`)。
2 分程度で打ち切ると**一度も完走しない**。

| 内訳 (別 run の個別実測) | 秒 |
|---|---|
| `test-lint` | 20 |
| `test-syntax` | 1 |
| `test-discovered` | **128** (2026-09-05 の並列化後の通し。直列時代は 326) |
| `test-bats` | 14 |
| `test-src` | **15〜71** (3 サンプルで 4.7 倍の幅。Go のキャッシュ状態で動く) |

🚨 **親行と子行は別 run の数字**。個別実測の合計は 414 秒で、通しの 433 秒とは 19 秒ずれる
(make の起動と集約の分 + 測定時の負荷差)。`test-discovered` の 326 と、下の内訳の合計 321 が
合わないのも同じ理由。**どの数字も 1 サンプル**なので、±数十秒は動く前提で読むこと。

`test-discovered` (直列) の内訳: `tests/zshrc` 187 / `tests/tmux` 56 / `tests/bin` 45 /
残り 7 ディレクトリで 33。`tests/zshrc` の中は **`av1ify` 124 + `concat` 56 = 96%**。

**2026-09-05 から `test-discovered` は 2 腕** (Makefile の `SERIAL_TEST_DIRS`): 共有資源に触る
`tests/tmux` / `tests/nvim` / `tests/zshrc/tmux-session` だけ直列、残りは
`run_tests_parallel` で並列 (2026-09-05 時点で直列 41 本 / 並列 70 本。本数は毎回 `[並列] 対象 N 件` の行に出る)。実測 (14 コア機、各 1 サンプル):

| | before (直列) | after (2 腕) |
|---|---|---|
| `make test` | **490 秒 — 内訳の合計** (test-lint 22 + test-runtime 380 + test-src 88)。**通しは未測** | **190 秒 (通し)** |
| `test-discovered` | 367 秒 — テスト 1 本ずつの計測の合計。**通しは未測** | **128 秒 (通し)** |

🚨 **before は合計、after は通しなので、この 2 つを直接引き算しない**
(`_claude/rules/perf-claims-need-measurement.md`)。この repo では過去に合計 414 秒 / 通し 433 秒と
19 秒ずれた実績があり、**before の通しは 490 秒より大きい**と見るのが妥当 = 改善幅は 300 秒より
大きい側にぶれる。正確に比べたいなら並列化前の commit で通しを 1 回測ること。
`test-src` の go test も 7 プロジェクトを並列に回す (`run_go_projects`。lint は golangci-lint の file lock のため直列のまま)。

🚨 **待ちの実体はエンコードではない**。av1ify / concat のテストは ffmpeg / ffprobe を
**shell script のモック**に差し替えており (`tests/zshrc/*/test_helper.sh` が `$TEST_TMP/mock_bin` を
PATH 先頭に置く)、時間はモック内の `grep` 連打による **fork のオーバーヘッド**で積まれる
(モック ffprobe は 1 呼び出しで最大 24 本の `grep -q` を回す)。

### CI の heavy / rest 入口

`test-discovered-heavy` (av1ify + concat を並列。**実測 35 秒**、直列なら 180 秒) と
`test-runtime-rest` (heavy を除いた残り。並列腕 + 直列腕) の 2 job。`make test` は
この 2 つを合わせた集合で、差分の正本は Makefile の `CI_HEAVY_TEST_DIRS` / `CI_HEAVY_PRUNE`。

## 何をいつ回すか

- **既定は `make test`** (433 秒)。コミット前はこれを通す
- **`make test-changed PATHS="<触ったファイル>"` で代替してよいのは、時間が取れないときだけ**。
  その場合は**全体を回していない事実を報告に書く** (「docs だけだから省いた」を前例として
  積まない。issue 185 項目 4 / issue 188)
- 🚨 **`test-lint` の発見式ゲート 6 本 (`check_*.sh`) は `test-changed` からは一度も入らない**
  (写像に無い。実測 2026-09-03: `.github/workflows/tests.yml` を渡しても
  `test-workflow-action-pins` は入らず、`scripts/check_skip_exit_code.sh` を渡してもそれ自身は
  走らない)。**shell / テストスクリプト / workflow / Makefile / CI の構造を触ったら
  `make test-lint` を明示的に回す**
- 🚨 写像の穴として既知: `_claude/settings.json` は `*.json` に先勝ちして `test-json` だけになり
  `tests/claude` へ落ちない / `_claude/rules-rationale/*.md` は「テスト対象なし」になる
  (issue 188 の発火元がまさに rules-rationale の新設だった)

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
- **`cd` は rc を見る** (`cd "$X" || exit 1`)。見ないと、失敗しても CWD (= repo root) のまま先へ進み
  **fixture が repo に書かれる**。実際に踏んだ (issue 204): zsh のテストを `bash` で直接実行すると
  `source "${0:A:h}/test_helper.sh"` が bash で `/test_helper.sh` に潰れて helper が 1 行も走らず、
  `TEST_TMP` が空のまま `cd "/inj2"` が失敗して repo root に fixture 3 件が残った。
  `make test-cd-rc` (scripts/check_cd_rc_in_tests.sh) が落とす。例外は行内 `cd-rc: allow` + 理由。
  🚨 **カウントを手で確かめるときは `/usr/bin/grep`** を使う (Claude Code の grep は ugrep 経由で
  `$` を行末アンカーと解釈し、`cd "$TEST_DIR"` を 0 件と返す)

## platform (macOS のみ)

対象は macOS だけ (issue 133)。**CI も全 workflow が macos-15 runner**なので、手元と CI の
userland は同じ。かつて「手元 BSD / CI GNU」の差を潰すために持っていた道具
(`make test-gnu` / `scripts/with_gnu_grep.sh` / `scripts/check_platform_dialect.sh`) は、
**対象が macOS だけになった時点で「正しい macOS の書き方」を弾く側に回った**ので外した
(例: 素の `stat -f %m` は macOS では正しいのに、あの検査は GNU フォールバックを要求していた)。

- BSD の書き方で構わない。「GNU でも動くように」だけを理由に分岐を足さない
- 🚨 **残っているのは「版」の差**。CI の `/bin/bash` は 3.2、開発機は Homebrew の 5 系。
  workflow が brew の bash を PATH 先頭に出して揃えている (下の節)

## 新品チェックアウトとの差 (`make test-fresh`)

platform の差が消えた今も残るのが**「手元には在るが git に載っていないもの」への依存**。
`make test-fresh` が使い捨て worktree (= 追跡されているものだけがある状態) で
`test-discovered` + `test-bats` を回すので、**push 前に自分で叩く** (`make test` からは
呼ばない。所要時間を倍にしないための opt-in)。

- 判定は「ignore されているか」ではなく **「git に載っているか」**。ignore (`tmp/` は
  `~/.gitignore_global` 由来で repo の `.gitignore` に無い) だけでなく、**空ディレクトリ**
  (`issues/next/` は git がディレクトリごと載せない) と untracked でも同じ形で壊れる
- HEAD で回るので**未コミットの変更は検査しない**。これは「新品チェックアウト」の定義そのもの
- 後始末の正本は `scripts/with_fresh_worktree.sh` (起動時に前回の残骸も掃除する。
  `trap` は SIGKILL では走らないため)

## tmux を触るテスト

- 冒頭で `unset TMUX TMUX_PANE` し、`-L <一意名>` か `TMUX_TMPDIR` で socket を隔離する。`$TMUX` が生きていると `TMUX_TMPDIR` は無視されて本番サーバへ向く (2026-07-07 に `make test` が本番を kill した。tests/tmux/test_fork_scratch.sh 冒頭が正本)
- `tests/tmux/lib/isolate_env.sh` は HOME / XDG / ロケールの隔離**のみ**で、socket は対象外
- scripts/ の unit テストは stub 方式 (PATH 先頭に偽 tmux / gum / fzf を置いて呼び出しを記録。共有アサートは tests/tmux/lib/stub_assert_helper.sh)。実サーバが要るテストだけ Darwin skip
- shim を PATH 先頭に置くときは実体を絶対パスで解決してから exec する (相対名は自分自身に解決して無音で無限再帰。`~/.claude/rules/path-shim-must-resolve-real-binary.md`)

## 並列実行と CI の分割

- `run_tests_parallel` に入れられるのは tempdir 独立で共有資源に触らないテストだけ。`test-discovered` は tests/ 全体をこれで回し、共有資源 (tmux サーバ / nvim) に触る `SERIAL_TEST_DIRS` (tests/tmux / tests/nvim / tests/zshrc/tmux-session) だけを直列の `run_tests` に回す。新ディレクトリは並列側に入るので、共有資源に触るテストを書いたら `SERIAL_TEST_DIRS` へ足す
- CI は heavy / rest の 2 job。分割の整合 (パッケージ依存・prune) は `make test-ci-group-deps` が検査する
- **並列度は Makefile の `NPROC`** (既定 = `getconf _NPROCESSORS_ONLN`)。`run_tests_parallel` の `xargs -P` に渡る。**コマンドラインでのみ上書きできる**: `make test NPROC=4` は効くが `NPROC=4 make test` は効かない (makefile の `:=` は環境変数より強い、という make の変数優先順位そのもの)
  - 🚨 **`make -j` はテストの並列度に効かない**。あれは make の target 並列で、テストの並列度は `xargs -P` 側が持つ。2 つは独立
  - 🚨 **性能 knob としては期待外れ**。テストの大半は CPU ではなく待ち (プロセス spawn / tmux サーバ起動 / 条件ポーリング) で、`real` に対する `user+sys` は 9〜43% (実測 2026-09-05。ffmpeg を回す av1ify だけが 82〜86%)。下げても大して遅くならず、上げても速くならない。効くのは直列腕を減らすこと (issue 262)

## bench

- `tests/<area>/bench_*.sh` は `metric=<name> ms=<value>` 行を出し、同ディレクトリの `bench_budgets.ci` と `tests/check_bench_budgets.sh` で照合する (CI の bench.yml が tests/run_bench.sh で複数回実行 → min 集約)。`make test` の対象ではない。予算値の決め方は各 `bench_budgets.ci` のコメントが一次情報

## 新しいテストを書いたら

- 壊す変更を 1 つ当てて red を見るまで commit しない (`~/.claude/rules/mutation-verify-new-tests.md`)。変異は使い捨て worktree で当てる (共有 tree では並行セッションの変異と混ざる)
