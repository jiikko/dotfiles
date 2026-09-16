# 昇格 (`escalateGroupKill`) は 1 度失敗すると二度と走らない / SIGKILL の直前に「もう終わったか」を見ていない

起票日: 2026-09-16
カテゴリ: bug / priority: medium
対象: `src/lockman/with.go` の `escalateGroupKill` と `runWith` の `escalate.Do`
出典: [381](381-bug-lockman-with-renew-latch-stops-renewal-forever.md) の敵対的レビュー (観点③ 並行・中断)
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

## 残タスク

- [ ] 1 の方針を決める (Once を消費しない / 報告を厚くする / 現状維持 + コメント)
- [ ] 2 を直す (1 行) か、コメントを訂正する。直すなら検査手段を先に決める
- [ ] EPERM の実測を追試する (`setsid` した子への `kill(-pgid, TERM)`)

## 関連

- [356](done/356-bug-lockman-with-releases-lock-while-grandchildren-run.md) — 昇格そのものの出典
- [381](381-bug-lockman-with-renew-latch-stops-renewal-forever.md) — 出典
- [385](385-design-lockman-on-lost-kill-vs-keep-renewing.md) — 昇格**ポリシー**側の矛盾
