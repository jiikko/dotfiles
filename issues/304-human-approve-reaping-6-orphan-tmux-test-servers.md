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

構造的な再発防止（テスト側の起動時掃除）は issue 305 に分けています。**この issue は既存 6 本の
処遇だけ**です。

## 確認したら

この issue を `issues/done/` へ移動してください（既読はファイルの位置で表す）。
