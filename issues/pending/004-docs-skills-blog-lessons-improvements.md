# Skills 改善（ブログ「Lessons from building Claude Code: How we use Skills」観点）

調査日: 2026-06-06
出典: https://claude.com/blog/lessons-from-building-claude-code-how-we-use-skills
対象: `_claude/skills/` 配下の既存スキル

既存の `issues/001-docs-claude-skills-improvements.md`（Forge 監査ベース：God File・DRY 等の内部構造）とは観点が異なる。
こちらはブログの教訓（description / Gotchas / 段階的開示 / コード合成 / メモリ / オンデマンド hook）に基づく。

---

## ✅ 本 PR で対応済み

### 1. description が「モデルの起動判断」用に書かれていない（教訓: descriptions for the model's decision-making）
トリガー語の有無がスキル間でバラバラだった。`cross-review` を手本に、発火フレーズ（日英）を全対象に追記。

- `c`: 名前が1文字 + トリガー語ゼロ → 「コミットして」「commit」「/c」を追記
- `codex-review`: cross-review と「レビューして」で競合するのに発火条件なし → 「codexでレビューして」等＋「単独レビュー用途」を明記
- `review-loop`: 「レビューループ」「make review で回して」等を追記
- `issue-sync`: 「issueを整理して」「完了issueを片付けて」等を追記
- `perf-analysis`: 「パフォーマンス分析」等＋「ThumbnailThumb 専用」を明記
- `smoke-test`: 「スモークテスト」「動作確認して」等＋「ThumbnailThumb 専用」を明記

### 2. Gotchas（落とし穴）セクションの新設（教訓: 最も価値が高いのは edge case / 失敗例）
- `c`: `## 落とし穴` を新設（add -A の巻き込み・クレデンシャル混入・dirty サブモジュール・stash 禁止・既存ステージ済み変更）
- `review-loop`: `## 落とし穴` を新設し、埋もれていた「master 誤 push」を筆頭に、誤スレッド返信・自コメント誤検出による無限ループ・定期ジョブ消し忘れ等を集約

### 3. 自明な記述のトリム（教訓: Don't state the obvious）
- `c`: モデルが既知の git 手順（status→diff→stage→commit の逐次説明）を圧縮し、価値のある「ルール」と「落とし穴」に比重を移した

---

## ⏳ Phase 2（設計判断を伴うため別 PR で検討）

### 4. コードで合成する（教訓: Compose with code）
- `perf-analysis` / `smoke-test`: 長い `bin/tt-client` 手順が inline。`scripts/load-test.sh` 等に切り出し、SKILL.md はオーケストレーション指示のみにする。
  - 保留理由: PROJECT_ID / CANVAS_ID 等の実行時プレースホルダの引数設計が必要で、bin/tt-client 無しでは検証できない。`ios-simulator-skill`（21 scripts 同梱）が手本。

### 5. 段階的開示（教訓: Progressive disclosure）
- `smoke-test`（クイック/標準/完全の全テストカタログが inline）/ `crash-log-analyzer`（299行）: 詳細マトリクスを `reference/*.md` に分離し、必要なモード時のみ参照させる。

### 6. オンデマンド hook（教訓: Use on-demand hooks like `/careful`）
- `review-loop`: スキル起動中だけ master への push をブロックする hook を仕込む（最大リスクの機械的防止）。
- `c`: `.env` / クレデンシャル混入をコミット前に機械ブロックする hook。
  - 保留理由: hook の配布・有効化の設計（settings.json との関係）を決める必要がある。

### 7. 永続メモリ（教訓: Store memory in ${CLAUDE_PLUGIN_DATA}）
- `review-loop` / `issue-sync`: セッションをまたいだ記憶がなく毎回ゼロから再判定。「前回どのコメント/issue を処理済みか」を append-only ログ / JSON 化する。
  - 参考: `audit` は既に「実行ログで重複実行防止」を実装済み（ブログの精神に合致。ただしプロジェクトローカル）。

### 8. プロジェクト固有スキルのスコープ（既存 issue #13 と重複）
- `perf-analysis` / `smoke-test` は ThumbnailThumb 専用なのにグローバル配置。各プロジェクトの `./.claude/skills` への移動を検討。
