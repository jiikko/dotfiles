# human: テスト / probe が残した tmux 孤児サーバ 6 本を消してよいか判断してほしい

起票日: 2026-09-06
期限: 2026-09-13
カテゴリ: human（共有リソースへの破壊的操作なので、承認なしにはやらない）
出典: /audit resource-leaks 2026-09-06

## 何を判断してほしいか

**下の 6 本を `tmux -L <name> kill-server` で消してよいか**（1 本ずつ / まとめて / 消さない）。

`_claude/rules/tmux-probe-requires-socket-isolation.md` が「破壊的な後片付けを依頼の外から
自発で足さない」と明記しているため、監査の側では**観測だけ**して手を出していません。

## 実測（2026-09-06、`ps` の read-only 観測のみ）

| socket | セッション | 経過 | 出どころ（推定） |
|---|---|---|---|
| `-L __readonly_review_test_82625` | `t` | **29 日** | read-only レビューのテスト |
| `-L rl5611` | `live` | 16 日 | ratelimit 系テスト |
| `-L rs5611` | `sp` | 16 日 | 同上 |
| `-S ./tmp/audit070/sock` | `probe` | 16 日 | issue 070 の probe |
| `-L dfms-38188` | `ms` | 2 日 | `tests/tmux/test_mark_seen.sh`（`SOCK="dfms-$$"`） |
| `-L t3-90539` | `zsh` | 1 日 | `-f _tmux.conf` を読ませる probe |

いずれも**隔離 socket**（`-L` / `-S` 明示）なので、消しても本番サーバには影響しません。

## 🚨 これは対象に含めないでください

```
58159  38-04:53:35  tmux new-session -d -s __tt_hold_1551
```

これは **default socket = 本番サーバ**のセッションです（`tmux_periodic_save.sh 58159` と
`tmux_server_watchdog.sh 58159` がぶら下がっており、`=frontend` / `=pj_energy_matching` /
`=ubiregi-server` の attach もこのサーバ）。監査の一次報告はこれを「孤児 7 本」に数えていましたが、
**判定を 1 つ誤ると実測 30 セッションが載る本番サーバに触れる**ので、明示的に除外しています。

## なぜ自動で消さないか

`scripts/tmux_reap_orphan_servers.sh` は「socket ファイルが消えたのにプロセスだけ生存」を対象にする
設計で、**socket が生きたまま放置されたテストサーバは意図的に対象外**（同スクリプト冒頭に明記。
「後片付けはテスト / probe 側の責務」）。今回の 6 本は全部こちら側なので、既存の掃除機構は
仕様どおり何もしません。

構造的な再発防止（テスト側の起動時掃除）は issue 305 に分けています。
**2026-09-09: 305 は ② Go テストの一時ディレクトリだけ解消**（`src/doctor/testtmp` の
起動時掃除）。**① tmux socket は継続**で、しかも**この issue の判断待ち**です — 自動回収を
先に入れると、下の既存 6 本を承認なしで消すことになるため、305 側で意図的に止めています。
**この issue は既存 6 本の
処遇だけ**です。

## 確認したら

この issue を `issues/done/` へ移動してください（既読はファイルの位置で表す）。

## 結果（2026-09-10）— ユーザー承認を得て 6 本すべて回収した

「孤児サーバを消していいよ」の承認を受けて実行。**本番（pid 58159）と、その時点で開いていた
scratch popup には触れていない**（実行後に `tmux ls` = 30 セッション、watchdog / periodic_save の
稼働も確認）。

### 🚨 issue の記述が 1 点ずれていた: socket は「既定の場所」に無い

最初 `tmux -L <name> ls` で確認したところ 4 本が
`error connecting to /private/tmp/tmux-501/<name> (No such file or directory)` を返し、
**「socket が消えてプロセスだけ生存」＝ `tmux_reap_orphan_servers.sh` の対象**だと一度誤診した。

実際は違った。**`TMUX_TMPDIR` が既定と違うだけで socket は生きていた**:

| socket | 実パス |
|---|---|
| `rl5611` | `/private/tmp/reapl.nkCnAU/tmux-501/rl5611` |
| `rs5611` | `/private/tmp/reaps.ZGmD3y/s p/tmux-501/rs5611`（**パスに空白**） |
| `dfms-38188` | `/private/var/folders/.../tmp.m55Isdav1G/tmux-501/dfms-38188` |
| `audit070` | `/Users/koji/dotfiles/tmp/audit070/sock`（**これだけ実体なし**） |

`lsof -p <pid> -a -U -F n` で実パスを取り、`-L` ではなく **`-S <実パス>`** で捉え直した。
`DRY_RUN=1 tmux_reap_orphan_servers.sh` が何も挙げなかったのはこのため
（あちらは `[ -S "$path" ]` で生存を見るので、生きている socket を正しく保護していた）。

### 実行手順（`tmux-probe-requires-socket-isolation.md` の規律どおり）

1. `ps` で 6 本の生存を確認（read-only）
2. **消す直前に**各 socket で `tmux -S <path> ls` を打ち、**本番のセッション名が出ないこと**を確認
   （検査から実行までの窓を最小にする。エントリ単位で plan → exec）
3. `tmux -S <path> kill-server` を 1 本ずつ（**bare `kill-server` は使わない**）
4. socket が実在しない 1 本（pid 30820）だけは、**消す直前に `ps -o command=` で
   コマンド行を照合してから** `kill -TERM`（pid 再利用を踏まないため）

### 結果

6 本とも消えた。`ps` に残る tmux は本番 1 本 + scratch popup のみ。

## 🚨 併せて見つかった: 死んだ socket **ファイル**が 535 個

`/private/tmp/tmux-501/` に **536 ファイル**あり、`lsof` でプロセスが開いているのは
**`default`（本番）の 1 個だけ**。残り **535 個は死んだ socket ファイル**（最古 2026-07-05）。

これは「孤児サーバ（プロセス）」とは別のクラスで、**プロセスを kill しないので危険度が低い**。
issue 305 ① が言う「溜まった証拠」がまさにこれなので、そちらへ実測を記録した。
**この issue の承認範囲はサーバ 6 本なので、ファイルの掃除は別途確認する。**
