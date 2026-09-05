# 262 perf: テストスイート高速化の計画 (直列腕の縮小 / CI の腕分割 / 並列度 knob)

起票日: 2026-09-05
カテゴリ: perf
優先度: **低** (E / A / D / F は決着済み。残る B・C は trigger 待ちなので pending)
出典: 2026-09-05 のセッション調査。実測は GitHub Actions run `33941911923` / `33938905925` /
      `33938663846` (macos-15) と、Makefile に既存のローカル実測 (14 コア)
レビュー: 2026-09-05 に codex の反証レビューを 1 周 (P1 2 / P2 4 / P3 2)。**うち 6 件を採用**
      (B の job/step 混在 / F-1 の閉包漏れ / F-2 の写像の実態 / E-1 の効果主張 / CPU 節の限界 /
      `Makefile` の行番号)。**1 件は却下**: 「`_ffprobe_helpers.zsh` を変えると av1ify が走らない」は
      誤り — `add_shell_targets` → `test-zshrc` が `tests/zshrc` 配下を再帰で拾うため走る (実測)。
      指摘の観察 (参照検出は空振りする) は正しいので、F-2 の記述はその形に直した

## 前提 — この計画で縮まないもの

**push ごとの CI 壁時計は `Bench / bench-tmux` の 347〜365s が決めている**。Tests を何倍速にしても
その陰から出ないので、**本 issue の効果は「Tests を単独再実行したときの待ち」と「ローカルの
`make test`」に限られる**。CI 全体を縮めたいなら bench 側の shard 化だが、それは
`.github/workflows/bench.yml` のヘッダで 2026-07-29 に見送り済みで、再評価の trigger は
「最長 job が 10 分を超えたら」。現在 5.8 分なので未達 (この issue はその判断を覆さない)。

- 🚨 bench の job 内の `BENCH_RUNS=20` は**意図的に直列**。並列にすると計測同士が CPU を
  奪い合い、レイテンシ計測そのものを汚染する。**ここを並列化してはいけない**

## 実測 (2026-09-05)

### CI: Tests の 2 腕

| job | 秒 |
|---|---|
| `dotfiles-tests (rest)` | 159 / 161 |
| `dotfiles-tests (heavy)` | 107 / 109 |

rest 140s (テスト step のみ) の内訳:

| 区間 | 秒 | 中身 |
|---|---|---|
| `test-syntax` | 7 | |
| 並列腕 | 29 | 41 本を `xargs -P 14` |
| **直列腕** | **87** | nvim 12 / **tmux 64** / tmux-session 11 |
| bats | 10 | `while read` の完全直列 |

内訳の合計は 133s で、テスト step 実測 140s との差 **7s は未説明** (make の起動・腕の切り替え。未計測)。

**不均等の正体は heavy/rest の振り分けではなく、rest が抱える直列腕 87s (rest の 62%)**。
`run_all_targets` は `for` ループなので、syntax → 並列腕 → 直列腕 → bats が 1 本の線に並ぶ。
heavy に何を移してもこの 87s は動かない。

直列腕の中では **`tests/tmux/test_schedule_keys.sh` 単体が 27s** (03:29:43→03:30:10) で、
tmux 腕 64s の 42%。

### ローカル: 並列度のスケール上限

Makefile の既存実測 (14 コア): 直列 1 本 **367s** → 並列腕 **122s** + 直列腕 **89s** = 211s。

`NPROC` を上げて縮むのは 122s 側だけで、89s は不変。理論下限は
**89s + bats + syntax ≒ 105s** (対 367s = **上限 3.5 倍**)。14 コアで既に 2.4 倍出ており、
**コアを増やしても頭打ち**。

### そもそも CPU ネックではない (2026-09-05 追測)

単体実行の `real` と `user+sys` (14 コア機、`/usr/bin/time -p`):

| テスト | real | CPU (user+sys) | CPU 率 |
|---|---|---|---|
| `bin/test_go_autobuild.sh` | **44.2s** | 4.1s | **9%** |
| `tmux/test_smooth_scroll.sh` | 4.4s | 0.5s | **12%** |
| `zshrc/concat/test_concat_force.sh` | 3.2s | 0.9s | 29% |
| `tmux/test_server_watchdog.sh` | 4.4s | 1.5s | 35% |
| `tmux/test_schedule_keys.sh` | 21.2s | 9.1s | 43% |
| `zshrc/av1ify/test_av1ify_ng_list.sh` | 7.4s | 6.1s | **82%** |
| `zshrc/av1ify/test_av1ify_avsync.sh` | 12.6s | 10.9s | **86%** |

- **heavy (av1ify/concat) だけが本当に CPU ネック** (ffmpeg)。しかも ffmpeg 自体が
  マルチスレッドなので `-P 14` で並べると**オーバーサブスクライブ**になる (CI の heavy 99s の
  内訳は未計測)
- **それ以外は 6〜9 割が待ち時間**。tmux サーバ起動 / プロセス spawn / 条件ポーリング /
  競合の窓を作る意図的な `sleep`
- 🚨 **この測り方は下振れする**。`/usr/bin/time -p` の `user+sys` は**待たれた子プロセスぶん
  だけ**を数えるので、以下の CPU は 1 秒も入っていない:
  - detach / reparent される **tmux サーバ**と `run-shell -b` の子孫
  - `go_autobuild` が起こす **async builder** (`bin/lib/go_autobuild.zsh` のバックグラウンド subshell)
  - ffmpeg が内部で起こす子

  したがって主張できるのは **「測定対象プロセスの CPU 率は低い」** までで、
  **「マシン全体で CPU が遊んでいる」の証明にはなっていない**。それを言うなら
  テスト実行中の負荷 (`ps` の総 CPU / load average) を別に測る必要がある。未計測

現時点で言えるのは「**CPU ネックだという証拠が無く、待ち主体である傍証は強い**」まで。
Amdahl の直列区間 (89s) が頭打ちの理由であることは変わらない。

## 施策

### A. `NPROC` を文書化する — **完了 (2026-09-05, `tests/CLAUDE.md`「並列実行と CI の分割」)**

並列度の knob は `Makefile:209` の `NPROC := $(shell getconf _NPROCESSORS_ONLN ...)` で、
`Makefile:214` の `xargs -P $(NPROC)` に渡っている。実測:

```
make -n test-discovered-heavy NPROC=3   → xargs -P 3    ✅
NPROC=3 make -n test-discovered-heavy   → xargs -P 14   ❌ (env は効かない)
```

env が効かないのは make の変数優先順位 (makefile の `:=` は env より強い) そのもので、バグではない。
問題は **どこにも書かれていないこと** (`grep -rn NPROC --include='*.md'` が 0 件)。
`make -j` はテストの並列度に一切効かない (target 並列と `xargs -P` は独立) ことも併記する。

書き先は `tests/CLAUDE.md` (無ければ `Makefile` のヘッダではなく、実際に読まれる入口)。
根拠: `_claude/rules/new-tool-requires-entrypoint-docs.md` (ヘッダコメントは入口に数えない)。

🚨 **性能 knob としては期待外れ**なので優先度を下げた (2026-09-05 の追測)。CPU ネックでない以上、
`NPROC` を下げても大して遅くならず、上げても速くならない。「knob が在ることが伝わらない」ことの
解消として価値は残るが、高速化の施策ではない。

**むしろ `-P` はコア数より多く取れる可能性がある**。待ちが 6〜9 割なら `-P` をコア数の 2〜3 倍に
すると縮むのが定石。ただし tmux サーバを数十個同時に起こすことになるので、**メモリと fd の上限を
先に測る**こと。未実測。

### B. 直列腕を CI の 3 本目の job へ出す

直列であること自体は変えず、**別 runner に置いて他と並列にする**。共有資源の前提を壊さない。

**job 単位で揃えた見積もり** (テスト step + job オーバーヘッド。heavy の実測 job 109s /
テスト step 99s から、オーバーヘッドを ~10s と置く。直列腕は tmux/nvim の toolchain 導入が
要るので +5s 見込む):

| job | テスト step | job 合計 |
|---|---|---|
| heavy | 99 (実測) | **109 (実測)** |
| rest (新) | 46 = syntax 7 + 並列腕 29 + bats 10 | ~56 |
| serial (新) | 87 | ~102 |
| **Tests の壁時計** | | **~109s** (現在 161s から **−32%**) |

🚨 **これは見積もりで、実測ではない**。3 分割後の律速は heavy 側 (109s) に移るので、
99s にはならない。オーバーヘッドと toolchain の実測が要る。

runner-minutes は増える (job 1 本ぶんの起動・checkout・toolchain)。

- グループ定義の出典は Makefile 側 (`SERIAL_TEST_DIRS`) に置いたまま、workflow の matrix に
  3 本目を足す形にする。依存コマンドも `CI_COMMANDS_*` が出典 (issue 073 の契約を壊さない)
- 新 job の依存は tmux/nvim が要る。`CI_COMMANDS_SERIAL` を足すことになる

### C. 直列腕を減らす (本丸だが重い)

A/B を全部やっても下限は直列腕の 87s。ここを削るのが唯一の伸びしろ。

`SERIAL_TEST_DIRS := tests/tmux tests/nvim tests/zshrc/tmux-session` の据え置き理由は Makefile 曰く
**「共有資源に触り、並列時の競合を検証していない」**。つまり「競合する」と確かめたのではなく
**未検証**。tmux テストには `-L` でソケットを隔離する既存の規律
(`_claude/rules/tmux-probe-requires-socket-isolation.md`) があるので、**隔離が効いている
ものは並列腕へ移せる可能性がある**。

- 1 本ずつ「独自 tempdir と `-L` ソケットだけで閉じているか」を確かめてから移す
  (Makefile の該当コメントが要求している手順)
- 移したら**並列で 3 回連続 green** を見る (`_claude/rules/mutation-verify-new-tests.md` の
  並行テストの規律)。1 回の green は flake を素通しする
- `test_schedule_keys.sh` の 27s は単体で tmux 腕の 42%。**まず何に 27s 使っているかを測る**
  (壁時計待ちなら `_claude/rules/avoid-wall-clock-assertions.md` の条件ポーリングへ)

### E. `sleep` を同期点に置き換える (推奨: 先にやる。効果最大)

2026-09-05 に `tests/` の `sleep` を全数分類した (`sleep 3600` / `sleep 300` の 25 件は
ダミープロセスを生かすためのもので実時間を待たないため除外。残り 60 件、静的合計 30.4s)。

| # | 分類 | 判定 |
|---|---|---|
| 1 | **ポーリングの刻み** (`until ...; do sleep 0.05`) | ✅ `avoid-wall-clock-assertions.md` の明示例外。触らない |
| 2 | **ダミープロセス** (`sleep 100 & kill` / `sh -c 'sleep 30'`) | ✅ 待っていない。ただし orphan 31 件の正体 |
| 3 | **競合の窓を作る入力** | ⚠️ 例外だが**同期点に置き換え可能**。ここが本丸 |
| 4 | **素の固定待ち** | ❌ 条件ポーリングにできる |

#### E-1. 分類 3 をイベント制御のゲートへ (~25s) — **完了 (2026-09-05, `6467aeea`)**

`tests/bin/test_go_autobuild.sh` を **44.0s → 18.3s (-58%)**。7 箇所の `FAKE_GO_SLEEP` を、
偽 go が「テストが release を置くまで待つ」ファイルゲートに置き換えた。

**FIFO ではなくファイルゲートを選んだ**: FIFO は呼び出しごとの分離・識別・`open()` の
reader/writer 相互待ち・失敗時の待ちプロセス回収がすべて自前になる (下の当初案が挙げていた
4 条件そのもの)。ファイルなら不要で、待ちの実体はポーリング (刻みの sleep は例外)。

🚨 **窓の担保を落として flake を作った**: `order` (「あとから始まったビルドが勝つ」) は
`sleep` が窓だけでなく**完走順**も兼ねており (A=1s が B=4s より先に終わる、という賭け)、
ゲート化でそれが消えて 12 回に 1 回落ちた。`wait "$A_PID"` は担保にならない
(`--async` のラッパーは builder を detach して即 exit する)。release 直後に「A の成果物が
入ったこと」を待つ形へ直して 14 回連続 green。**窓の作り方を変えるときは、その sleep が
他に何を担保していたかを列挙する**こと。

変異 6 本すべてで、期待した assert が最初に落ちることまで確認済み。

##### 当初の見立て (参考)

`tests/bin/test_go_autobuild.sh` は **並列腕の律速** (CI で最後に `[ok]` が出るテスト)。
real 44.2s / CPU 4.1s = **91% 待ち**で、セクションごとの実測から待ちの正体は `FAKE_GO_SLEEP=1〜4`:

```
521.7 → 524.6  2.9s   ビルド中に着地したソース更新を飲まない
531.9 → 536.6  4.7s   あとから始まった新しいビルドが古いビルドに負けない
537.2 → 541.5  4.3s   走行中の builder の作業ファイルを消さない
542.2 → 545.6  3.4s   spawn したビルドは HUP で死なない
(他 5 箇所)           合計 ~25s
```

これは「偽 go build が遅いビルドを演じる」= `avoid-wall-clock-assertions.md` が
**明示的に例外にしている「競合の窓そのものを作る sleep」**。縮めると検証したい競合が
起きなくなるので、**秒数を削るのは不可**。

**が、窓を時間で作る必要はない。** 偽 go を `sleep 4` ではなく **FIFO 待ち**
(`read < $FIFO`。テストが明示的に「進め」と言うまでブロック) にすると、
**窓の開始と終了をテストが決められる**ようになる (今は「4 秒あれば十分だろう」という当て推量)。

主張できるのはここまで。以下は**成り立たない** (2026-09-05 の反証レビューで指摘):

- ❌ 「実時間がゼロになる」— FIFO の open/read 自体と、その後の**プロセススケジューリング・
  rename・lock 解放・別 builder の観測**には窓が残る。減らせるのは**固定待ちのぶんだけ**
- ❌ 「原理的に flake が消える」— 上と同じ理由で、決定性が上がるだけ

**実装に必要なもの** (これを見積もらずに着手しない):

- ビルド呼び出しごとに FIFO を分ける + どの呼び出しかを識別する手段
- FIFO を書くタイミングをテスト側で保証する (書き忘れると**永久ブロック**する)
- 失敗・シグナル・cleanup で FIFO 待ちプロセスを回収する
- `open()` の reader/writer 相互待ちを避ける

同型: `tests/tmux/test_schedule_keys.sh` の fire 中の割り込みを作る 5 箇所
(:310 / :338 / :362 / :371 ほか)。🚨 **こちらは `/bin/sleep` を絶対パスで直接呼んでおり、
PATH 上の sleep stub を意図的に bypass している** = 本物の待ち。「`STUB_SLEEP_LOG` があるから
実 sleep を待たない」は **fire される側**の話で、テスト本体が作る外側の窓には当たらない。

🚨 置き換えたら**変異検証をやり直す**。窓の作り方を変えることは、そのテストが検出していた
競合の形を変えることでもある (`mutation-verify-new-tests.md`「競合の窓そのものを作るための
`sleep` は入力であり、テストの主張を決めている」)。並行テストなので **3 回連続 green** も要る。

#### E-2. 分類 4 を条件ポーリングにする (~5s) — **完了 (2026-09-05, `0a9db0d0`)**

実測 (ローカル単体実行、before → after):

| テスト | before | after |
|---|---|---|
| `tmux/test_server_watchdog.sh` | 4.42s | 2.90〜3.04s |
| `tmux/test_ctrl_v_paste.sh` | 2.67s | 0.52〜0.75s |
| `tmux/test_log_kill_command.sh` | 2.82s | 1.80〜2.21s |
| `claude/test_claude_links_sync.sh` | 2.85s | 1.65〜1.67s |
| `tmux/test_snapshot_health.sh` | 1.69s | 0.95〜1.22s |
| **合計** | **14.45s** | **7.8〜9.0s** |

bounded-wait は `tests/lib/wait_until.sh` に一本化した (rc だけ返し、診断と終了は呼び出し側)。

**残り**:
- `zshrc/lazy-loading/test_version_managers.sh:16` の `sleep 1` は **触らない**。cleanup の
  リトライ backoff で、go telemetry が非同期に書くファイルを待つもの。観測できる成立条件が
  無く、しかも失敗時しか走らないので通常経路のコストはゼロ
- `tests/bin/test_go_autobuild.sh:147` の `wait_for` が `tt_wait_until` と重複したまま。
  E-1 で同じファイルを触るので、そのときに寄せる
- 🚨 `tmux/test_ctrl_v_paste.sh` に**別セッションが同日追加したトースト検査**が、また
  手書きの `for _ in $(seq 60)` ループを持っている (5 実装目)。`tt_wait_until` へ寄せる

##### 元の分類 4 の一覧

| 場所 | 現状 | 置き換え |
|---|---|---|
| `tmux/test_ctrl_v_paste.sh:75,104` | `sleep 1` ×2 | `capture-pane` にマーカーが出るまでポーリング |
| `tmux/test_server_watchdog.sh:58,78,95,108,130` | `sleep 0.3〜0.5` ×5 = 2.1s | lock dir の出現/消滅を条件に |
| `tmux/test_log_kill_command.sh:156,181` | `sleep 0.3` ×2 | ログ行の出現待ち |
| `claude/test_claude_links_sync.sh:99` | `sleep 1` | link の出現待ち |
| `tmux/test_snapshot_health.sh:175` | `kill` 後の `sleep 0.3` | `kill -0` が偽になるまで |
| `zshrc/lazy-loading/test_version_managers.sh:16` | `sleep 1` | 要調査 |

🚨 `test_server_watchdog.sh` は**同一ファイル内で作法が割れている** (:47 と :181 は既に
ポーリング形、:58 以降は固定 sleep)。直すときはファイル全体を揃える。

条件は**関数**にすること (`wait_for "..." test -n "$(...)"` はコマンド置換が呼び出し時に
1 度だけ展開され、必ず時間切れになる。`avoid-wall-clock-assertions.md` の 🚨)。

#### E-3. 触らないもの

- `tmux/test_smooth_scroll.sh` の 12 箇所 — `sleep 0.3 # リピート判定 (150ms) を跨ぐ` のように
  **仕様の時定数を跨ぐ入力**。fake clock を注入できない領域なので残すのが正解
- `scripts/test_run_make_targets_parallel.sh:92` の `sleep 5` — `kill -9 $PPID` の後に残るだけ
  (分類 2)。**壁時計には効いていないはずだが未計測**。orphan 31 件の 1 つの可能性が高い

#### 見込み

🚨 **未実測の見込み**。E-1 が削れるのは `FAKE_GO_SLEEP` の固定待ち **~25s** のうち、
FIFO 同期のオーバーヘッドを引いたぶん。「44s → 20s 弱」は算術的には導けないので、
**着手したら before/after を実測して本文を更新する** (`perf-claims-need-measurement.md`)。
`test_go_autobuild.sh` が並列腕の律速であることは実測済みなので、縮めば並列腕 29s も縮む。
E-2 は直列腕から 4〜5s の見込み。**CI 全体は相変わらず bench-tmux 待ち**で、効くのは
ローカルの `make test` と Tests の単独再実行。

### D. 却下 — 動的バランシング (parallel_tests 相当)

並列腕は 41 本 29s まで縮んでおり、振り分けの最適化より直列腕の方が桁で効く。
実行時間ベースの振り分けを入れる価値は現時点で無い。

### F. 却下 — av1ify/concat を独立 workflow にして `paths` で絞る

「av1ify 系の sh / test を変更したときだけ走らせたい」という提案 (2026-09-05)。
**やれるが損の方が大きい**ので却下する。同じ提案が再生成されるのを防ぐため理由を残す。

**F-1. 推移閉包に共有 leaf が 3 つ入る。** av1ify/concat テストが触るのは **10 ファイル**:

```
bin/binav1c
zshlib/_av1ify.zsh → _av1ify_encode.zsh, _av1ify_postcheck.zsh, _video_health.zsh, _ansi_colors.zsh
zshlib/_concat.zsh → _concat_helpers.zsh, _ansi_colors.zsh
_av1ify_encode / _av1ify_postcheck / _video_health → _ffprobe_helpers.zsh
_av1ify_encode / _concat_helpers                  → _fs_helpers.zsh
```

末端の **`_ansi_colors.zsh` / `_ffprobe_helpers.zsh` / `_fs_helpers.zsh` は av1ify 専用ではない**
(`_ffprobe_helpers.zsh` は `_repair_mp4.zsh` / `_validate_mp4.zsh` からも使われる)。

- paths に**入れる** → 共有 leaf を触るたびに走るので絞り込み効果が薄い
- paths に**入れない** → `_ffprobe_helpers.zsh` を壊しても av1ify テストが 1 度も走らない

🚨 **「静的な列挙では正解が無い」は言い過ぎ**だった (反証レビューの指摘。撤回する)。
共有 leaf を列挙に含めれば正しさは保てる。成立する却下理由は
**①絞り込み効果が薄くなる ②依存が増えたときの更新漏れが検出されない** の 2 点。

**F-2. 判定が 2 実装になる (ただし現行の写像は粗い)。**
`scripts/test_changed.sh` が既に「この変更でどのテストを回すか」を動的に答えている。ただし
**当初この issue が書いた「参照しているテストディレクトリを精密に発見する」は誤り**
(反証レビューの指摘。訂正する)。実際の経路は 2 本で、精度が違う:

- `add_shell_targets()` (`:92`) — `zshlib/*` なら無条件で `test-zshrc` を回す。
  `test-zshrc` は `test-dir DIR=tests/zshrc` → `run_tests` の `find` が**再帰**するので、
  **av1ify/concat も丸ごと走る**
- `add_test_dirs_referencing()` (`:111`) — `tests/*/` を**深さ 1 だけ** basename で `grep -rl`。
  `tests/zshrc/av1ify` は個別には見えず、返るのは `tests/zshrc` まで

実測 (`make test-changed PATHS=... DRY_RUN=1`):

```
zshlib/_av1ify_encode.zsh  → targets: ... test-zshrc / tests: tests/zshrc
zshlib/_ffprobe_helpers.zsh → targets: ... test-zshrc            ← 参照検出は空振り
```

つまり **`_ffprobe_helpers.zsh` の変更でも av1ify は走る**が、それは精密な参照検出ではなく
`test-zshrc` という**粗い網**のおかげ。ここが F-2 の要点で、
**paths filter を入れるとこの粗い網より狭くなる = 現状からの退行になる**。

同じ罠をこの repo は 1 回踏んでコメントに残している (`src_glogx.yml`: statusline を paths に
入れ忘れて「shell だけ変えた push で検査が 1 度も走らない」を red team に指摘された)。
`mutation-verify-new-tests.md`「同じ判定を 2 箇所で別実装していないか」。

**F-3. 壁時計が縮まない。** heavy は 99s で元々短い側なので、独立させても Tests は rest の
161s のまま。節約できるのは runner-minutes 99s/push だけ。

**再評価の trigger**: heavy が rest を追い越したとき (= av1ify のテストが増えて 161s を超えたとき)。
そのときも `paths` ではなく **job の中で `test_changed.sh` の写像を使って skip 判定する**
(判定を 1 箇所に保つ)。ただし「スクリプト判断で skip した job は緑」なので、
`verify-execution-not-just-exit-code.md` に従い **skip したことを job の出力に出す**配線が要る。

## 未確認 / 観測ポイント

- rest ジョブの後始末に **`Terminate orphan process: pid (...) (sleep)` が 31 件**出ている。
  分類 2 の `sleep` が job 終了まで生き残っている形。害は出ていない (runner ごと消える) が、
  **壁時計を食っているかは未計測**。E に着手するなら最初に測る
- CI の heavy 99s の内訳 (ffmpeg のオーバーサブスクライブがどれだけ効いているか) は未計測
- `run_tests_parallel` の `NPROC` を CI で明示していない (runner のコア数任せ)。macos-15 の
  コア数と、それが並列腕 29s にどう効いているかは未実測
- `-P` をコア数の 2〜3 倍にしたときのメモリ / fd 上限は未実測 (A 節)

## 進め方の提案

**E は完了 (E-1 `6467aeea` / E-2 `0a9db0d0`)、D と F は却下、A は完了 (2026-09-05, `tests/CLAUDE.md`)。
残りは B と C だけで、どちらも trigger 待ちなので `issues/pending/` へ置く。**

- **B (直列腕を 3 本目の job へ)** — trigger: **「Tests だけを再実行したときの待ちが長い」**と
  実際に感じたとき (flake の切り分け中など)。CI 全体の壁時計は bench-tmux 347s が決めるので、
  B を入れても push ごとの体感は 1 秒も変わらない
- **C (直列腕を減らす)** — trigger: **B の完了**。着手は `tests/tmux/test_schedule_keys.sh` の
  27s が何に使われているかを測るところから (E-1 で偽 go に入れたゲートと同じ手が使えるかもしれない)

## 着手条件 (pending の理由)

上記のとおり **B / C はどちらも「痛みが出てから」**。今やっても CI の壁時計は縮まず、
runner-minutes だけ増える。**bench-tmux が 10 分を超えたら**まず bench 側の shard 化
(`.github/workflows/bench.yml` のヘッダに 2026-07-29 の判断あり) が先で、この issue はその後。
