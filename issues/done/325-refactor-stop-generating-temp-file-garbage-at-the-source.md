# refactor: 一時ファイルが残らない構造を作る（掃除機構ではなく発生源の遮断）

起票日: 2026-09-06
カテゴリ: refactor
優先度: 中（個別の漏れは 298 / 299 / 305 / 307 で塞げるが、**同じ穴が独立に再生産され続ける**のが本題）
出典: /audit resource-leaks 2026-09-06 の横断観察 + ユーザーからの問い
（「zsh の一時 dir を作って時間経過で消すコマンドは使っていないのか」）

> 🚨 **旧番号 309 からの改番**（2026-09-07）。commit `bc11659f` の message が「309 に起票」と
> 書いているのはこの issue のこと。並行セッションが同じ時刻帯に
> `done/309-feat-hook-issue-progress-check-stop.md` を採番していたため、
> tracked 参照が 0 件だったこちらを動かした（README「参照の少ない側を空き番号へ寄せる」）。

## なぜ個別修正では閉じないか

2026-09-06 の監査で見つかった一時ファイルの漏れは、**全部が同じ穴の別インスタンス**だった:

| issue | 場所 | 漏れる経路 |
|---|---|---|
| 298 | `tmux_schedule_keys.sh:cmd_wizard` | 正常復帰（`local` 変数が EXIT 時に空展開） |
| 299 | 同 `ui_run` / `check_go_project_lanes.sh` | 中断（trap の対象に入っていない） |
| 305 | Go の `TestMain` 3 箇所 / tmux テストの socket | 中断（`defer` / `trap` に到達しない） |
| 307 | `check_syntax.zsh` | 中断（**zsh は TERM で EXIT trap を走らせない**） |

**各スクリプトが素の `mktemp` と自前の `trap` を書いている**ので、書く人ごとに違う穴が開く。
`zshlib/` `_zshrc` `scripts/lib/` に一時ディレクトリの共通ヘルパーは **1 つも無い**（`mktemp` の grep が 0 件）。

## 「時間で消える」は当てにできない（実測 2026-09-06）

| 確認 | 結果 |
|---|---|
| `/etc/periodic/` `/etc/defaults/periodic.conf` | **存在しない**（BSD の `daily.clean-tmps` 系は macOS 15 に無い） |
| TMPDIR の最古エントリ | **2026-07-04** |
| `kern.boottime` | **2026-07-04 18:52**（uptime **64 日**） |
| 7 日より古いエントリ | **2,766 個**（30 日超が 746 個）が残存 |

最古ファイルの日付が起動時刻と一致するので、**`/var/folders/.../T` は起動時に一掃されるが、
稼働中は誰も掃除していない**。64 日無再起動の使い方では GC として機能しない。
`make clean-tmp` はあるが対象は **repo の `./tmp`** で TMPDIR ではない。

## 🚨 第一手は「掃除機構」ではない

[`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) §0-A
（作らずに済む構造を 1 度問う）に従うと、今回はその答えが実測で出ている。

**14,147 個の出どころはテストだった**（実測: `tests/tmux/test_schedule_keys.sh` を 1 回走らせると
**ちょうど 61 個**増える。残存ファイルの時刻分布も全部 61 の倍数で、14,147 ÷ 61 ≒ **232 回の実行**ぶん）。
中身も fixture（`tabbed<TAB>1788589589<TAB>main:3 claude<TAB>a<TAB>b`）で、人が入れた予約ではない。

テストが `TMPDIR` を隔離していれば、ゴミは**テスト自身の使い捨てディレクトリの中**に落ち、
そのディレクトリごと消える。**掃除機構も TTL も破壊的操作も新設せずに済む。**

```
現状: テスト → 実機の $TMPDIR に 61 個/回 → 誰も消さない
修正: テスト → export TMPDIR="$TMP_DIR" → 使い捨て dir の中 → dir ごと消える
```

## 設計（3 層。上から順に着手し、下の層は上を入れて実測してから判断する）

### 層 1: 発生源を断つ（最優先・破壊的操作ゼロ）

**テストは実機の `TMPDIR` を使わない。**

- 既に受け皿がある: `tests/tmux/lib/isolate_env.sh` が `HOME` / `XDG_DATA_HOME` /
  `TT_DEBOUNCE_STATE_DIR` を隔離している。**`TMPDIR` だけが対象外**で、
  `test_smooth_scroll.sh:34` が「source 後に自前で export」する形で 1 本だけ足している
  （同ファイルのコメントがその経緯を書いている）
- → **`TMPDIR` を `isolate_env.sh` 本体へ移す**。`test_smooth_scroll.sh` の自前 export は消す
- → `isolate_env.sh` を source していないテストにも同じ隔離を配る
  （実測: テスト 130 本のうち `TMPDIR` に言及しているのは **18 本**、
  `export TMPDIR` しているのは **1 本**だけ）

**機械で守る**: 「repo のスクリプトを起動しているのに `TMPDIR` を隔離していないテスト」を落とす検査。
`scripts/check_*.sh` の系列（`check_pipefail_grep_q.sh` / `check_cd_rc_in_tests.sh` /
`check_skip_exit_code.sh` / `check_trigger_log_writers.sh`）に並べる。

🚨 字句 gate なので
[`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) §8:
書く前に**脅威モデル**（うっかり書く典型形を止める / 意図的迂回は review の責務）と
**「検出しないと決めた形」**をヘッダに書く。canary は本走査と同じ関数を通す。

### 層 2: 正常経路と中断経路の両方で消す（共通ヘルパー）

層 1 を入れても、**人がウィザードを使ったぶんの漏れは残る**（298 の本体）。

- `mktemp` した側が登録し、**1 本の trap** がまとめて消すヘルパーを `scripts/lib/` に置く
- 🚨 **shell ごとに trap の張り方が違う**（実測）:

  | shell | EXIT trap は TERM で走るか |
  |---|---|
  | bash | **走る** |
  | zsh 5.9 | **走らない**（INT / HUP では走る） |

  → zsh 側は `TERM` / `INT` / `HUP` を明示的に張る。
  bash は **EXIT trap を 1 本しか持てない**ので、配列に一本化して張り直しを禁じる（issue 299 の却下理由）
- 🚨 **`local` 変数を trap 本文から参照しない**。正常復帰後の EXIT では空展開する（issue 298 の実測）。
  ヘッダにこの理由を書く

### 層 3: 起動時掃除（最後の手段。**層 1・2 を入れて実測してから判断する**）

**先に作らないこと。** 発生源が層 1 で止まるなら、掃除機構は
「破壊的操作を 1 本増やしただけで、ゴミは別経路から出続ける」という最悪の形になる。

作ると決めた場合の必須条件（[`sandbox-real-destructive-test-apis.md`](../../_claude/rules/sandbox-real-destructive-test-apis.md)）:

- 対象を**自分の prefix の前方一致**に限定し、`TMPDIR` が想定の形かを**実行前に検査して外れたら失敗**させる（fail-closed）
- `os.Lstat` で symlink を skip、`Uid == os.Getuid()` を確認
- 「pid が生きていない」判定は**pid 再利用の窓**を持つ。`tmux -L <name> kill-server` のように
  被害が自分の資源へ閉じる操作に留める
- **判定は「消した件数」**。0 件を成功にしない
- 参照実装: `scripts/with_fresh_worktree.sh:sweep_stale`（自分の prefix かつ pid が生きていないものだけ）

## 🚨 前提の訂正: macOS の `mktemp` は `TMPDIR` 環境変数を見ない（実測 2026-09-08）

この issue の層 1 は「スクリプトの `mktemp` は `${TMPDIR:-/tmp}` 配下へ落ちる」を前提にしていたが、
**macOS ではその前提が形によって成り立たない**。`TMPDIR` を使い捨て dir に export した状態で実測:

| 形 | `TMPDIR` に従うか |
|---|---|
| `mktemp -d`（引数なし） | ✗ 実機の `/var/folders/.../T` |
| `mktemp`（引数なし） | ✗ 実機 |
| `mktemp -d -t pfx` | ✗ 実機 |
| `mktemp -t pfx` | ✗ 実機 |
| `mktemp -d "$TMPDIR/x.XXXXXX"` | ✓ 隔離 dir |

BSD の `mktemp` は `_CS_DARWIN_USER_TEMP_DIR` を使い、**`-t` を付けても環境変数を参照しない**。
`TMPDIR` に従うのは**パスを自分で組み立てる形だけ**。

これで層 1 の射程が確定した:

- ✓ 効く: `scripts/tmux_schedule_keys.sh:116` の `SK_TMP_PREFIX="${TMPDIR:-/tmp}/schedkeys"` の形
  （14,147 個の元凶。これは既に issue 298 の対応で塞がっている）
- ✗ 効かない: テスト自身の `mktemp -d`（引数なし）。**後始末はテスト自身の trap の責任**で、
  隔離では原理的に消せない → issue 305 の領域

## 実測: スイート 1 回で実機 `$TMPDIR` に残るもの（2026-09-08）

| | 件数 |
|---|---|
| 修正前（この issue の起票時点、schedkeys 未対応） | 61 個/回 + 累計 14,147 個 |
| 本 issue の着手時点（298 対応後） | **8 個** |
| 層 1 実装後 | **5 個** |

残る 5 個の内訳:

- **3 個は repo 由来ではない**: `TemporaryDirectory.*`（中身は `.keep-directory`）。
  repo 全体を grep して 0 件で、8/25 から継続的に発生し**今日だけで 45 個**ある。
  make test 中に増えたのは別プロセスとの同時発生。**この issue のスコープ外**
- **2 個は「テスト自身の `mktemp -d`」の後始末漏れ**:
  `tests/zshrc/av1ify/test_av1ify_clipboard.sh`（`test_helper.sh:12` の `TEST_TMP="$(mktemp -d)"`。
  `trap cleanup EXIT` で `rm -rf` しているのに残る。単独実行でも再現）と tmux 系。
  上表のとおり **TMPDIR 隔離では消せない形**なので issue 305 へ送る

## 受け入れ条件

- [x] `TMPDIR` の隔離が `tests/tmux/lib/isolate_env.sh` 本体に入り、`test_smooth_scroll.sh` の自前 export が消える
- [x] **隔離を全テストに配る** — issue は「isolate_env を source していないテストにも配る」と
      書いていたが、45 本を個別に触る代わりに**`Makefile` の runner 2 箇所**（`run_tests` /
      `run_tests_parallel`）でテストごとに使い捨て `TMPDIR` を作って渡し、終わったら消す形にした。
      1 箇所で全テストに効き、**テスト側の後始末漏れも runner が回収する**
      （掃除機構の新設ではなく、自分が作ったディレクトリを捨てているだけ）
- [ ] **テストスイートを 1 回通しても実機 `$TMPDIR` のエントリ数が増えない** — **未達（5 個）**。
      内訳は上記のとおりで、**3 個は repo 外、2 個は TMPDIR 隔離では原理的に消せない形**。
      層 1 でできることは尽きているので、残りは issue 305（テストの一時資源の後始末）へ送る
- [x] 検査が**集約経路から実行され、その出力行が出る**
      → `tests/scripts/test_runner_isolates_tmpdir.sh`。`tests/` 配下の `test_*.sh` は
      `run_tests_parallel` が自動で拾うので配線は不要（`make test` のログに `[ok]` 行が出る）
- [x] **変異検証 6 本**すべてで狙ったケースが red:
      直列 runner の `TMPDIR` を外す / 同 `rm -rf` を外す / 並列 runner の `TMPDIR` を外す /
      同 `rm -rf` を外す / `isolate_env.sh` の `export TMPDIR` を外す / `define` 名を変えて
      抽出を壊す（= 抽出が空でも「違反 0 件」で緑にならないことの canary）
- [x] **層 3（起動時掃除）は作らない**。理由: ①発生源側で 14,147 → 5 個まで落ちており、
      残り 5 個のうち 3 個は repo 外なので**掃除機構を作っても自分のゴミは 2 個しか減らない**
      ②その 2 個は「テストの trap が効いていない」バグで、掃除機構はそれを隠すだけ
      ③実機 `$TMPDIR` を対象にした削除は「自分が作っていないものを消す」禁止事項に触れる。
      この判断は `Makefile` の runner のコメントにも残した
      （[`pending-issue-rationale-in-code.md`](../../_claude/rules/pending-issue-rationale-in-code.md)）
- [x] **層 2（共通ヘルパー）は作らない**。runner が使い捨て `TMPDIR` を配る形にしたことで、
      「各テストが自前の trap を正しく書く」への依存が減った。人がウィザードを使ったぶんの漏れ
      （298 の本体）は既に `sk_mktemp` が登録制で解決済み。**新しいヘルパーを 1 本増やす価値より、
      既存 78 本を書き換えるコストの方が大きい**（`_claude/rules/verify-design-intent-before-refactor.md`）

## 静的 gate を作らなかった理由

issue は「隔離していないテストを落とす検査」を求めていたが、**軸が構文にあると実害と相関しない**
（[`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) §8）。
母集合を数えた実測:

- `mktemp -d` を使うテスト: **78 本**（うち隔離あり 6 本）
- repo のスクリプトを起動しているテスト: **48 本**（うち隔離なし **45 本**）
- それらが実際に実機へ残す残骸: **2 個**

初日から 45 件の allowlist が必要になる gate は gate として機能しない。代わりに
**runner 側の隔離（1 箇所）を pin する検査**にした。母集合が 2 つの `define` なので allowlist が要らない。

## やらないこと

- ✗ TTL で消す cron / launchd を新設する（発生源が残ったまま破壊的操作が増える）
- ✗ `TMPDIR` 全体を対象にした掃除（自分が作っていないものを消す）
- ✗ 層 3 を層 1 より先に作る

## 関連

- issue 298 / 299（`tmux_schedule_keys.sh` の 2 経路）/ 305（テストの一時資源）/ 307（zsh の TERM）
- issue 308（この監査の記録。却下理由と未決着）
