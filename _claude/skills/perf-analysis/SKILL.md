---
name: perf-analysis
version: 1.0.0
description: Performance Analysis - パフォーマンス分析。./tmp/perf.log を分析してボトルネックを特定し、パフォーマンス改善 Issue を作成するスキル。
---

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
