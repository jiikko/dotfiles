---
name: style-review
version: 1.1.0
description: "Automated WCAG compliance verification and Light/Dark mode review for CSS/UI styling."
---

# Style Review

CSS/UI のアクセシビリティとスタイル品質を一括検証するスキル。

## Quick Start

```
/style-review [対象ファイル/ディレクトリ]
```

例:
- `/style-review styles/` - styles ディレクトリ内の CSS を検証
- `/style-review src/components/Button.css` - 特定ファイルを検証
- `/style-review` - カレントディレクトリの CSS/SCSS を検証

## 検証項目

### 1. WCAG コントラスト比（WCAG 2.2 準拠）

| テキストタイプ | AA 最小 | AAA 推奨 | 該当 SC |
|--------------|---------|----------|---------|
| 通常テキスト (< 24px / < 18.67px bold) | 4.5:1 | 7:1 | 1.4.3 / 1.4.6 |
| 大きいテキスト (≥ 24px / ≥ 18.67px bold) | 3:1 | 4.5:1 | 1.4.3 / 1.4.6 |
| UI コンポーネント・グラフィック | 3:1 | -- | 1.4.11 |
| フォーカスインジケーター（状態変化） | -- | 3:1 (+ 2px solid) | 2.4.13 |

> **注意**: 大きいテキストは 18pt (24px) 以上、または 14pt (18.67px) 以上かつ bold。`18px` と `18pt` は異なる値。

### 2. Light/Dark モード検証

両モードでの可読性を確認：
- テキストコントラスト
- ボーダー視認性
- シャドウ効果
- アイコン/SVG の視認性

### 3. インタラクション状態

- `:hover` 状態のコントラスト
- `:focus` 状態の視認性（2px+ solid outline、破線の場合は 4px+）
- `:focus` 要素が他の要素に隠されない（SC 2.4.11）
- `:active` 状態
- `:disabled` 状態の区別（3:1 以上推奨）

### 4. モーション設定

- `prefers-reduced-motion` 対応
- アニメーション時間の適切性
- 点滅コンテンツの有無

## 実行フロー

```
0. 引数パース
   ├── 対象パス（デフォルト: カレントディレクトリ）
   ├── --wcag-level AA|AAA（デフォルト: AA）
   ├── --only contrast|focus|motion（デフォルト: 全検証）
   └── --json（デフォルト: Markdown 出力）

1. 対象ファイル特定
   ├── Glob で *.css, *.scss, *.module.css を検索
   └── 0 件の場合 → エラー: "対象 CSS ファイルが見つかりません"

2. css-expert エージェント起動（Task ツール使用）
   ├── 下記の指示テンプレートで起動
   └── 応答なしの場合 → エラー: フォールバック処理

3. 結果統合
   ├── 重要度順にソート（High → Medium → Low）
   └── Pass/Fail 判定

4. レポート出力
   ├── デフォルト: Markdown テーブル形式
   └── --json: JSON 形式
```

## css-expert 指示テンプレート

css-expert を Task ツールで起動する際、以下のプロンプトを使用する：

```
以下の CSS ファイルを WCAG 2.2 基準で検証してください。

対象ファイル: [ファイルパス一覧]
WCAG レベル: [AA|AAA]
検証カテゴリ: [contrast|focus|motion|all]

【検証項目】
1. コントラスト比（WCAG SC 1.4.3 / 1.4.6 / 1.4.11）
   - テキスト色と背景色のペアを抽出
   - CSS 変数は :root での定義値を解決して計算
   - prefers-color-scheme: dark ブロック内の値も検証
   - AA 基準: 通常テキスト 4.5:1、大きいテキスト 3:1
   [AAA の場合: AAA 基準: 通常テキスト 7:1、大きいテキスト 4.5:1]

2. フォーカス状態（WCAG SC 2.4.7 / 2.4.11 / 2.4.13）
   - :focus, :focus-visible スタイルの有無
   - outline: none の検出（代替スタイルなしは NG）
   - フォーカスインジケーターの視認性

3. モーション設定（WCAG SC 2.3.1）
   - prefers-reduced-motion メディアクエリの有無
   - animation/transition を使用するセレクターの列挙

【出力形式】JSON で出力してください:
{
  "agent": "css-expert",
  "file": "ファイルパス",
  "issues": [
    {
      "line": 行番号,
      "severity": "high" | "medium" | "low",
      "category": "accessibility",
      "description": "問題の説明",
      "suggestion": "修正案",
      "wcag": "該当 SC 番号"
    }
  ],
  "wcag_check": {
    "contrast_pass": true/false,
    "light_mode_tested": true/false,
    "dark_mode_tested": true/false,
    "focus_visible": true/false,
    "reduced_motion": true/false
  }
}
```

### CSS 変数の解決方法

CSS 変数（`var(--color)`）を含む色定義の検証手順:

```
1. :root ブロックから変数定義を抽出
2. @media (prefers-color-scheme: dark) 内の再定義も抽出
3. Light/Dark それぞれのコンテキストで変数値を解決
4. 解決後の実際の色値でコントラスト比を計算

解決不能な場合（外部ファイル参照等）:
→ "解決不能" としてマークし、手動確認を推奨
```

## Pass/Fail 基準

### 合格条件

| レベル | 条件 |
|-------|------|
| **Pass** | High: 0 件、wcag_check の全項目が true |
| **Conditional Pass** | High: 0 件、Medium: 5 件以下 |
| **Fail** | High: 1 件以上 |

### severity 判定基準

| severity | 基準 |
|----------|------|
| **high** | WCAG AA 違反（コントラスト比未達、outline:none 代替なし） |
| **medium** | ベストプラクティス逸脱（AAA 未達、prefers-reduced-motion 未対応） |
| **low** | 改善推奨（disabled 状態のコントラスト、タッチターゲットサイズ） |

## 出力形式

### 標準出力（デフォルト: Markdown）

```markdown
## Style Review 結果

### 概要
- 検証ファイル数: N
- 問題検出数: X (High: H, Medium: M, Low: L)
- 判定: Pass | Conditional Pass | Fail

### WCAG コントラスト
| ファイル | 行 | 要素 | 現在比率 | 必要比率 | 状態 |
|---------|---|-----|---------|---------|------|
| button.css | 42 | .btn-text | 2.8:1 | 4.5:1 | ❌ |
| card.css | 15 | .card-title | 5.2:1 | 4.5:1 | ✅ |

### Light/Dark モード
| ファイル | 問題 | Light | Dark |
|---------|-----|-------|------|
| theme.css | --text-secondary | ✅ | ❌ 2.1:1 |

### フォーカス状態
| ファイル | セレクター | フォーカス表示 |
|---------|----------|--------------|
| button.css | .btn | ❌ outline: none |

### 修正推奨
1. **High**: button.css:42 - コントラスト比を 4.5:1 以上に
2. **Medium**: theme.css:78 - ダークモードの --text-secondary を調整
```

### JSON 出力（`--json` フラグ）

```json
{
  "agent": "style-review",
  "summary": "3 files, 2 issues",
  "verdict": "fail",
  "issues": [
    {
      "file": "button.css",
      "line": 42,
      "severity": "high",
      "category": "accessibility",
      "description": "コントラスト比 2.8:1 は WCAG AA 基準 4.5:1 未満",
      "suggestion": "color: #374151 に変更（5.9:1）",
      "wcag": "1.4.3"
    }
  ],
  "wcag_check": {
    "contrast_pass": false,
    "light_mode_tested": true,
    "dark_mode_tested": true,
    "focus_visible": false,
    "reduced_motion": true
  }
}
```

## エラーハンドリング

| 状況 | 対応 | 出力例 |
|------|------|--------|
| 対象ファイル 0 件 | 即時終了、メッセージ表示 | "対象 CSS ファイルが見つかりません。パスを確認してください" |
| CSS パースエラー | 該当ファイルをスキップ、他を続行 | `{"file": "broken.css", "error": "CSS parse error at line 42"}` |
| CSS 変数解決不能 | "解決不能" としてマーク | `"description": "var(--external-color) は解決不能。手動確認推奨"` |
| エージェント起動失敗 | リトライ 1 回、失敗時は手動レビューを推奨 | "css-expert エージェント起動失敗。手動レビューを実施してください" |

## 連携エージェント

| Agent | 役割 | 起動条件 |
|-------|-----|---------|
| `css-expert` | CSS 分析、WCAG 検証 | 常時（Task ツールで起動） |
| `nodejs-expert` | ビルドツール設定 | PostCSS/Sass 設定ファイルが存在する場合 |

## カスタマイズ

### WCAG レベル変更

```
/style-review --wcag-level AAA
```

AAA 基準（より厳格）:
- 通常テキスト: 7:1
- 大きいテキスト: 4.5:1

### 特定カテゴリのみ検証

```
/style-review --only contrast
/style-review --only focus
/style-review --only motion
```

## 関連リソース

- [WCAG 2.2 Guidelines](https://www.w3.org/TR/WCAG22/)
- [WebAIM Contrast Checker](https://webaim.org/resources/contrastchecker/)
- [Lighthouse Accessibility](https://developers.google.com/web/tools/lighthouse/audits/contrast-ratio)
