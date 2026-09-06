# bug: 一括取得待ちの detailsLoading の札が世代交代で永久に残り、パネルが「取得中」で固まる

起票日: 2026-09-05
カテゴリ: bug
優先度: 低（発火に `-n 401 以上`（既定は 20）が要る。ただし当たると**パネルが恒久的に固まる**）

🚨 **初版は症状を両方向で誤っていた**（敵対的レビューで訂正）。「再描画が止まらない」は
起きず、代わりに見落としていた**可視の症状**がある。詳細は「症状」節。

## 何が起きているか

`tui.go:fetchPanelDetails` は「進行中の一括取得に含まれる SHA は、その結果を待つ」分岐で
**`m.detailsLoading[sha] = true` を立て、Cmd を 1 本も発行せずに `return nil` する**。

この札を降ろす唯一の経路は `ciResultMsg` ハンドラの
`for _, sha := range msg.shas { delete(m.detailsLoading, sha) }` だが、その手前に

```go
if msg.epoch != m.fetchEpoch {
    return m, m.maybeTick()   // ← delete ループごと丸ごと捨てられる
}
```

があり、**世代不一致のチャンクは札の解放ごと捨てられる**。

`fetchEpoch` を進めるのは `startCIFetch` だけで、呼び出しは 2 箇所:

| 呼び出し元 | `detailsLoading` の扱い |
|---|---|
| `applyLogData` 経由 | 直前で `m.detailsLoading = map[string]bool{}` と作り直す → **安全** |
| `refetchAfterPush` | `m.statuses` / `m.details` は消すが **`detailsLoading` に一切触れない** |

`refetchAfterPush` は新しい取得対象を `startCIFetch(all)` に渡し、`startCIFetch` が
`capFetchSHAs` で **先頭 400 件**（`fetchTotalSHAs = fetchMaxSHAs 100 × fetchConcurrency 4`）へ
丸める。一方、旧 `toFetch` は「**未キャッシュの** commit」なので、先頭にキャッシュ済みが
並んでいると `commits[400]` 以降を含みうる。両者が一致しないため、
**差集合に入った SHA の札は以後どの経路でも降りない**（`closePanel` も消さない）。

結果 `len(m.detailsLoading) > 0` が恒久的に真になり、`tui.go:spinnerActive` が下りず
`maybeTick` が再アームし続ける。

## 症状（初版は両方向に誤っていた）

### ❌ 「80ms ごとに `invalidateLines` が全行を組み直す」は起きない

`tickMsg` ハンドラの invalidate ゲートは `detailsLoading` を**明示的に除外**していて、
しかもコメントで名指ししている:

```go
// list に毎フレーム変化する内容 (loading スピナー) が乗るのは fetch/awaitCI の 2 状態
// だけ。他の spinnerActive 条件 (panelHasRunningJob/pullAnimating/detailsLoading/…) の
// スピナー・経過時間は panelLines/diffBoxLines 側 (lines() の外) で毎フレーム描かれる…
if m.fetching() || len(m.awaitCI) > 0 {
    m.invalidateLines()
}
```

production 経路で再現させた状態（`fetching()=false` / `awaitCI=0` / `detailsLoading=1`）で
`tickMsg` を 1 拍送った実測: `linesValid=true`（= 組み直していない）。
残るのは「tick チェーンが終わらない = 12.5fps で View が回り続ける」だけで、
**そのコストは未実測**（パネルを閉じていれば描くものは何も無い）。

### ✅ 本当の主症状: 開いているパネルが「取得中」で永久に固まる

`panelLines` は `m.detailsLoading[m.panelSHA]` を**最初に**分岐する:

```go
switch {
case m.detailsLoading[m.panelSHA]:
    rows = []string{paint(m.spinner()+" CI job を取得中...", ansiDim, m.colored)}
```

`refetchAfterPush` は `m.details[sha]` を消すが `panelSHA` は触らないので、
**開き直す必要すらなく、開いたままのパネルのスピナーが永久に回り、CI job が二度と出ない**。
実測:

```
panelSHA==target: true / 札=true
開き直さずのパネル: "│ ⠋ CI job を取得中...                                    │░"
```

**優先度の根拠はこちらに置くこと。**「ユーザーからは何も見えない」は誤り。

## 再現（決定論的に確認済み）

使い捨てテスト（commit していない）で再現した:

```go
m.hasRepo = true
m.toFetch = []string{"sha-beyond-cap"}
m.pendingFetches = 1
epochBefore := m.fetchEpoch

m.fetchPanelDetails("sha-beyond-cap")   // Cmd は nil、札だけ立つ
m.startCIFetch([]string{"other-sha"})   // refetchAfterPush 相当: 世代が進む
m.Update(ciResultMsg{epoch: epochBefore, shas: []string{"sha-beyond-cap"}, batch: CIBatch{}})
```

結果:

```
札が残った: detailsLoading=map[sha-beyond-cap:true]
spinnerActive() が下りない (12.5fps の再描画が止まらない)
```

## 発火条件（すべて同時に成立する必要がある）

1. **`glogx -n 401 以上`（または `-n -1`）で起動している**。🚨 **既定は `-n 20`**
   （`options.go:defaultMaxCount`）。`refetchAfterPush` は `startCIFetch(all)` に全 commit を渡し、
   `capFetchSHAs` が `commits[0:400]` へ丸めるので、`sha ∉ 新 toFetch ⟺ sha が commit index ≥ 400`。
   **`-n` を明示的に大きくしない限り原理的に成立しない**
2. 先頭にキャッシュ済み commit が 1 件以上ある（旧 `toFetch` = 未キャッシュ分なので、
   これがあると index 400 以降へ届く）
3. 一括取得が飛んでいる最中に、**index 400 以降**のコミットでパネルを開く
4. その窓の中で push が完走し、`refetchAfterPush` が世代を進める
5. 旧世代のチャンク（その SHA を含む）が世代交代の**後**に着弾する

実測（production 経路の再現ハーネス）:

| 条件 | 結果 |
|---|---|
| `n=410` / パネル index=400 | **stuck=true**（再現） |
| `n=20`（既定相当）/ index=10 | stuck=false（再現しない） |

- **silent に壊れる**: build も lint もテストも緑
- 🚨 **「以後どの経路でも降りない」は言い過ぎ**（初版の記述）。`applyLogData` が
  `m.detailsLoading` を作り直すので、**次の pull / `reloadLog` / 見張り由来の読み直しで降りる**。
  正しくは「**次の読み直しまで降りない**」

## これは issue 223 と同じ失敗モード

`issues/done/223-bug-glogx-phantom-awaitci-never-gives-up.md` が `awaitCI` で直したのと
**まったく同型**（`spinnerActive()` の 1 項が永久に真 → 80ms ごとの `invalidateLines` が
セッション中回り続ける）。223 の対応は「`awaitCI` ⊆ commits の SHA」に閉じており、
**同じ式に並ぶ兄弟の `detailsLoading` は掃かれていない**。

規範自体は既に repo 内にある: `gitlog_watch.go:handleGitLogProbe` は closed 経路で

> 🚨 飛んでいる測定の札もここで降ろす: 世代を進めた後に届く結果は gen 違いで捨てられるので、
> 降ろさないと measuring が永久に true のまま = **以降ひとつも測らなくなる** (静かに機能停止)

と明文化し、`measuring` / `reloading` / `pollArmed` を明示的に降ろしている。
**`tui.go` の epoch ガードだけがこの規律を満たしていない。**

## 対応 (2026-09-06)

不変条件を「**札には必ず、その SHA を名指して届く live Cmd がある**」に置き、
待機分岐で立てた札だけを `detailsWaiting` で区別した。`startCIFetch`（世代を進める唯一の
choke point）で、新 `toFetch` に含まれない**待機の札だけ**を解放する。
実取得分岐の札は自前の `detailMsg` が降ろすので触らない。

🚨 **実装中に 2 つ踏んだ**:

1. 起票時の再現テストに書いた「`spinnerActive()` が下りない」は**交絡していた**。
   `startCIFetch` の直後は `pendingFetches > 0` で `fetching()` が正当に真になるので、
   札の有無に関わらず真。さらにフィクスチャ既定では `usageOv.loading()` も真だった。
   両方を潰してから見る形へ直した（「札が残る」自体は本物）
2. 逆向きのテスト（実取得の札を落とさない）が、最初は**待機と実取得を同時に持つ状態を
   作っていなかった**ため、`if len(m.detailsWaiting) > 0` のガードで解放ループへ到達せず、
   「区別をやめて全部落とす」変異が到達不能になって素通りした

変異検証 3 本すべて red: 解放を丸ごと外す / 区別をやめて全部落とす /
待機分岐で `detailsWaiting` を立てない。

## 推奨対応（起票時）

不変条件を「**`detailsLoading` ⊆ 現在の取得対象**」とし、世代を進める唯一の choke point で
ある `startCIFetch` に札の解放を持たせる。`startCIFetch` は既に
`fetchEpoch` / `toFetch` / `pendingFetches` / `ghErr` の立て直しを集約している関数なので、
「旧世代が降ろす責任を負っていた札」の解放も同じ場所に置くのが筋。

### 🚨 初版の案（`startCIFetch` で新 `toFetch` に無いキーを落とす）は別経路を壊す

敵対レビューが実装して実測した。`fetchPanelDetails` には札を立てる分岐が **2 種類**ある:

| 分岐 | Cmd | 初版の案を当てると |
|---|---|---|
| **待機分岐**（`m.fetching() && slices.Contains(m.toFetch, sha)`） | **出さない** | ✅ 正しく降ろせる（これが本 issue のバグ） |
| **実取得分岐** | `detailMsg` の Cmd を**出す** | ❌ **飛んでいる取得の札まで落とす** |

実測（`n=410`, index=405、取得が in-flight の状態で push）:

```
（修正あり）openPanel cmd=true 札(前)=true 札(後)=false panel="│ (CI job 情報なし) │"
再オープン: 新たな fetch Cmd=true      ← 同一 SHA への 2 本目の GraphQL
```

帰結は 2 つ: ①取得が本当に飛んでいるのに「(CI job 情報なし)」を出す ②開き直すと
`fetchPanelDetails` の 1 行目ガードが外れているので **同一 SHA へ 2 本目の GraphQL が出る**。
後者は `fetchPanelDetails` のコメントが名指しで避けている事故（「同一 SHA への GraphQL が
並行し、完了順で statuses/details が上書きされる (codex レビュー指摘)」）。

同じ話が `maybeFetchETABasis` が立てる札（`basisMsg` 待ち）にも当たる。

🚨 **既存スイートはこの修正を当てても全部緑**（落ちたのは red team のテストだけ）。
duplicate fetch を守るテストが 1 本も無いので、**「既存テスト緑」を安全の根拠にできない**。

### 守るべき不変条件はキー集合ではない

正しい不変条件は「`detailsLoading` ⊆ `toFetch`」ではなく、
**「札には必ず、その SHA を名指して届く live Cmd がある」**。

これを破っているのは `fetchPanelDetails` の**待機分岐だけ**（Cmd を 1 本も出さずに札を立てる）で、
実取得分岐と `maybeFetchETABasis` の札は破っていない。**`startCIFetch` でキー集合だけを見る形は
この 2 種類を区別できない。**

対応はこの区別を前提に設計すること（待機分岐の札に「誰が降ろすか」を持たせる、
待機分岐でも軽量な Cmd を出して自分で降ろす、など）。
🚨 **呼び出し側（`refetchAfterPush`）に `delete` を足す形は採らない** — 次に `startCIFetch` を
呼ぶ経路が増えたときに同じ書き忘れが再発する（227 の「全画面ビューアを個別に列挙しない」と同じ理由）。

🚨 **`startCIFetch` は「開始点」を全部は集約していない**。`startCIFetch` 自身の doc は
「開始点は 3 箇所 (起動 / reloadAfterPull / refetchAfterPush)」と書いているが、
**起動経路は `newBrowseModel` がインライン展開**していて（`m.cancel` と ctx を共有する意図的例外）
この関数を通らない。choke point をここに置くならその事実に触れること。

### 回帰テストの向き

現状 `tui_nav_test.go` の `spinnerActive` テーブルは「**立てたら true**」の向きしか持たない。
必要なのは逆向き — 「**非同期の世代交代・キャンセル・閉じるを跨いでも最終的に false へ戻る**」。
上の再現テストがそのまま雛形になる。

さらに横断的には、`spinnerActive()` は OR の **18 項**からなり、1 項でも永久に真になると
同じことが起きる。各項について「真にする経路」と「**偽に戻す全経路**」を対にした
テーブル駆動テストを置くと、この失敗モードが構造的に閉じる。

🚨 **issue 028 P3 は逆向きの失敗モードなので「2 度踏んでいる」ではない**（初版の誤り）。
028 P3 は「新しい非同期処理を足したとき `spinnerActive` への追記を忘れて**項が欠け**、
tick が回らずアニメが止まる」= 偽陰性側で、028 の完了記録も
`TestBrowseSpinnerActiveSources`（立てたら true の網羅）で閉じている。
**この失敗モード（項が永久に真 = 偽陽性）は 223 で 1 度踏んでいる**が正しい。
028 は「回帰テストの向きを論じる節」で、その教訓が効かない理由として引ける。

## 反証の試み

`tui.go` のコメント・`src/glogx/CLAUDE.md`・`issues/` と `issues/done/`（028 / 223 を確認）を
探したが、「世代不一致のチャンクで札を降ろさないのは意図的」と書いた箇所は無かった。
むしろ `gitlog_watch.go` に逆の規範が明記されている。

## 関連

- `issues/done/223-bug-glogx-phantom-awaitci-never-gives-up.md`（同一の失敗モード。兄弟の掃き漏れ）
- `gitlog_watch.go:handleGitLogProbe`（規範の正本）
