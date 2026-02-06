# Claude Code Team Agents 使い方ガイド

## Team Agents とは

Team Agents は Claude Code の実験的機能で、複数の Claude セッションをチームとして協調動作させる。
各チームメイトは独立したコンテキストウィンドウを持ち、並行して作業できる。

### forge skill との違い

| 観点 | forge skill | Team Agents |
|------|-------------|-------------|
| 実行モデル | 1セッション内で subagent を順次/並行起動 | 複数の独立セッションが並行動作 |
| コンテキスト | subagent の結果は要約されて返る | 各セッションがフルコンテキストを保持 |
| コミュニケーション | lead → subagent の一方向 | チームメイト同士の直接通信が可能 |
| コスト | 低〜中（結果が要約される） | 高（各セッションがフルの Claude インスタンス） |
| 適したタスク | 定型的なレビュー/実装フロー | 複雑な調査、競合仮説の検証 |

## セットアップ

```bash
# dotfiles を clone 済みなら
cd ~/dotfiles && bash setup.sh

# これで以下が設定される:
# ~/.claude/teams/        → チームプリセット定義
# ~/.claude/settings.local.json → CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1
```

### 手動で有効化する場合

```bash
export CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1
claude
```

### 表示モード

```bash
# デフォルト（in-process）: 同一ターミナル内で切り替え
claude

# tmux 分割表示: 各チームメイトが別ペインに表示
claude --teammate-mode tmux

# tmux が必要:
# macOS: brew install tmux
# Linux: sudo apt install tmux
```

## 使い方

### 基本: 自然言語でチームを作成

Claude に話しかけるだけでチームが作成される:

```
3人のチームメイトを作成して、このコードをレビューしてください:
- セキュリティの観点
- パフォーマンスの観点
- テストの観点
対象: Sources/Services/AuthService.swift
```

### プリセットを参考にする

`_claude/teams/` に用意されたプリセットを参考にして指示できる:

#### review-team（レビューチーム）

```
レビューチームを作成してください:
- code-quality-reviewer: コード品質の観点からレビュー
- architecture-reviewer: 設計の観点からレビュー
- security-reviewer: セキュリティの観点からレビュー
対象: Sources/ViewModels/CanvasViewModel.swift
```

#### implementation-team（実装チーム）

```
実装チームを作成してください:
- researcher: グレースフルシャットダウンのベストプラクティスを調査
- architect: 既存コードの構造を分析して設計方針を提案
- test-designer: テスト戦略を設計
タスク: HTTP サーバーにグレースフルシャットダウンを実装
```

#### debug-team（デバッグチーム）

```
デバッグチームを作成してください:
- hypothesis-a: メモリリークが原因かどうか調査
- hypothesis-b: 非同期処理のデッドロックが原因かどうか調査
- codebase-explorer: 関連するコードの依存関係を調査
バグ: アプリが長時間使用後にフリーズする
```

#### fullstack-team（フルスタックチーム）

```
フルスタックチームを作成してください:
- frontend-specialist: React コンポーネントの実装
- backend-specialist: API エンドポイントの実装
- quality-gate: 実装完了後に品質チェック
タスク: ユーザープロフィール編集機能の追加
```

## 操作方法

### in-process モード（デフォルト）

| キー | 操作 |
|------|------|
| `Shift+Up/Down` | チームメイト切り替え |
| `Enter` | チームメイトのセッション表示 |
| `Escape` | チームメイト中断 |
| `Ctrl+T` | タスクリスト表示 |

### チーム管理コマンド（自然言語）

```
# タスク割り当て
researcher にこのモジュールの調査を依頼して

# チームメイト間通信
architect の結果を test-designer に共有して

# 個別終了
researcher のセッションを終了して

# 全体終了
チームを解散して
```

## 制限事項

- 実験的機能のため、`/resume` でチームメイトは復元されない
- 1セッション1チームのみ（複数チーム同時管理は不可）
- チームメイトが別チームを作ることはできない（ネスト不可）
- tmux/iTerm2 の分割表示は VS Code Terminal では未対応
- lead セッションは固定（途中変更不可）

## コスト意識

Team Agents は各チームメイトが独立した Claude セッションを消費する。
コスト効率を考慮して:

- **小規模タスク** → subagent（通常のエージェント）で十分
- **中規模タスク** → forge skill を使用
- **大規模・複雑なタスク** → Team Agents が効果的
