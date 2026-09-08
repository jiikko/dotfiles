# 326 docs: codex-drive の実装 / fix プロンプトに「production に test 専用の状態・API を足さない」を定型で入れる

> 🚨 **旧番号 291 から改番** (2026-09-07)。`issues/done/291-feat-glogx-epic-shows-done-children-by-default.md`
> と番号が衝突し、`test_issue_numbers_unique.sh` が赤になっていた。参照を数えて**少ない方**を動かした:
> こちら = tracked 参照 0 件 / commit 参照は起票の `6c53b7a8` のみ、epic 側 = tracked 2 件
> (`done/293` `done/294`) + 敵対レビューの commit 複数 (`e1c2c6ee` `e67eab07`)。
> commit message は履歴なので直せない — 旧 commit の「291」はこのファイルを指すことがある。


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

- [x] SKILL.md の `[2]` プロンプト定型 (`## 制約`) と `[3]` の diff 精読チェックに上記が入っている
      (commit `docs(326): codex-drive のプロンプトに「production に test 専用の状態を足さない」を入れる`)
- [x] `templates/` — **該当なし**。中身は `merger.md` (並列出力の集約者) と
      `review-lens-header.md` (レビュー lens 共通ヘッダ) の 2 本だけで、実装 brief の雛形は
      SKILL.md 本文の `## 制約` にインラインで書かれている
- [x] 実例 (obaket 650 M1 fix3 → fix4) を SKILL.md 内の実測根拠として残した (`[3]` 側)
- [x] fix 系プロンプト — **`[2]` の定型を使い回す形**なので同じ追記でカバーされる
      (SKILL.md「修正して `[2]` に戻ったときの再開地点」)。fix 専用のプロンプト定型は存在しない
- [x] 姉妹 skill への横展開は不要と確認: `## 制約` の実装プロンプト定型を持つのは
      codex-drive だけ (codex-lead は実装が Claude、forge は専門家エージェント)

## 関連

- obaket `issues/epic/bandwidth-limit/` (650) の M1 / M2 checkpoint、`tmp/codex-drive-design.650.md` の r4 / r3 の節
- `_claude/rules/refuse-low-value-coverage.md` — seam が本番側の複雑性を上げる場合の扱い
- `_claude/rules/adversarial-review-own-safeguards.md` §7 — 指摘への修正が新しい安全機構になる (test seam も同じ)
