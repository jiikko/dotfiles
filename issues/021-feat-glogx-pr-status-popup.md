# 021 feat(glogx): PR 状態ポップアップ (`P`)

## 背景

PR バッジ (`#123` の色) で open/merged/closed は分かるが、「マージできる状態か」(レビュー承認・conflict・CI) はブラウザを開かないと分からない。PR 番号は一括 GraphQL (`associatedPullRequests`) で既に取れており、フィールドを足すだけでデータは揃う (2026-07-23 ユーザー承認)。

## やること

- コミット一覧で `P` (大文字): カーソル位置コミットの PR 状態ポップアップを開く (小文字 `p` = ブラウザで開く、は現状維持)
- GraphQL の `associatedPullRequests` ノードにフィールド追加:
  - `title` / `isDraft` / `reviewDecision` (APPROVED / CHANGES_REQUESTED / REVIEW_REQUIRED / null) / `mergeable` (MERGEABLE / CONFLICTING / UNKNOWN) / `baseRefName` / `headRefName`
- 表示は job パネルと同じ overlayBox 流儀 (コミット直下に重ねる)。内容例:

  ```text
  ┌ PR #123: トーストの出入りを横スライドにする ───┐
  │ OPEN (draft ではない)  feature/x → master      │
  │ レビュー: ✓ APPROVED                           │
  │ conflict: なし (MERGEABLE)                     │
  │ CI: ✗ 1 job 失敗 (コミット側の表示と同じ出典)  │
  └────────────────────────────────────────────────┘
  ```

- PR なしコミットでは notice (「紐づく PR はありません」= p と同じ文言)
- 閉じる操作は他ポップアップと揃える (`q`/`h`/`Esc`/`P` toggle)。`o`/`y` で PR URL を開く/コピー
- キャッシュ: `prCache` を拡張するとファイルキャッシュ (cache.go) の PR 形式に波及するため、詳細フィールドは **セッション内メモリキャッシュのみ** とし、既存 `PRRef` (number/url/state) は変えない。詳細は `P` 押下時にオンデマンド単発 GraphQL で取る方が影響半径が小さい (一括クエリの肥大も避けられる)

## 注意

- `mergeable` は GitHub 側の遅延計算で UNKNOWN が返ることがある → 「計算中」と表示 (リトライまではしない)
- reviewDecision はブランチ保護設定が無い repo では null → 「(レビュー必須ではない)」表示

## 関連

- 019/020 より優先度低 (3 番手)。UI (新オーバーレイ 1 枚) の分だけ実装が重い
