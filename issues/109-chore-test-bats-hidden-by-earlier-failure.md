# 無関係な 1 本の失敗が `test-bats` を丸ごと隠す (CI 未検証のまま緑に見える)

起票日: 2026-08-26
種別: chore
優先度: **P2** (2026-08-25 に 2 回発生し、修正が CI 未検証のまま数時間放置された)

## 確認できた事実

`Makefile:162`

```make
test-runtime-rest: test-syntax test-discovered-rest test-bats
```

`test-discovered-rest` が Error 1 で落ちると make はそこで中断し、**`test-bats` が 1 度も
実行されない**。CI のログにも bats の出力が 1 行も出ない。`Makefile:76` の `test-runtime`
(手元用) も同じ形。

## 何が起きたか (2026-08-25)

同じ日に 2 回、`tests/` 配下の 1 本が落ちて `test-bats` が走らなかった:

1. `tests/zshrc/tmux-session/test_resurrect_owner_fingerprint.sh` (platform 依存)
2. `tests/tmux/test_log_restore_hook.sh` (grep の `\t`)

どちらのときも `tests/codex_fanout.bats` の修正が既に push 済みだったが、**CI 上で 1 度も
実行されていなかった**。「CI が緑になったら確認する」と考えていると、赤の原因が別にある間は
永久に検証されない。実際、修正から CI での実行証拠が取れるまで数時間かかった。

## 下がるリスク

- 「push した修正が CI で検証されている」という前提が、**無関係な失敗によって静かに崩れる**
- 落ちたテストを直す人は自分の担当分しか見ないため、隠れた未実行に気づく契機がない

## 対応案 (どれを採るか着手時に判断)

1. **CI のジョブを分ける** — `test-bats` を `test-discovered-rest` と別 job にする。
   独立して結果が出るので隠れない。CI 時間は並列化で相殺できる可能性がある
2. **`make -k` 相当にする** — 最後まで走らせて失敗を集約する。ただし exit code の集約と
   ログの読みやすさを自前で作る必要がある
3. **現状維持 + 運用で補う** — 「CI が緑になった」ではなく「**自分のテスト名がログに出た**」を
   確認する規律にする (`verify-execution-not-just-exit-code.md` の実践)

3 は既にルールとして存在するが、**人間の注意力に依存する**ため 1 が構造的。

## trigger

CI のジョブ構成を次に触るとき。または同じ「隠れて未実行」が 3 回目に起きたとき。

## 関連

- `_claude/rules/verify-execution-not-just-exit-code.md` — 「exit code ではなく実行された証拠で判定する」
