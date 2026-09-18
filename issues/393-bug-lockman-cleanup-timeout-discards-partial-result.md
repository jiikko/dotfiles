# 期限切れした cleanup が `removed=0` と報告する (実際は数千件消している)

起票日: 2026-09-18
カテゴリ: bug / priority: **medium**
対象: `src/lockman/timeout.go` の `CleanupTimed`、`src/lockman/main.go` の `dispatch`
出典: [issue 391](done/391-bug-lockman-os-exit-skips-defer-leaves-scratch.md) の反証レビュー
反証レビュー: 未実施。**数値は起票者の実測**（下表）

## 問題

`CleanupTimed` は期限切れのとき、**掃除の部分結果を捨てて** `CleanupResult{Errors: [...]}` を返す:

```go
res, err := withTimeout(l.timeout, func(_ *abandon) (CleanupResult, error) { return l.Cleanup(force), nil })
if err != nil {
    return CleanupResult{Errors: []string{err.Error()}}   // ← res を捨てている
}
```

`dispatch` はこれを `removed=%d` で表示する。つまり**実際には数千件を削除しているのに
`removed=0` と出る**。

`dispatch` のコメントは、まさにこの場面を名指しして逆のことを約束している:

> 件数も一緒に出す: 部分的に進んでいるのか何も進んでいないのかは、**失敗している場面でこそ知りたい**

**その唯一の場面で数字が嘘になる** (「何も進んでいない」と読める値が出る)。

## 実測 (2026-09-18、ローカル APFS)

tmp/ に 2 時間前に打刻した残骸 10,000 件を置き、`.cleanup_at` を 20 分前にして
`acquire --io-timeout 100ms` を 3 回:

| 回 | rc | tmp 残 | 実際に減った数 | 報告 |
|---|---|---|---|---|
| 1 | 0 | 7,451 | **2,549** | `removed=0 errors=[I/O timeout]` |
| 2 | 0 | 5,019 | **2,432** | `removed=0 errors=[I/O timeout]` |
| 3 | 0 | 2,564 | **2,455** | `removed=0 errors=[I/O timeout]` |

3,000 件まで減ると 1 回で完走し、`removed=<実数>` が出て打刻もされる。

## なぜ気にするか

1. **人が「掃除が全く進んでいない」と誤診する**。実際は排水中で、数回放っておけば収束する。
   誤診すると `rm -rf` のような手作業へ誘導しうる (lock dir に対して最も危険な操作)
2. **`removed` は「部分的に進んでいるか」を見るための唯一の出力**。ここが 0 に固定されると、
   「掃除が本当に 1 件も進まない状態」(権限ドリフト等) と**区別が付かない**

## 対応の候補 (未決)

- `withTimeout` の期限切れ時に、**部分結果を捨てずに返す**。`Cleanup` は
  `res` をポインタで積んでいくので、期限切れ時点のスナップショットを読むと
  **データ競合**になる (内側の goroutine はまだ書いている)。素朴にはできない
- 件数を `atomic` で持たせ、期限切れ時にその時点の値を読む
- 報告を「`removed=不明 (期限切れ。実際には進んでいる可能性がある)`」にする。
  🚨 **数を偽るより「分からない」と言うほうが正直**で、
  `adversarial-review-own-safeguards.md` の「判定不能を専用の値で出す」に沿う。**これが第一候補**


## 進捗 (2026-09-19): 実装完了 (下の「敵対的レビュー 1 周目」で改訂済み)

### 候補の選択: **atomic のカウンタ** (issue の第一候補「removed=不明」は採らなかった)

| 候補 | 判定 |
|---|---|
| (a) 部分結果 (`res`) をそのまま返す | **却下**。見捨てた goroutine がまだ `res` を書いているのでデータ競合 (issue 本文どおり) |
| (b) **atomic で件数を持たせ、期限切れ時点の値を読む** | **採用** |
| (c) `removed=不明` と報告する | **却下**。数を偽らない点は解決するが、issue の「なぜ気にするか」の**2 番目** (排水中か 1 件も進まないかの区別) が解けない。`removed` は部分進捗を見るための唯一の出力なので、「不明」に固定しても「本当に 1 件も進まない状態」と区別が付かないまま |

### 実装

- `cleanupProgress` を新設し、`Cleanup(force bool, progress ...*cleanupProgress)` で受ける。
  `sweepDir` は消すたびに `res.Removed++` と `p.add()` の**両方**を進める
  (🚨 **1 件ずつ**。一括で足す形は issue 393 の症状に戻る。1 周目 P1-1)
- `CleanupTimed` は期限切れ時に `p.snapshot()` (件数 **+ sweep 中のエラー**) と `Partial: true` を返す。
  **`res` は読まない** (データ競合)
- `CleanupResult.Partial` を新設。true = 「**少なくとも** N 件。掃除は継続中」
- `dispatch` の文面 (1 周目 P2-1 / P2-2 で改訂):
  0 件なら `removed=判定不能 (…)` / それ以外は `removed>=N (期限切れ。ここまでは確認済み)`
- 🚨 **progress は可変長引数**。掃除の入口を 2 つに割ると `timeout_wiring_test.go` の gate
  (「生の I/O は timeout.go からしか呼ばない」) が見ている名前と実際の入口がずれる。
  実際に `cleanupWithProgress` を別メソッドへ切り出したら **gate が落ちた** (= gate は正しく働いた)。
  入口を 1 つに保ち、渡さない呼び出し側 (テスト 20 箇所) は今までどおり書ける

### テスト (最終形は 5 本。`cleanup_partial_test.go`)

🚨 **初版は seam を窓の手前 (`.cleanup_at` の FIFO = sweep の後) に置いており、
一括カウントの変異を検出できなかった** (1 周目 P1-1)。窓の内側 (`sweepPauseHook`) へ移した。

| テスト | 何を固定するか |
|---|---|
| `TestCleanupTimedReportsPartialProgress` | **sweep の途中**で期限切れにし、`0 < removed < 全件` と `Partial` を返す (報告が実態を上回らないことも見る) |
| `TestCleanupCLISaysIndeterminateWhenNothingRemoved` | 0 件の期限切れは `removed=判定不能`。「進んでいる」と断定しない |
| `TestCleanupTimedKeepsSweepErrorsOnTimeout` | sweep 中のエラーを期限切れで捨てない |
| `TestCleanupMarksCompleteRunAsNotPartial` | 対照。完走した掃除に `Partial` が立たない |
| `TestCleanupCLIDistinguishesPartialFromComplete` | CLI の出口で `removed>=N` が出る |

### 変異検証

**下の「敵対的レビュー 1 周目」の表が最新** (この時点の 2 本は M3 = 一括カウントを検出できていなかった)。

### 結果

- `go test -race ./...` 緑 (34.3s) / `go vet` 緑
- `dispatch` のコメント (「失敗している場面でこそ知りたい」) と実装が一致した

## 敵対的レビュー 1 周目 (2026-09-19。全数勘定)

指摘 9 件、**採用 8 / 記録 1 / 却下 0**。🚨 **P1 は「私の回帰テストが、私が直したバグを守って
いなかった」**。核の判断 (atomic で件数を共有する) 自体は「壊せなかった」と確認された。

| # | 指摘 | 判定 |
|---|---|---|
| P1-1 | 新テストの seam (`.cleanup_at` を FIFO にする) は **sweep の後**で詰まるので、「sweep の途中で期限切れ」を**構造的に再現できない**。実際、カウンタを sweep の後に一括で足す変異 (実装者が将来やりそうな「最適化」) が**全スイート緑**で通り、その変異は本番で `removed=0` を再現した (レビュー実測: 10 件消して `Removed=0`)。**この commit の回帰テストは、この commit が直したバグの再導入を検出しない** | **採用**。`sweepPauseHook` を **sweep のループ内 (窓の内側)** に新設し、「0 < removed < 全件」を決定論的に固定。🚨 この seam は**見捨てられた goroutine から読まれ続ける**ので atomic で持つ (素の変数だと `-race` がテストの後始末との競合を検出した。実測) |
| P2-1 | 文面「掃除は途中で、次回の続きから減る」が**無条件の断定**。詰まった位置が sweep の後 (打刻) なら sweep は完走しているので「途中」は偽 (レビュー実測: tmp 残 0 でこの文が出た) | **採用**。断定をやめ「ここまでは確認済み」へ |
| P2-2 | **0 件でも同じ文が出る**。旧版の `removed=0` は曖昧なだけだったが、新文面は**偽の肯定**を足しており、issue の動機 2 を**逆向きに壊していた** (「進んでいるから待とう」へ誤誘導) | **採用 (最重要)**。`removed=判定不能 (1 件も消せていないのか、消している途中なのかは分からない)` へ。判定不能を allow / deny のどちらにも丸めない |
| P2-3 | 期限切れ時に **sweep 中のエラーだけが捨てられていた** (件数は救ったがエラーは救っていない)。エラーは「排水中」と「権限ドリフトで恒久的に詰まっている」を分ける情報で、動機 2 に対しては件数より効く | **採用**。progress にエラーも積んで持ち帰る |
| P2-4 | `removed == residue` の完全一致 assert が壁時計依存 (仕様は「少なくとも N 件」なのにテストは完全一致を要求。落ちると「修正が退行した」と読める偽の赤) | **採用**。`0 < removed < 全件` へ |
| P3-1 | 既存 `TestIOTimeoutWrapsDeferredCleanup` の `Removed != 0` は**消えた不変条件**を pin していた (fixture に残骸を 1 件足すと正しい挙動に偽の赤。レビュー実測) | **採用**。意図を書き直した |
| P3-2 | 可変長引数の根拠コメントが gate の実挙動より強い。切り出しで落ちるのは**件数の下限 canary** であって包み忘れの検出ではない (レビュー実証: timeout.go に別の wrapped 名を 1 つ足すと、切り出したままでも gate は通る) | **採用**。コメントを訂正し、「入口を 1 つに保つ規律はこのコメントが正本」と明記 |
| P3-3 | `sweepDir` が `p == nil` で panic する非対称 (`Cleanup` 側は nil を弾いている) | **採用**。`add` / `addErr` / `snapshot` を nil 安全に |
| P3-4 | `Cleanup(force, p1, p2)` は p2 を黙って無視する / `json:"partial,omitempty"` は `CleanupResult` が marshal されないので飾り | **記録**。前者はコメントに明記 (callsite は 1 件)。後者は既存の `removed` / `skipped` と同じ形 |

### 変異検証 (レビュー対応後。ケース名ごとの PASS/FAIL)

| 変異 | 結果 |
|---|---|
| **M3: カウンタを sweep の後に一括で足す** (= issue 393 の症状そのもの) | `TestCleanupTimedReportsPartialProgress` **FAIL** (対応前は**全スイート緑**だった) |
| M4: 0 件のときも「進んでいる」文面へ戻す | `TestCleanupCLISaysIndeterminateWhenNothingRemoved` **FAIL** |
| M5: sweep 中のエラーを捨てる (旧挙動) | `TestCleanupTimedReportsPartialProgress` + `TestCleanupTimedKeepsSweepErrorsOnTimeout` **FAIL** |
| (対応前に確認済み) 期限切れ時に部分結果を捨てる | `TestCleanupTimedReportsPartialProgress` + `TestCleanupCLIDistinguishesPartialFromComplete` FAIL |
| (対応前に確認済み) CLI の書き分けを潰す | `TestCleanupCLIDistinguishesPartialFromComplete` FAIL |

### 壊せなかった経路 (レビューが実験で確認)

- **atomic の選択は守られている**: `atomic.Int64` を素の `int64` へ戻す変異は `-race` が検出する。
  期限切れ枝は chan を通らないので **happens-before が張られない**ため、構造的に検出される
- **`res` を期限切れ枝で読む経路は存在しない** (`withTimeout` は zero value を返し、
  見捨てた goroutine の `res` は buffered chan に置かれて誰も読まない)
- **gate の迂回**: `l.Cleanup(force, p)` が timeout.go 以外へ漏れる形は作れなかった
  (AST の判定は可変長引数の有無に影響されない)
- **`Skipped` を期限切れ枝で落としている点**: 実害を作れなかった (`Skipped: true` を返す枝は
  I/O をほぼ伴わず、期限切れと共存する入力を作れない)

### 2 周目が要る

§7 の打ち切り条件 (a)(b) を**満たさない**: 1 周目の対応で **production 側に新しい判定と seam を
新設した** (`sweepPauseHook` / `Partial` × `Removed==0` の文面の出し分け / progress のエラー収集)。
攻め口: ①新設した seam が production で no-op であること・seam 自身が窓を変えていないこと
②文面の出し分けの各枝 ③エラー収集の競合 (mutex の範囲) ④新テストの fixture が
「sweep の途中」を本当に作れているか (M3 が red になり続けるか)

## 敵対的レビュー 2 周目 (2026-09-19。全数勘定)

指摘 6 件、**採用 5 / 訂正 1 / 却下 0**。**M3 (一括カウント) は red のまま**で、
1 周目 P1-1 の対応が有効であることをレビューが独立に確認した。

| # | 指摘 | 判定 |
|---|---|---|
| P1-1 | 1 周目の P2-3 対応は **`os.Remove` 由来のエラーしか運んでいなかった**。`ReadDir` の EACCES / `Info` の失敗 / `serverNow` / 打刻の失敗は `res.Errors` にしか入らず、期限切れ枝は `res` を読めない (データ競合) ので**構造的に消える**。🚨 落ちていた側 (`ReadDir` の EACCES = **権限ドリフトの典型**) こそが動機 2 の本命で、レビュー実測では権限ドリフトの stderr が「健全に排水中」と**1 文字も違わなかった** | **採用**。「どのエラーを運ぶか」を判断で選ぶのをやめ、**蓄積先を `cleanupProgress` 一本にして `res` はそこから導く** (`sweepDir` から `*CleanupResult` を外した)。線引きの判断ごと消える |
| P1-2 | 1 周目が新設した seam のせいで `TestCleanupTimedReportsPartialProgress` が **flaky**。見捨てた goroutine が `stampCleanup` で `.cleanup_at` を作り直し、`t.TempDir()` の RemoveAll と競合して `directory not empty` の**無関係な赤**になる (レビュー実測: 並行負荷の高い環境で `go test -race ./...` 8 回中 2 回。しかも**そのレビュー自身の変異判定を 1 回汚染した**) | **採用**。🚨 こちらの環境では **16 回中 0 回**で再現しなかったが (単独 10 / フル 6)、**機構は成立している**ので直した。成立条件 (打刻の出現) を上限つきでポーリングしてから終わる |
| P2-1 | `removed>=N (…)` の括弧の中身が**どのテストにも守られていなかった**。1 周目に「偽の断定」として消した文面 (「掃除は途中で、次回の続きから減る」) を戻す変異が**全スイート緑**。否定リストは `判定不能` 側の fixture にしかなく、もう一方の枝の文面は原理的に現れない。しかも否定リストの語は **production から消えた死んだリテラル**で、一字一句戻したときしか鳴らない | **採用**。両方の書式を `main_test.go` と同じく**完全一致の regexp** で pin した (同じ package が「部分一致で pin しない」と書いているのに、新設した 2 書式だけがその外にあった) |
| P3-1 | 1 周目の変異表の M5 の行が実態より広い (`TestCleanupTimedReportsPartialProgress` は sweep エラーを持たない fixture なので落ちない) | **訂正**。下の表で 1 テストのみに直した |
| P3-2 | 「報告 ≤ 実態」の assert は、見捨てた goroutine が消し続けるので**成立する方向へ動き続ける** (ほとんど何も検査しない) | **採用**。P1-2 の待ちを入れた後に**最終状態**を assert する形へ |
| — | `p.errs` / `p.removed` のデータ競合 / `snapshot` が 2 つの読みを跨ぐこと / seam の production コスト / テスト間の混線 / gate の迂回 | **壊せなかった** (mutex とコピーで aliasing なし、`t.Parallel` は 0 件、可変長引数は AST 判定に影響しない) |

### 変異検証 (2 周目の修正分)

| 変異 | 結果 |
|---|---|
| M3: カウンタを sweep の最後に一括計上 (= issue 393 の症状) | `TestCleanupTimedReportsPartialProgress` **FAIL** (他の 4 本は PASS) |
| M6: P2-1 が消した偽の断定を戻す | `TestCleanupCLIDistinguishesPartialFromComplete` **FAIL** |
| M7: `ReadDir` のエラーを progress へ積まない (1 周目の取りこぼしを再現) | `TestCleanupDoesNotStampWhenReadDirFails` **FAIL** |
| (1 周目) M4: 判定不能の枝を削除 | `TestCleanupCLISaysIndeterminateWhenNothingRemoved` FAIL |
| (1 周目) M5: sweep エラーを捨てる | `TestCleanupTimedKeepsSweepErrorsOnTimeout` FAIL (**1 周目の表にあった `ReportsPartialProgress` は誤り**。あちらの fixture は sweep エラーを持たない。P3-1 で訂正) |

### 3 周目が要る

§7 の打ち切り条件 (a)(b) を**満たさない**: 2 周目の対応が **production の構造を変えた**
(エラーの蓄積先を progress へ一本化し、`sweepDir` の signature から `*CleanupResult` を外した)。
攻め口: ①蓄積先の一本化で `res.Errors` の内容・順序・重複が変わっていないか
②`errCount()` を打刻の判定に使ったことの影響 (mutex の範囲 / 見捨てた goroutine との関係)
③待ち (`waitFor`) を入れたテストが、待つことで**窓を再現しなくなっていないか** (M3 が red のままか)
④完全一致 pin にしたことで `TestDispatchWarnsOnCleanupFailureWithoutVerbose` と二重管理になっていないか

## 敵対的レビュー 3 周目 (2026-09-19。全数勘定)

指摘 6 件、**採用 4 / 記録 2 / 却下 0**。**production (実装) は 1 つも壊せておらず**、
指摘はすべて**テストの検出力**だった。

| # | 指摘 | 判定 |
|---|---|---|
| P1-1 | 2 周目に「ほとんど何も検査しない」として消した「報告 ≤ 実態」の assert は、**変異を当てずに vacuous と判定していた** (`mutation-verify-new-tests.md` の「等価変異と判定するなら経路を列挙してから言う」の不履行)。実際は期限切れ枝で「**報告 > 実態**」を守る**唯一の上限**で、消した後は `removed` を 2 倍に水増しする変異が**全スイート緑**で通る (**自分でも再現**)。issue 393 が直した嘘の**鏡像** (「消していないのに消したと言う」) | **採用**。元の位置 (待ちの**前**) に戻した。片側 assert なので、間に入る追加削除は `got` を増やすだけ = 偽の赤にならない。置き換えに入れていた `res.Removed >= residue` は 20 行上と**条件が完全に同じ重複**だったので削除 |
| P2-1 | `p.add()` を `os.Remove` の**前**へ動かす変異 (削除の「試行」を数える形。実装者が自然に書きうる) も**全スイート緑**。`Removed` を exact equality で pin するテストは 4 本あるが**どれも fixture に削除失敗を持たず**、削除失敗を持つ 2 本は `Removed` を 1 mm も見ていなかった。🚨 **2 周目の退行ではなく既存の穴**だが、P1-1 で上限系が消えて母集合ゼロになっていた | **採用**。既存 fixture (`TestCleanupDoesNotStampWhenRemoveFails`) に assert を 1 行 |
| P3-2 / P3-4 | 自前の `waitFor` は package 内 3 本目のポーリング実装 (既存の `waitForFile` で足りる) / regexp の `skipped=(?:true\|false)` が両方を受ける | **採用**。既存ヘルパーへ寄せ、`skipped=false` に締めた (1 周目に「実害を作れなかった」と記録した「期限切れ枝が Skipped を落とす」がこれで pin された) |
| P3-1 | `waitFor` の終了条件 (打刻の出現) は **`p.errCount() == 0` のときしか起きない**ので、将来この fixture がエラーを持つと「掃除は終わっているのに 10 秒待って落ちる」。また打刻は goroutine の最後の**ファイル作成**であって最後の**動作**ではない | **記録**。fixture 専用の条件であることをテストのコメントに明記した。構造的な正解は「goroutine が返った」を直接観測する seam だが、それは新しい機構なので足さない |
| P3-3 | `defer func(){ res.Removed, res.Errors = p.snapshot() }()` は、body 内の `res.Errors = append(...)` を **silent no-op** にする。旧来の書き方へ戻す変異は 3 本 FAIL で守られているが、「**新しい**エラー源を `res.Errors` へ足す」形は静かに消える | **記録**。構造で消す案 (局所変数にして return 直前に組む) は production の構造変更なので、やるなら別 commit + もう 1 周 |
| — | 待ちが窓を潰していないか (M3 再実験) / `waitFor` が vacuous でないか / `errCount()` の影響 / `res.Errors` の内容・順序・重複 / `main_test.go` との二重管理 | **壊せなかった** (レビューが全部実験。特に **M3 は red のまま**で、待ちは窓 assert より後なので構造的に隠せない。`res.Errors` への append は production に **0 件**で、重複も欠落も作れなかった) |

### 変異検証 (3 周目の修正分)

| 変異 | 結果 |
|---|---|
| M8: 期限切れ枝で `removed` を 2 倍に水増しする | `TestCleanupTimedReportsPartialProgress` **FAIL** (修正前は全スイート緑) |
| M9: `p.add()` を `os.Remove` の前へ (試行を数える) | `TestCleanupDoesNotStampWhenRemoveFails` **FAIL** (修正前は全スイート緑) |
| M3 (再確認): カウンタを sweep の最後に一括計上 | `TestCleanupTimedReportsPartialProgress` **FAIL** のまま |

### 4 周目は不要 (§7 の打ち切り条件)

打ち切り条件の例外 (a)(b) を満たす: **(a) 3 周目の対応は production を 1 行も変えていない**
(テストの assert 追加・重複削除・ヘルパーの共通化・regexp の締め。判定ロジックの新設はゼロ)。
**(b) 各修正を直接の実測で確認した** (変異 2 本で red、M3 の red 継続、`go test -race ./...` 緑)。
🚨 環境条件を新たに持ち込む修正も無い。
**記録に回した P3-3 (構造で footgun を消す) を実装するなら、それは production の構造変更なので
別 commit + もう 1 周が要る** — 本 issue のスコープでは記録に留める。

## 残タスク

- [x] 反証レビュー 1 周目。P1 1 件 + P2 4 件 + P3 3 件を採用、P3 1 件を記録 (変異 3 本で red)
- [x] 2 周目 (§7)。P1 2 件 + P2 1 件 + P3 1 件を採用、1 周目の変異表を 1 行訂正 (変異 3 本で red)
- [x] 3 周目 (§7)。P1 1 件 + P2 1 件 + P3 2 件を採用、P3 2 件を記録 (変異 2 本で red)。
      **4 周目は不要** (打ち切り条件 (a)(b) を満たす。production は 1 行も変えていない)
- [ ] **スコープ外 (記録)**: `Cleanup` の `defer` が `res.Errors = append(...)` を silent no-op に
      する footgun を構造で消す (局所変数にして return 直前に組む)。production の構造変更なので
      別 issue / 別 commit + もう 1 周が要る
- [x] 候補の選択 → (b) atomic のカウンタ。(c) は「排水中か 1 件も進まないか」を区別できないので却下
- [x] `dispatch` のコメントと実装を一致させた (`removed=N` / `removed>=N` の書き分け)
