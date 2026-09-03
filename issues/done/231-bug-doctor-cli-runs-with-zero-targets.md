# doctor: `cli:` の非 ref エントリは対象 0 件でも外部コマンドを実行し、報告は「触れず」と言う

起票日: 2026-09-04
種別: bug (error-handling / 不変条件違反)
優先度: **P1** (実行したのに「何も起きていない」と報告する = 記録が嘘になる)
出典: audit (security / error-handling) 2026-09-04 / forge-Standard。指摘は main agent がコードで裏取り済み

## 該当

- `src/doctor/disk/delete.go: execCLI` の `!cliNeedsRef(argv)` 分岐
- `src/doctor/disk/delete.go: planDelete` の末尾 (`out.Outcome = OutcomePlanned` を無条件に立てる)
- `src/doctor/disk/delete.go: verifyEntry` の `touched == 0` 分岐
- カタログ該当: `go-modcache` (`cli:go clean -modcache`) / `brew-cleanup-residue` (`cli:brew cleanup`)

## 症状

`planDelete` は、渡された Item が**全部 `unmatchedItem` (Skipped / Failed) になった後でも**
末尾で `out.Outcome = OutcomePlanned` を立てる。`execCLI` の非 ref 分岐は **Item を 1 つも見ずに**
`run1(argv)` を実行する:

```go
if !cliNeedsRef(argv) {
    rec, res := run1(argv)          // ← Item の Outcome を見ていない
    out.Commands = append(out.Commands, rec)
    for i := range out.Items { if out.Items[i].Outcome != OutcomePlanned { continue } ... }
    return
}
```

非対称が要点: **`rm` / `trash` / `cli` の ref 版は Item 単位で `Outcome != OutcomePlanned` を弾く**のに、
非 ref 版だけ弾かない。その後 `verifyEntry` は `touched == 0` を見て
`OutcomeSkipped`「触れる対象がありませんでした」(または `OutcomeFailed`) を書くので、
**`brew cleanup` / `go clean -modcache` が実際に走ったのに、結果画面と記録は「触れず」と断言する**。

## 発火条件

確認画面 (下見) を出してから `y` を押すまでのあいだに、選んだ対象が

- 他のプロセスに消される (キャッシュなので日常的に起きる)
- guard の判定が変わって候補から外れる (mtime が起動時刻より新しくなる等)
- 走査時と別の実体に差し替わる

のいずれかで**全件が fresh 側の index に無くなる**とき。対象が 1 件のエントリでは
「1 つ消えるだけ」で成立する。

## silent か

**silent。** `out.Commands` にコマンドの記録は残るので削除ログには `$ brew cleanup` が出るが、
エントリの結末語は「🚫 触れず」/「❌ できず」で、`Freed` は 0。読み手は「実行されなかった」と読む。
`planHasWork` はエントリの Outcome を見るので UI も止めない。

## 反証の試み

- 「`brew cleanup` は対象非依存だから実行しても害はない」→ 害は**削除ではなく報告**。
  `delete.go` 冒頭の不変条件「解放量は『コマンドが成功したこと』から計算しない」と
  「結末を 2 値に畳まない」を、**逆向き** (実行したのに触っていない扱い) に破っている
- 「既存テストが守っているのでは」→ `delete_test.go` の cli ケースは全 Item が fresh index に
  在る fixture しか作らないので、この経路を**構造的に踏まない**

## 最小の修正方向

`execCLI` の非 ref 分岐に、ref 版と同じ「触る Item が 1 つでも Planned か」の前提を入れる
(1 件も無ければコマンドを実行せず `OutcomeSkipped`)。判定を 2 箇所に書かないよう、
`planDelete` 側で「全 Item が非 Planned なら entry を Skipped にする」へ寄せる方が根治に近い
(`rm` / `trash` / cli-ref の 3 経路も同じ前提を暗黙に持っているため、出典が 1 つになる)。

## 変異検証の形

fresh index に載らない Item だけを渡す fixture を作り (`Run` は fake、`sandboxAllowCommand` で
コマンド名を登録)、**fake が 1 回も呼ばれないこと**を assert する。
変異 = 修正した前提を外す (無条件 `run1`) → fake の呼び出し回数 0 → 1 で red。
「Outcome が Skipped か」だけを見る assert にしないこと (修正前もそう報告するので vacuous になる)。

## 対応 (2026-09-04)

`planDelete` の末尾に「触る対象 (Outcome == OutcomePlanned) が 1 件も無ければ entry を
Planned にしない」ゲートを入れた。判定を `execCLI` に足さず `planDelete` を出典にしたのは、
同じ前提を rm / trash / cli-ref の 3 経路も暗黙に持っているため (2 実装にしない)。
結末語は `untouchedOutcome` に出し、`verifyEntry` の `touched == 0` 分岐と共有する。

### 敵対的レビューの全数勘定 (opus / red team、指摘 6 件)

| 指摘 | 判定 | 対応 |
|---|---|---|
| P2-2 ゲートの `failed > 0` 側をテストが 1 本も守っていない | **本物** (変異が緑と実測) | `TestDeleteCLISkipsCommandWhenTargetsAreNoLongerCandidates` を追加 |
| P3-1 cli の Item 集合がコマンドの効果の真部分集合だと消せなくなる | 現行カタログでは発火せず | ゲート直近に不変条件をコメントで明記 (実装で強制できないため) |
| P3-2 `failed > 0 \|\| out.Reason == ""` が常に真 (死んだ条件) | **本物** | 条件を落とし、到達時に Reason が空である理由をコメントに残した |
| A: ゲートが正当な削除を止める | **壊せなかった**。propose はゲートより手前で return / `planItem` の cli 非 ref は常に Planned を返す / sim-runtime の全 Ref 不正は旧実装でもコマンド 0 回 | なし |
| B: ゲートの素通り | **壊せなかった**。`planned >= 1` には fresh scan で実在を確認した Item が要る | なし |
| F: `untouchedOutcome` へ寄せたことの意味変化 | **壊せなかった**。旧実装と同値 | なし |

### 変異検証

- `planDelete` を無条件 `OutcomePlanned` へ戻す → `TestDeleteCLISkipsCommandWhenNothingToTouch` が red
- ゲートを `planned == 0 && failed == 0` に弱める → `TestDeleteCLISkipsCommandWhenTargetsAreNoLongerCandidates` が red

いずれも `go build` 通過を確認してから判定した。
