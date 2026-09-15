# `--io-timeout` で倒した acquire の goroutine が、失敗を報告した後に lock を置いていく

起票日: 2026-09-12
カテゴリ: bug / priority: **high**
対象: `src/lockman/util.go` の `withTimeout` / `main.go` の `cmdAcquire` と `dispatch` の defer `Cleanup`
出典: [issue 358](done/358-refactor-lockman-cleanup-selftoken-is-production-unreachable.md) の敵対レビュー 5 周目 (観点③「並行・中断」)
反証レビュー: 未実施。**出典は opus 1 体による実測 A-B**（下の表）。数値はそのまま転記している

## 問題

`cmdAcquire` は `timed()` = `withTimeout(l.timeout, fn)` で `Acquire` を包むが、
`util.go` が明記するとおり **期限超過した goroutine は回収されない**。
その直後に `dispatch` の `defer l.Cleanup(...)` が `serverNow()` + 3 dir の
readdir/remove を回し、**その間に見捨てられた goroutine が `tryPlace` を完走して
lock を置く**。

呼び出し側から見えるのは「失敗」だけで、実態は「lock を握っている」。

## 2026-09-16 追記: 見捨てられた goroutine が置いていくものが**もう 1 つ**増えた (366 より)

[366](done/366-bug-lockman-stale-takeover-sometimes-has-two-winners.md) で `tryTakeover` に
「引き継ぎの調停の目印」(`tmp/<gen>.takeover`) を足した。見捨てられた goroutine は
**lock だけでなくこの目印も置いていく**ので、この issue の射程が広がっている。

- 置いていかれた目印は、その世代の引き継ぎを**猶予いっぱい塞ぐ**。366 側は猶予を過ぎた
  目印を回収する機構を入れて上限を作ってあるが、**根治はこの issue の側** (見捨てられた
  goroutine が副作用を残さないようにする) でしかできない
- 同じ理由で **`with.go` の `signal.Notify` が `cmd.Start()` の後**であること
  ([363](363-bug-lockman-with-signal-handler-installed-too-late.md)) も射程が広がった。
  取得中の Ctrl-C は Go 既定の即死で defer が走らないため、目印が残る
- **この 2 つは「稀なクラッシュ」ではなく既定経路**である、というのが 366 の敵対的レビューの
  P1 だった。366 の回収機構はその指摘を受けて足したもので、362 / 363 が閉じれば
  回収機構は「取りこぼしの受け皿」に戻せる (外せるかは 366 の残存窓を見て判断する)

## 実測 (A-B)

`--io-timeout 1ms`、毎回新規 dir、「rc≠0 かつ lock が存在する」を漏れと数える。

| 腕 | 漏れ |
|---|---|
| 現行 | 6/150・14/150・15/150 = **35/450 (7.8%)** |
| `dispatch` の defer `Cleanup` を登録しない | **0/150 ×3 = 0/450** |

`go build` は両腕とも rc=0。

### 用量反応 (掃除が因果にあることの確定)

graveyard に 7 日超の残骸を仕込んで `Cleanup` を重くする:

| graveyard | 漏れ (40 試行) |
|---|---|
| 0 件 | 1/40 |
| 200 件 | **40/40** |
| 1000 件 | **40/40** |

200 件は共有ロック dir では普通の状態。

### production の呼ばれ方での再現 (`zshlib/_av1ify_lock.zsh` と同じ `--token-file` 形)

```
rc=1
stdout: []
stderr: [lockman: I/O が 1ms 以内に返らない (マウントが応答しない可能性): 判定不能]
token-file の中身: []
lock: 存在する -> {"token":"adb37...","ttl_ms":1800000,"label":"av1ify pid=93592",...}
check  rc=3
status rc=0 stdout=[held by koji@kojiM3MBP (... expires_in=1799s)]
```

`_av1ify_lock.zsh:185-199` は rc≠0 でトークンファイルを消して中止する。
→ **誰も解放できない lock が 30 分残る**。回復手段は `lockman break` だけ。

## 358 との関係 (増幅の勘定)

**起源は元からの穴**（`withTimeout` の設計）だが、issue 358 の 3・4 周目が入れた
「失敗したら打刻しない」ゲートは**これを増幅する**。失敗が持続する dir では
レート制限が効かず毎回フル sweep になり (358 の 4 周目が +43% と実測)、
defer が長くなるほど上の用量反応どおり漏れ率が上がる。
358 側の `cleanup.go` のコメントには、この issue 番号つきで勘定を書き直した
(commit `0e0d0ea5`)。

## 357 との違い

[issue 357](done/357-bug-lockman-with-bypasses-io-timeout.md) は「**包まれていない** I/O が
ある」話。本 issue は「**包んだ** I/O の goroutine が、報告した後に勝つ」話で、
357 を直しても消えない (357 の修正で `Cleanup` を `timed` に包むと defer は短くなるが、
goroutine が回収されない事実は変わらない)。

## 対応の候補 (未決)

- 見捨てた goroutine に「もう要らない」を伝え、`tryPlace` が成功しても即 `Release` する
- あるいは `withTimeout` の戻りで「不確定」を表明し、呼び出し側 (`_av1ify_lock.zsh` を含む)
  に `check` での確認を促す
- 🚨 **どちらも「掃除を速くする」では解決しない**。用量反応は機構の特定に使っただけで、
  窓は 0 にならない

## 357 の実装後の状況 (2026-09-15)

**357 は完了したが、この issue は予告どおり解消していない。** 357 は包みを全経路へ入れた
だけで、**見捨てた goroutine が後から lock を置く**という本 issue の主張はそのまま残る
(`withTimeout` は回収できない)。

357 で変わった点は 2 つあり、どちらも本 issue の**前提**に効く:

1. **`with` が `withTimeout` を通るようになった**。これまで `with` は包みの外だったので
   本 issue の経路に居なかったが、これからは居る
2. ただし **tick ごとの goroutine 蓄積には上限を置いた** — 最初の更新失敗で ticker を
   止めるので、1 回の `with` で見捨てる goroutine は**最大 1 本**
   ([issue 359](359-research-lockman-resource-leaks-perf-audit-2026-09-11.md) の項目 4 の
   trigger が発火したもの)

つまり「見捨てた 1 本が後から lock を置く」窓は `with` にも開いたが、**本数は増えない**。

## 残タスク

- [ ] 反証レビュー (この issue の主張を現コードと突き合わせて反証する)
- [ ] 対応方針の決定 — **357 の実装後のコードで判断すること** (`timeout.go` に包みが
      集約されたので、「見捨てた goroutine が書き込む前に無効化する」類の対処を
      入れるなら 1 箇所で済む)
- [ ] **漏れる副作用は lock だけではない (2026-09-16 追記)**。366 で足した引き継ぎの
      調停の目印 (`tmp/<gen>.takeover`) も置いていくので、A-B の「漏れ」の数え方を
      **「rc≠0 かつ lock が存在する」から「rc≠0 かつ (lock または目印が存在する)」へ
      広げる**こと。旧い数え方のままだと、対処を入れた後の A-B が目印の漏れを見ずに
      0/450 を出す (直っていない側が緑に見える)
- [ ] 目印の漏れを塞いだら、366 の回収機構 (`reclaimTakeoverClaim` / `takeoverClaimGrace`)
      を「取りこぼしの受け皿」へ格下げできるか再評価する。**外すかどうかは 366 の残存窓
      (`lock.go` の `tryTakeover` の 🚨) を見てから**決める — 回収機構は 363 (Ctrl-C) の
      経路も受けているので、362 だけ閉じても外せない
