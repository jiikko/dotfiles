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

## 2026-09-16 追記: 漏れる副作用は **3 種類** (着手前の read-only 調査で全数を洗った)

本文と 366 の追記は ① lock 本体 と ② 引き継ぎの調停の目印 を挙げているが、**③ がもう 1 つある**。
A-B の数え方 (下の残タスク) はこの 3 つを数えないと、直っていない側が緑に見える。

| # | 副作用 | 作る場所 | 放置されたときに何が起きるか | 誰が回収するか |
|---|---|---|---|---|
| ① | **lock 本体** | `tryPlace` の成功 | 誰も解放できない lock が TTL ぶん残る (既定 30 分)。回復手段は `lockman break` だけ | 誰も。TTL 切れ後に他者が引き継ぐまで |
| ② | **引き継ぎの調停の目印** `tmp/<gen>.takeover` | `placeTakeoverClaim` | その世代の引き継ぎを**猶予いっぱい塞ぐ** | 366 の `reclaimTakeoverClaim` が猶予後に回収 |
| ③ | **回収の目印** `tmp/<gen>.takeover.<mtime nanos>` | `reclaimTakeoverClaim` の O_EXCL | **②の回収そのものを塞ぐ** (下記) | `sweepDir(tmp, scratchRetention=1h)` |

### ③ の詳細 (コードで確認済み。2026-09-16)

`reclaimTakeoverClaim` は `mark` を **O_EXCL で取ってから** `refreshTakeoverClaim` で目印の打刻を
戻す。**成功パスでは `mark` を消さない**が、それは無害である — 成功すると claim の mtime が
更新されるので、以後の観測者は**別の名前**を計算し、古い `mark` は誰にも参照されないまま
`sweepDir(tmp, 1h)` が拾う。

**害になるのは「mark を作った後・`refreshTakeoverClaim` の前に見捨てられた」とき**:

1. claim の mtime は**古いまま**なので、後続の観測者は**同じ `mark` 名**を計算する
2. `O_EXCL` が EEXIST で落ちる → `return false, nil` (譲る) → **その世代の引き継ぎが止まる**
3. 人に見えるのは、`mark` 自身が猶予 (`takeoverClaimGrace`) を超えて初めて出る警告 1 行だけ
   (それ以前は**良性の競合と区別が付かないので黙る**設計。実測 2026-09-16 の commit を参照)

つまり ③ は「②の回収機構を塞ぐ」層で、②とは別に数える必要がある。
🚨 **③ を塞がないまま ①② だけ直すと、A-B は 0 件を出すが「引き継ぎが猶予ぶん止まる」は残る。**

### ④ 書きかけ (`tmp/<token>.json`) は数えなくてよい

`tryPlace` の `defer os.Remove(tmp)` が**見捨てられた goroutine の中でも走る** (goroutine が
完走すれば消える)。プロセスごと死んだ場合だけ残り、それは `sweepDir(tmp, 1h)` の担当。

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
- [ ] **漏れる副作用は lock だけではない (2026-09-16 追記。上の表が全数)**。A-B の「漏れ」の
      数え方を **「rc≠0 かつ lock が存在する」から「rc≠0 かつ (① lock ② `tmp/*.takeover`
      ③ `tmp/*.takeover.<nanos>` のどれかが存在する)」へ広げる**こと。旧い数え方のままだと、
      対処を入れた後の A-B が目印の漏れを見ずに 0/450 を出す (直っていない側が緑に見える)。
      🚨 **③ は本 issue の本文にも 366 の追記にも無かった** (着手前の調査で見つけた)。
      ③ は「②の回収機構を塞ぐ」層なので、①② を直しても「引き継ぎが猶予ぶん止まる」は残る
- [ ] 目印の漏れを塞いだら、366 の回収機構 (`reclaimTakeoverClaim` / `takeoverClaimGrace`)
      を「取りこぼしの受け皿」へ格下げできるか再評価する。**外すかどうかは 366 の残存窓
      (`lock.go` の `tryTakeover` の 🚨) を見てから**決める — 回収機構は 363 (Ctrl-C) の
      経路も受けているので、362 だけ閉じても外せない

## 引き継ぎ (2026-09-16 時点。**実装は未着手**)

**状態: 着手前の read-only 調査だけ済んでいる。上の「漏れる副作用は 3 種類」がその成果。**
claim は外したので誰でも拾ってよい。

### いま拾う人にとっての前提の変化 (重要)

**380 と 383 が入ったことで、取り消しの道具が安全になった。**この issue の対処は
「見捨てた goroutine が置いた副作用を取り消す」形になるが、**380 以前にそれを書くのは危険だった**
(取り消しの `Release` が「他人の lock を消す」経路そのものだったため)。いまは:

| 取り消す対象 | 使える道具 | いつ安全になったか |
|---|---|---|
| ① lock 本体 | **`Release(token)`** — token + 期限 + **読んだ実体の identity** を照合する | 380 |
| ② claim / ③ mark | `os.SameFile` 照合つきの削除 (`cleanupOwn` と同じ形) | 383 の 2 周目 |

### 実装方針の案 (未検証。着手時に再評価すること)

1. `withTimeout` は**自分が見捨てたことを知っている**ので、それを `fn` へ伝える
   (`timeout.go` に包みが集約済み = 配線は 1 箇所で済む。357 の成果)
2. `Acquire` 側は、`tryPlace` が成功した**直後**に「もう要らないか」を見て、要らなければ
   即 `Release(meta.Token)` する。②③ も同様に自分が置いたものだけを消す
3. 🚨 **取り消し自体が同じ詰まったマウントへの I/O** なので、**窓は 0 にならない**。
   best-effort + TTL 頼みになる。「閉じた」と書かないこと (この issue の文脈で最も誤りやすい主張)

### 着手する人への注意 (この一連の作業で踏んだもの)

- 🚨 **A-B の数え方を ①②③ に広げてから測る**。旧い数え方 (lock だけ) のままだと、
  対処を入れた後の A-B が 0 件を出して**直っていない側が緑に見える**
- 🚨 **再試行との干渉をテストで固定する**。見捨てられた goroutine が後から走るあいだに、
  同じプロセスが再取得して別 token を持っている可能性がある。`Release(T1)` は token を
  照合するので安全側に倒れるが、**固定しないと次の人が壊す**
- 🚨 **seam を「guard が効く側」に置かない**。380 の 1 周目で、seam が guard の後ろにあったため
  「残っている窓を構造的に観測できない」テストを書いた (11 個目の fixture の嘘)
- 🚨 変異は**ケース名ごとの PASS/FAIL** で読む。スイートの rc で読むと、別のテストが落ちているのを
  「守られている」と誤読する
- この領域は**敵対レビューが毎周 P1 を出す**。383 は 4 周、380 は 1 周かけた。
  §7 の「修正を入れた周は必ずもう 1 周」を見込んでスケジュールすること

### 参考になる既存実装

- `lock.go` の `tryTakeover` — 「観測した世代から決まる名前を O_EXCL で取って調停 →
  破壊的操作の直前に再照合」(366)。ただし `Renew` のように**何度も呼ばれる**経路には
  コストが違うので、そのまま持ち込めるかは未検討
- `lock.go` の `cleanupOwn` — 「自分が作ったものだけを、identity を照合して消す」(383 の 2 周目)
- `lock.go` の `readLockInfo` — 「照合の土台は読んだ実体そのものから取る」(380 の 1 周目)

