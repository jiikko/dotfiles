# Review Output Format Template

This is a shared template for consistent review output across all agents.

## Standard Review Structure

```markdown
## [Domain] 分析結果

### 1. サマリー
[1-2文で主要な発見を要約]

### 2. 現状分析
[現在の状態を客観的に記述]

### 3. 発見事項

#### 重大度: 高
- **[Issue Title]**
  - 場所: `filename:line_number`
  - 問題: [問題の説明]
  - 影響: [なぜ問題なのか]
  - 推奨: [具体的な修正案]

#### 重大度: 中
- [同様の形式]

#### 重大度: 低
- [同様の形式]

### 4. 推奨アクション
1. [優先度順のアクション]
2. [次のアクション]

### 5. トレードオフ
| 選択肢 | メリット | デメリット |
|--------|---------|-----------|
| A | ... | ... |
| B | ... | ... |
```

## Severity Levels

| Level | Criteria |
|-------|----------|
| **High** | Security vulnerabilities, data loss risks, critical bugs, breaking changes |
| **Medium** | Performance issues, maintainability concerns, inconsistencies |
| **Low** | Code style, minor improvements, documentation gaps |

## Cross-Review Format

When performing cross-review:

```markdown
### クロスレビュー結果

| 項目 | 判定 | コメント |
|------|------|---------|
| [指摘1] | ✅ 同意 | [理由] |
| [指摘2] | ⚠️ 要検討 | [懸念点] |
| [指摘3] | ❌ 過剰 | [理由] |
| [追加] | 💡 追加指摘 | [新しい発見] |
```

## Usage

In your agent definition:

```markdown
## Review Output Format

See @_common/output-format-template.md for standard structure.

### Domain-Specific Sections
- [Any additional sections for this domain]
```
