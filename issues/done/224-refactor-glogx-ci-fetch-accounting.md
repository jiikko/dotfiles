# CI 取得の会計に単一の所有者が無い — `fetching` は派生値なのに 4 箇所で手書き同期している

出典: audit (responsibility) 2026-09-03 / forge-Minimum。**中核 (`fetching` の派生性と手書き箇所) は実測済み**。

## 一番はっきりしている形: `fetching` は `pendingFetches > 0` の派生値

`browseModel.fetching` は独立した状態ではなく、`pendingFetches` から**導出できる値**でしかない。
実際 `tui.go` の中でそう書いている箇所がある:

```go
m.fetching = m.pendingFetches > 0   // ciResultMsg の着弾処理
```

にもかかわらず、**4 箇所で手書きの代入**をして同期を保っている:

| 箇所 (`src/glogx/tui.go`) | 代入 |
|---|---|
| `newBrowseModel` | `fetching: len(toFetch) > 0` |
| `startCIFetch` | `m.fetching = true` |
| `ciResultMsg` の着弾 | `m.fetching = m.pendingFetches > 0` |
| `applyLogData` の取得なし分岐 | `m.fetching = false` |

さらに**同じ不変条件をコメントが 3 箇所で再掲している** (`fetching = pendingFetches > 0` を保て、の主旨)。
「実装で強制できていないから、人が読む場所に書いて守らせている」状態そのもの
(`~/.claude/rules/comment-no-restate-enforced.md` の裏返し: 強制できるなら再掲は要らない)。

**発火条件**: 新しく「一括取得を始める / 終える」経路を足したとき、代入を 1 つ書き忘れる。
`fetching` を見ているのはスピナー (`spinnerActive`)・再描画ゲート (`tickMsg`)・
パネルの「進行中の一括取得を待つ」ガード・`ciPollMsg` の並行抑止 — **どれも silent に狂う**
(スピナーが回りっぱなし / 逆にパネルが待たずに二重取得する)。build もテストも通る。

### 最小の修正 (これ単独で 1 commit にできる)

**`fetching` フィールドを削除し、`func (m *browseModel) fetching() bool { return m.pendingFetches > 0 }`
に置き換える。** 派生をメソッドにすれば、同期漏れという失敗モードが**構造的に消える**。
再掲コメント 3 箇所もそのとき削除する (強制されるものはコメントに書かない)。

複雑性が下がったことは行数ではなく **「不変条件を手で書く箇所が 4 → 0」** で示せる。

## 背景: 会計の所有者が不在

CI の取得・追従サブシステムは、フィールドが `browseModel` に平置きされ、`tui.go` 全体から触られている
(参照数の実測: `statuses` 19 / `details` 20 / `awaitCI` 13 / `fetching` 13 / `toFetch` 11 /
`detailsLoading` 11 / `ciPollGen` 5 / `pendingFetches` 5 / `prCache` 5 / `fetchEpoch` 4 /
`awaitAttempts` 4 / `ciPolling` 4 / `prBusy` 4 / `ciPollInFlight` 3)。

変更理由が 5 つに割れている:

1. 一括取得の分割と世代管理 (`startCIFetch`)
2. チャンク着弾の会計 (`ciResultMsg` の case。**40 行が Update の switch にインライン**)
3. 追従ポーリングの single-flight / 世代 / 打ち切り (`ciPollMsg`)
4. パネル詳細・ETA basis のオンデマンド取得
5. 終了時 `SaveCache` 用の `fetched` 蓄積

**抽象度が不統一**なのが目に見える形: 取得の**開始**は `startCIFetch` という名前と契約を持つのに、
**着弾**は 590 行の switch の中に 40 行べた書きされている (他の case は 1〜3 行の委譲)。

### 構造の方向 (実需要が来たときに)

会計だけを閉じた型へ出す:

```go
type ciFetch struct { toFetch []string; pending int; epoch int }
func (f *ciFetch) begin(shas []string) [][]string
func (f *ciFetch) accept(epoch int) bool
func (f *ciFetch) settleChunk(shas []string)
func (f *ciFetch) busy() bool          // ← fetching フィールドは消える
```

`ciResultMsg` の 40 行は `settleChunk` へ移し、Update を他の case と同じ委譲に揃える。

⚠️ **orchestration は移さない**。`fetchPanelDetails` / `maybeFetchETABasis` / `ciPollTargets` は
commits / panel と構造的に結合しており、`job_detail_overlay.go` 冒頭が
「panel-frame と ETA・CI 取得は details/statuses/commits と構造的に結合するため browseModel に残す」
と**既に判断を明記している**。移すのは**会計**であって orchestration ではない (この既存判断と矛盾しない)。

⚠️ **型を作る部分は投機的にやらない**。`~/.claude/rules/verify-design-intent-before-refactor.md` の
「実変更 trigger 待ち」に従い、**次に CI 取得の経路を触る変更が来たとき**に着手する。
`fetching` の削除だけは trigger 不要 (単独で失敗モードが 1 つ消えるため)。

## 探して見つからなかった範囲 (2026-09-03)

`fetching` / `pendingFetches` / `startCIFetch` / `ciPoll` を issues 全体 (open + next + pending + done)
で grep したが、**CI 取得の会計を扱った既存 issue は無い** (統合先なし)。

⚠️ 近傍で**却下済み**の指摘があるので再提案しないこと:
issue [071](071-research-design-audit-2026-08-20.md) の「`actionModal` が相互排他な UI 状態を
7 本の bool で持つ」は、**issue 074 が同じ `action_modal.go` を map 化する予定**という理由で
切り出さないと決まっている。本 issue の `fetching` は `action_modal.go` ではなく
`browseModel` の CI 会計なので別件だが、「bool を型へ寄せる」提案として混同しないこと。

## 関連

- issue 223 (`awaitCI` の phantom。本件の「所有者不在」の帰結の 1 つ)
- issue 227 (同じ「単一の概念が手書きで散る」形。あちらは全画面ビューア)
- issue [085](085-refactor-glogx-chrome-composition-dup.md) — 「doc が一本化を主張しているのに
  実体は N コピー」を実際に寄せた前例 (変異検証の形も同じ)

## 対応 (2026-09-04。`7fcc8a56`)

**最小の修正だけを実施した。** `ciFetch` 型への切り出しは本文の指示どおり**やっていない**
(実変更 trigger 待ち。次に CI 取得の経路を触る変更が来たときに着手する)。

フィールド `fetching` を消し、`func (m *browseModel) fetching() bool { return m.pendingFetches > 0 }`
に置き換えた。**不変条件を手で書く箇所が 4 → 0。** 再掲していたコメント 3 箇所も整理した。

### 🚨 起票時の前提に 1 つ誤りがあった — 4 箇所の代入は全部が等価ではない

`newBrowseModel` の `fetching: len(toFetch) > 0` **だけは意味が違う**。この値を読む
`if m.fetching {` は**その下のチャンク分割より前**にあり、`pendingFetches` はまだ 0。
つまりここは「取得中か」ではなく **「これから取得を始めるか」** の判定で、
issue の指示どおり素朴に派生へ置き換えると**起動時の一括取得が丸ごと走らなくなる**。

元の式 (`len(toFetch) > 0`) へ戻し、次に触る人が踏まないよう理由をコメントで残した。
変異 M2 (ここを派生へ戻す) で red 3 件を確認しているので、この落とし穴は今後テストが止める。

### テスト側も派生を直接立てていた

6 箇所で `m.fetching` に代入していた (= 手書き同期のテスト版)。派生元の `pendingFetches`
を立てる形へ直した。これも「同じ不変条件を 2 箇所で書く」の一形態。

### 変異 3 本

| 変異 | 結果 |
|---|---|
| 派生を反転する (`pendingFetches == 0` を取得中とする) | red 3 件 |
| コンストラクタの条件を派生へ戻す (上の落とし穴) | red 3 件 |
| 着弾時の `pendingFetches` 減算を落とす | red 2 件 |

⚠️ 復元で 1 度失敗した。worktree で `git checkout -- tui.go` を使ったが、実装は未コミット
なので **HEAD の古い版**に戻り、以降の変異が「ビルド不能」になって測れていなかった
(第 3 の結果を red と読み違えるところだった)。控えを 1 世代取って `cp` で戻す形へ直した
([`mutation-verify-new-tests.md`](../../_claude/rules/mutation-verify-new-tests.md) の「復元の作法」)。

`make test` rc=0。

## 残した作業 (この issue では扱わない)

- `ciFetch` 型への切り出しと `ciResultMsg` の 40 行の移動 — **実変更 trigger 待ち**
- 会計以外 (orchestration) は移さない判断は `job_detail_overlay.go` の既存記述のまま

⚠️ **敵対的レビューは未実施。** 派生への置き換えは失敗モードを 1 つ消す方向の変更で、
新しい安全機構を作ったわけではないが、判断ロジック (スピナー・ガード・並行抑止の出典) が
動いているので、通す価値はある。
