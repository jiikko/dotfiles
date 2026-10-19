# 837 (bug): 未 push の判定を表示用の CI 状態から取るので、GitHub の repo が解決できないときと path filter のときに b が push を断る (P3)

> 🚨 **担当中: air5 (glogx)**（2026-10-19〜）

起票日: 2026-10-19

## 概要

`src/glogx/main.go` の `mergePlan` は、`!plan.hasRepo` のとき全コミットを `gitdata.StateNone` にし、`UnpushedSHAs` (`rev-list --not --remotes`) で取った未 push の結果を捨てる。
`tui.go` の `confirmPush` / `unpushedCount` は表示用の statuses から未 push を数えるので、0 件になって `b` が push を断る。
根は「push してよいか・何件か」を、表示用の CI の状態に依存させていること。

## 詳細

### 1. GitHub の repo が解決できない repo で `b` が断る

- `hasRepo=false` は「remote が GitHub でない」と同じではない: `internal/gitdata/github.go` の `ResolveRepo` は、upstream が GitHub でなければ GitHub の origin へ戻る。発火条件は「その手順で GitHub の repo が得られない」(GitHub を持たない repo)
- README は GitHub 以外のホスティングを**未対応**と明記し、そのとき CI 欄を ⬜ にすると書いている。⬜ の表示自体は既知の設計。残る問題は 2 つ: `b` を押すと「未 push のコミットはありません」で断る (`git push` 自体は通る) / 状態記号の表の ⬜ の説明「push 済みだが Check が存在しない」と食い違う
- 裏取り (監査の中で実施): `mergePlan(repoPlan{hasRepo:false, unpushed:{sha0,sha1}})` の後に `b` を押すテストで `status=none`・`pushConfirm=false`、描いた一覧は全コミットが ⬜

### 2. path filter・`-n` で件数が欠ける (GitHub の repo でも起きる)

- 未 push の件数は画面に出ているコミットだけを数える。`glogx -- <path>` でその path を触る未 push が無いと `b` は断る。未 push が表示の上限より多いと確認の件数が少なく出る
- コードを読んで確認した (描いていない)

## 対応方針

- 表示用の CI の状態と、「`git push` で何が送られるか」(可否・件数) を分ける。`UnpushedSHAs` の結果を `mergePlan` で捨てず、push の判定はそちらから取る
- 件数を `@{u}..HEAD` に置き換えるだけでは足りない (upstream が未設定・push 先と upstream が違う設定を扱えない。codex の反証レビューの指摘)。`git push` が実際に送る先と同じ解決をする
- GitHub を解決できない repo では ⬜ と別の記号にするか、表の説明を直す。`o` / `y` を hint から外す
- 2 は 1 と別に直してよい (GitHub の repo でも起きるので、こちらの方が当たる人は多い)

## 関連ファイル

- `src/glogx/main.go` (`mergePlan`)
- `src/glogx/tui.go` (`confirmPush` / `unpushedCount`)
- `src/glogx/internal/gitdata/github.go` (`ResolveRepo`)、`src/glogx/internal/gitdata/gitlog.go` (`UnpushedSHAs`)
- `src/glogx/README.md` (状態記号の表・未対応の一覧)

## 進捗

- [ ] 1 push の判定を表示用の状態から切り離す
- [ ] 2 件数を表示中のコミットに依存させない
