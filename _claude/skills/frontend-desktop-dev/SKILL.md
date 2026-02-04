---
name: frontend-desktop-dev
version: 1.0.0
description: "Comprehensive frontend and desktop development skill combining CSS, Node.js, and Electron expertise. Use for building web frontends, Node.js backends, or Electron desktop applications. Automatically coordinates between css-expert, nodejs-expert, and electron-expert agents."
---

# Frontend & Desktop Development

フロントエンド/デスクトップ開発のための統合スキル。CSS、Node.js、Electron の専門家エージェントを統合活用します。

## Quick Start

```
/frontend-desktop-dev [タスク説明]
```

例:
- `/frontend-desktop-dev Electron でシステムトレイアプリを作成`
- `/frontend-desktop-dev レスポンシブなダッシュボードの CSS を設計`
- `/frontend-desktop-dev Node.js で高負荷 API を最適化`

## 専門家エージェント

このスキルは以下のエージェントを活用します：

| Agent | 専門領域 | 主なユースケース |
|-------|---------|-----------------|
| `css-expert` | CSS/SCSS/CSS-in-JS | レイアウト、アニメーション、レスポンシブデザイン |
| `nodejs-expert` | Node.js/TypeScript | 非同期処理、ストリーム、API、パフォーマンス |
| `electron-expert` | Electron | デスクトップアプリ、IPC、ネイティブ統合、配布 |

## タスク別ワークフロー

### Electron アプリ開発

```
1. electron-expert: アーキテクチャ設計（main/renderer 分離）
2. nodejs-expert: メインプロセスロジック
3. css-expert: UI スタイリング
4. electron-expert: パッケージング・配布
```

### Web フロントエンド

```
1. css-expert: レイアウト・スタイル設計
2. nodejs-expert: ビルドツール設定（Webpack, Vite）
3. css-expert: パフォーマンス最適化
```

### Node.js バックエンド

```
1. nodejs-expert: API 設計・実装
2. nodejs-expert: パフォーマンス・セキュリティ
```

## 技術スタック対応

### CSS
- CSS3, SCSS/Sass, PostCSS
- CSS-in-JS (styled-components, Emotion)
- Tailwind CSS, CSS Modules
- Flexbox, Grid, Container Queries

### Node.js
- Express, Fastify, Nest.js
- npm, pnpm, yarn
- Streams, Worker Threads
- TypeScript, ESM

### Electron
- Main/Renderer architecture
- IPC, preload scripts
- electron-builder, electron-forge
- Auto-updates, code signing

## セキュリティ考慮事項

### Electron
- Context Isolation **必須**
- Preload で最小限の API のみ expose
- IPC 入力の完全なバリデーション
- CSP (Content Security Policy) 設定

### Node.js
- 入力バリデーション (Zod, Joi)
- Rate limiting
- Helmet でセキュリティヘッダー
- 依存関係の脆弱性チェック

### CSS
- XSS 対策（動的スタイルの sanitize）
- ユーザー入力を直接 CSS に使用しない

## パフォーマンス指標

| 領域 | 目標 | 計測方法 |
|------|-----|---------|
| CSS | FCP < 1.5s, CLS < 0.1 | Lighthouse |
| Node.js | P99 latency < 200ms | APM ツール |
| Electron | 起動時間 < 2s | 内部計測 |

## 関連リソース

### 公式ドキュメント
- [MDN CSS](https://developer.mozilla.org/docs/Web/CSS)
- [Node.js Docs](https://nodejs.org/docs/)
- [Electron Docs](https://www.electronjs.org/docs/)

### 品質基準
- [CSS Guidelines](https://cssguidelin.es/)
- [Node.js Best Practices](https://github.com/goldbergyoni/nodebestpractices)
- [Electron Security](https://www.electronjs.org/docs/latest/tutorial/security)
