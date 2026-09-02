# Forge エージェント定義

この文書は Forge skill で使用するエージェントの共通定義です。

> **⚙️ forge.js との同期**: モード別ロスター・言語別置換・Maximum 追加エージェントは
> `_claude/workflows/forge.js` の `baseRoster()` / `resolveRoster()` が実装している。エージェントを
> 追加・置換したら forge.js も同期更新すること。**「条件付き必須」「追加エージェント（検出パターン）」の
> 判定（async→swift-concurrency-expert 等）は main Claude がこのファイルを読んで `extraAgents` を決め、
> forge.js に渡す**（コード内容を読む判断は skill 層）。プロンプトテンプレートは agentType の
> システムプロンプトに委ねるため forge.js には簡潔版のみ持つ。

---

## 共通出力フォーマット仕様

**重要**: 全エージェントは以下の構造で出力すること。

### 必須セクション

```markdown
## [ドメイン] 分析/レビュー結果

### 概要
[1-2文で主要発見をサマリー]

### 発見事項

#### High: [カテゴリ]
- **[Issue Title]**
  - 場所: `filepath:line_number`
  - 問題: [詳細説明]
  - 影響: [なぜ問題か]
  - 推奨: [修正案]

#### Medium: [カテゴリ]
[同様の形式]

#### Low: [カテゴリ]
[同様の形式]

### 推奨アクション
1. [優先度順アクション]
2. [次のアクション]
```

### 優先度ラベル（統一基準）

| ラベル | 定義 | 対応期限 | 具体例 |
|--------|------|---------|--------|
| **High** | セキュリティ脆弱性、クリティカルバグ、破壊的変更、データロスリスク | 即時 | 強制アンラップ、retain cycle、未処理エラー |
| **Medium** | パフォーマンス問題、保守性懸念、テストカバレッジ不足、一貫性違反 | 計画的 | 不要な再描画、50行超メソッド、命名不統一 |
| **Low** | コードスタイル、軽微な改善、ドキュメント不足 | 任意 | コメント追加、フォーマット調整 |

### ファイル参照形式（統一）

```
場所: `filepath:line_number`

例:
- 場所: `CanvasViewModel.swift:150`
- 場所: `Sources/Models/Element.swift:42-55`（範囲指定）
```

### クロスレビュー時の記号

> **詳細**: `~/.claude/skills/forge/_common/cross-review.md` の「出力形式」セクションを参照

| 記号 | 意味 |
|------|------|
| ✅ | 同意（指摘が妥当） |
| ⚠️ | 要検討（追加の考慮が必要） |
| ❌ | 過剰（指摘が過剰反応） |
| 💡 | 追加指摘（見落とされた問題） |

---

## 並行エージェント統合戦略

> **重要**: 並行エージェント起動**前**に統合戦略を定義すること。これにより、セッション中断時も再開が容易になる。

### 統合戦略の指定（必須）

並行エージェント起動時に、以下を明示する：

```
【統合戦略】
1. 出力形式: JSON | Markdown
2. マージルール: 重複排除方法（同一ファイル+行+カテゴリ）
3. 矛盾解決: 両論併記 | 高 confidence 優先 | 多数決
4. 最終出力: テーブル | リスト | JSON
```

### 構造化 JSON 出力の活用

並行エージェントには以下の JSON 形式での出力を指示する：

```json
{
  "agent": "agent-name",
  "file": "target-file",
  "issues": [
    {
      "line": 42,
      "severity": "high" | "medium" | "low",
      "category": "category-name",
      "description": "問題の説明",
      "suggestion": "修正案"
    }
  ]
}
```

> **詳細**: `~/.claude/skills/forge/_common/cross-review.md` の「構造化出力フォーマット」セクションを参照

### 統合失敗時のリカバリー

| 状況 | 対応 |
|------|------|
| セッション中断 | JSON 出力があれば手動マージ可能 |
| エージェント出力形式不一致 | 統合エージェントが正規化 |
| 矛盾が解決不能 | ユーザーに判断を委ねる |

---

## 必須エージェント（6+1つ）

> **⚠️ ここに書くのはエージェント名と役割だけ**。プロンプト本文は forge v2 (`forge.js`) の汎用 `investigatePrompt()` / `reviewPrompt()` が生成するので、変えたいときは `forge.js` を編集する (かつてここに載せていた per-agent プロンプトは実行時に使われておらず、削除した。履歴は `git log` にある)。実運用で意味を持つのは、この後の「条件付き必須」「追加エージェント（検出パターン）」「言語別置換」テーブル (skill 層が `extraAgents` を決めるのに使う)。「(model: main 継承)」は固定値ではなく、その時のセッションモデルを指す。

以下の6エージェント + swiftui-performance-expert（Phase 4 常時必須）を Standard/Maximum/Ultra モードで使用します。

### 1. swift-language-expert (model: main 継承)

Swift 言語面 (async/await・actor・メモリ管理・エラーハンドリング・protocol/generics)。

### 2. swiftui-macos-designer (model: main 継承)

SwiftUI/macOS の View・State 設計と macOS HIG 準拠。

### 3. research-assistant (model: main 継承) - Phase 1 のみ

公式ドキュメント・ベストプラクティスの調査 (複数ソースのクロスチェックと信頼性評価つき)。

### 4. Explore (model: main 継承)

既存コードベースの横断調査 (関連ファイル・呼び出し箇所の洗い出し)。

### 5. architecture-reviewer (model: main 継承)

レイヤー分離・責務・依存方向のレビュー。

### 6. swiftui-test-expert (model: main 継承)

テスト戦略と XCTest/XCUITest の設計・不安定テストの診断。

### 7. swiftui-performance-expert (model: main 継承) - Phase 4 常時必須

再描画・メモリ・メインスレッドブロックの検出。

---

## 条件付き必須エージェント

| 条件 | エージェント | モデル |
|------|-------------|--------|
| async/await, actor, Task を含む | swift-concurrency-expert | main 継承 |
| ファイル操作、外部入力、API 通信 | security-auditor | main 継承 |

---

## 追加エージェント（タスク/ファイル内容に応じて選択）

### Swift/macOS 関連

| 検出パターン | エージェント |
|-------------|-------------|
| `NSViewRepresentable`, `NSHostingView`, `makeNSView` | appkit-swiftui-integration-expert |
| `Canvas`, `Layer`, `Tool`, エディタ関連 | image-editing-expert |
| `Codable`, `JSONEncoder`, `FileManager`, `SwiftData` | data-persistence-expert |
| `NSStatusItem`, `Keychain`, `SecurityScoped` | macos-system-integration-expert |
| Swift 大規模構造変更 | swift-architecture-designer |

### フロントエンド/デスクトップ関連

| 検出パターン | エージェント |
|-------------|-------------|
| `.css`, `.scss`, `styled-components`, `@media`, `flexbox`, `grid` | css-expert |
| `.js`, `.ts`, `package.json`, `express`, `fastify` | nodejs-expert |
| `electron`, `BrowserWindow`, `ipcMain`, `ipcRenderer`, `electron-vite`, `electron-forge`, `@electron-forge`, `electron-store`, `safeStorage` | electron-expert |

### バックエンド関連

| 検出パターン | エージェント |
|-------------|-------------|
| `.go`, `go.mod`, `goroutine`, `chan` | go-architecture-designer |
| `.rb`, `Gemfile`, `Rails`, `ActiveRecord` | rails-domain-designer |

### リファクタリング関連

| 検出パターン | エージェント |
|-------------|-------------|
| 大規模リファクタリング、Extract Method/Class、Strangler Fig | refactoring-patterns |

---

## Maximum 専用エージェント

Maximum モード選択時は、dependency-analyzer と test-coverage-advisor を**必ず追加で並行起動**する。
refactoring-patterns は条件つき（下記）。

### dependency-analyzer (model: main 継承)

変更の影響半径 (依存関係・循環・波及先) の分析。

### test-coverage-advisor (model: main 継承)

テストの穴と優先順位の提案 (リスクの高い箇所から)。

### refactoring-patterns (model: main 継承) - リファクタ系タスクのみ

**条件**: タスクが「リファクタ」「分割」「抽出」「移動」「整理」を含む場合

安全な構造変更 (Extract Method/Class・Strangler Fig・段階移行) の設計。

---

## 言語別エージェント置換ルール

非 Swift プロジェクトでは、必須エージェント #1-2 を以下の言語別エージェントに置き換える：

| 検出条件 | 置換エージェント | 代替する必須エージェント |
|---------|-----------------|----------------------|
| `.go`, `go.mod` | go-architecture-designer | swift-language-expert |
| `.rb`, `Gemfile`, Rails | rails-domain-designer | swift-language-expert, swiftui-macos-designer |
| `.css`, `.scss`, CSS-in-JS | css-expert | swiftui-macos-designer |
| `.js`, `.ts`, `package.json` | nodejs-expert | swift-language-expert |
| `electron`, `BrowserWindow` | electron-expert | (追加のみ、置換なし) |
