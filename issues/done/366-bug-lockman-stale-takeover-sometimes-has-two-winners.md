# `tryTakeover` の TOCTOU で引き継ぎの勝者が 2 人になる (原因特定・修正済み)

起票日: 2026-09-12
カテゴリ: bug / priority: **high**（**実害を確定**。窓を広げると 10/10 で二重取得が再現した）
対象: `src/lockman/lock.go` の `tryTakeover` / `src/lockman/lock_test.go:88` の同テスト
出典: [issue 358](358-refactor-lockman-cleanup-selftoken-is-production-unreachable.md) の敵対レビュー 5 周目 (観点②) と、7 周目の作業中に再観測
反証レビュー: 敵対的レビュー実施 (2026-09-15。下の「敵対的レビュー」節)

## 問題

**「期限切れの引き継ぎを同時に狙っても、引き取れるのは 1 人だけ」**という
lockman の中核の不変条件を測るテストが、低頻度で `引き継ぎの勝者が 2 人 (期待 1)` で落ちる。
テスト自身のコメントが「ここが二重取得の最頻出経路」と書いている面。

**これがテストのハーネス由来なのか production の race なのかは未特定**。
主張の重さから priority: high としているが、**実害が確定したわけではない**。

## 実測 (2026-09-12 / darwin arm64 / go1.25.4)

単独実行 (`-run 'TestStaleTakeoverHasExactlyOneWinner$'`):

| 条件 | 失敗 |
|---|---|
| HEAD (`2c474a4d`)、`-count=100`、**他のエージェントが並行実行中** | **5/100** |
| HEAD、`-count=300`、静穏 | **0/300** |
| 5 周目より前 (`7ca2533b`)、`-count=100`、静穏 | 0/100 |
| 5 周目より前、`-count=300`、静穏 | **1/300** |
| HEAD、**`-race`** `-count=100` | 0/100 |
| full suite `-count=1` ×8 (静穏) | 0/8 |

観測された値はすべて「勝者が 2 人」(3 人以上は出ていない)。

**独立の観測**: issue 358 の 5 周目 (観点②) のレビュワーが、full suite 約 50 回のうち
2 回の原因不明 FAIL を観測し、うち 1 件がこのテストだった (並列実行中)。

## 分かっていること

- **issue 358 の掃除機構の変更が原因ではない**。`Acquire` は `Cleanup` を呼ばない
  (`grep -n 'Cleanup(' lock.go` が 0 件) ので、このテスト中に 358 で触ったコードは走らない。
  **両腕 (HEAD / 5 周目より前) の双方で再現した**
- **負荷に感応する**。並行して他の重いプロセスが走っているときに率が上がる
  (5/100 が出たのはその条件。静穏では 400 回中 1 回)
- **`-race` では出ない** (400 回相当で 0)。検出器が遅くする方向に効いて窓が閉じる

## 未特定 (次に見るべきところ) — **2026-09-15 に全部決着。下の「結論」が正本**

> 🚨 以下は起票時点の推測で、**3 点とも外れていた** (下の「却下した仮説」を見ること)。
> 次の監査がここから同じ推論を再生成しないよう、消さずに残して結論へ誘導する。


- fixture の `ttl := 50 * time.Millisecond` + `time.Sleep(3 * ttl)` が、
  `serverNow()` の mtime 粒度 (サーバ側の打刻) に対して十分かどうか。
  粒度が 1 秒の FS では「期限切れ」の判定自体が揺れる
- `tryTakeover` の rename 引き継ぎで、2 つの goroutine が**別々の**期限切れ lock を
  見て両方成功する経路があるか (1 人目が引き継いだ直後の新 lock を、2 人目が
  まだ古い state で見ている窓)
- ハーネス由来なら、判定軸を壁時計から外す
  ([`avoid-wall-clock-assertions.md`](../../_claude/rules/avoid-wall-clock-assertions.md))。
  [364](../364-bug-lockman-with-release-failure-and-graveyard-retention.md) の 4 番
  (`TestRenewExtendsHold` の壁時計依存) と同じ族の可能性がある

## 再現手順

```sh
cd src/lockman
# 静穏だと 300〜400 回に 1 回。負荷をかけると率が上がる
go test -run 'TestStaleTakeoverHasExactlyOneWinner$' -count=300 ./...
```

## 結論 (2026-09-15): production の race。ハーネス由来ではない

`tryTakeover` の **「期限切れと判定する」から「rename で退ける」までが TOCTOU** だった。
rename は名前に対する操作なので、判定してから rename するまでに名前の指す先が入れ替わる。

壊れる順序 (2 者):

1. B が lock (世代 T0) を読み、期限切れと判定する
2. A が引き継ぎを完走する — T0 を graveyard へ退け、**自分の新しい lock T_A を置く**
3. B が古い判定のまま `rename(lock, graveyard/...)` を打つ → **T_A が退けられる**
4. B の `tryPlace` が空いた lock に成功する → **A と B の 2 人が勝つ**

`tryTakeover` の「rename なら勝者は 1 人に絞られる」というコメントは**偽**だった
(原子なのは操作であって、「判定したあの lock を動かす」ことではない)。

### 実測 (2026-09-15 / darwin arm64 / go1.25.4)

| 条件 | 結果 |
|---|---|
| 素の `-count=200` (静穏) | 1 件 FAIL (再現はするが統計的) |
| **判定と rename のあいだに `time.Sleep(60ms)` を挿入** | **10/10 FAIL。勝者 2〜5 人** |
| 同上で graveyard の中身を読む | **勝者の新しい lock が 2 件入っていた** (残り 1 件が死んだ lock) |

graveyard の直接証拠 (1 実行ぶん):

```
死んだ lock の token = c820f076...  / 勝者 3 人
graveyard: token=5000ac23... label=takeover -> 勝者の新しい lock (不当な退去)
graveyard: token=222f72dc... label=takeover -> 勝者の新しい lock (不当な退去)
graveyard: token=c820f076... label=dead     -> 死んだ lock (正当な退去)
```

### 却下した仮説 (次の監査が再生成しないように残す)

- **❌ ハーネス由来 / 壁時計依存**: 窓を広げると 10/10 で決定論的に再現し、graveyard に
  勝者の lock が入る。fixture の `ttl` や `time.Sleep` の長さとは無関係。
  **[364](../364-bug-lockman-with-release-failure-and-graveyard-retention.md) の 4 番
  (`TestRenewExtendsHold` の壁時計依存) とは別族**なので、あちらの結論を流用しない
- **❌ mtime 粒度 (serverNow の打刻)**: 粒度が粗いなら「期限切れの判定自体が揺れる」形に
  なるはずだが、実際は判定は正しく、判定**後**の rename が別の対象を掴んでいた
- **❌ issue 358 の掃除機構**: 元の issue の記述どおり無関係 (両腕で再現)

## 修正

`tryTakeover` を 2 段にした。どちらも「判定できないなら退けない」へ倒す
(取り逃した引き継ぎは次の acquire で済むが、誤った引き継ぎは二重実行になる)。

1. **調停**: 観測した世代 (lock の token) から決まる名前 `tmp/<gen>.takeover` を
   **O_EXCL** で取る。同じ世代を見た者は同じ名前を狙うので、退ける役が 1 人に絞られる。
   置き場が `tmp/` なのは掃除の保持期間が短いほう (`scratchRetention` = 1h) だから —
   目印を取った直後に落ちたプロセスがあると、その世代は掃除まで引き継げなくなる
2. **直前の再照合**: 破壊的操作の直前に lock を読み直し、**同じ世代・同じ mtime** かを
   確かめる。1 だけでは、目印が掃除された後に現れる遅延観測者と、`Break` / `Release` の
   割り込みが残る

3. **回収**: 目印の作成者が死んだ場合 (`--io-timeout` の発火と Ctrl-C はどちらも defer を
   飛ばす**既定経路**) に備え、猶予を過ぎた目印を回収する。猶予は目印に書かれた作成者の
   `--io-timeout` 申告から決める (ms のまま `maxIOTimeout` で頭打ち → 最大 15m)。
   回収の調停は「**観測した目印の打刻から決まる名前** `<claim>.<nanos>` を O_EXCL で取る」形。
   **目印そのものは消さない・動かさない** — 消す / 動かす版は同じ TOCTOU を 1 段下で
   作り直しており、8 本同時で役が 5 人になった (実測)

補助: 世代 id に mtime を混ぜない (延長で動くと調停をすり抜ける)。token は他ホストが
書いた値なのでパスに使う前に小文字 hex へ検疫する (`isHexToken`)。譲る / 回収するときは
理由を stderr に出す (`check` は期限切れを free と答えるので、黙ると追跡不能の矛盾になる)。

`os.Link` + `SameFile` 案は採らなかった: `tryPlace` が smbfs で `os.Link` が ENOTSUP に
なりうる前提で O_EXCL へ落としている以上、link 必須の調停は**本番でだけ無音で消える**。
O_EXCL の作成は link が通らない環境でも成立する。

### 残した窓 (直さないと決めた)

再照合から rename までのあいだに、保持者の `Release` と別者の `acquire` が両方通ると、
置かれたばかりの lock を退けうる。`Release` は期限切れでは通らないので、成立には
「保持者だけが期限内と判定する」時計の際どさが要る (syscall 2 つぶんの窓)。0 にするには
rename ではなく「inode を指定した削除」が要り、POSIX にその原始操作が無い。
再開の trigger: 実 lock を使った並行実験でこの経路を再現できたとき。

## 検証

回帰テストは **seam (`takeoverObservedHook`) で順序を決定論にして**書いた。素の競争は
`-count=200` に 1 回しか落ちないので、統計テストは「負荷次第で緑になる assert」になる
([`avoid-wall-clock-assertions.md`](../../_claude/rules/avoid-wall-clock-assertions.md))。
assert は勝者の数だけでなく **graveyard に入ってよいのは死んだ lock だけ**まで見る。

2 段構えなので、**段ごとに単独で red になる**ことを確認した
([`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) §1.5)。
変異はすべて「ビルドできたこと」を別に確認し、判定は rc ではなくテストごとの PASS/FAIL で行った。

| 変異 | red になったテスト |
|---|---|
| 1 段目 (調停) を外す | `TestConcurrentTakeoverElectsExactlyOneEvictor` ほか 5 件 |
| 2 段目 (再照合) を外す | `TestStaleTakeoverRefusesRenewedLock` / `...ClaimSwept` |
| 2 段目のうち mtime 照合だけ外す | `TestStaleTakeoverRefusesRenewedLock` |
| **両段を外す (修正前の実装へ戻す)** | **11 件** |
| 成功時に目印を残さない | 3 件 |
| token の検疫を外す / ブラックリスト化 | `TestTakeoverClaimStaysInsideTmpForHostileToken` / `TestTakeoverGenerationAndTokenQuarantine` |
| 回収の猶予を常に満たす / 常に無視 | 3 件 / 4 件 |
| 回収の調停 (mark の名前) を外す | `TestConcurrentReclaimElectsExactlyOneEvictor` ほか |
| 回収後に打刻を戻さない / クライアント打刻にする | `TestTakeoverReclaimsAbandonedClaim` ほか |
| 回収失敗で mark を残す | `TestReclaimLeavesNoMarkWhenRefreshFails` |
| 猶予の fallback を 0 にする / 倍率を 720 倍にする | `TestTakeoverClaimGraceBounds` ほか |
| 申告された猶予を無視する / **先に変換して wrap させる** | `TestTakeoverClaimGraceBounds` ほか |
| refresh の mtime 照合を外す | `TestRefreshTakeoverClaimYieldsWhenClaimWasReplaced` |
| 譲る理由 / 回収中の理由の warnf を消す | `TestTakeoverYieldsToLiveClaim` / `TestTakeoverYieldsAndWarnsWhenReclaimInProgress` |
| 回収の seam を外す | `TestConcurrentReclaimElectsExactlyOneEvictor` |
| 退けない経路で目印を外さない | `TestStaleTakeoverRefusesRenewedLock` |

**全 22 変異が red**。ビルド不能だった 2 本は「red でも green でもない第 3 の結果」として
扱い、当て直した。

🚨 **変異が green に転じたことで、テスト関数を 1 本消していたのが発覚した**
(`TestTakeoverClaimGraceBounds` を書き換えるとき、その後ろに追記していた
`TestReclaimLeavesNoMarkWhenRefreshFails` とヘルパーを巻き込んだ)。HEAD から復元した。
**変異検証は「テストが効くか」だけでなく「テストが在るか」の canary にもなる。**

🚨 **既存の `TestStaleTakeoverHasExactlyOneWinner` は「両段を外す」変異でも緑だった**
(200 回に 1 回しか落ちないので、1 回の実行では捕まらない)。この issue が測っていた
テストは、退行検出力がほぼ無い。新しい 5 本が実質の守りで、既存の 1 本は smoke test。

## 横展開で見つかった別件 (この issue では直さない)

同じ「照合してから名前に対して破壊的操作」の形が 2 箇所ある。同じ seam の手口で
**両方とも再現した** (2026-09-15):

- `Renew`: 照合後の `O_TRUNC` 書き込みが、引き継がれた後の**別人の lock を自分のメタで
  上書き**した。これは `Renew` のコメントが書いていた「再開の trigger: 実 lock を使った
  並行実験でこの上書きを再現できたとき」の**発火**にあたる
- `Release`: 照合後の `os.Remove` が、引き継がれた後の**別人の lock を消した**

→ issue 380 へ起票。

## 敵対的レビュー (6 周。全数勘定)

観点を分けて直列で回した (`adversarial-review-own-safeguards.md` §5 / §7)。
**レビュワーの出力も無検閲では採らない** — 3 周とも「読解による導出」を含んでいたので、
採否を決める前に変異を当てて実測した。

| 周 | 観点 | 採用 | 却下 / 誤り | 別 issue へ振り分け |
|---|---|---|---|---|
| 1 | ①壊す | 5 (実装 1 + コメント 4) | 0 | 0 |
| 2 | ②素通り | 8 (実装 3 + テスト 5) | 1 | 0 |
| 3 | ③並行・中断 | 5 (実装 3 + コメント 2) | 0 | 3 |
| 4 | 3 周目の差分 (3 観点) | 6 (実装 5 + テスト 1) | 0 | 0 |
| 5 | 4 周目の差分 (3 観点) | 5 (実装 2 + テスト 3) | 0 | 0 |
| 6 | 二重取得のみ | 1 (コメントの反証) | 0 | 0 |

### 1 周目 (①壊す) — 採用 5

**P1: 回収機構が無いと、目印を取った直後に死んだプロセスがその世代を掃除まで塞ぐ。**
これは**私がこの修正で持ち込んだ可用性の退行**だった。決定的だったのは「死ぬ経路が
既定の利用で踏める」という点:

- `withTimeout` は**固まった goroutine を回収せずに返る** (`timeout.go` のコメント自身が
  「プロセスの終了で解放される前提の使い捨て」と明記)。`--io-timeout` の発火は
  応答しないマウントで普通に起きる = lockman が存在する理由そのもの
- `with.go` の `signal.Notify` は `cmd.Start()` の後なので、**取得中の Ctrl-C は Go 既定の即死**

塞ぐ長さが `scratchRetention` = 1h > 既定 TTL 30m となり、「TTL を過ぎれば誰かが引き継げる」
という道具の契約を割っていた。→ 猶予つきの回収機構を足した。

**P2 / P3 (コメント 4 件)**: 残存窓の受容理由が `Break` を数え落としていた
(`Break` は期限検査も token 照合もしない無条件 rename なので**時計の際どさを一切必要としない**) /
窓の幅は「syscall 2 つ」とは限らない (rename 自身が固まれば stall の長さ) /
`notoken-` 経路の理由が違う (救われているのは「Renew が触らない」からではなく
「その窓の lock は常に fresh で expired を通らない」から) / 1 段目が
「同じ lock は誰が読んでも同じ parse 結果になる」という暗黙の仮定に乗っている。

### 2 周目 (②素通り) — 採用 8 / 却下 1

指摘された変異を**全件実測**した (レビュワーは「実行していない」と明記していた)。

| 指摘 | 実測 | 採否 |
|---|---|---|
| 猶予の fallback が無検査 | 全 green | 採用 |
| 退けない経路で目印を外すのが無検査 | 全 green | 採用 |
| `isHexToken` をブラックリストへ弱体化しても緑 | 全 green | 採用 |
| 猶予の倍率が上側で無検査 (720 倍でも緑) | 全 green | 採用 |
| 打刻をクライアント側 (`os.Chtimes`) にしても緑 | 全 green | 採用 |
| 診断の `warnf` が観測不能 | 全 green | 採用 |
| **世代 id に mtime を混ぜても緑** | **3 テストが red** | **却下 (誤り)** |

🚨 **却下の理由**: 世代 id に mtime を混ぜる変異は `TestTakeoverYieldsToLiveClaim` /
`TestTakeoverReclaimsAbandonedClaim` / `TestTakeoverGraceHonorsClaimDeclaredTimeout` を
red にする。テストヘルパーが目印のパスを token だけから組むため、production が
`token-<mtime>` を計算すると目印が見つからず落ちる。**この不変条件は既に守られていた**
(ただし偶然なので、明示の単体テストも足した)。

実装の穴も 3 件出た (どれもプロセスが落ちなくても踏む): `refreshTakeoverClaim` の失敗で
mark が残りその世代が掃除まで塞がる / `placeTakeoverClaim` の `Close` 失敗で目印が漏れる /
申告値の桁違いで猶予が負になる。

### 3 周目 (③並行・中断) — 採用 5 / 振り分け 3

**P3-1 (実測で確定): 溢れガードが手前の wrap で素通りされていた。** 2 周目で足した
「溢れたら無限の猶予へ倒す」ガードより手前で `time.Duration(c.TimeoutMS) * time.Millisecond`
自体が wrap しており、`TimeoutMS = MaxInt64` は **-1ms** になって猶予が既定の 30s
(= **最短 = 回収する側**) に落ちていた。**コメントが宣言していた向きと真逆**。
ms のまま `maxIOTimeout` で頭打ちにしてから変換する形へ直した。

**P2-2: 打刻を戻す `O_TRUNC` が無条件だった。** `reclaimTakeoverClaim` の doc が
「消したり動かしたりしてはいけない」と書いている当のことを、書き込みでやっていた。
開く前に観測した打刻と同じかを照合するようにした。

**P2-1 / P3-2**: 書き込み失敗の握り潰し / mark の EEXIST で譲る枝が無言。どちらも直した。

**別 issue へ振り分け (3 件)**: `with.go` の renew ラッチが恒久的に更新を止める →
[381](381-bug-lockman-with-renew-latch-stops-renewal-forever.md) (新規) /
`Renew` の上書きは自分の deferred Release が窓を開ける →
[380](../380-bug-lockman-renew-and-release-act-on-name-after-check.md) /
取得中の Ctrl-C が残す中間状態とシグナル転送の枝の非対称 →
[363](../363-bug-lockman-with-signal-handler-installed-too-late.md)。

### 4 周目 (3 周目の差分限定) — 採用 6

**3 周目で「Stat で照合してから開く」形にしたのが、4 件の問題を同時に作っていた。**
`refreshTakeoverClaim` を **fd 照合** (先に開いてから `f.Stat()`) へ作り替えて一括で閉じた:

- 2 つ目の Open の **ENOENT が未配線**になっていた。3 周目より前は open 1 つで
  `placeTakeoverClaim` へ配線されていたのが、Stat を前置した結果 Stat 側にだけ残った。
  「掃除が浚った」という正常系が `errBusy` でない hard error になり、
  **`acquire --wait` のリトライループにすら入らない**状態だった
- Stat→Open の照合は依然 TOCTOU で、`O_TRUNC` が照合前に中身を破壊していた
- 止まりうる syscall が 1 つ多く、中断で最悪の wedge へ倒れる帯が広がっていた

**良性の競合で `lockman break` を 7 回勧めていた** (実測)。`Break` は自分の doc が
「最も現実的に二重取得を作る操作」と書いているもの。mark が猶予より古いときだけ鳴らす形へ。

併せて専用 sentinel `errClaimReplaced` の新設 (`errBusy` 流用は譲る失敗の集合が将来黙って
広がる) / 到達不能になった分岐の削除 / 申告値の上限を専用定数へ分離 (UI 制約の流用をやめる)。

### 5 周目 (4 周目の差分限定) — 採用 5

**診断が「とうに死んだ作成者」の申告で閾値を作り、人へ無関係な pid を見せていた。**
mark を置いた回収者の飛行上限は回収者自身の `--io-timeout` なのに、目印の body で判定して
いたので「回収者はまだ飛行中なのに止まっていると診断」しうる。その助言で `break` を打つと
役が 2 人になる — **4 周目で取り除いた「break への誘導」を条件を変えて作り直していた**。

**4 周目の中心の修正 (fd 照合) が 1 本のテストにも守られていなかった。** パス照合へ戻す変異が
全テスト緑で通る (単一プロセスのテストには「照合と書き込みのあいだに置き直す第三者」が
居ないため)。seam を足して固定した。

🚨 **seam の位置を 2 回動かした**。「書き込みの直前」でも「照合の直後」でも変異は緑のまま
通る (破壊が seam より前に済む)。**「打刻を得た直後・照合より前」**まで出して初めて red。

他に `f.Truncate` が無検査 (fixture の新旧 body が同じ長さで no-op だった) /
`errClaimReplaced` の配線が無検査。

### 6 周目 (二重取得だけに限定) — **新しい生成器は見つからず**。コメントの反証 1 件

問いを「同時に lock を保持できるのは 1 人だけ、を破る順序はあるか」だけに絞った。
結果は **「新しい二重取得の生成器は見つけられなかった」**。

ただし **5 周目に私が書いた「その順序で一意性を担保するのは 2 段目の再照合と `tryPlace` の
O_EXCL」は偽**だと反証された。役が 2 人になった後は、2 人とも同じ (token, mtime) を観測して
いるので 2 段目は両方通り、先に退けた側が lock を置いた後、もう一方の rename が**その置いた
ばかりの lock** を退けて自分のを置ける — **366 の元の signature がそのまま出る**。
2 段目が守るのは古い state で来た観測者であって、正当に役を持つ 2 人目ではない。
**役の一意性を作っているのは 1 段目だけ**。コメントを反証どおりに直した (コード変更なし)。

## 停止条件 (なぜ 6 周で止めたか)

`adversarial-review-own-safeguards.md` §7 は「修正した周はもう 1 周」を求め、§8 は
「迂回を直すたびに新しい迂回が出るなら軸が違う」と言う。ここでの判断:

- **脅威モデル**: 協調するホスト・ユーザーどうしが誤って同時に走ることだけを止める
  (悪意ある同一ホストユーザーは射程外。`lock.go` の const ブロックが正本)
- **収束している**: 実装の欠陥は「機構が無い」(1 周目) → 「私の修正が作った退行」(3〜4 周目)
  → 「診断と検査の穴」(5 周目) → **「生成器は無い。コメントが過大」(6 周目)** と移っている。
  6 周目は排他機構そのものを破る順序を 1 つも出していない
- **意図的に閉じないもの** (どれもコードに明記):
  - 2 段目から `os.Rename` までの窓 (`Break` の無条件 rename / rename 自身の stall)。
    POSIX に「inode を指定した削除」が無い
  - mark を取ってから打刻を戻すまでに死ぬと掃除まで塞ぐ (可用性と正しさの trade-off)
  - 役が 2 人になる生成器 4 つのうち、猶予超過は設計上の trade-off、parse 非決定性は
    未確認リスク、見捨てられた goroutine は [362](../362-bug-lockman-abandoned-timeout-goroutine-leaves-lock.md)
  - `f.Write` のエラー伝播は実行時の I/O 失敗が要るためテスト不能 (**未検証**と明記)
- **次の監査に渡す検査可能な痕跡**: 「graveyard に、現に誰かが保持している token の lock が
  入っている」= 二重取得が起きた証拠

### 崩せなかったもの (次の監査が同じ指摘を再生成しないように残す)

3 周のレビュワーが攻めて**壊せなかった**と明記した経路。同じ重さで記録する:

- **元の攻撃 (判定〜rename を 60ms 広げて勝者 2〜5 人) は 1 段目だけで閉じている**。
  2 者は同じ (token, mtime) を観測する以上、同じ調停の名前を狙う
- **`Release` と takeover の競合は閉じている**。`serverNow` を取る順序が逆
  (`tryTakeover` は readLock の前、`Release` は後) なので、takeover 側の判定が保守側に
  倒れており、単調なかぎり Release は必ず `errNotOwner` に落ちる
- **目印が掃除で消えた後に現れる遅延観測者は 2 段目が確実に止める** (mtime と世代の両方を見る)
- **token 経由のパストラバーサルは閉じている**。2 つの id 体系 (hex / `notoken-`) は
  `isHexToken` が `[0-9a-f]` に絞るのでエイリアスしえない
- **3 つの `return true` 経路 (`mtime.IsZero` / rename ENOENT / `mtime2.IsZero`) から二重取得は
  作れない**。どれも直後に `tryPlace` へ行くだけで、勝者は 1 回の原子操作で 1 人に決まる
- **mark 名の衝突 (mtime 粒度が粗い FS) は起きない**。回収は「猶予 (最短 30s) を超えた」が
  前提なので、観測した mtime と refresh 後の新しい mtime が同一値になりえない
- **`sweepDir` がフラグ経由で飛行中の目印を浚うことはない** (`--io-timeout <= 5m` なので
  猶予は最大 15m < `scratchRetention` 1h)
- **`placeTakeoverClaim` の Close 失敗時の `os.Remove(claim)` で他人の目印は消せない**
  (到達時点で claim は自分が O_EXCL で作った直後)
- **mark の body がどう壊れても役の割り当てには影響しない** (mark の body の読者は warnf の
  分岐だけ。mark を回収・削除・rename する経路はコード上ゼロ)
- **`f.Truncate` の中間状態 (不正 JSON) で猶予が縮んでも踏み潰す相手が居ない**。mark 名が
  「読者自身が stat した mtime」から決まるので、その mtime を観測している者は必ず同じ mark を
  既に O_EXCL で握っており、縮んだ猶予の読者は EEXIST で譲る
- **seam の追加で production の順序は 1 mm も変わっていない** (no-op var。`f.Close()` の
  位置関係も全経路で確認済み。二重 close も leak も無い)
- **掃除が mark を浚って目印だけが残る形は不活性**。目印は mark の後に refresh されるので
  `claim.mtime > mark.mtime` = mark のほうが先に消えるが、後続は refresh 後の mtime から
  別の mark 名を計算する
- **`tryTakeover` の `defer os.Remove(claim)` は他者の目印を消さない** (譲る経路は defer 登録
  より前に return する)

### この修正が新設した wedge (trade-off の明示)

mark を取ってから打刻を戻すまでにプロセスが死ぬと、目印の打刻が観測値のまま凍り、
以後の観測者は全員同じ mark 名を計算して弾かれる。復帰は掃除だけなので **最悪 ~1h10m**、
既定 TTL 30m を超える。366 以前はこの状態が無かった (目印も mark も無いので死んだプロセスは
誰も塞がなかった)。

**「退ける役が 2 人」を「退ける役が 0 人、最長 ~1h10m」と交換している。** 排他の道具として
正しさを優先した判断で、人の脱出口は `lockman break` (warnf が案内する)。
根治は [362](../362-bug-lockman-abandoned-timeout-goroutine-leaves-lock.md) /
[363](../363-bug-lockman-with-signal-handler-installed-too-late.md) の側で、両方が閉じれば
この回収機構は「取りこぼしの受け皿」へ格下げできる。

## 残タスク

- [x] ハーネス由来か production の race かを切り分ける → **production の race**
- [x] production の race だった場合の修正 → 2 段の調停 + 再照合
- [x] ハーネス由来だった場合は判定軸を壁時計から外す → 該当なし (ただし回帰テストは
      seam で決定論にし、壁時計に依存させていない)
- [x] 敵対的レビューを観点を分けて通す (①壊す / ②素通り / ③並行・中断 + 差分への 3 周。計 6 周)
- [ ] `Renew` / `Release` の同型 → [380](../380-bug-lockman-renew-and-release-act-on-name-after-check.md)
- [ ] `with` の renew ラッチが恒久的に更新を止める → [381](381-bug-lockman-with-renew-latch-stops-renewal-forever.md)
- [ ] この修正が新設した wedge (mark の取りこぼし) の根治 →
      [362](../362-bug-lockman-abandoned-timeout-goroutine-leaves-lock.md) /
      [363](../363-bug-lockman-with-signal-handler-installed-too-late.md) が閉じたら回収機構を再評価する
