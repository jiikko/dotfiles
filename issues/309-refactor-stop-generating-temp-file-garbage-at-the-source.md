# refactor: 一時ファイルが残らない構造を作る（掃除機構ではなく発生源の遮断）

起票日: 2026-09-06
カテゴリ: refactor
優先度: 中（個別の漏れは 298 / 299 / 305 / 307 で塞げるが、**同じ穴が独立に再生産され続ける**のが本題）
出典: /audit resource-leaks 2026-09-06 の横断観察 + ユーザーからの問い
（「zsh の一時 dir を作って時間経過で消すコマンドは使っていないのか」）

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

[`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md) §0-A
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
[`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md) §8:
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

作ると決めた場合の必須条件（[`sandbox-real-destructive-test-apis.md`](../_claude/rules/sandbox-real-destructive-test-apis.md)）:

- 対象を**自分の prefix の前方一致**に限定し、`TMPDIR` が想定の形かを**実行前に検査して外れたら失敗**させる（fail-closed）
- `os.Lstat` で symlink を skip、`Uid == os.Getuid()` を確認
- 「pid が生きていない」判定は**pid 再利用の窓**を持つ。`tmux -L <name> kill-server` のように
  被害が自分の資源へ閉じる操作に留める
- **判定は「消した件数」**。0 件を成功にしない
- 参照実装: `scripts/with_fresh_worktree.sh:sweep_stale`（自分の prefix かつ pid が生きていないものだけ）

## 受け入れ条件

- [ ] `TMPDIR` の隔離が `tests/tmux/lib/isolate_env.sh` 本体に入り、`test_smooth_scroll.sh` の自前 export が消える
- [ ] **テストスイートを 1 回通しても実機 `$TMPDIR` のエントリ数が増えない**
      （判定は rc ではなく**前後の件数**。これが層 1 の合否そのもの）
- [ ] 隔離していないテストを落とす検査が**集約経路から実行され、その出力行が出る**
      （[`verify-execution-not-just-exit-code.md`](../_claude/rules/verify-execution-not-just-exit-code.md)）
- [ ] 共通ヘルパーに対する**変異検証**: 登録した掃除を外すと残留数が増えて red
- [ ] 層 3 を作るかどうかを、層 1・2 を入れた後の**実測**に基づいて判断し、結論をこの issue に書き戻す
      （作らないと決めたなら、その理由を該当コード直近のコメントにも残す
      = [`pending-issue-rationale-in-code.md`](../_claude/rules/pending-issue-rationale-in-code.md)）

## やらないこと

- ✗ TTL で消す cron / launchd を新設する（発生源が残ったまま破壊的操作が増える）
- ✗ `TMPDIR` 全体を対象にした掃除（自分が作っていないものを消す）
- ✗ 層 3 を層 1 より先に作る

## 関連

- issue 298 / 299（`tmux_schedule_keys.sh` の 2 経路）/ 305（テストの一時資源）/ 307（zsh の TERM）
- issue 308（この監査の記録。却下理由と未決着）
