# human: lockman の --io-timeout が実 SMB のサーバ不達で効くか確かめる

- 起票: 2026-09-15
- カテゴリ: human（人間しかできない作業。実 SMB 共有とサーバの停止操作が要る）
- 期限: 2026-10-15
- 出典: [issue 357](357-bug-lockman-with-bypasses-io-timeout.md) の残タスク

## なぜ人がやるのか

357 で `--io-timeout` の包みを全経路へ入れ、FIFO ハーネスで「その位置で詰まっても期限内に
戻る」ことを固定した。だが **FIFO は本番に置かれない**。証明できているのは機構であって、
**smbfs のサーバ不達が実際にどこでブロックするか**は別に実測が要る
（`_claude/rules/verify-execution-not-just-exit-code.md`「隔離環境での失敗も本番の失敗ではない」）。

包みが当たっている位置は `readLock` / `serverNow` / `OpenFile` / `ReadDir` / `Rename` で、
smbfs がそのどれで止まるか（あるいは ETIMEDOUT で即エラーになるか）で、
`--io-timeout` が効くのか「そもそも要らなかった」のかが変わる。

## 手順

1. SMB 共有をマウントし、その配下に作業ディレクトリ `$D` を作る
2. `lockman with $D --io-timeout 10s --ttl 60s -- sleep 300` を走らせる
3. 走行中に**サーバ側を落とす**（またはネットワークを切る）
4. 次を記録する:
   - `with` が 10 秒前後で戻るか、戻らないか
   - 戻るなら exit code（期待: **125** = 判定不能）
   - stderr に `I/O timeout: I/O が 10s 以内に返らない` が出るか
   - 子（`sleep 300`）が残っていないか
5. 同じ条件で `lockman check $D --io-timeout 10s` も測る（期待: **rc=3**。「空いている」= 0 に
   倒れないことが 091:496 の本体）
6. サーバを戻した後、`.lockman/lock` が残っているか（残っていれば TTL 切れまで他マシンは待つ）

## 記録してほしいこと

- 上の 6 点の実測値（rc / 所要時間 / stderr / 残骸）
- 詰まった位置が分かるなら（`sample <pid>` のスタック）。どの syscall で止まったか
- **もし即エラー（ETIMEDOUT 等）で返ったなら、それも成果**。「smbfs は無限にブロックしない」が
  確定すれば、357 の前提（README の「対象環境」）自体を見直せる

## 関連

- [issue 357](357-bug-lockman-with-bypasses-io-timeout.md) — 包みを入れた実装と FIFO ハーネス
- [issue 091](done/091-feat-lockman-directory-lease-lock.md) — 仕様の正本（:418 / :496）
