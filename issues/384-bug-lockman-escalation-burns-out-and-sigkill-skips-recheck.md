# 昇格 (`escalateGroupKill`) は 1 度失敗すると二度と走らない / SIGKILL の直前に「もう終わったか」を見ていない

起票日: 2026-09-16
カテゴリ: bug / priority: medium
対象: `src/lockman/with.go` の `escalateGroupKill` と `runWith` の `escalate.Do`
出典: [381](done/381-bug-lockman-with-renew-latch-stops-renewal-forever.md) の敵対的レビュー (観点③ 並行・中断)
反証レビュー: 未実施

どちらも 381 の修正が作ったものではない (`escalateGroupKill` は 381 で触っていない)。
381 のレビューで見つかったので切り出す。

## 1. TERM が失敗すると昇格機構がそのプロセスの残り全部で焼き切れる

```go
if err := killGroup(pgid, syscall.SIGTERM); err != nil {
    warnf("%v", err)
    return          // ← ここで返ると、escalate.Do は消費済みなので二度と昇格しない
}
```

`escalate.Do` は `sync.Once` なので、**以後どんな確実な喪失 (`errNotOwner`) を知っても
昇格は二度と走らず、SIGKILL 段にも到達しない**。出るのは warn 1 行だけで、他者が引き継いだ後も
子が走り続ける = 昇格が防ぐはずだった二重実行そのもの。

発火条件 (レビュワーが実測): **子が `setsid()` すると、元の pgid への `kill(-pgid, SIGTERM)` は
EPERM を返し、子は無傷で生き残る**。ESRCH ではないので「もう居ない」でもない。
`with.go` は「setsid した子孫には届かない」を**到達範囲**の限界として既に記録しているが、
「届かないどころか機構ごと焼き切れる」副作用は書かれていない。

🚨 **本セッションでは追試していない** (EPERM の実測はレビュワーのコピー環境)。

直す前に決めること: EPERM なら SIGKILL も EPERM になるので、再試行しても届かない。
「Once を消費しない」ことに実効的な意味があるのは、**別の理由で TERM が失敗した場合**
(一時的な EAGAIN 等) だけかもしれない。まず「届かないことを報告する」を厚くする方が効くかも。

## 2. SIGKILL の直前に `exited` を見直していない

```go
select {
case <-exited:
    return // 猶予の内に終わった
case <-time.After(grace):
}
warnf(...)
_ = killGroup(pgid, syscall.SIGKILL)   // ← ここで exited を見ていない
```

両方 ready のとき Go は一様ランダムに選ぶ。レビュワーの実測で **100202/200000 = 50.1%** が
timer 側。timer 枝を取った後、SIGKILL の直前に `exited` を見直していない (あいだに warnf の
stderr write がある)。窓はプロセス終了で閉じない — `<-done` の後も deferred `ReleaseTimed` が
最大 `--io-timeout` (既定 10s) 走るので、昇格 goroutine には到達機会がある。

**被害は薄い**: 回収済みで空になったグループへの `kill(-pgid, SIGKILL)` は ESRCH。
単なる pid 再利用では当たらず、**再利用した pid がそのまま pgid の group leader** である
必要がある。実害は**未確認リスク**。

ただし `escalateGroupKill` の冒頭のコメントは「敵対レビューが『close(exited) を先にしたので
塞いだ』は SIGKILL 側だけだと指摘した」と書いており、**SIGKILL 側が塞がっていると読める**。
直さないならコメントの訂正が要る。

修正は 1 行 (SIGKILL の直前に関数先頭と同じ `select { case <-exited: return; default: }`)。
🚨 **テストで固定するのが難しい** (50% のランダム選択なので、変異を当てても緑になりうる)。
seam を入れて「timer 枝を取った後に exited が閉じている」状態を作れるかを先に検討する。

## 進捗 (2026-09-16)

### 1. TERM が失敗すると昇格が焼き切れる → **Once は消費したままにし、報告を厚くした**

`kill(2)` の失敗は **EPERM / ESRCH / EINVAL しか無く、一過性のものが無い**
(`EAGAIN` も `EINTR` も返らない)。EPERM (子が `setsid()` した等) なら SIGKILL も EPERM、
ESRCH なら撃つ相手が居ない。**同じ pgid への再試行に実効的な意味は無い**ので、
issue 本文の見立てどおり「届かないことを報告する」側を厚くした:

```
昇格できない (%v)。**子は走り続けている可能性がある**: lease は既に他者が持っているので、
二重実行になっていないか確認すること (pgid=%d)
```

旧版は `warnf("%v", err)` だけで、**何が起きているか (二重実行の疑い) が出ていなかった**。

### 2. SIGKILL の直前に `exited` を見直していない → **直した (guard 1 つ)**

`escalateBeforeKillHook` という seam を足し、「timer 枝を取った後に子が終わった」状態を
テストから決定論で作れるようにした (本番では 50% のランダム選択に依存していて作れない)。
関数冒頭のコメントも直した (旧版は「SIGKILL 側は塞がっている」と読めたが塞がっていなかった)。

## 結果

テスト 3 本を新設 (`escalate_recheck_test.go`)。変異検証:

| 変異 | 結果 |
|---|---|
| SIGKILL 直前の見直しを外す | `TestEscalateRechecksExitedBeforeSigkill` red |
| TERM 失敗時の報告を `warnf("%v", err)` へ戻す | `TestEscalateReportsWhenSignalCannotReach` red |

🚨 **fixture が 2 回とも嘘をついた** (どちらも「有無で結果が変わらない観測」):

1. 生死を `kill(pid, 0)` で見ていた → SIGKILL された子は wait するまで**ゾンビとして残り**、
   `kill(pid, 0)` は成功し続ける。判定を `cmd.Wait()` の完了へ変えた
2. `trap "" TERM; sleep 30` は**グループごと TERM で死ぬ** (trap を張った sh は生き延びるが、
   同じグループの `sleep` が TERM で死に、sh の `sleep` が返って連鎖終了する)。
   `while :; do sleep 0.2; done` にした
3. さらに `kill(pid, 0)` で「起動した」と判定していたため、**trap を張り終える前に TERM が届いて**
   いた (SIGKILL 側の判定に一度も到達していなかった)。子が ready ファイルを書くのを待つ形にした

`go test -race ./...` 緑 / `make test` は EXIT=0 / 83 件報告 / 失敗 0。

## 残タスク

- [x] 1 の方針を決めた (Once は消費したまま + 報告を厚くする。理由は `kill(2)` に一過性の失敗が無いこと)
- [x] 2 を直した (seam 付きで検査手段も用意した)
- [ ] **未実施**: EPERM の実測の追試 (`setsid` した子への `kill(-pgid, TERM)`)。
      今回のテストは `killGroup` 自身が弾く pgid=1 で「届かない形」を作っており、
      **報告の中身は固定できているが EPERM そのものは再現していない**
- [ ] **未実施**: 反証レビュー (本 issue の実装に対する敵対的レビュー)

## 関連

- [356](done/356-bug-lockman-with-releases-lock-while-grandchildren-run.md) — 昇格そのものの出典
- [381](done/381-bug-lockman-with-renew-latch-stops-renewal-forever.md) — 出典
- [385](385-design-lockman-on-lost-kill-vs-keep-renewing.md) — 昇格**ポリシー**側の矛盾
