# 291 docs: codex-drive の実装 / fix プロンプトに「production に test 専用の状態・API を足さない」を定型で入れる

## 背景 (obaket 650 M1 / M2、2026-09-05〜06)

codex-drive で codex に実装させると、敵対レビューの「テストが interleaving を強制していない」系の指摘に対して
**production 型に test 専用の同期状態・観測 API を足して**応答する形が 2 回続いた:

- M1 fix3: `TransferPacingLeaseEndState` (production actor) に `duplicateEndCleanupCount` / `duplicateEndWaiters` /
  `waitForDuplicateEndCleanup()` を追加 (duplicate end の到達通知)。r4 で「production に入った test 専用状態」
  として指摘 → fix4 で撤去し、テスト側の bounded yield + actor flag に置き換えた
- M2 r3: `TransferBandwidthSettingsStore` の in-flight gate に対し、レビュー lens 自身が「test 用 barrier を注入」
  を最小修正案として出した (採らず、変異の実測で検知力を判定)

codex は「レビューで指摘された不変条件をテストで固定する」ことを最優先に解くので、production の複雑性を
上げる seam を躊躇なく足す。これは `refuse-low-value-coverage.md` の「テストのために production へ seam /
抽象を足して本番側の複雑性を上げるなら、それはテスト困難の判定材料」と正面から衝突する。

## 提案

`~/.claude/skills/codex-drive/SKILL.md` の `[2]` (codex に実装させる) と fix 系プロンプトの定型に 1 項を足す:

> production 型に **test 専用の状態・観測 API・barrier** を足さない (counter / waiter / 到達通知 / 停止用 seam)。
> interleaving を固定したいときは、テスト側だけで観測できる形 (bounded yield + flag、fake の注入、既存の
> internal 診断 counter) に留め、それでも固定できないなら「pin なし」として commit message に記録する。
> 判断基準: `refuse-low-value-coverage.md` の「テスト困難 × 価値」表

あわせて `[3]` の diff 精読チェックに「production に test 専用の状態が入っていないか (名前に `ForTests` /
`Diagnostics` / `waitFor…` が付く stored property・actor・public/internal API)」を 1 行足す。

## 受け入れ条件

- [ ] SKILL.md の `[2]` プロンプト定型と `[3]` チェック項目に上記が入っている
- [ ] `templates/` に定型があるなら (実装 brief の雛形) そちらにも入れる
- [ ] 実例 (obaket 650 M1 fix3 → fix4) を `rules-rationale/` ではなく SKILL.md 内の実測根拠として 1 行残す
  (SKILL.md は「実測根拠を意図的に残す」方針)

## 関連

- obaket `issues/epic/bandwidth-limit/` (650) の M1 / M2 checkpoint、`tmp/codex-drive-design.650.md` の r4 / r3 の節
- `_claude/rules/refuse-low-value-coverage.md` — seam が本番側の複雑性を上げる場合の扱い
- `_claude/rules/adversarial-review-own-safeguards.md` §7 — 指摘への修正が新しい安全機構になる (test seam も同じ)
