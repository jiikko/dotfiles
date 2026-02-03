# Phase 4〜4.2: 専門家レビュー・クロスレビュー・統合

## Phase 4: 専門家レビュー（両モード共通）

> **Minimum モードの場合**: 3エージェントのみを**直列実行**する。Phase 4.1/4.2 は省略。
> **Maximum Sequential モードの場合**: 全エージェントを**直列実行**する。

### ファイル内容に基づくエージェント選択

**重要**: ファイルパスのパターンマッチだけでなく、**実際のファイル内容を読み取って**適切なエージェントを選択する。

| 検出パターン | 追加エージェント |
|-------------|-----------------|
| `NSViewRepresentable`, `NSHostingView`, `makeNSView` | appkit-swiftui-integration-expert |
| `Canvas`, `Layer`, `Tool`, エディタ関連 | image-editing-expert |
| `Codable`, `JSONEncoder`, `FileManager`, `SwiftData` | data-persistence-expert |
| `URLSession`, `FileHandle`, ユーザー入力処理 | security-auditor |
| `NSStatusItem`, `Keychain`, `SecurityScoped` | macos-system-integration-expert |
| `@State`, `@Observable`, View 構造体 | swiftui-macos-designer |
| `async`, `await`, `actor`, `Task` | swift-concurrency-expert |

### 必須エージェント（6つ）

```
1. swift-language-expert (model: opus)
   prompt: "以下のコードを Swift 言語の観点からレビューしてください。
   - async/await, actor の使い方
   - メモリ管理（retain cycle, weak/unowned）
   - エラーハンドリング
   - プロトコル/ジェネリクスの設計

   **プロジェクト固有ルール（CLAUDE.md より）**:
   - 強制アンラップ (force unwrap) は禁止
   - `canvases[0]` 等の直接アクセス禁止（`.first` を使用）
   - `@unchecked Sendable` 使用時は安全性の理由をコメントで説明
   - ImageRenderer, NSHostingView はメインスレッドでのみ使用可

   対象: [ファイルパス]
   問題点と改善案を箇条書きで報告してください。"

2. swiftui-macos-designer (model: opus)
   prompt: "以下のコードを SwiftUI/macOS の観点からレビューしてください。
   - State管理（@State, @StateObject, @Observable）
   - View の再描画パフォーマンス
   - macOS HIG 準拠
   - NSViewRepresentable の使い方

   **プロジェクト固有ルール（CLAUDE.md より）**:
   - `@State` プロパティは private にすべき
   - `@ObservedObject` の直接初期化禁止（`@StateObject` を使用）
   - `.id(UUID())` 禁止（安定した識別子を使用）

   対象: [ファイルパス]
   問題点と改善案を箇条書きで報告してください。"

3. swiftui-test-expert (model: opus)
   prompt: "以下のコードをテスト戦略の観点から**徹底的に**レビューしてください。
   - テストカバレッジ戦略
   - リグレッションテスト
   - 非同期処理のテストパターン
   - **エッジケースの網羅的検討**
   - **フレーキーテストのリスク分析**
   対象: [ファイルパス]
   問題点に対するテスト戦略を箇条書きで報告してください。"

4. architecture-reviewer (model: opus)
   prompt: "以下のコードをアーキテクチャ観点から**徹底的に**レビューしてください。
   - レイヤー配置の適切性
   - 依存方向
   - 責務分離
   - テスタビリティ
   - **将来的な拡張性**
   対象: [ファイルパス]
   問題点と改善案を箇条書きで報告してください。"

5. Explore (model: opus)
   prompt: "以下のコードに関連する既存コードを**徹底的に**調査してください。
   - 類似機能の実装箇所
   - 影響を受けるファイル
   - 既存のテストパターン
   - **潜在的な影響範囲の深掘り**
   対象: [ファイルパス]
   関連ファイルと影響範囲を報告してください。"

6. swiftui-performance-expert (model: opus) ★常時必須
   prompt: "以下のコードのパフォーマンスをレビューしてください。
   - 不要な再描画
   - メモリ使用量
   - 重い処理のメインスレッドブロック
   対象: [ファイルパス]
   パフォーマンス上の問題点を報告してください。"
```

### 条件付き必須エージェント

| 条件 | 追加エージェント | モデル |
|------|-----------------|--------|
| async/await, actor, Task を含む | swift-concurrency-expert | opus |
| ファイル操作、外部入力、API 通信 | security-auditor | opus |

### Maximum 専用エージェント（Phase 4）

```
7. dependency-analyzer (model: opus)
   prompt: "以下のコードの依存関係を分析し、レビュー観点を提供してください。
   - ファイル間の結合度評価
   - 循環依存の検出
   - 変更が他のファイルに与える影響
   対象: [ファイルパス]
   依存関係の問題点を報告してください。"

8. test-coverage-advisor (model: opus)
   prompt: "以下のコードのテストカバレッジをレビューしてください。
   - テストギャップの特定
   - 追加すべきテストケース
   - リグレッションリスクの評価
   対象: [ファイルパス]
   テストに関する推奨事項を報告してください。"
```

---

## Phase 4.1: クロスレビュー（両モード共通）

Phase 4 の各エージェント出力を、**別の観点を持つエージェントが検証**する。

### クロスレビューのペアリング

| 元エージェント | レビュー担当 | 検証観点 |
|---------------|-------------|---------|
| swift-language-expert | architecture-reviewer | 言語の指摘が設計と整合しているか |
| swiftui-macos-designer | swiftui-performance-expert | UI の指摘がパフォーマンスを考慮しているか |
| architecture-reviewer | swift-language-expert | 設計の指摘が言語制約を考慮しているか |
| swiftui-performance-expert | swiftui-macos-designer | パフォーマンス改善がUX を損なわないか |
| swiftui-test-expert | Explore | テストの指摘が既存パターンと整合しているか |
| Explore | swiftui-test-expert | 関連コードの指摘にテスト観点が含まれているか |

### クロスレビュー プロンプト

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

## Phase 4.2: 統合レビュー（両モード共通）

クロスレビュー完了後、**統合エージェント（opus）**を起動して結果を統合。

### 統合エージェント プロンプト

```
以下は Phase 4 のレビュー結果と、Phase 4.1 のクロスレビュー結果です。
これらを統合し、メイン Claude に報告するための最終結果を作成してください。

【統合対象】
[各エージェントのレビュー出力 + クロスレビュー結果]

【統合ルール】
1. 重複する指摘を排除（同じ行・同じ問題への指摘）
2. クロスレビューで「⚠️ 要検討」とされた指摘には注釈を追加
3. クロスレビューで「❌ 過剰」とされた指摘は除外（理由と共に記録）
4. 「💡 追加指摘」を統合結果に含める
5. 各指摘の出典（エージェント名）を保持
6. 優先度順にソート（High → Medium → Low）

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

### 重複排除の基準

```
同じ指摘とみなす条件:
1. 同じファイル・同じ行番号
2. AND 同じ問題カテゴリ（例: retain cycle, performance, etc.）
```

### 矛盾の解決

```
矛盾がある場合の処理:
1. 両方の見解を「⚠️ 要検討」セクションに記載
2. 各エージェントの根拠を併記
3. メイン Claude はユーザーに判断を委ねる（独自判断しない）
```
