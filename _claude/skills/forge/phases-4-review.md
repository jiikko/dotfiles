# Phase 4〜4.2: 専門家レビュー・クロスレビュー・統合

> **⚙️ 実行は `forge.js`（Workflow）**: Phase 4（専門家レビュー）・4.1（クロスレビュー）・4.2（統合）は
> `_claude/workflows/forge.js` が決定論的に実行する。main Claude は **`Workflow({scriptPath, args:{kind:"review", ...}})`** を
> 起動するだけで、自分でエージェントを並べてはいけない（Ultra モードは `kind:"ultra"` → `phase-4.3-ultra.md`）。
> 起動契約は SKILL.md「forge.js 呼び出し契約」を参照。
> **このファイルは forge.js が実装している仕様の索引**であり、ロスター/ペアリング/統合の詳細は下記共通ファイルにある。
> 仕様を変更したら `forge.js` も同期更新すること。

## Phase 4: 専門家レビュー（両モード共通）

> **モード別動作**: `~/.claude/skills/forge/_common/modes.md` を参照

### エージェント選択

**重要**: ファイルパスのパターンマッチだけでなく、**実際のファイル内容を読み取って**適切なエージェントを選択する。

> **エージェント定義・プロンプト・選択ルール**: `~/.claude/skills/forge/_common/agents.md` を参照
> **モード別のエージェント数・実行方式**: `~/.claude/skills/forge/_common/modes.md` を参照

---

## Phase 4.1: クロスレビュー（両モード共通）

> **Minimum モード**: このフェーズは省略
> **Minimum+ モード**: 実行（3エージェント間ペアリング。`~/.claude/skills/forge/_common/cross-review.md` の「Minimum+ モード用ペアリング」を参照）
> **Ultra モード**: このフェーズは省略（Phase 4.3 で代替）

Phase 4 の各エージェント出力を、**別の観点を持つエージェントが検証**する。

### クロスレビュー仕様

> **詳細**: `~/.claude/skills/forge/_common/cross-review.md` を参照

---

## Phase 4.2: 統合レビュー（両モード共通）

> **Minimum モード**: このフェーズは省略
> **Minimum+ モード**: 実行
> **Ultra モード**: このフェーズは省略（Phase 4.3 で代替）

クロスレビュー完了後、**統合エージェント**を起動して結果を統合。forge.js では `agentType` を指定しない汎用エージェント（セッションの main モデルを継承）が担う — ドメイン特化でなく横断的な重複排除・矛盾検出・優先度付けが役割のため。

### 統合仕様

> **詳細**: `~/.claude/skills/forge/_common/cross-review.md` の「Phase 4.2 統合用」を参照

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
