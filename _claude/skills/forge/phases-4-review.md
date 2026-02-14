# Phase 4〜4.2: 専門家レビュー・クロスレビュー・統合

> **重要**: このファイル内の `@_common/` 参照は自動解決されません。SKILL.md の「共通ファイルの読み込み」セクションに従って、事前に Read してください。

## Phase 4: 専門家レビュー（両モード共通）

> **モード別動作**: @_common/modes.md を参照

### ファイル内容に基づくエージェント選択

**重要**: ファイルパスのパターンマッチだけでなく、**実際のファイル内容を読み取って**適切なエージェントを選択する。

> **エージェント定義**: @_common/agents.md を参照
> **検出パターン**: @_common/agents.md の「追加エージェント」セクションを参照

### Phase 4 で使用するエージェント

**必須エージェント（6+1つ）**:
- swift-language-expert
- swiftui-macos-designer
- swiftui-test-expert
- architecture-reviewer
- Explore
- swiftui-performance-expert ★常時必須

**Minimum / Minimum+ モード時のエージェント（3つのみ、直列）**:
1. swift-language-expert
2. architecture-reviewer
3. swiftui-test-expert

**非 Swift プロジェクトの場合**:
- @_common/agents.md の「言語別エージェント置換ルール」に従い、必須エージェント #1-2 を言語別エージェント（css-expert, nodejs-expert, go-architecture-designer 等）に置換

**条件付き・追加エージェント**:
- @_common/agents.md の「条件付き必須エージェント」「追加エージェント」を参照

**Maximum 専用エージェント**:
- dependency-analyzer
- test-coverage-advisor

---

## Phase 4.1: クロスレビュー（両モード共通）

> **Minimum モード**: このフェーズは省略
> **Minimum+ モード**: 実行（3エージェント間ペアリング。@_common/cross-review.md の「Minimum+ モード用ペアリング」を参照）
> **Ultra モード**: このフェーズは省略（Phase 4.3 で代替）

Phase 4 の各エージェント出力を、**別の観点を持つエージェントが検証**する。

### クロスレビュー仕様

> **詳細**: @_common/cross-review.md を参照

---

## Phase 4.2: 統合レビュー（両モード共通）

> **Minimum モード**: このフェーズは省略
> **Minimum+ モード**: 実行
> **Ultra モード**: このフェーズは省略（Phase 4.3 で代替）

クロスレビュー完了後、**統合エージェント（opus）**を起動して結果を統合。

### 統合仕様

> **詳細**: @_common/cross-review.md の「Phase 4.2 統合用」を参照

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
