# 640 (refactor): 時間の上限付きでコマンドを実行する処理を、共通の道具 1 つに寄せる

> 🚨 **担当中: dotfiles-4d**（2026-10-06〜）

起票日: 2026-10-06

## 概要

「コマンドを時間の上限付きで実行し、時間が来たら止める」処理が、dotfiles に 2 通りの方式で 12 か所ある。
方式ごとに、子プロセスまで止めるか・上限を超えたときの rc・依存が違う。共通の道具を 1 つ作り
(coreutils の `timeout` 相当 + 子プロセスごと止める)、手組みと `timeout` / `gtimeout` 頼みを置き換える。
道具を Go の CLI (`src/`) にするか bash の共有関数 (`bin/lib/`) にするかは、下の「決めること」で決める。

きっかけは issue 639 で `bin/lib/claude_bin.sh` に同じ手組みを 1 つ増やしたこと
(`tests/bin/test_go_autobuild_warmup.sh` の `runs_within` とほぼ同じ形)。

## 詳細 (2026-10-06 時点の棚卸し)

**a. coreutils の `timeout` / `gtimeout` に頼る** (7 ファイル。どれも tests/)

| ファイル | 箇所 | 上限 |
|---|---|---|
| `tests/claude/test_deny_bare_tmux_kill.sh` | `TIMEOUT_BIN` (timeout → gtimeout、どちらも無ければ exit 1)。rc=124 を時間切れと判定 | 10 秒 (`HOOK_TIMEOUT`。settings.json の hook の上限に合わせる) |
| `tests/claude/test_deny_piped_push_then_destroy.sh` | 同上 | 10 秒 |
| `tests/claude/test_next_claim_push.sh` | 同上 | 10 秒 |
| `tests/claude/test_next_claim_unshared.sh` | 同上 | 10 秒 |
| `tests/claude/test_warn_discarding_checkout.sh` | 同上 | 10 秒 |
| `tests/bin/test_go_autobuild.sh` | `TIMEOUT_BIN="$(command -v timeout)"` (gtimeout へのフォールバックは無い) | 5 秒 |
| `tests/zshrc/bench_zsh.sh` | `command -v timeout` があるときだけ `timeout 60` を付ける。**無ければ上限なしで黙って走る** (素の macOS) | 60 秒 |

**b. 背景で起動して kill する手組み** (5 か所)

| ファイル | 箇所 | 上限 | 時間切れの rc | 子プロセス |
|---|---|---|---|---|
| `bin/lib/claude_bin.sh` | `_claude_bin_version_ok` (0.1 秒ごとに `kill -0`、超えたら `kill -9`) | 10 秒 (`CLAUDE_BIN_VERSION_TIMEOUT`) | 124 | 直接の子だけ |
| `tests/bin/test_go_autobuild_warmup.sh` | `runs_within` (同じ形) | 引数 | 1 | 直接の子だけ |
| `scripts/check_assert_reaches_exit.sh` | `run_probe` (`( sleep $PROBE_TIMEOUT; kill -9 $pid ) &` の変種。タイマーが pid だけを持つので、pid の再利用で別のプロセスを撃つ余地がある) | 180 秒 (`PROBE_TIMEOUT`) | 137 (kill -9 の wait) | 直接の子だけ |
| `bin/mutate-verify` | `--timeout` (背景起動 + rc ファイルのポーリング + `stop_tree`) | 1800 秒 (`MUTATE_VERIFY_TIMEOUT`) | 10 | **子孫まで** (`stop_tree`: 木を凍らせて集め、TERM → 5 秒 → KILL) |
| `bin/codex-fanout` | `launch_with_watchdog` (perl `setpgrp` で専用のプロセスグループを作り、`kill -- -pgid` でグループごと止める。「macOS に setsid が無い」ことへの対処が既にある) | 1200 秒 (`CODEX_FANOUT_TIMEOUT`、manifest の `timeout_s`) | 未確認 | **グループごと** |

**対象外と判定したもの** (kill はあるが上限付きの実行ではない。生存確認・後始末・孤児の掃除):
`tests/pro-con/test_e2e_multi_screen_dispatcher_killed.sh`、`tests/tmux/test_socket_cleanup.sh`、
`_claude/statusline-command.sh`、`zshlib/_av1ify.zsh`、`scripts/lib/tmux_resurrect_guards.sh`、
`scripts/tmux_reap_orphan_servers.sh`、`scripts/lib/worktree_scratch.sh`、`scripts/tmux_schedule_keys.sh`。
`bin/` `scripts/` `_claude/hooks/` `zshlib/` に `timeout N cmd` の直接呼び出しは無い (tests/ だけ)。
Go の中 (`exec.CommandContext` + `context.WithTimeout`、`src/subproc`) は対象外。

**今の問題**

- 子孫まで止めるのは `bin/mutate-verify` (ps で木を辿る) と `bin/codex-fanout` (プロセスグループ) の 2 つで、方式も違う。
  他は直接の子だけを kill するので、孫 (node の子、sh -c の先) が残りうる
- a 方式は coreutils に依存する。CI では `gtimeout` を入れているのは rest グループだけ (`Makefile` の
  `CI_COMMANDS_REST`。heavy グループには無い)。macOS の手元でも `brew install coreutils` が前提になる
- 時間切れの rc が方式で違う (124 / 1 / 137 / 10)。置き換えで rc の契約が変わるので、呼び出し側の判定 (rc を見ている箇所) を
  1 つずつ確かめる必要がある (未調査)
- `src/subproc` と `src/process_supervisor` は Go のライブラリで CLI を持たない (`package main` が無い)。
  シェルから使える上限付き実行の道具は今は無い

## 対応方針

1. 道具を 1 つ作る。引数は coreutils の `timeout` に寄せる: `<名前> <秒> <コマンド> [引数…]`。時間切れは rc=124
   (a 方式のテストが rc=124 で判定しているので置き換えが素直になる。b 方式は呼び出し側の rc の判定を合わせて直す)
   - **子孫まで止める**: 専用のプロセスグループで起動し、時間切れではグループに TERM → 猶予 → KILL
     (`bin/codex-fanout` の `launch_with_watchdog` が同じことを perl `setpgrp` でしている。まずそれを参照する)
   - プロセスグループの落とし穴を扱う: 子が `setsid` / `setpgid` でグループを抜けると漏れる
     (`bin/mutate-verify` の `stop_tree` は ps で木を辿ってこれを拾う。どちらで足りるかは設計で決める)。
     端末から対話で使われる場合の前景グループ・SIGTTOU・Ctrl-C は pty で旧実装と比べる
     (`adversarial-review-own-safeguards.md` §1 の端末の項)
   - stdin / stdout / stderr はそのまま通す。シグナルを受けたら子に伝える (Ctrl-C で道具だけ死んで子が残らない)
2. 置き換える。順番は依存の少ない方から: b 方式 → a 方式
   - `bin/mutate-verify` の `stop_tree` (凍らせてから集める) を、置き換えで落とさないか確かめてから
   - a 方式を置き換えると CI の `gtimeout` が要らなくなるので、`CI_COMMANDS_REST` と
     `.github/actions/ensure-toolchain` の `gtimeout` の写像を外せるかを確かめる
   - `tests/zshrc/bench_zsh.sh` の「timeout が無ければ上限なし」の黙った縮退も無くなる
3. 道具そのもののテスト: 時間内に終わる / 時間切れで rc=124 / 孫まで止まる / 子の rc をそのまま返す /
   シグナルを伝える。`avoid-wall-clock-assertions` に従い、所要時間ではなく「止まったか・何が残ったか」で判定する。
   commit の前に、子孫を止める処理を外す変異で「孫が残る」ケースが red になることを確かめる

## 決めること

- **道具の形: Go の CLI (`src/`) か、bash の共有関数 (`bin/lib/`) か**
  - Go を `go_autobuild` のラッパーで置くと、Go の有無・ビルド・lock に依存する。`tests/bin/test_go_autobuild.sh` は
    Go が無い経路を `timeout` で検査しているので、その検査が検査対象と同じ仕組みに頼る循環になる。初回ビルドの待ちが
    上限を食う問題もある (`tests/bin/test_go_autobuild_warmup.sh` が warmup の時間を見ているとおり実在する)
  - bash の共有関数なら依存は無く、`bin/codex-fanout` の perl `setpgrp` 方式をそのまま移せる。ただし子孫の扱い
    (`stop_tree` 相当) を bash で持つと長くなる
- 道具の名前
- 置き換えを 1 つの issue でやるか、b と a で分けるか

## 反証レビューの結果 (2026-10-06、read-only のサブエージェント 1 本)

- 採用: `bin/codex-fanout` の漏れ (子孫を止める 2 例目)、`tests/zshrc/bench_zsh.sh` の漏れ (a 方式の 7 件目)、
  b 方式の時間切れの rc が 124 にそろっていない、`run_probe` の pid 再利用、`go_autobuild` で包むときの循環と初回ビルドの待ち、
  プロセスグループの落とし穴と端末での挙動、変異で red を確かめる工程
- 棚卸しのその他の事実 (ファイル名・秒数・a 方式のフォールバック・CI の gtimeout は rest だけ) は反証されなかった

## 関連ファイル

- `bin/lib/claude_bin.sh`、`tests/bin/test_go_autobuild_warmup.sh`、`scripts/check_assert_reaches_exit.sh`、`bin/mutate-verify`、`bin/codex-fanout`
- 上の a 方式の tests/ 7 ファイル
- `Makefile` (`CI_COMMANDS_REST`)、`.github/actions/ensure-toolchain`
- `src/subproc`、`src/process_supervisor` (Go 内部の実行。CLI は持たない)
- issue 639 (きっかけ)

## 進捗

- [ ] 設計 (Go か bash か・名前・子孫の止め方・issue の分け方)
- [ ] `src/<名前>/` の実装とテスト
- [ ] b 方式 5 か所の置き換え (呼び出し側の rc の判定を合わせる)
- [ ] a 方式 7 ファイルの置き換えと、CI の gtimeout の要否
