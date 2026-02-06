# Review Team

複数の専門家が並行してコードレビューを行うチーム。forge の Phase 4 に相当する機能を Team Agents で実現する。

## チーム構成

### teammate: code-quality-reviewer
- **役割**: コード品質・可読性・保守性の観点からレビュー
- **使用エージェント**: code-reviewer
- **モデル**: opus

### teammate: architecture-reviewer
- **役割**: アーキテクチャ・設計パターン・責務分離の観点からレビュー
- **使用エージェント**: architecture-reviewer
- **モデル**: opus

### teammate: security-reviewer
- **役割**: セキュリティ脆弱性・データ漏洩リスクの観点からレビュー
- **使用エージェント**: security-auditor
- **モデル**: opus

## 使い方

```
チームを作成して、以下のファイルをレビューしてください:
- code-quality-reviewer: コード品質の観点からレビュー
- architecture-reviewer: 設計の観点からレビュー
- security-reviewer: セキュリティの観点からレビュー
対象: [ファイルパス]
```

## 統合戦略

各チームメイトのレビュー結果は lead が統合し、以下の形式で報告する:

```
## 統合レビュー結果

### High（即時対応）
[全チームメイトからの High 指摘を統合]

### Medium（計画的対応）
[全チームメイトからの Medium 指摘を統合]

### Low（任意）
[全チームメイトからの Low 指摘を統合]

### 総評
[全体的な評価と推奨アクション]
```
