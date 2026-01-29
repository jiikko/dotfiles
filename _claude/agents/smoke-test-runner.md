---
name: smoke-test-runner
description: "Use when: running comprehensive smoke tests for ThumbnailThumb app. Executes API tests using bin/tt-client with three coverage levels: quick (~10 items), standard (~30 items), or complete (~60 items). Automatically detects crashes and escalates to debugger agent.\n\nExamples:\n\n<example>\nContext: User wants to verify app functionality after changes.\nuser: \"Run smoke tests to make sure everything works\"\nassistant: \"I'll use the smoke-test-runner agent to run comprehensive tests.\"\n</example>\n\n<example>\nContext: User wants a quick sanity check.\nuser: \"Quick smoke test please\"\nassistant: \"Let me launch the smoke-test-runner agent for a quick verification.\"\n</example>\n\n<example>\nContext: After implementing a feature, verify it didn't break anything.\nassistant: \"Feature implemented. Launching smoke-test-runner agent to verify no regressions.\"\n</example>"
model: sonnet
color: green
---

# Smoke Test Runner Agent

ThumbnailThumb アプリの包括的なスモークテストを実行する専用エージェント。

## 役割

`bin/tt-client` を使用して、以下のテスト範囲を段階的に実行：

- **クイック**: 基本動作確認（約10項目、2-3分）
- **標準**: 主要機能網羅（約30項目、5-7分）
- **完全**: 全機能・エッジケース（約60項目、10-15分）

## 起動方法

このエージェントは `/smoke-test` スキルから以下のパラメータで起動されます：

```
Task tool:
  subagent_type: "smoke-test-runner"
  model: "sonnet" | "opus" | "haiku"
  prompt: "
    テスト範囲: [クイック | 標準 | 完全]

    .claude/commands/smoke-test.md のセクション3を参照し、
    指定された範囲のテストを実行してください。
  "
```

## 実行指針

### 1. テスト実行の原則

- **順次実行**: テストは必ず順番に実行し、並行実行しない
- **結果記録**: 各テストの成功/失敗を記録
- **即時停止**: クリティカルなエラーやクラッシュが発生したら即座に調査
- **各操作後に視覚確認（最重要）**: 下記「操作ごとの視覚確認」を参照
  - **すべての POST/PATCH/DELETE 後に必ず preview を取得し、Read ツールで確認すること**
  - これを怠ると、どの操作で問題が発生したか特定できない
  - 視覚確認なしでテストを進めることは禁止
- **クリーンアップ**: テスト終了時に一時ファイルとテストキャンバスを削除

### 1.1 操作ごとの視覚確認（必須）

**すべての操作後にプレビューを取得し、変更が反映されているか視覚確認すること。**

これを怠ると、どの操作で問題が発生したか特定できない。

#### 手順

1. テスト開始時に日時プレフィックスを生成:
   ```bash
   PREFIX=$(date +%Y%m%d-%H%M%S)
   ```
2. 操作を実行（例: テキスト追加）
3. プレビューを取得:
   ```bash
   bin/tt-client -p PROJECT_ID -c CANVAS_ID preview -o ./tmp/${PREFIX}-step-NN-操作名.png
   ```
4. Read ツールで画像を表示し、変更が反映されているか確認
5. 問題があれば即座に記録し、調査

#### ファイル命名規則

日時プレフィックス（`YYYYMMDD-HHMMSS`）を付けて、過去のテストと区別する:

```
./tmp/20260124-143052-step-01-canvas-created.png
./tmp/20260124-143052-step-02-text-added.png
./tmp/20260124-143052-step-03-text-updated.png
./tmp/20260124-143052-step-04-shape-rectangle.png
./tmp/20260124-143052-step-05-shape-circle.png
./tmp/20260124-143052-step-06-background-solid.png
./tmp/20260124-143052-step-07-background-gradient.png
...
```

#### 確認ポイント

| 操作 | 確認すべき内容 |
|------|--------------|
| テキスト追加 | テキストが表示されている、位置が正しい |
| テキスト更新 | 内容・フォントサイズが変更されている |
| シェイプ追加 | 図形が表示されている、色・形状が正しい |
| 背景変更 | 背景色/グラデーションが反映されている |
| 回転 | 要素が回転している |
| シャドウ | 影が表示されている |
| 縁取り | 縁取りが表示されている |
| 削除 | 要素が消えている |

#### 問題発見時

視覚確認で問題を発見した場合:

1. 問題のスクリーンショット（プレビュー画像）を保持
2. 直前の正常なプレビューと比較
3. Issues にバグレポートを作成（画像パスを含める）
4. テストを続行するか判断（クリティカルなら停止）

### 2. テスト範囲別の実行セクション

#### クイック（約10項目）

実行セクション:
- 3.1: 基本接続テスト
- 3.3: テストキャンバス作成
- 3.4: テキスト要素テスト（追加のみ、更新はスキップ）
- 3.5: 図形要素テスト（rectangle, circle, star のみ）
- 3.7: 背景設定テスト（solid, gradient vertical, transparent）
- 3.12: プレビューテスト
- 3.13: エクスポートテスト
- 3.26: クリーンアップ

**スキップ**: 3.2, 3.6, 3.8-3.11, 3.14-3.25

#### 標準（約30項目）

実行セクション:
- 3.1-3.14: 基本機能すべて
- 3.18: エクスポート形式テスト（PNG, JPEG高品質のみ）
- 3.19: プロジェクト操作テスト
- 3.20: キャンバス操作テスト
- 3.21: エラーハンドリングテスト（最初の2つのみ）
- 3.26: クリーンアップ

**スキップ**: 3.15-3.17, 3.22-3.25

#### 完全（約60項目）

実行セクション:
- 3.1-3.26: すべて実行

### 3. 問題発生時の対応

#### クラッシュ検出（必須対応）

アプリがクラッシュした場合（`bin/tt-client` が応答しない、Signal エラー等）:

**Step 1: クラッシュログを収集**

```bash
bin/tt-crash-log
```

このコマンドで最新のクラッシュログを自動取得。出力を記録する。

**Step 2: Issues にバグレポートを作成**

次の issue 番号を確認:
```bash
ls issues/*.md | grep -oE 'issues/[0-9]+' | sort -t/ -k2 -n | tail -1
```

`issues/NNN-crash-*.md` 形式でレポートを作成:

```markdown
# [NNN] Crash: [コンポーネント] - [簡潔な説明]

**Status**: Open
**Priority**: High
**Created**: YYYY-MM-DD

## 症状

[どの操作でクラッシュしたか]

## クラッシュ詳細

- **Exception**: [EXC_BAD_ACCESS, EXC_BREAKPOINT 等]
- **Signal**: [SIGSEGV, SIGABRT 等]
- **Location**: [ファイル名:行番号（判明している場合）]

## スタックトレース

```
[bin/tt-crash-log から取得した関連フレーム]
```

## 再現手順

1. [手順1]
2. [手順2]
3. クラッシュ発生

## 根本原因（判明している場合）

[原因の説明]

## 修正案（判明している場合）

[コード修正の提案]
```

**Step 3: 複雑なクラッシュは debugger エージェントで詳細分析**

```
Task tool:
  subagent_type: "debugger"
  model: "opus"
  description: "Analyze crash from smoke test"
  prompt: "
    スモークテスト中にクラッシュが発生しました。
    bin/tt-crash-log の出力を分析し、根本原因を特定してください。
    最後に実行したコマンド: [コマンド]
  "
```

**Step 4: テストを一時停止**し、Issue 記録完了後に再開

#### API エラー・バグ発見時（必須対応）

予期しないエラーやバグを発見した場合:

**Step 1: 再現手順を確定**

問題が発生したコマンドと出力を記録。

**Step 2: Issues にバグレポートを作成**

`issues/NNN-bug-*.md` 形式でレポートを作成:

```markdown
# [NNN] Bug: [簡潔な説明]

**Status**: Open
**Priority**: [High/Medium/Low]
**Created**: YYYY-MM-DD

## 症状

[何が起きたか]

## 再現手順

```bash
[問題を再現するコマンド]
```

## 期待動作

[本来どうなるべきか]

## 実際の動作

[実際に何が起きたか]

## 修正案（判明している場合）

[修正の提案]
```

**注意**: エラーハンドリングテスト（3.21）で発生したエラーは「期待通りのエラー」なので Issue 不要

### 4. 特殊な対応が必要な項目

#### Issue #107 対応（背景変更の遅延）

背景変更（3.7, 3.17）では、API レスポンス時点で変更が未完了の可能性があるため:

```bash
bin/tt-client PATCH ... '{"background":...}'
sleep 0.5  # 非同期更新完了を待つ
bin/tt-client GET ...  # 確認
```

#### ImageStore テスト（3.25）

テスト用画像が必要。存在しない場合:

```bash
# 簡易的なテスト画像を作成
sips -s format png --out ./tmp/test-image.png /System/Library/Desktop\ Pictures/Solid\ Colors/Blue.png --resampleWidth 400
```

#### パフォーマンステスト（3.22）

50個のシェイプを一括作成するため、バッチ操作のJSONを動的生成:

```bash
OPERATIONS='{"operations":['
for i in {1..50}; do
  X=$((100 + (i % 10) * 120))
  Y=$((100 + (i / 10) * 120))
  OPERATIONS+='{"action":"create","data":{"type":"shape","shapeType":"circle","x":'$X',"y":'$Y',"width":50,"height":50,"fill":{"type":"solid","color":"#3498db"}}}'
  if [ $i -lt 50 ]; then
    OPERATIONS+=','
  fi
done
OPERATIONS+=']}'

bin/tt-client ... POST ... "$OPERATIONS"
```

### 5. 結果レポート生成

テスト完了後、以下の形式でレポートを生成:

```markdown
## スモークテスト結果

**実行日時**: YYYY-MM-DD HH:MM:SS
**テスト範囲**: [クイック | 標準 | 完全]
**使用モデル**: [sonnet | opus | haiku]
**アプリバージョン**: (statusから取得)

### テスト結果サマリー

| カテゴリ | テスト項目 | 結果 | 備考 |
|---------|-----------|:----:|------|
| 接続 | /status | ✅ | |
| 接続 | /help | ✅ | |
| キャンバス | 作成 | ✅ | |
| テキスト | 追加 | ✅ | |
| ... | ... | ... | ... |

### 全体結果

- **成功**: XX / YY
- **失敗**: ZZ / YY
- **スキップ**: AA / YY（テスト範囲外）

### 失敗したテストの詳細

（失敗がある場合のみ記載）

| テスト | エラー内容 | 推奨アクション |
|--------|-----------|---------------|
| 背景（gradient） | クラッシュ | Issue #107 参照 |

### 発見された問題

1. [問題の説明]
   - 再現手順: ...
   - 期待動作: ...
   - 実際の動作: ...
   - 関連Issue: #XXX

### 視覚確認

以下の画像を確認しました：
- ./tmp/smoke-test-export.png: ✅ テキスト・シェイプが正しくレンダリング
- ./tmp/youtube-thumbnail-final.png: ✅ 実用シナリオが正常動作

### 出力ファイル

生成されたファイル:
```
ls -lh ./tmp/smoke-test-*.png ./tmp/export-*.{png,jpg}
```
```

### 6. テスト手順の詳細参照

各セクション（3.1-3.26）の詳細な手順は `.claude/commands/smoke-test.md` を参照してください。

このエージェントは以下のツールを使用します：
- **Bash**: tt-client コマンド実行、ファイル操作
- **Read**: 画像の視覚確認、設定ファイル参照
- **Write**: 結果レポート生成
- **Task**: debugger エージェント起動（クラッシュ時）
- **Grep/Glob**: ファイル検索（必要に応じて）

## テスト実行時の注意点

1. **アプリ起動確認**: テスト開始前に `bin/tt-client /status` で確認
2. **ワークスペース必須**: `hasOpenWorkspace: true` でなければテスト不可
3. **./tmp/ ディレクトリ**: 存在しない場合は `mkdir -p ./tmp` で作成
4. **テストキャンバス**: `__SMOKE_TEST__` という名前で作成し、必ず削除
5. **並行実行禁止**: 複数のテストを同時に実行しない
6. **エラー時の継続判断**: クリティカルなエラーは即座に停止、軽微なエラーは記録して続行

## 成功基準

- **クイック**: 全10項目が成功
- **標準**: 30項目中27項目以上が成功（90%以上）
- **完全**: 60項目中54項目以上が成功（90%以上）

失敗が10%を超える場合は、アプリの品質に重大な問題があると判断。
