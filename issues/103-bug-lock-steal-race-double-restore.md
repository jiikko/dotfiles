# 103 bug: 取り残し lock の「奪う」経路が非アトミックで、復元が二重に走る

起票日: 2026-08-25 / 出典: issue 078 の統合に対する敵対的レビュー (実測つき)

## 症状

`~/.cache/tt-restore-run/lock` が **owner 死亡状態で残っている**とき、2 本の
`scripts/tmux_restore_runner.sh` が 1〜5ms ずれて起動すると **両方が lock 取得に成功し、
`restore.sh` が並行実行される**。観測ログには `restore-manual-begin` と `restore-end` が
2 行ずつ並ぶ。

この lock の存在理由そのもの (復元中フラグ `@tt-restore-in-progress` と pane 生成の競合。
2026-07-30 の 22/29 部分復元) が無効化される。

## 発火条件

1. 取り残しの lock がある (異常終了・SIGKILL・再起動跨ぎ)。
   restore の経路は `tt_lock_sweep_stale` を**呼ばない**設計なので、回収はこの奪取パスだけが担う
2. 2 本の runner が 1〜5ms 差で起動する (`C-t C-r` の連打 / auto-restore と手動の重なり)
3. 双方が `tt_lock_owner_alive` = false を観測 → 双方が `rm -rf` → 双方が `mkdir` に成功

## 実測 (2026-08-25)

| 条件 | 結果 |
|---|---|
| 関数レベル・B の遅れ 0.001s | 60/60 で二重取得 |
| 同 0.004s | 58/60 |
| 同 0.005s | 54/60 |
| 同 0s / 8ms 以上 | 0/60 (窓の外) |
| E2E (実 `tmux_restore_runner.sh`) | **40/40 で `restore.sh` が並行実行** |

**統合が持ち込んだ退行ではない**。`git show HEAD:scripts/tmux_restore_runner.sh` の旧実装でも
同条件で 20/20 再現する。issue 078 で 3 本のコピーが 1 関数
(`scripts/lib/tmux_resurrect_guards.sh` の `tt_lock_acquire`) に集約されたので、
**直す場所が 1 箇所になった**という位置づけ。

## なぜ即修正しなかったか

レビューが提案した「`rm -rf` + `mkdir` を `mv` + `mkdir` に替える」だけでは窓が閉じない:

- A: mkdir 失敗 → owner 不在 → `mv dir dir.stale.A` → `mkdir dir` 成功 → A が保持
- B: A が owner を書く前に owner 判定 → 不在と観測 → **`mv` が A の新しい lock を掴む** →
  `mkdir dir` 成功 → B も保持

「owner の生存確認 → 奪う」が非アトミックである限り、確認と取得の間に窓が残る。
`mkdir` は原子的だが、**奪取は「消す + 作る」の 2 段**なのでそこが穴。閉じるには
acquire/release を対で設計し直す必要があり、単独の設計 + 敵対レビューを別途通すべきと判断した。

## 併せて直すもの (同じ設計変更の中で)

**release が owner 無条件**。`tmux_periodic_save.sh` / `tmux_server_watchdog.sh` /
`tmux_restore_runner.sh` の trap は `rm -rf "$LOCK_DIR"` で、**先に終わった側がまだ走っている側の
lock を消す**。この repo は既に正解を持っている: `scripts/tmux_resurrect_save.sh` の
`tt_save_release_lock_if_owner` が「判定〜削除の間に別プロセスが取得した新 lock を消すと
並行 save を招く」という理由で条件付き解放になっている。`tt_lock_acquire` を共通化した以上、
対になる `tt_lock_release_if_owner` が無いのは非対称。

## 着手時の注意

- **変異検証の置き場は既にある**: `tests/zshrc/tmux-session/test_resurrect_lock_acquire.sh`
  (rc=0/1/2 と掃除条件を pin 済み)。レースの再現は 1〜5ms の窓を作る必要があるので、
  関数レベルのハーネス (2 プロセスを遅延つきで起こす) を足すこと
- コード側の痕跡: `tt_lock_acquire` の奪取箇所にこの issue 番号つきでコメントを残してある

## 反証できなかった観点 (レビューが「壊せなかった」と明記したもの)

- **統合の前後で挙動が変わる経路**: 旧版と新版を同一 fixture で 31 ケース実行し、
  rc / stdout+stderr / lock の残存と pid の中身 / 観測ログが**全ケース一致**
  (restore_runner 10 / periodic_save 14 / watchdog 7)。戻り値分岐化した restore_runner も
  ログ文言まで一致
- `tt_lock_sweep_stale` の glob が `$base` の外を消す経路 (空白入りディレクトリ含めクォート済み)
- `.lock` シンボリックリンク経由の破壊 (`rm -rf` はリンク自体しか消さない。`/etc` へのリンクで実測)
- `basename` が pid でない値を返す経路 (`0.lock` / `-1.lock` は `SERVER_PID` の数値制限で到達不能)
- shell 差 (guards.sh を source する 13 本はすべて bash。`#!/bin/sh` の
  `tmux_reap_orphan_servers.sh` は source していない)

## 未確認リスク

- `periodic_save` / `watchdog` の `<pid>.lock` での同レース。lock 名が生きているサーバ pid なので
  「同一サーバ pid の owner 無し lock が残っている」状況を自然発生させられず未確認。
  関数は共通なので理屈上は同じ窓があるはず
- `tt_same_proc` の fail-open (`ps -o lstart=` が生存 pid に空を返す環境) は macOS/Linux で再現不能
