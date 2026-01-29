# Performance Analysis - パフォーマンス分析

`./tmp/perf.log` を分析してボトルネックを特定し、パフォーマンス改善 Issue を作成するスキル。

## 使い方

```
/perf-analysis
```

## 前提条件

- ThumbnailThumb アプリが起動していること
- **複数のキャンバス（3つ以上推奨）が含まれるワークスペース**が開かれていること
- 各キャンバスに要素が配置されていること（リアルな負荷テストのため）

## 実行手順

### 1. 事前準備

```bash
# アプリの状態確認
bin/tt-client /status

# 既存の perf.log をクリア
rm -f ./tmp/perf.log
```

ワークスペースが開かれていない場合、または要素が少ない場合はユーザーに準備を依頼する。

### 2. 負荷テストの実行

以下の操作を API 経由で実行し、`./tmp/perf.log` にログを蓄積する。

#### 2.1 キャンバス切り替えテスト

```bash
# キャンバス一覧を取得
bin/tt-client --current GET '/projects/{projectId}/canvases'

# 各キャンバスに順番に切り替え（3回以上）
bin/tt-client -p PROJECT_ID POST '/projects/{projectId}/canvases/CANVAS_ID_1/switch'
bin/tt-client -p PROJECT_ID POST '/projects/{projectId}/canvases/CANVAS_ID_2/switch'
bin/tt-client -p PROJECT_ID POST '/projects/{projectId}/canvases/CANVAS_ID_3/switch'
```

#### 2.2 要素追加テスト

```bash
# テキスト追加（複数回）
bin/tt-client text "パフォーマンステスト1" 200 200
bin/tt-client text "パフォーマンステスト2" 400 200
bin/tt-client text "パフォーマンステスト3" 600 200

# 図形追加（複数回）
bin/tt-client shape rectangle 200 400
bin/tt-client shape circle 400 400
bin/tt-client shape star 600 400
```

#### 2.3 要素更新テスト

```bash
# 要素一覧を取得
bin/tt-client --current elements

# 複数要素を連続更新（位置、サイズ、プロパティ）
bin/tt-client --current PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{elementId}' '{"x":250,"y":250}'
bin/tt-client --current PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{elementId}' '{"fontSize":96}'
bin/tt-client --current PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{elementId}' '{"rotation":15}'
```

#### 2.4 プレビュー生成テスト

```bash
# プレビュー連続取得（3回以上）
bin/tt-client --current preview -o ./tmp/perf-test-1.png
bin/tt-client --current preview -o ./tmp/perf-test-2.png
bin/tt-client --current preview -o ./tmp/perf-test-3.png
```

#### 2.5 バッチ操作テスト

```bash
# 大量要素の一括作成
bin/tt-client --current POST '/projects/{projectId}/canvases/{canvasId}/elements/batch' '{"elements":[{"type":"text","text":"Batch1","x":100,"y":600},{"type":"text","text":"Batch2","x":200,"y":600},{"type":"text","text":"Batch3","x":300,"y":600},{"type":"text","text":"Batch4","x":400,"y":600},{"type":"text","text":"Batch5","x":500,"y":600}]}'
```

#### 2.6 Undo/Redo 連続テスト

```bash
# Undo を連続実行
bin/tt-client undo
bin/tt-client undo
bin/tt-client undo

# Redo を連続実行
bin/tt-client redo
bin/tt-client redo
bin/tt-client redo
```

#### 2.7 クリーンアップ

```bash
# テストで追加した要素を削除
bin/tt-client --current clear

# テスト用プレビューファイルを削除
rm -f ./tmp/perf-test-*.png
```

### 3. ログ分析

#### 3.1 ログファイルの読み込み

```bash
# perf.log の内容を確認
cat ./tmp/perf.log
```

#### 3.2 分析観点

以下の観点でログを分析する:

**1. 処理時間（duration_ms）**
- 100ms 以上の操作を警告
- 500ms 以上の操作を重大な問題として報告
- 同種操作の平均・最大・最小を算出

**2. CPU 使用率（cpu_percent）**
- 50% 以上のスパイクを検出
- ドラッグ操作中の CPU 推移を確認

**3. メモリ使用量（mem_mb）**
- 操作前後のメモリ増加量を確認
- メモリリークの兆候（増加し続ける）を検出

**4. 操作別パフォーマンス**
- `addImage` / `addImageFromURL`: 画像読み込み
- `addText` / `addShape`: 要素追加
- `drag*`: ドラッグ操作（フレームレート確認）
- `deleteSelected`: 削除操作
- `removeBackground`: 背景除去（時間がかかる操作）

**5. ドラッグ操作の分析**
- `total_frames` / `duration_ms` からフレームレートを算出
- 60fps を下回る場合は問題として報告

### 4. Issue 作成

分析結果を元に、`issues/` ディレクトリに Issue を作成する。

**次の Issue 番号を確認:**
```bash
ls issues/*.md | grep -oE 'issues/[0-9]+' | sort -t/ -k2 -n | tail -1
```

**Issue フォーマット:**

```markdown
# Issue NNN: パフォーマンス改善 - [特定された問題]

## 概要

perf.log 分析により特定されたパフォーマンス問題。

## 分析日時

YYYY-MM-DD HH:MM:SS

## テスト環境

- キャンバス数: X
- 要素数（平均）: Y
- macOS バージョン: (必要に応じて)

## 発見された問題

### 1. [問題タイトル]

**重大度**: 高 / 中 / 低

**測定値**:
| 操作 | 平均 | 最大 | 最小 | 閾値 |
|------|------|------|------|------|
| xxx  | Xms  | Xms  | Xms  | 100ms |

**ログ抜粋**:
```json
[PERF] {"op":"xxx","event":"end","duration_ms":XXX,...}
```

**推定原因**:
- 原因1
- 原因2

**改善案**:
- 案1
- 案2

### 2. [次の問題...]

## パフォーマンスサマリー

| カテゴリ | 操作 | 平均時間 | 状態 |
|---------|------|---------|------|
| 要素追加 | addText | Xms | ✅/⚠️/❌ |
| 要素追加 | addImage | Xms | ✅/⚠️/❌ |
| ドラッグ | dragMove | X fps | ✅/⚠️/❌ |
| プレビュー | preview | Xms | ✅/⚠️/❌ |

**凡例**: ✅ 良好 / ⚠️ 要注意 / ❌ 要改善

## 推奨アクション

1. [優先度高] xxx
2. [優先度中] yyy
3. [優先度低] zzz

## 関連

- [PerfLog.swift](ThumbnailThumb/Sources/Services/PerfLog.swift)
- 関連 Issue: #XXX
```

### 5. 問題発見時の対応

重大な問題が見つかった場合は、以下のエージェントを活用して深掘り:

#### パフォーマンス問題の深掘り

| 問題の種類 | 使用エージェント | モデル |
|-----------|-----------------|--------|
| View再描画問題 | **swiftui-performance-expert** | opus |
| メモリリーク | **swift-language-expert** | opus |
| async/actorの問題 | **swift-concurrency-expert** | opus |
| 全体的な遅さ | **debugger** | opus |
| アーキテクチャ起因 | **architecture-reviewer** | opus |

#### 調査手順

1. **swiftui-performance-expert エージェント起動** - View 再描画、メモリ問題の徹底分析（opus）
2. **クラッシュログの確認** - パフォーマンス問題がクラッシュを引き起こしていないか
3. **詳細調査** - Instruments 等での追加プロファイリングを提案
4. **Issue の優先度付け** - 他の Issue との関連を確認

**重要**: パフォーマンス問題は表面的な症状だけでなく、根本原因まで追求する。Opus モデルの深い分析能力を活用して、単なるワークアラウンドではなく本質的な解決策を提案すること。

## ログフォーマット

### start/end イベント

```json
{"op":"addText","event":"start","timestamp_ms":1234567890123,"cpu_percent":12.5,"mem_mb":256.3}
{"op":"addText","event":"end","timestamp_ms":1234567890223,"duration_ms":100.0,"cpu_percent":15.2,"mem_mb":258.1}
```

### ドラッグイベント

```json
{"op":"dragMove","event":"move","count":3,"frame":45,"timestamp_ms":1234567890123,"cpu_percent":25.0,"mem_mb":260.0}
{"op":"dragMove","event":"end","count":3,"total_frames":120,"timestamp_ms":1234567891123,"duration_ms":1000.0,"cpu_percent":18.0,"mem_mb":262.0}
```

### プロパティ変更イベント

```json
{"op":"updateText","prop":"fontSize","value":96,"timestamp_ms":1234567890123,"cpu_percent":10.0,"mem_mb":256.0}
```

## 閾値設定

| 項目 | 良好 | 要注意 | 要改善 |
|------|------|--------|--------|
| 要素追加 | < 50ms | 50-200ms | > 200ms |
| プレビュー生成 | < 100ms | 100-300ms | > 300ms |
| ドラッグ FPS | > 50fps | 30-50fps | < 30fps |
| CPU スパイク | < 30% | 30-60% | > 60% |
| メモリ増加/操作 | < 1MB | 1-5MB | > 5MB |

## 注意事項

- テストは負荷をかけるため、**本番データには実行しないこと**
- テスト用に追加した要素は必ずクリーンアップすること
- ログは追記モードなので、分析前に `rm -f ./tmp/perf.log` でクリアすること
- ドラッグ操作は API 経由では実行できないため、必要に応じて手動操作を依頼すること
