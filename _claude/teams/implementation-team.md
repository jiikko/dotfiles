# Implementation Team

設計・実装・テストを並行して進めるチーム。forge の Phase 0〜3 に相当する機能を Team Agents で実現する。

## チーム構成

### teammate: researcher
- **役割**: タスクに関連する公式ドキュメント・ベストプラクティスの調査
- **使用エージェント**: research-assistant
- **モデル**: opus

### teammate: architect
- **役割**: 設計方針の策定・既存コードとの整合性確認
- **使用エージェント**: architecture-reviewer
- **モデル**: opus

### teammate: test-designer
- **役割**: テスト戦略の設計・テストコードの作成
- **使用エージェント**: test-runner
- **モデル**: opus

## ワークフロー

1. **並行調査フェーズ**: researcher, architect が同時に調査開始
2. **設計フェーズ**: 調査結果を元に lead が設計方針を決定
3. **実装フェーズ**: lead が実装、test-designer がテスト作成
4. **検証フェーズ**: 全チームメイトで結果を確認

## 使い方

```
実装チームを作成してください:
- researcher: [タスク] に関するベストプラクティスを調査
- architect: 既存コードを分析して設計方針を提案
- test-designer: テスト戦略を設計
タスク: [実装したい機能の説明]
```
