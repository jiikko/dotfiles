# bug: 一括取得待ちの detailsLoading の札が世代交代で永久に残り、再描画が止まらなくなる

起票日: 2026-09-05
カテゴリ: bug
優先度: 中（issue 223 と同一の失敗モード。発火経路は狭いが、踏むとセッション中ずっと消耗する）

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
`maybeTick` が再アームし続ける。画面は静止しているのに **80ms ごとに `invalidateLines` が
全行を組み直し続ける**。

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

## 発火条件

1. 一括取得が飛んでいる最中に、`toFetch` に載っている SHA でパネルを開く
2. その間に push が完走し、`refetchAfterPush` が世代を進める
3. 旧世代のチャンク（その SHA を含む）が世代交代の**後**に着弾する
4. かつ その SHA が新しい `toFetch`（= commits の先頭 400 件）に**含まれない**

4 が成立するのは、先頭にキャッシュ済み commit が並んでいて旧 `toFetch` が
インデックス 400 以降まで届いていたとき。**狭いが、閉じてはいない**。

- **silent に壊れる**: build も lint もテストも緑。症状は「アイドルに戻らない」だけで、
  ユーザーからは何も見えない（バッテリーと CPU だけが減る）

## これは issue 223 と同じ失敗モード

`issues/done/223-bug-glogx-phantom-awaitci-never-gives-up.md` が `awaitCI` で直したのと
**まったく同型**（`spinnerActive()` の 1 項が永久に真 → 80ms ごとの `invalidateLines` が
セッション中回り続ける）。223 の対応は「`awaitCI` ⊆ commits の SHA」に閉じており、
**同じ式に並ぶ兄弟の `detailsLoading` は掃かれていない**。

規範自体は既に repo 内にある: `gitlog_watch.go:handleGitLogProbe` は closed 経路で

> 🚨 飛んでいる測定の札もここで降ろす: 世代を進めた後に届く結果は gen 違いで捨てられるので、
> 降ろさないと measuring が永久に true のまま = 静かに機能停止

と明文化し、`measuring` / `reloading` / `pollArmed` を明示的に降ろしている。
**`tui.go` の epoch ガードだけがこの規律を満たしていない。**

## 推奨対応

不変条件を「**`detailsLoading` ⊆ 現在の取得対象**」とし、世代を進める唯一の choke point で
ある `startCIFetch` に札の解放を持たせる。`startCIFetch` は既に
`fetchEpoch` / `toFetch` / `pendingFetches` / `ghErr` の立て直しを集約している関数なので、
「旧世代が降ろす責任を負っていた札」の解放も同じ場所に置くのが筋。

具体的には `startCIFetch` で、新 `toFetch` に含まれない `detailsLoading` のキーを落とす。

🚨 **呼び出し側（`refetchAfterPush`）に `delete` を足す形は採らない**。次に `startCIFetch` を
呼ぶ経路が増えたときに同じ書き忘れが再発する（227 の「全画面ビューアを個別に列挙しない」と
同じ理由）。

### 回帰テストの向き

現状 `tui_nav_test.go` の `spinnerActive` テーブルは「**立てたら true**」の向きしか持たない。
必要なのは逆向き — 「**非同期の世代交代・キャンセル・閉じるを跨いでも最終的に false へ戻る**」。
上の再現テストがそのまま雛形になる。

さらに横断的には、`spinnerActive()` は OR の 15 項からなり、1 項でも永久に真になると
同じ消耗が起きる（issue 028 P3 と 223 で既に 2 度踏んでいる）。各項について
「真にする経路」と「**偽に戻す全経路**」を対にしたテーブル駆動テストを置くと、
この失敗モードが構造的に閉じる。

## 反証の試み

`tui.go` のコメント・`src/glogx/CLAUDE.md`・`issues/` と `issues/done/`（028 / 223 を確認）を
探したが、「世代不一致のチャンクで札を降ろさないのは意図的」と書いた箇所は無かった。
むしろ `gitlog_watch.go` に逆の規範が明記されている。

## 関連

- `issues/done/223-bug-glogx-phantom-awaitci-never-gives-up.md`（同一の失敗モード。兄弟の掃き漏れ）
- `gitlog_watch.go:handleGitLogProbe`（規範の正本）
