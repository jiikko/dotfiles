# 640 (refactor): 時間の上限付きでコマンドを実行する処理を、共通の道具 1 つに寄せる

起票日: 2026-10-06

## 概要

「コマンドを時間の上限付きで実行し、時間が来たら止める」処理が、dotfiles に 2 通りの方式で 12 か所ある。
方式ごとに、子プロセスまで止めるか・上限を超えたときの rc・依存が違う。共通の道具を 1 つ作り
(coreutils の `timeout` 相当 + 子プロセスごと止める)、手組みと `timeout` / `gtimeout` 頼みを置き換える。
道具を Go の CLI (`src/`) にするか bash の共有関数 (`bin/lib/`) にするかは、下の「決めること」で決める。

きっかけは issue 639 で `bin/lib/claude_bin.sh` に同じ手組みを 1 つ増やしたこと
(`tests/bin/test_go_autobuild_warmup.sh` の `runs_within` とほぼ同じ形)。

## 詳細 (2026-10-06 時点の棚卸し)

**a. coreutils の `timeout` / `gtimeout` に頼る** (7 ファイル 8 か所。どれも tests/)

| ファイル | 箇所 | 上限 |
|---|---|---|
| `tests/claude/test_deny_bare_tmux_kill.sh` | `TIMEOUT_BIN` (timeout → gtimeout、どちらも無ければ exit 1)。rc=124 を時間切れと判定 | 10 秒 (`HOOK_TIMEOUT`。settings.json の hook の上限に合わせる) |
| `tests/claude/test_deny_piped_push_then_destroy.sh` | 同上 | 10 秒 |
| `tests/claude/test_next_claim_push.sh` | 同上 | 10 秒 |
| `tests/claude/test_next_claim_unshared.sh` | 同上 | 10 秒 |
| `tests/claude/test_warn_discarding_checkout.sh` | 同上 | 10 秒 |
| `tests/bin/test_go_autobuild.sh` | `TIMEOUT_BIN="$(command -v timeout)"` (gtimeout へのフォールバックは無い) | 5 秒 |
| `tests/bin/test_go_autobuild.sh` (`bin/broken` を起動する行) | 素の `timeout 5` (`TIMEOUT_BIN` とは別。2026-10-06 の設計時に見つけた 8 件目) | 5 秒 |
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

## 設計 (2026-10-06 決定)

ユーザーの選択: **Go の CLI**、**置き換えは 640 で全部** (commit は段ごとに分ける)。

- 名前: `runtimeout` (`src/runtimeout/`、入口は `bin/runtimeout` の go_autobuild ラッパー。同期ビルド)
- 使い方: `runtimeout [-f] [-k <猶予秒>] <秒> <コマンド> [引数…]`。秒は小数可、`0` は上限なし (`bin/mutate-verify` の `--timeout 0` が要る)
- rc: 子の rc をそのまま返す (シグナルで死んだら 128+n) / 時間切れ 124 / 道具自身の誤用 125 (未知のフラグを含む。
  go_autobuild の温めが `--__autobuild_warmup__` で即終了を要求する) / 起動できない 126 / 見つからない 127。coreutils の `timeout` と同じ
- `-f`: 子を呼び出し元のグループのまま起こす (止めるのは ps の木だけ)。`bin/mutate-verify` が使う
  (分けると `stty tostop` の端末で SIGTTOU で止まる・Ctrl-Z が効かない、が mutate-verify の red team で実測済み。設計レビュー P1-1)
- 既定では子は専用のプロセスグループで起動する (`Setpgid`)。道具自身は呼び出し元のグループに残る
  (coreutils は道具ごと新しいグループへ移る。こちらは呼び出し元のグループを変えないので、`kill -- -<pgid>` で
  道具を止めていた呼び出し側は、道具の pid へ TERM を送る形に直す)
- stdin / stdout / stderr は fd をそのまま渡す (Go がパイプを挟まない形。EOF とパイプの意味を変えない)
- 止め方 (時間切れ): `bin/mutate-verify` の `stop_tree` を Go へ移す。`/bin/ps -Ao pid=,ppid=,pgid=,stat=` で
  「子のグループの全員 ∪ 子から ppid でたどれる子孫」を集め、SIGSTOP で凍らせて累積し (最大 10 周)、TERM → CONT →
  猶予 (既定 5 秒、`-k`) の間に消えなければ KILL。グループ宛ての kill (`kill(-pgid)`) も併せて撃つ
  (ps の取りこぼしの保険)。`setsid` でグループを抜けた子も、親が生きていれば木でたどれる
- シグナル: TERM / HUP を受けたら転送より先に凍らせて止める (設計レビュー P2-3)。INT / QUIT は子へ伝え
  (`-f` では端末から子にも届くので伝え直さない)、猶予の間に子が終わらなければ止める。道具がシグナルを受けて子もシグナルで
  死んだら、道具も同じシグナルで死に直す (rc=128+n で返ると、呼び出し元の bash は子が INT を処理したと読んで先へ進む。P3)
- `/bin/ps` は絶対パスで呼ぶ (`tests/bin/test_go_autobuild.sh` が PATH を絞って起動するため)
- 止められないもの (`stop_tree` と同じ): 時間切れより前に親を離れて init の子になり、かつグループも抜けたもの
  (daemon 化した子・tmux -L のサーバ等)。子が先に終わって reap された後は、木でたどれない (グループ宛てだけが届く)
- 呼び方: スクリプトもテストも `bin/lib/runtimeout.sh` の `runtimeout_resolve <repo root>` で起動時に 1 回だけ解決し、
  **バイナリの絶対パス** (`$RUNTIMEOUT`、export) で呼ぶ。既に入っていればそれを使う (PATH を絞るテストへ渡す形)。
  解決できなければ rc=1 で、呼び出し側は失敗にする (緑にしない)。run ごとにラッパーを通すと、再ビルドの失敗 rc=1 が子の rc=1 と
  見分けられない (設計レビュー P1-2 / P2-1)。テストは PATH・HOME・偽の go を差し替える前に呼ぶ (P2-5)
- 循環: `tests/bin/test_go_autobuild.sh` は go_autobuild でビルドした道具で go_autobuild を検査する形になる。
  go_autobuild が壊れると道具のビルドが落ち、helper の exit 1 で赤になる (黙って緑にはならない) ので受ける
- CI: Bench の zsh ジョブ (`bench_zsh.sh`) は Go を持たないので setup-go を足す (`check_assert_reaches_exit.sh` は
  `make test` からも CI からも呼ばれない手動の棚卸しなので、Lint には足さない)。
  rest は既に Go がある。`gtimeout` は誰も使わなくなるので `CI_COMMANDS_REST` と `ensure-toolchain` の写像から外す
- 置き換え先の rc の契約の変化: `bin/codex-fanout` は時間切れが 143 → 124 (`tests/codex_fanout.bats` の期待値も直す)、
  `scripts/check_assert_reaches_exit.sh` は 137 → 124 (呼び出し側は非 0 を一律「不明」に読むので意味は変わらない)。
  `runs_within` は「上限内に終わったか」(子の rc は見ない) なので `rc != 124` で読む。
  `bin/mutate-verify` は検証コマンド自身も 124 を返しうるので、「rc=124 かつ経過が上限以上」のときだけ時間切れと読む (P2-2)
- `tests/zshrc/bench_zsh.sh` の startup は道具の起動の分 (手元で中央値 1.69ms → 4.24ms、30 回) を min-of-5 で測って差し引く
  (指標の意味を変えない)

## 設計の反証レビュー (2026-10-06、read-only のサブエージェント 1 本、opus)

P1 2 / P2 5 / P3 4。採用は上の設計節に反映済み (P1-1 `-f` / P1-2・P2-1 起動時の 1 回の解決 / P2-2 経過時間の併用 /
P2-3 TERM で凍結を先に / P2-4 rc=143 を前提にした記述 (codex-drive の SKILL.md・lens のテンプレート・codex-fanout) /
P2-5 helper の呼び出し位置 / P3 死に直し・ErrDot・zombie の除外 (実装済みだった)・bench の計測値のずれ)。
P3 の「`--__autobuild_warmup__` が flag パッケージで rc=2 になる」は、flag パッケージを使わず自前で読んでいるので該当しない。
ぼやきに挙がった `scripts/check_assert_reaches_exit.sh` の `if ! run_probe …; then rc=$?` (rc が常に 0) は本物で、同じ変更で直した。

## 棚卸しの反証レビューの結果 (2026-10-06、read-only のサブエージェント 1 本)

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

- [x] 設計 (Go か bash か・名前・子孫の止め方・issue の分け方) — 上の「設計」節
- [x] `src/runtimeout/` の実装とテスト — feat(runtimeout) の commit
- [x] b 方式 5 か所の置き換え (呼び出し側の rc の判定を合わせる) — refactor(runtimeout) の b 方式の commit
- [x] a 方式 7 ファイル 8 か所の置き換えと、CI の gtimeout の要否 — refactor(runtimeout) の a 方式の commit (gtimeout は外した)

## 結果 (2026-10-07)

- 検証: worktree で root の `make test` (runtime 112 件 ok、skip は worktree 由来の `test_claude_links_complete` 1 件、Go 全 module rc=0)。
  最後のテストファイル 1 行の分割の後は `make test-lint` と `tests/bin/test_claude_bin.sh` を回し直した。
  `src/runtimeout` は CI と同じ `GOTOOLCHAIN=go1.25.0` でも `go test -race` が通った
- 呼び出し側の個別の確認: `tests/bin/test_mutate_verify.sh` 48 ケース / `tests/codex_fanout.bats` + `codex_run.bats` 31 件 /
  `tests/bin/test_claude_bin.sh` 11 件 / warmup 10 本 (runtimeout 自身を含む) / `bench_zsh.sh` 完走 /
  `check_assert_reaches_exit.sh tests/issues` (9 件中 rc に出る 8)、`PROBE_TIMEOUT=0.05` で時間切れが `rc=124` と報告され、残骸なし
- 変異 (scratchpad にコピーして当てた。全部 red): 木でたどらない / グループの全員を集めない / 猶予の後の KILL を外す /
  グループ宛ての送信を外す / Setpgid を外す / INT を Notify しない / 死に直しを外す / `-f` を無視する / 時間切れの rc を変える /
  ps が無いとき root を集めない / HUP を常に Notify する / root を最初から集合に入れる (red team 2 周目の退行。確率的: 2/113・4/115) /
  `runtimeout_resolve` が別のバイナリを信用する / `resolve_claude` が照合を迂回する
  - 緑のまま (テストで固定しない): TERM で凍結を先にする順序。差が出る窓が数 ms で外から作れない (mutate-verify の stop_tree と同じ扱い。main.go のコメント)
- 実測: runtimeout の起動の分は手元で中央値 1.69ms → 4.24ms (30 回)。`bench_zsh.sh` の startup はこの分を min-of-5 で差し引く

## 実装の敵対的レビュー (2026-10-07、read-only のサブエージェント、opus、3 周)

- 1 周目 P1 0 / P2 4 / P3 3。採用: `-f` で ps が使えないと止まらない (root を必ず集める) / nohup の HUP 無視を外していた
  (起動時に無視されていた HUP は Notify しない) / `bench_zsh.sh` を端末から実行すると `zsh -i` が SIGTTOU で止まる (`-f`) /
  `RUNTIMEOUT` を無条件に信用する (この checkout のバイナリと一致するときだけ使う) / `CLAUDE_BIN_VERSION_TIMEOUT=0` の意味の反転 /
  子の終了と上限が同時に来たときの 124。記録のみ: 止める途中に道具自身が KILL されると木が T のまま残る (README の制約)
- 2 周目 P1 1 / P3 4。P1 は 1 周目の修正の退行 (root を最初から集合に入れると凍らず、fork した子が逃げる)。集め終わった後に足す形に直し、
  fork し続ける子のテストで固定した。採用: TERM は Go が常に捕まえるので Ignored の判定は HUP だけ / `resolve_claude` が照合を迂回 /
  パスの表記揺れを `pwd -P` で揃える / 桁の大きすぎる上限値。記録のみ: 刈り取り済みの子の pid へ撃ちうる数 ms の窓 (README の制約。
  根治は刈り取りを止め終えるまで遅らせる設計変更で、pid の再利用が要るので見送った)
- 3 周目 P1 0 / P2 0 / P3 3。全部採用: `cd` に CDPATH / chpwd の出力が混ざる / bash 3.2 の ja ロケールで `[!0-9]` が全角数字を通す /
  テストの rc=2 が「用意できない」と区別できない。修正は判定を足さない小さなもので直接の実測で確かめたので、4 周目は回さない

## 残タスク

- なし (未確認: Ctrl-C を pty で送る実機の試行はしていない。`-f` の INT は端末から子へ直接届く前提で、mutate-verify の既存テストの範囲で確認)
