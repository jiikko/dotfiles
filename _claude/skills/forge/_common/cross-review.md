# Forge クロスレビュー仕様

この文書は Phase 1.1 と Phase 4.1 で共通のクロスレビュー仕様です。

## クロスレビューの目的

各エージェントの出力を、**別の観点を持つエージェントが検証**することで、見落としや過剰反応を防ぐ。

## ペアリングルール

### Swift/macOS プロジェクト用

| 元エージェント | レビュー担当 | 代替（不在時） | 検証観点 |
|---------------|-------------|--------------|---------|
| swift-language-expert | architecture-reviewer | - | 言語機能の選択が設計に適合しているか |
| swiftui-macos-designer | swiftui-performance-expert | architecture-reviewer | UI 設計がパフォーマンスに影響しないか |
| architecture-reviewer | swift-language-expert | - | 設計が Swift の言語機能を活かしているか |
| swiftui-test-expert | Explore | - | テスト戦略が既存パターンと整合しているか |
| Explore | swiftui-test-expert | - | 特定した類似コードのテストカバレッジは十分か |
| research-assistant | security-auditor | swift-language-expert | 調査したベストプラクティスにセキュリティ懸念はないか |
| swiftui-performance-expert | swiftui-macos-designer | - | パフォーマンス改善がUX を損なわないか |

### Minimum+ モード用ペアリング（3エージェント間）

Minimum+ モードでは3エージェントのみのため、以下の専用ペアリングを使用:

| 元エージェント | レビュー担当 | 検証観点 |
|---------------|-------------|---------|
| swift-language-expert | architecture-reviewer | 言語機能の選択が設計に適合しているか |
| architecture-reviewer | swift-language-expert | 設計が Swift の言語機能を活かしているか |
| swiftui-test-expert | architecture-reviewer | テスト戦略がアーキテクチャと整合しているか |

> **注意**: Standard 以上では swiftui-test-expert のレビューは Explore が担当するが、Minimum+ では Explore が不在のため architecture-reviewer が代替する。

### フロントエンド/デスクトップ用

| 元エージェント | レビュー担当 | 検証観点 |
|---------------|-------------|---------|
| css-expert | nodejs-expert | CSS の指摘がビルド設定と整合しているか |
| nodejs-expert | security-auditor | Node.js の指摘がセキュリティを考慮しているか |
| electron-expert | nodejs-expert | Electron の指摘が Node.js パターンと整合しているか |
| electron-expert (フレームワーク統合) | css-expert | フレームワーク統合がスタイリング/CSP と整合しているか |
| electron-expert (データ永続化) | security-auditor | ストレージのセキュリティ（暗号化、パス検証）が適切か |

### バックエンド用

| 元エージェント | レビュー担当 | 検証観点 |
|---------------|-------------|---------|
| go-architecture-designer | security-auditor | Go の指摘がセキュリティを考慮しているか |
| rails-domain-designer | security-auditor | Rails の指摘がセキュリティを考慮しているか |

### 共通

| 元エージェント | レビュー担当 | 検証観点 |
|---------------|-------------|---------|
| refactoring-patterns | architecture-reviewer | リファクタリング案がアーキテクチャと整合しているか |
| swift-architecture-designer | swift-language-expert | 構造変更が言語制約を考慮しているか |

---

## クロスレビュー プロンプトテンプレート

### Phase 1.1 用（事前調査のクロスレビュー）

```
以下は [元エージェント名] の調査結果です。
[観点] の観点から検証し、以下を報告してください。

検証対象:
[元エージェントの出力全文]

検証項目:
1. 事実の正確性: 記載内容に誤りはないか
2. 見落とし: 重要な考慮点が漏れていないか
3. リスク: 提案された方針に潜在的なリスクはないか
4. 補足: 追加で考慮すべき点はあるか

出力形式:
- ✅ 検証OK: [問題なしの項目]
- ⚠️ 要注意: [注意が必要な項目と理由]
- ❌ 要修正: [修正が必要な項目と修正案]
- 💡 補足: [追加の考慮点]
```

### Phase 4.1 用（レビュー結果のクロスレビュー）

```
以下は [元エージェント名] のコードレビュー結果です。
[観点] の観点から検証し、以下を報告してください。

レビュー対象ファイル: [ファイルパス]

元レビュー結果:
[元エージェントの出力全文]

検証項目:
1. 指摘の妥当性: 各指摘は正当か、過剰反応ではないか
2. 見落とし: 重要な問題が見落とされていないか
3. 修正案の適切性: 提案された修正は副作用を起こさないか
4. 優先度の妥当性: 重要度の判断は適切か

出力形式:
- ✅ 同意: [妥当と判断した指摘]
- ⚠️ 要検討: [追加の考慮が必要な指摘と理由]
- ❌ 過剰: [過剰反応と判断した指摘と理由]
- 💡 追加指摘: [元レビューが見落とした問題]
```

---

## 統合エージェント プロンプトテンプレート

### 共通統合ルール

```
【統合ルール】
1. 重複する情報/指摘を排除
2. クロスレビューで「⚠️ 要注意/要検討」とされた項目には注釈を追加
3. クロスレビューで「❌ 要修正/過剰」とされた項目は除外（理由と共に記録）
4. 「💡 補足/追加指摘」を統合結果に含める
5. 各情報の出典（エージェント名）を保持
6. 優先度順にソート（High → Medium → Low）
```

### Phase 1.1 統合用

```
以下は Phase 1 の調査結果と、それぞれのクロスレビュー結果です。
これらを統合し、メイン Claude に報告するための最終結果を作成してください。

【統合対象】
[各エージェントの出力 + クロスレビュー結果]

【出力形式】
## 統合済み調査結果

### 1. 実装方針（合意済み）
[全エージェントが合意した方針]

### 2. 要注意事項
[クロスレビューで指摘された注意点]

### 3. 参考にする類似コード
[Explore + クロスレビューで検証済み]

### 4. 未解決の矛盾
[エージェント間で見解が分かれた点]
```

### Phase 4.2 統合用

```
以下は Phase 4 のレビュー結果と、Phase 4.1 のクロスレビュー結果です。
これらを統合し、メイン Claude に報告するための最終結果を作成してください。

【統合対象】
[各エージェントのレビュー出力 + クロスレビュー結果]

【出力形式】
## 統合済みレビュー結果

### 🔴 High Priority（即時対応）
| # | 指摘内容 | ファイル:行 | 指摘元 | クロスレビュー |
|---|---------|------------|--------|--------------|
| 1 | [内容] | [パス:行] | [エージェント] | ✅ 同意 |

### 🟡 Medium Priority（推奨）
[同様のテーブル]

### 🟢 Low Priority（任意）
[同様のテーブル]

### ⏸️ 除外された指摘（過剰と判断）
| # | 元の指摘 | 除外理由 | 判断者 |
|---|---------|---------|--------|

### ⚠️ 要検討（エージェント間で意見が分かれた）
| # | 指摘内容 | 賛成 | 反対 | 論点 |
|---|---------|-----|------|------|
```

---

## 重複排除の基準

```
同じ指摘とみなす条件:
1. 同じファイル・同じ行番号
2. AND 同じ問題カテゴリ（例: retain cycle, performance, etc.）
```

## 矛盾の解決

```
矛盾がある場合の処理:
1. 両方の見解を「⚠️ 要検討」セクションに記載
2. 各エージェントの根拠を併記
3. メイン Claude はユーザーに判断を委ねる（独自判断しない）
```

---

## 構造化出力フォーマット（並行エージェント統合用）

並行エージェント実行時、各エージェントは以下の JSON 形式で出力することで、統合を決定論的に行える。

### 目的

- 統合処理を機械的・決定論的に実行
- セッション中断時の再開を容易に
- 重複排除と矛盾検出を自動化

### 標準 JSON スキーマ

```json
{
  "agent": "agent-name",
  "phase": "1" | "4",
  "timestamp": "2025-02-05T12:00:00Z",
  "target": "filepath or task description",
  "summary": "1-2 文のサマリー",
  "issues": [
    {
      "id": "unique-issue-id",
      "line": 42,
      "severity": "high" | "medium" | "low",
      "category": "category-name",
      "description": "問題の詳細説明",
      "suggestion": "具体的な修正案",
      "confidence": 0.95,
      "references": ["filepath:line", "url"]
    }
  ],
  "recommendations": [
    {
      "priority": 1,
      "action": "アクションの説明",
      "rationale": "理由"
    }
  ],
  "cross_review_notes": {
    "reviewed_by": "reviewer-agent-name",
    "verdict": "agree" | "needs_discussion" | "disagree",
    "comments": "追加コメント"
  }
}
```

### カテゴリ一覧（統一）

| カテゴリ | 説明 |
|---------|------|
| `security` | セキュリティ脆弱性 |
| `performance` | パフォーマンス問題 |
| `accessibility` | アクセシビリティ（WCAG 等） |
| `architecture` | 設計・アーキテクチャ |
| `maintainability` | 保守性・可読性 |
| `correctness` | ロジックエラー |
| `consistency` | 既存コードとの一貫性 |
| `testing` | テスト関連 |
| `documentation` | ドキュメント不足 |

### 統合時のマージルール

```
1. 同一 issue の判定:
   - same file + same line + same category → 重複とみなす
   - 重複時は severity が高い方を採用

2. 矛盾の判定:
   - 同一箇所で異なる recommendation → 矛盾としてマーク

3. 優先度ソート:
   - high (severity) → confidence → line number
```

### プロンプトへの組み込み例

```
並行エージェント実行時は、以下の JSON 形式で出力してください：

{
  "agent": "your-agent-name",
  "file": "target-file-path",
  "issues": [
    {
      "line": number,
      "severity": "high" | "medium" | "low",
      "category": "category-name",
      "description": "問題の説明",
      "suggestion": "修正案"
    }
  ]
}

これにより統合処理が機械的に行えます。
```
