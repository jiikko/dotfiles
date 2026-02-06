# Full-Stack Team

フロントエンド・バックエンド・インフラの専門家が並行して作業するチーム。

## チーム構成

### teammate: frontend-specialist
- **役割**: フロントエンド実装・UI/UX レビュー
- **使用エージェント**: css-expert, nodejs-expert, electron-expert のいずれか（技術スタックに応じて選択）
- **モデル**: opus

### teammate: backend-specialist
- **役割**: バックエンド実装・API 設計・データモデリング
- **使用エージェント**: rails-domain-designer, go-architecture-designer, nodejs-expert のいずれか
- **モデル**: opus

### teammate: quality-gate
- **役割**: 横断的なコード品質・セキュリティ・テスト
- **使用エージェント**: code-reviewer, security-auditor
- **モデル**: opus

## 使い方

```
フルスタックチームを作成してください:
- frontend-specialist: [フロントエンド側のタスク]
- backend-specialist: [バックエンド側のタスク]
- quality-gate: 実装が完了したら品質チェック
タスク: [機能の説明]
```

## 注意事項

- フロントエンド/バックエンド間の API 仕様は lead が調整する
- 各チームメイトは独立したコンテキストを持つため、共有すべき情報は lead 経由で伝達
