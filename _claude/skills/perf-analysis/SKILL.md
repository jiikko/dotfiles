---
name: perf-analysis
version: 1.1.0
description: Performance Analysis - パフォーマンス分析。./tmp/perf.log を分析してボトルネックを特定し、パフォーマンス改善 Issue を作成するスキル。「パフォーマンス分析」「perf-analysis」「ボトルネックを調べて」で発火。ThumbnailThumb 専用（bin/tt-client と ./tmp/perf.log 出力に依存。bin/tt-client が無いプロジェクトでは発火しない）。
---

# Performance Analysis - パフォーマンス分析

`./tmp/perf.log` を分析してボトルネックを特定し、パフォーマンス改善 Issue を作成するスキル。

## 使い方

```
/perf-analysis
```

## 前提条件

- ThumbnailThumb リポジトリのルートで実行すること（`bin/tt-client` が存在しない場合、このスキルは使用不可）
- ThumbnailThumb アプリが起動していること（`bin/tt-client /status` で確認）
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

キャンバスが 3 つ未満、またはカレントキャンバスの要素が 0 件（`bin/tt-client --current elements` で確認）の場合は、負荷テストの前提を満たさないため、テストを開始せずユーザーに準備を依頼する。

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
# カレントキャンバスの全要素を削除（注意: テスト前から存在した要素も消える）
bin/tt-client --current clear

# テスト用プレビューファイルを削除
rm -f ./tmp/perf-test-*.png
```

ユーザーの既存要素が残っているキャンバスでは `clear` を使わず、テストで追加した要素 ID を控えておき個別に DELETE すること。

### 3. ログ分析

#### 3.1 ログファイルの読み込み

```bash
# perf.log の内容を確認
cat ./tmp/perf.log
```

#### 3.2 分析観点

以下の観点でログを分析する:

**1. 処理時間（duration_ms）**
- 操作種別ごとの判定は「閾値設定」表を優先する
- 表に無い操作は 100ms 以上を要注意、500ms 以上を要改善として報告
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

### 4. 結果報告と Issue 作成

分析結果は以下を含めて報告する: 操作別の平均/最大 duration_ms と判定（良好/要注意/要改善）、CPU スパイク・メモリリーク兆候の有無、要改善項目とその根拠となるログ行。

**「要改善」判定が 1 件以上ある場合のみ** `issues/` ディレクトリに Issue を作成する（要注意のみの場合は報告に留める）。新規 Issue は issue-creation-codex-review ルールに従い codex review を通す。

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
- ドラッグ操作は API 経由では実行できない。ドラッグ FPS の分析が依頼に含まれる場合のみユーザーに手動ドラッグを依頼し、含まれない場合は「分析対象外」と結果報告に明記すること
