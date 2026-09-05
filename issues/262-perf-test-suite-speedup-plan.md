# 262 perf: テストスイート高速化の計画 (直列腕の縮小 / CI の腕分割 / 並列度 knob)

起票日: 2026-09-05
カテゴリ: perf
優先度: **低** (CI 全体の壁時計は 1 秒も縮まない。下記「効かないこと」を先に読む)
出典: 2026-09-05 のセッション調査。実測は GitHub Actions run `33941911923` / `33938905925` /
      `33938663846` (macos-15) と、Makefile に既存のローカル実測 (14 コア)

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

## 施策

### A. `NPROC` を文書化する (推奨: 先にやる。1 行)

並列度の knob は `Makefile:185` の `NPROC := $(shell getconf _NPROCESSORS_ONLN ...)` で、
`xargs -P $(NPROC)` に渡っている。実測:

```
make -n test-discovered-heavy NPROC=3   → xargs -P 3    ✅
NPROC=3 make -n test-discovered-heavy   → xargs -P 14   ❌ (env は効かない)
```

env が効かないのは make の変数優先順位 (makefile の `:=` は env より強い) そのもので、バグではない。
問題は **どこにも書かれていないこと** (`grep -rn NPROC --include='*.md'` が 0 件)。
`make -j` はテストの並列度に一切効かない (target 並列と `xargs -P` は独立) ことも併記する。

書き先は `tests/CLAUDE.md` (無ければ `Makefile` のヘッダではなく、実際に読まれる入口)。
根拠: `_claude/rules/new-tool-requires-entrypoint-docs.md` (ヘッダコメントは入口に数えない)。

### B. 直列腕を CI の 3 本目の job へ出す

直列であること自体は変えず、**別 runner に置いて他と並列にする**。共有資源の前提を壊さない。

| | 現在 | 3 分割後 |
|---|---|---|
| heavy | 99 | 99 |
| rest | **161** | 46 (syntax + 並列腕 + bats) |
| serial (新) | — | 87 |
| **Tests の壁時計** | **161s** | **≒ 99s** (−38%) |

コストは job 1 本ぶんの起動・checkout・toolchain ~10〜15s (heavy の job 109s に対しテスト本体 99s)。
runner-minutes は増える。

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

### D. 却下 — 動的バランシング (parallel_tests 相当)

並列腕は 41 本 29s まで縮んでおり、振り分けの最適化より直列腕の方が桁で効く。
実行時間ベースの振り分けを入れる価値は現時点で無い。

## 未確認 / 観測ポイント

- rest ジョブの後始末に **`Terminate orphan process: pid (...) (sleep)` が 31 件**出ている。
  テストが起こした `sleep` が job 終了まで生き残っている。害は出ていない (runner ごと消える) が、
  この 31 個ぶんの待ちが壁時計を食っている可能性は**未計測**。C に着手するなら最初に測る
- `run_tests_parallel` の `NPROC` を CI で明示していない (runner のコア数任せ)。macos-15 の
  コア数と、それが並列腕 29s にどう効いているかは未実測

## 進め方の提案

**A のみを先に入れる** (1 行、リスクゼロ、knob の存在が伝わる)。B は「Tests の再実行待ちが長い」
という実際の痛みが出てから。C は B の後、`test_schedule_keys.sh` の 27s を測るところから。
