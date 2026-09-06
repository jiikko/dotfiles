# test: 端末インジェクションの回帰テストが、production 到達不能な面だけを見ている

起票日: 2026-09-07
カテゴリ: test
優先度: 中（**glogx 唯一の端末インジェクション回帰テスト**が、守るべき面を守っていない）
出典: /audit dead-code 2026-09-06。production 呼び出し数は私が機械で数え直した

対象: `src/glogx/usage_cache.go`（射程コメント）/ `src/glogx/untrusted_display_test.go`

## 何が食い違っているか

`usage_cache.go` のコメントは、キャッシュに書かれた `Label` の出口をこう名指ししている:

> **codex 枠は Source で拾う**ので、キャッシュに書かれた Label がそのまま
> `RenderLine` / `RenderTableGroups` / `RenderDashboard` の **3 経路**へ出る（敵対レビューが再現）

実測（2026-09-06。`src/glogx` の非テスト Go ファイル）:

| シンボル | production 呼び出し |
|---|---|
| `RenderLine` | **0 件** |
| `RenderTable` | **0 件** |
| `RenderTableGroups` | 2 件（`usage_overlay.go` / `usage/render.go` 内部） |
| `RenderDashboard` | 2 件（`ratelimit_dashboard.go` / `tools/dial-preview`） |

**コメントが名指しした 3 経路のうち 1 本（`RenderLine`）は production に存在しない。**

## なぜ testing カテゴリなのか（documentation ではなく）

問題はコメントの不正確さだけではない。**glogx 唯一の端末インジェクション回帰テスト**
`TestUsageCacheSanitizesCodexLabel` が汚染 `Label` を観測している面は
`RenderLine` と `RenderTable` の **2 つだけ**で、**どちらも production 到達不能**。

つまりこのテストは、
[`mutation-verify-new-tests.md`](../_claude/rules/mutation-verify-new-tests.md) の
**「fixture が退行から不可視な場所にある」**形そのもの。
live の 2 経路（`RenderTableGroups` / `RenderDashboard`）でサニタイズが外れても、
このテストは**緑のまま**通る可能性が高い。

## 併せて直すもの

### ① 入口サニタイズが「出口の数」と「フィールド名」を列挙する形になっている

`usage_cache.go` の該当ブロックは、経路数（「3 経路」）とフィールド名を本文に列挙している。
**経路やフィールドが増えるたびに嘘が再生産される**構造。
[`comment-no-restate-enforced.md`](../_claude/rules/comment-no-restate-enforced.md) の逆で、
**実装が強制していない数をコメントが宣言している**。

### ② `usage.Window.Raw` が write-only

サニタイズ対象外の untrusted 文字列が**永続キャッシュに載り続ける**。
書くだけで誰も読まないなら、載せない方が安全（issue 316 の表にも出る）。

## 推奨対応

1. **テストの観測面を live の 2 経路へ移す**（`RenderTableGroups` / `RenderDashboard`）。
   これが本 issue の中心
2. **変異検証**: `usage_cache.go` のサニタイズを外すと、移した後のテストが red になることを確認する
   （現状の面のままだと red にならない可能性がある。**まず現状で変異を当てて、
   緑のまま通ることを確かめてから**移すと、移した価値が測れる）
3. コメントから**経路数の列挙を消す**。「表示に載る文字列は入口で 1 回通す」という規律だけを残し、
   出口の数え上げは書かない
4. `Window.Raw` を消すか、サニタイズ対象に入れる
5. `RenderLine` / `RenderTable` の扱いは issue 316 の表と同じ commit で決める

## 受け入れ条件

- [ ] 回帰テストが production 到達可能な面を観測している
- [ ] **現状の面で変異が緑のまま通ることを実測してから**移す（移す価値の証明）
- [ ] 移した後、サニタイズを外す変異で red
- [ ] コメントから経路数の列挙が消えている

## 関連

- issue 230（`usage` の codex ラベルがサニタイズされていなかった元の指摘）
- issue 316（`RenderLine` / `RenderTable` / `Window.Raw` の扱い）
- issue 284（サニタイズのテストに positive control が無い、という同族の指摘）
