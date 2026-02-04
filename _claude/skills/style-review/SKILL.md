---
name: style-review
version: 1.0.0
description: "CSS/UI スタイルの WCAG コンプライアンス検証とライト/ダークモード確認を自動化するスキル。722+ セッションの UI スタイリング作業を効率化。"
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

### 1. WCAG コントラスト比（AA 基準）

| テキストタイプ | 最小コントラスト比 |
|--------------|-----------------|
| 通常テキスト (< 18px) | 4.5:1 |
| 大きいテキスト (≥ 18px bold / 24px) | 3:1 |
| UI コンポーネント | 3:1 |
| 非テキスト（アイコン等） | 3:1 |

### 2. Light/Dark モード検証

両モードでの可読性を確認：
- テキストコントラスト
- ボーダー視認性
- シャドウ効果
- アイコン/SVG の視認性

### 3. インタラクション状態

- `:hover` 状態のコントラスト
- `:focus` 状態の視認性（2px+ outline または同等）
- `:active` 状態
- `:disabled` 状態の区別

### 4. モーション設定

- `prefers-reduced-motion` 対応
- アニメーション時間の適切性
- 点滅コンテンツの有無

## 出力形式

### 標準出力

```markdown
## Style Review 結果

### 概要
- 検証ファイル数: N
- 問題検出数: X (High: H, Medium: M, Low: L)

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

### JSON 出力（並行エージェント統合用）

`--json` フラグで JSON 形式を出力：

```json
{
  "agent": "style-review",
  "summary": "3 files, 2 issues",
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

## 実行フロー

```
1. 対象ファイル特定
   └── Glob で *.css, *.scss, *.module.css を検索

2. css-expert エージェント起動
   └── WCAG 検証観点でレビュー

3. 結果統合
   └── 重要度順にソート、修正推奨を生成

4. レポート出力
   └── Markdown テーブル形式
```

## 連携エージェント

| Agent | 役割 |
|-------|-----|
| `css-expert` | CSS 分析、WCAG 検証 |
| `nodejs-expert` | ビルドツール設定（必要時） |

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

- [WCAG 2.1 Guidelines](https://www.w3.org/WAI/WCAG21/quickref/)
- [WebAIM Contrast Checker](https://webaim.org/resources/contrastchecker/)
- [Lighthouse Accessibility](https://developers.google.com/web/tools/lighthouse/audits/contrast-ratio)
