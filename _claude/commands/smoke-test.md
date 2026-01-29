# Smoke Test - アプリ全体動作確認

`bin/tt-client` を使用してアプリの全体的な動作確認を行うスキル。

## 使い方

```
/smoke-test
```

## 実行フロー

このスキルは以下の流れで実行されます：

1. **事前チェック**: アプリとワークスペースの状態確認
2. **質問1**: テスト範囲の選択（クイック/標準/完全）
3. **質問2**: 使用モデルの選択（Sonnet推奨/Opus/Haiku）
4. **subagent起動**: 選択された設定でスモークテストを実行
5. **結果レポート**: テスト結果のサマリーを表示

## テスト範囲

このスキルは以下の項目を包括的にテストします：

- **基本機能**: Status, Help, Projects, Canvases
- **エレメント作成**: Text, Shape (全6種), Image
- **エレメント詳細**: フォント、文字間隔、アウトライン、グラデーション塗り、角丸、ストローク
- **背景設定**: Solid, Gradient (linear/radial), Transparent
- **変換**: 回転、シャドウ、縁取り
- **操作**: 並び替え、複製、削除、Undo/Redo
- **出力**: Preview, Export (PNG/JPEG, 品質指定)
- **バッチ操作**: 一括作成
- **プロジェクト/キャンバス**: 作成、削除、切り替え、複製、クリア
- **エラーハンドリング**: 無効ID、不正JSON、バリデーション
- **パフォーマンス**: 大量エレメント、速度計測
- **実用シナリオ**: YouTubeサムネイル作成
- **ImageStore**: 重複排除、保存、復元、未使用削除

## 実行手順

### ステップ1: 事前チェック

まず、アプリの状態を確認：

```bash
bin/tt-client /status
```

**確認項目**:
- `hasOpenWorkspace: true` であること
- false の場合 → ユーザーにワークスペースを開くよう案内して終了

### ステップ2: テスト範囲を質問

**AskUserQuestion ツールで2つの質問をすること**:

#### 質問1: テスト範囲

```json
{
  "questions": [
    {
      "question": "どのレベルのスモークテストを実行しますか？",
      "header": "テスト範囲",
      "multiSelect": false,
      "options": [
        {
          "label": "クイック（約10項目、2-3分）",
          "description": "基本動作確認のみ。Status, キャンバス作成, テキスト/シェイプ追加, 背景設定, Preview, Export"
        },
        {
          "label": "標準（約30項目、5-7分）(推奨)",
          "description": "主要機能の網羅的確認。クイック + 全6種シェイプ, 回転/シャドウ, Undo/Redo, バッチ, エクスポート形式, プロジェクト/キャンバス操作"
        },
        {
          "label": "完全（約60項目、10-15分）",
          "description": "すべての機能とエッジケース。標準 + テキスト/シェイプ詳細, エラーハンドリング, パフォーマンス, YouTubeサムネイルシナリオ, ImageStore"
        }
      ]
    }
  ]
}
```

#### 質問2: 使用モデル

```json
{
  "questions": [
    {
      "question": "どのモデルでスモークテストを実行しますか？",
      "header": "モデル選択",
      "multiSelect": false,
      "options": [
        {
          "label": "Sonnet（推奨）",
          "description": "速度と精度のバランスが良い。問題発見時にOpusのdebuggerエージェントを自動起動。通常はこれを推奨。"
        },
        {
          "label": "Opus",
          "description": "最高精度だが時間とコストがかかる。複雑な問題が予想される場合や、完全テストで徹底的に調査したい場合に使用。"
        },
        {
          "label": "Haiku",
          "description": "高速だが精度は低い。簡単なクイックテストのみに推奨。問題診断には不向き。"
        }
      ]
    }
  ]
}
```

### ステップ3: subagent を起動

AskUserQuestion の回答に基づいて、**Task ツールで general-purpose subagent を起動**してください。

#### モデルの決定

ユーザーの選択から適切なモデル名を抽出：
- "Sonnet（推奨）" を選択 → `model: "sonnet"`
- "Opus" を選択 → `model: "opus"`
- "Haiku" を選択 → `model: "haiku"`

#### テスト範囲の決定

ユーザーの選択から実行セクションを決定：

| 選択 | 実行セクション |
|------|--------------|
| "クイック" | 3.1, 3.3, 3.4（テキスト追加のみ）, 3.5（rectangle, circle, starのみ）, 3.7, 3.12, 3.13, 3.26 |
| "標準" | 3.1-3.14, 3.18（PNG/JPEG高品質のみ）, 3.19, 3.20, 3.21（2つのエラーパターンのみ）, 3.26 |
| "完全" | 3.1-3.26（すべて） |

#### Task ツールの呼び出し

**重要**: 専用の `smoke-test-runner` subagent を使用してください。

```
Task tool を以下のパラメータで呼び出す：

subagent_type: "smoke-test-runner"
model: （上記で決定したモデル名: "sonnet" | "opus" | "haiku"）
description: "Execute smoke test - [選択されたテスト範囲]"
prompt: "
ThumbnailThumb アプリのスモークテストを実行してください。

**テスト範囲**: [ユーザーが選択した範囲: クイック | 標準 | 完全]

.claude/agents/smoke-test-runner.md の指示に従い、
.claude/commands/smoke-test.md のセクション3を参照して、
指定された範囲のテストを実行してください。

テスト完了後、結果レポートを生成してください。
"
```

**例**:

ユーザーが「標準」「Sonnet」を選択した場合:

```
Task({
  subagent_type: "smoke-test-runner",
  model: "sonnet",
  description: "Execute smoke test - 標準",
  prompt: "ThumbnailThumb アプリのスモークテストを実行してください。\n\n**テスト範囲**: 標準\n\n.claude/agents/smoke-test-runner.md の指示に従い、.claude/commands/smoke-test.md のセクション3を参照して、指定された範囲のテストを実行してください。\n\nテスト完了後、結果レポートを生成してください。"
})
```

---

# 以下は subagent 用の参照情報

このセクション以降は、起動された subagent が参照するテスト手順と詳細情報です。

## テスト範囲の詳細

| 選択肢 | テスト項目数 | 所要時間 | 対象 |
|--------|------------|---------|------|
| **クイック** | 約10項目 | 2-3分 | 基本動作確認のみ |
| **標準** | 約30項目 | 5-7分 | 主要機能の網羅的確認 |
| **完全** | 約60項目 | 10-15分 | すべての機能・エッジケース |

#### クイック（約10項目）

- 基本接続（Status, Help）
- キャンバス作成
- テキスト追加
- シェイプ追加（rectangle, circle, star）
- 背景設定（solid, gradient, transparent）
- Preview
- Export
- クリーンアップ

#### 標準（約30項目）

クイック + 以下：

- 全6種シェイプ
- 回転・シャドウ
- Undo/Redo
- バッチ操作
- エクスポート形式（JPEG）
- プロジェクト操作
- キャンバス複製・クリア
- 基本的なエラーハンドリング

#### 完全（約60項目）

標準 + 以下：

- テキスト詳細プロパティ（フォント、文字間隔、アウトライン、グラデーション）
- シェイプ詳細プロパティ（角丸、ストローク、辺数変更、radialグラデーション）
- 放射状グラデーション背景
- エクスポート品質比較
- 全エラーハンドリングパターン
- パフォーマンステスト（50個のエレメント）
- YouTubeサムネイル作成シナリオ
- 出力品質検証
- ImageStore（重複排除、保存、復元、未使用削除）

**質問例**:
```
どのレベルのスモークテストを実行しますか？

1. クイック（約10項目、2-3分）- 基本動作確認
2. 標準（約30項目、5-7分）- 主要機能網羅
3. 完全（約60項目、10-15分）- すべての機能

選択してください（1/2/3）:
```

ユーザーの選択に応じて、該当するテスト項目のみを実行すること。

### 3. テストの実行

以下のテストを順番に実行し、各ステップの結果を記録する。

**テスト範囲に応じた実行**:
- **クイック**: 3.1, 3.3, 3.4（一部）, 3.5（3種のみ）, 3.7, 3.12, 3.13, 3.26
- **標準**: 3.1-3.14, 3.18（一部）, 3.19, 3.20, 3.21（一部）, 3.26
- **完全**: 3.1-3.26（すべて）

**重要: 視覚確認の義務**

**すべての操作後に必ず preview を取得し、Read ツールで視覚確認すること。**

視覚確認を行わないと、以下の問題が検出できない:
- API は成功を返すが、実際には要素が表示されない
- 色・サイズ・位置が指定と異なる
- 背景が反映されない
- 回転・シャドウ・縁取りが適用されていない

#### 視覚確認の手順（全テストで共通）

**Step 1: テスト開始時に日時プレフィックスを生成**
```bash
PREFIX=$(date +%Y%m%d-%H%M%S)
echo "Test session: $PREFIX"
```

**Step 2: 各操作後に実行**
```bash
# 1. 操作を実行（例: テキスト追加）
bin/tt-client POST ... '{"type":"text","text":"テスト",...}'

# 2. プレビューを取得（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-NN-操作名.png

# 3. Read ツールで画像を表示し、変更が反映されているか確認
# 例: テキストが表示されているか、位置が正しいか、色が正しいか

# 4. 問題があれば即座に記録
```

**Step 3: 問題発見時**
- プレビュー画像を保持
- 直前の正常なプレビューと比較
- Issues にバグレポートを作成
- テスト続行可否を判断

---

**重要な注意事項**:

以下のセクション 3.1-3.14 では、各操作後の視覚確認手順が明示的に記載されています。
**セクション 3.15-3.26 についても、同じパターンで視覚確認を行ってください。**

具体的には:
1. POST/PATCH/DELETE など、キャンバスを変更する操作の直後
2. `bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-NN-操作名.png` を実行
3. Read ツールで画像を表示し、変更が反映されているか確認
4. 問題があれば即座に記録

**視覚確認が記載されていないセクションでも、この手順を省略してはいけません。**

#### 3.1 基本接続テスト

```bash
# ステータス確認
bin/tt-client /status

# ヘルプ取得
bin/tt-client /help
```

#### 3.2 プロジェクト・キャンバス確認

```bash
# プロジェクト一覧
bin/tt-client /projects

# 現在のキャンバス情報
bin/tt-client --current canvas

# キャンバス一覧
bin/tt-client --current GET '/projects/{projectId}/canvases'
```

#### 3.3 テストキャンバス作成

```bash
# テスト開始時刻のプレフィックスを生成（最初の1回のみ）
PREFIX=$(date +%Y%m%d-%H%M%S)
echo "Test session: $PREFIX"

# 新規キャンバス作成（テスト用）
bin/tt-client --current POST '/projects/{projectId}/canvases' '{"name":"__SMOKE_TEST__","preset":"youtube"}'
```

レスポンスから新しいキャンバスIDを取得し、以降のテストで使用する。

**重要**: 作成したキャンバスIDを `-c` オプションで指定すること。

```bash
# 視覚確認: 空のキャンバスを確認
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-01-canvas-created.png
```

**Read ツールで確認**:
- 1280x720 の空キャンバス（白背景）が表示される

#### 3.4 テキスト要素テスト

```bash
# テキスト追加
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements' '{"type":"text","text":"スモークテスト","x":640,"y":200,"fontSize":72}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-02-text-added.png
```

**Read ツールで確認**:
- 「スモークテスト」というテキストが表示されている
- 画面上部中央（y:200）に配置されている
- フォントサイズが大きい（72pt）

```bash
# テキスト更新（elementIdをレスポンスから取得）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{elementId}' '{"text":"更新テスト","fontSize":96}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-03-text-updated.png
```

**Read ツールで確認**:
- テキストが「更新テスト」に変わっている
- フォントサイズが大きくなっている（96pt）

#### 3.5 図形要素テスト（全6種類）

```bash
# 矩形追加
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements' '{"type":"shape","shapeType":"rectangle","x":640,"y":400,"width":200,"height":100,"fill":{"type":"solid","color":"#FF0000"}}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-04-shape-rectangle.png
```

**Read ツールで確認**:
- 赤い矩形が中央下部に表示されている

```bash
# 円追加
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements' '{"type":"shape","shapeType":"circle","x":300,"y":400,"width":100,"height":100}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-05-shape-circle.png
```

**Read ツールで確認**:
- 円が左側に追加されている

```bash
# 星追加
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements' '{"type":"shape","shapeType":"star","x":900,"y":400,"width":120,"height":120,"sides":5}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-06-shape-star.png
```

**Read ツールで確認**:
- 5つの角を持つ星が右側に表示されている

```bash
# 三角形追加
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements' '{"type":"shape","shapeType":"triangle","x":100,"y":500,"width":100,"height":100}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-07-shape-triangle.png
```

**Read ツールで確認**:
- 三角形が左下に追加されている

```bash
# 多角形追加（六角形）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements' '{"type":"shape","shapeType":"polygon","x":250,"y":500,"width":100,"height":100,"sides":6}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-08-shape-polygon.png
```

**Read ツールで確認**:
- 六角形が追加されている

```bash
# 矢印追加
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements' '{"type":"shape","shapeType":"arrow","x":400,"y":500,"width":150,"height":80}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-09-shape-arrow.png
```

**Read ツールで確認**:
- 矢印が追加されている
- 全6種類のシェイプが画面上に配置されている

#### 3.6 画像要素テスト

**必須**: テスト用画像を準備してから実行。

```bash
# テスト画像の準備（なければスクリーンショット等を使用）
# 例: screencapture -x ./tmp/test-image.png
# または: sips -s format png --out ./tmp/test-image.png /System/Library/Desktop\ Pictures/Solid\ Colors/Blue.png --resampleWidth 400

# 画像追加
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements' '{"type":"image","imagePath":"./tmp/test-image.png","x":200,"y":300}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-10-image-added.png
```

**Read ツールで確認**:
- 画像が左上（x:200, y:300）に配置されている
- 画像が正しく表示されている（破損していない）

#### 3.7 背景設定テスト

```bash
# 単色背景
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}' '{"background":{"type":"fill","fill":{"type":"solid","color":"#3498db"}}}'

# Issue #107 対応: PATCH 後に少し待機（非同期更新完了のため）
sleep 0.5

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-11-bg-solid.png
```

**Read ツールで確認**:
- 背景が青色（#3498db）に変わっている
- すべての要素がその上に表示されている

```bash
# グラデーション背景（縦方向、angle:90）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}' '{"background":{"type":"fill","fill":{"type":"gradient","gradientType":"linear","angle":90,"stops":[{"color":"#e74c3c","position":0},{"color":"#9b59b6","position":1}]}}}'
sleep 0.5

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-12-bg-gradient-v.png
```

**Read ツールで確認**:
- 背景が赤→紫の縦グラデーションになっている（上から下）

```bash
# グラデーション背景（横方向、angle:0）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}' '{"background":{"type":"fill","fill":{"type":"gradient","gradientType":"linear","angle":0,"stops":[{"color":"#2ecc71","position":0},{"color":"#3498db","position":1}]}}}'
sleep 0.5

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-13-bg-gradient-h.png
```

**Read ツールで確認**:
- 背景が緑→青の横グラデーションになっている（左から右）

```bash
# グラデーション背景（対角線、angle:45）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}' '{"background":{"type":"fill","fill":{"type":"gradient","gradientType":"linear","angle":45,"stops":[{"color":"#f39c12","position":0},{"color":"#e74c3c","position":1}]}}}'
sleep 0.5

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-14-bg-gradient-d.png
```

**Read ツールで確認**:
- 背景がオレンジ→赤の対角線グラデーションになっている

```bash
# 透明背景に戻す
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}' '{"background":{"type":"transparent"}}'
sleep 0.5

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-15-bg-transparent.png
```

**Read ツールで確認**:
- 背景が白色（透明）に戻っている

#### 3.8 回転テスト

```bash
# テキストを回転（45度）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{textElementId}' '{"rotation":45}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-16-rotation-text.png
```

**Read ツールで確認**:
- テキストが45度傾いている

```bash
# 図形を回転（90度）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{shapeElementId}' '{"rotation":90}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-17-rotation-shape.png
```

**Read ツールで確認**:
- 図形が90度回転している

```bash
# 画像を回転（-30度）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{imageElementId}' '{"rotation":-30}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-18-rotation-image.png
```

**Read ツールで確認**:
- 画像が-30度（反時計回り）回転している

#### 3.9 シャドウ・縁取りテスト

```bash
# テキストにシャドウ追加
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{textElementId}' '{"shadow":{"color":"#000000","blur":10,"offsetX":5,"offsetY":5,"opacity":0.5}}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-19-shadow-text.png
```

**Read ツールで確認**:
- テキストに黒い影が付いている（右下方向）

```bash
# テキストに縁取り追加（袋文字）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{textElementId}' '{"stroke":{"color":"#ffffff","width":3}}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-20-stroke-text.png
```

**Read ツールで確認**:
- テキストに白い縁取りが付いている

```bash
# テキストに二重縁取り追加
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{textElementId}' '{"secondStroke":{"color":"#000000","width":6}}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-21-double-stroke-text.png
```

**Read ツールで確認**:
- テキストに白+黒の二重縁取りが付いている

```bash
# 図形にシャドウ追加
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{shapeElementId}' '{"shadow":{"color":"#333333","blur":8,"offsetX":3,"offsetY":3,"opacity":0.7}}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-22-shadow-shape.png
```

**Read ツールで確認**:
- 図形に影が付いている

```bash
# 図形に縁取り追加
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{shapeElementId}' '{"stroke":{"color":"#2c3e50","width":4}}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-23-stroke-shape.png
```

**Read ツールで確認**:
- 図形に縁取りが付いている

```bash
# 画像にシャドウ追加
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{imageElementId}' '{"shadow":{"color":"#000000","blur":15,"offsetX":8,"offsetY":8,"opacity":0.6}}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-24-shadow-image.png
```

**Read ツールで確認**:
- 画像に影が付いている

```bash
# 画像に縁取り追加
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{imageElementId}' '{"stroke":{"color":"#ffffff","width":5}}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-25-stroke-image.png
```

**Read ツールで確認**:
- 画像に白い縁取りが付いている

#### 3.10 要素操作テスト

```bash
# 要素の重なり順変更
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements/{elementId}/actions/bring-to-front'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-26-reorder.png
```

**Read ツールで確認**:
- 指定した要素が最前面に移動している

```bash
# 要素複製
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements/{elementId}/actions/duplicate'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-27-duplicate.png
```

**Read ツールで確認**:
- 同じ要素が複製されて表示されている（位置が少しずれている）

```bash
# 要素削除
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID DELETE '/projects/{projectId}/canvases/{canvasId}/elements/{elementId}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-28-deleted.png
```

**Read ツールで確認**:
- 指定した要素が消えている

#### 3.11 Undo/Redo テスト

```bash
# Undo実行（直前の削除を取り消す）
bin/tt-client undo

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-29-undo.png
```

**Read ツールで確認**:
- 削除した要素が復活している

```bash
# Redo実行（取り消しを取り消す = 再度削除）
bin/tt-client redo

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-30-redo.png
```

**Read ツールで確認**:
- 要素が再度削除されている

#### 3.12 プレビューテスト

```bash
# プレビュー画像を取得して保存
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-31-preview.png
```

**Read ツールで確認**:
- 640x360 のプレビュー画像が生成されている
- すべての要素が正しくレンダリングされている

#### 3.13 エクスポートテスト

```bash
# PNG エクスポート（フル解像度）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID export -o ./tmp/${PREFIX}-step-32-export.png
```

**Read ツールで確認**:
- 1280x720 のフル解像度画像が生成されている
- プレビューよりも高解像度で鮮明

#### 3.14 バッチ操作テスト

```bash
# 複数要素の一括作成
# 注意: フィールド名は "data" (Issue #110 参照)
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements/batch' '{"operations":[{"action":"create","data":{"type":"shape","shapeType":"circle","x":900,"y":100,"width":80,"height":80,"fill":{"type":"solid","color":"#1abc9c"}}},{"action":"create","data":{"type":"shape","shapeType":"circle","x":1000,"y":100,"width":80,"height":80,"fill":{"type":"solid","color":"#e67e22"}}}]}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-step-33-batch.png
```

**Read ツールで確認**:
- 2つの円が右上に追加されている（緑とオレンジ）

#### 3.15 テキスト詳細プロパティテスト

```bash
# フォント変更
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{textElementId}' '{"fontName":"Helvetica Neue"}'

# 文字間隔調整
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{textElementId}' '{"letterSpacing":5}'

# アウトライン（袋文字）設定
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{textElementId}' '{"outline":{"enabled":true,"color":"#000000","width":4}}'

# テキストにグラデーション塗り
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{textElementId}' '{"textFill":{"type":"gradient","gradientType":"linear","angle":90,"stops":[{"color":"#FF00FF","position":0},{"color":"#00FFFF","position":1}]}}'

# 要素確認
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID elements text
```

#### 3.16 シェイプ詳細プロパティテスト

```bash
# 角丸設定（rectangle）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{rectangleElementId}' '{"cornerRadius":20}'

# ストローク幅・色変更
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{shapeElementId}' '{"strokeWidth":5,"strokeColor":"#FF0000"}'

# 多角形の辺数変更（polygon）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{polygonElementId}' '{"sides":8}'

# シェイプにグラデーション塗り
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}/elements/{shapeElementId}' '{"fill":{"type":"gradient","gradientType":"radial","angle":0,"stops":[{"color":"#FFFFFF","position":0},{"color":"#000000","position":1}]}}'
```

#### 3.17 放射状グラデーションテスト

```bash
# 放射状グラデーション背景
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}' '{"background":{"type":"fill","fill":{"type":"gradient","gradientType":"radial","angle":0,"stops":[{"color":"#ffffff","position":0},{"color":"#000000","position":1}]}}'
sleep 0.5
```

#### 3.18 エクスポート形式テスト

```bash
# PNG エクスポート（デフォルト）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID export -o ./tmp/export-png.png

# JPEG エクスポート（品質指定）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID export -o ./tmp/export-jpg-high.jpg --format jpeg --quality 0.95

# JPEG エクスポート（低品質）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID export -o ./tmp/export-jpg-low.jpg --format jpeg --quality 0.5

# ファイルサイズ比較
ls -lh ./tmp/export-*.{png,jpg}

# 解像度確認
sips -g pixelWidth -g pixelHeight ./tmp/export-png.png ./tmp/export-jpg-high.jpg
```

#### 3.19 プロジェクト操作テスト

```bash
# 新規プロジェクト作成
bin/tt-client POST /api/v1/projects '{"name":"__SMOKE_TEST_PROJECT__"}'
# レスポンスから TEST_PROJECT_ID を取得

# プロジェクト切り替え
bin/tt-client POST "/api/v1/projects/${TEST_PROJECT_ID}/switch"

# プロジェクト一覧で確認
bin/tt-client /projects

# 元のプロジェクトに戻す
bin/tt-client POST "/api/v1/projects/${PROJECT_ID}/switch"

# テストプロジェクト削除
bin/tt-client DELETE "/api/v1/projects/${TEST_PROJECT_ID}"
```

#### 3.20 キャンバス操作テスト

```bash
# キャンバス複製
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/duplicate'
# レスポンスから DUPLICATED_CANVAS_ID を取得

# 複製されたキャンバスの確認
bin/tt-client -p PROJECT_ID -c DUPLICATED_CANVAS_ID canvas

# キャンバスクリア
bin/tt-client -p PROJECT_ID -c DUPLICATED_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/clear'

# クリア確認（要素が空になっているはず）
bin/tt-client -p PROJECT_ID -c DUPLICATED_CANVAS_ID canvas

# 複製キャンバス削除
bin/tt-client -p PROJECT_ID DELETE "/projects/{projectId}/canvases/${DUPLICATED_CANVAS_ID}"
```

#### 3.21 エラーハンドリングテスト

```bash
# 存在しないプロジェクトID
bin/tt-client GET /api/v1/projects/INVALID_PROJECT_ID/canvases
# 期待: エラーレスポンス、適切なエラーメッセージ

# 存在しないキャンバスID
bin/tt-client -p PROJECT_ID GET '/projects/{projectId}/canvases/INVALID_CANVAS_ID'
# 期待: エラーレスポンス

# 不正なJSON
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements' '{"type":"text"'
# 期待: JSON parse エラー

# 必須フィールド不足
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements' '{"type":"text"}'
# 期待: バリデーションエラー（text フィールド必須）

# 不正なカラーフォーマット
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}' '{"background":{"type":"fill","fill":{"type":"solid","color":"invalid"}}}'
# 期待: カラーフォーマットエラー
```

#### 3.22 パフォーマンステスト

```bash
# 大量エレメント作成（50個のシェイプ）
# バッチ操作で一度に作成
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

bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements/batch' "$OPERATIONS"

# プレビュー生成時間計測
time bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/performance-preview.png

# エクスポート時間計測
time bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID export -o ./tmp/performance-export.png

# キャンバスクリア（後続テストのため）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/clear'
```

#### 3.23 実用的なシナリオテスト（YouTubeサムネイル作成）

**このテストでは、実際にプロダクションで使用できるYouTubeサムネイルを作成します。各ステップで視覚確認を行い、プロ級の仕上がりを確認してください。**

```bash
# シナリオ: YouTubeサムネイルを実際に作成
# 1. 背景グラデーション
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID PATCH '/projects/{projectId}/canvases/{canvasId}' '{"background":{"type":"fill","fill":{"type":"gradient","gradientType":"linear","angle":135,"stops":[{"color":"#667eea","position":0},{"color":"#764ba2","position":1}]}}}'
sleep 0.5

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-yt-step-1-background.png
```

**Read ツールで確認**:
- 紫〜青の対角線グラデーションが美しく表示されている

```bash
# 2. メインタイトル（大きい白文字、黒縁取り、シャドウ）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements' '{"type":"text","text":"HOW TO CODE","x":640,"y":250,"fontSize":120,"textFill":{"type":"solid","color":"#FFFFFF"},"outline":{"enabled":true,"color":"#000000","width":8},"shadow":{"enabled":true,"color":"#00000080","offset":{"x":5,"y":5},"radius":10}}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-yt-step-2-title.png
```

**Read ツールで確認**:
- 「HOW TO CODE」が中央上部に大きく表示されている
- 白文字に黒縁取りがあり、読みやすい
- 影が適度に付いて立体感がある

```bash
# 3. サブタイトル（小さめ黄色文字）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements' '{"type":"text","text":"in 10 minutes","x":640,"y":400,"fontSize":48,"textFill":{"type":"solid","color":"#f1c40f"}}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-yt-step-3-subtitle.png
```

**Read ツールで確認**:
- 「in 10 minutes」が中央に黄色で表示されている
- メインタイトルとのバランスが良い

```bash
# 4. 装飾シェイプ（星）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements' '{"type":"shape","shapeType":"star","x":200,"y":150,"width":100,"height":100,"fill":{"type":"solid","color":"#f39c12"},"rotation":15}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-yt-step-4-star.png
```

**Read ツールで確認**:
- オレンジ色の星が左上に配置されている
- 15度傾いて動きがある

```bash
# 5. 装飾シェイプ（矢印）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements' '{"type":"shape","shapeType":"arrow","x":1050,"y":500,"width":180,"height":90,"fill":{"type":"solid","color":"#e74c3c"}}'

# 視覚確認（必須）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-yt-step-5-arrow.png
```

**Read ツールで確認**:
- 赤い矢印が右下に配置されている
- サムネイル全体のバランスが良い

```bash
# 6. プレビュー確認（640x360）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID preview -o ./tmp/${PREFIX}-yt-preview.png
```

**Read ツールで確認**:
- すべての要素が調和している
- YouTubeサムネイルとして使えるクオリティ

```bash
# 7. フル解像度エクスポート（1280x720）
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID export -o ./tmp/${PREFIX}-yt-final.png
```

**Read ツールで確認**:
- 高解像度（1280x720）で鮮明
- テキストが読みやすく、プロ級の仕上がり
- **このサムネイルは実際にYouTubeで使用できるレベル**

#### 3.24 出力品質検証

```bash
# Preview vs Export の解像度比較
echo "=== Preview ==="
sips -g pixelWidth -g pixelHeight ./tmp/smoke-test-preview.png

echo "=== Export ==="
sips -g pixelWidth -g pixelHeight ./tmp/smoke-test-export.png

# ファイルサイズ比較
echo "=== File Sizes ==="
ls -lh ./tmp/smoke-test-*.png ./tmp/export-*.{png,jpg} 2>/dev/null

# 画像確認（ユーザーに視覚確認を依頼）
echo "視覚確認: 以下の画像を開いて、テキスト・図形が正しくレンダリングされているか確認してください"
echo "- ./tmp/youtube-thumbnail-final.png"
echo "- ./tmp/smoke-test-export.png"
```

#### 3.25 ImageStore & 保存・復元テスト

**前提**: テスト用画像ファイルが必要。なければ `./tmp/test-image.png` を作成する。

```bash
# テスト画像の準備（既存のサンプル画像またはスクリーンショットを使用）
# 例: cp /path/to/sample.png ./tmp/test-image.png

# --- 2.12.1 重複排除テスト ---

# 2つ目のテストキャンバスを作成
bin/tt-client --current POST '/projects/{projectId}/canvases' '{"name":"__SMOKE_TEST_2__","preset":"youtube"}'
# レスポンスから TEST_CANVAS_ID_2 を取得

# 同じ画像を両方のキャンバスに追加
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID POST '/projects/{projectId}/canvases/{canvasId}/elements' '{"type":"image","imagePath":"./tmp/test-image.png","x":400,"y":300}'

bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID_2 POST '/projects/{projectId}/canvases/{canvasId}/elements' '{"type":"image","imagePath":"./tmp/test-image.png","x":400,"y":300}'

# --- 2.12.2 保存テスト ---

# ワークスペースを保存
bin/tt-client POST '/workspace/save'
# success: true を確認、path からワークスペースURLを取得

# --- 2.12.3 ImageStore 重複排除検証 ---

# /status からワークスペースパスを取得
bin/tt-client /status
# workspaceUrl フィールドを確認

# ImageStore/images/ のファイル数を確認
# 同じ画像を2キャンバスに追加したが、1ファイルのみ存在すべき
ls WORKSPACE_PATH/ImageStore/images/ | wc -l
# 期待値: 1（重複排除が機能している）

# --- 2.12.4 復元テスト ---

# ワークスペースを再読み込み
bin/tt-client POST '/workspace/load' '{"path":"WORKSPACE_PATH"}'

# 要素が正しく復元されているか確認
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID elements image
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID_2 elements image
# 両方のキャンバスに画像要素が存在することを確認

# --- 2.12.5 未使用画像削除テスト ---

# 画像を削除
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID DELETE '/projects/{projectId}/canvases/{canvasId}/elements/IMAGE_ELEMENT_ID'

# 保存（この時点で TEST_CANVAS_ID_2 にはまだ同じ画像がある）
bin/tt-client POST '/workspace/save'

# ImageStore を確認（まだ画像は残っているはず）
ls WORKSPACE_PATH/ImageStore/images/ | wc -l
# 期待値: 1（別キャンバスで使用中）

# 2つ目のキャンバスの画像も削除
bin/tt-client -p PROJECT_ID -c TEST_CANVAS_ID_2 DELETE '/projects/{projectId}/canvases/{canvasId}/elements/IMAGE_ELEMENT_ID_2'

# 再保存
bin/tt-client POST '/workspace/save'

# ImageStore を確認（未使用画像が削除されるはず）
ls WORKSPACE_PATH/ImageStore/images/ | wc -l
# 期待値: 0（未使用画像が削除された）

# --- 2.12.6 クリーンアップ（テスト用キャンバス2） ---

bin/tt-client -p PROJECT_ID DELETE '/projects/{projectId}/canvases/TEST_CANVAS_ID_2'
```

**検証ポイント**:

| テスト項目 | 期待結果 |
|-----------|---------|
| 重複排除 | 同じ画像を2キャンバスに追加しても ImageStore 内は1ファイル |
| 保存 | POST /workspace/save が success: true を返す |
| 復元 | 再読み込み後も画像要素が正しく表示される |
| 未使用削除 | 全キャンバスから削除後、保存すると ImageStore から画像が消える |

#### 3.26 クリーンアップ

```bash
# テストキャンバスを削除
bin/tt-client -p PROJECT_ID DELETE '/projects/{projectId}/canvases/TEST_CANVAS_ID'

# 一時ファイル削除
rm -f ./tmp/smoke-test-*.png ./tmp/export-*.{png,jpg} ./tmp/youtube-thumbnail*.png ./tmp/performance-*.png
```

### 4. 結果レポート

以下の形式で結果を報告:

```markdown
## スモークテスト結果

**実行日時**: YYYY-MM-DD HH:MM:SS
**アプリバージョン**: (statusから取得)

### テスト結果サマリー

| カテゴリ | テスト項目 | 結果 | 備考 |
|---------|-----------|:----:|------|
| 接続 | /status | ✅/❌ | |
| 接続 | /help | ✅/❌ | |
| プロジェクト | 一覧取得 | ✅/❌ | |
| キャンバス | 作成 | ✅/❌ | |
| キャンバス | 削除 | ✅/❌ | |
| テキスト | 追加 | ✅/❌ | |
| テキスト | 更新 | ✅/❌ | |
| 図形 | 矩形追加 | ✅/❌ | |
| 図形 | 円追加 | ✅/❌ | |
| 図形 | 星追加 | ✅/❌ | |
| 図形 | 三角形追加 | ✅/❌ | |
| 図形 | 多角形追加 | ✅/❌ | 六角形 |
| 図形 | 矢印追加 | ✅/❌ | |
| 画像 | 追加 | ✅/❌ | |
| 背景 | 単色 | ✅/❌ | |
| 背景 | グラデーション（縦） | ✅/❌ | |
| 背景 | グラデーション（横） | ✅/❌ | |
| 背景 | グラデーション（対角） | ✅/❌ | |
| 背景 | 透明 | ✅/❌ | |
| 回転 | テキスト回転 | ✅/❌ | 45度 |
| 回転 | 図形回転 | ✅/❌ | 90度 |
| 回転 | 画像回転 | ✅/❌ | -30度 |
| シャドウ | テキスト | ✅/❌ | |
| シャドウ | 図形 | ✅/❌ | |
| シャドウ | 画像 | ✅/❌ | |
| 縁取り | テキスト（単一） | ✅/❌ | |
| 縁取り | テキスト（二重） | ✅/❌ | |
| 縁取り | 図形 | ✅/❌ | |
| 縁取り | 画像 | ✅/❌ | |
| 要素操作 | 重なり順変更 | ✅/❌ | |
| 要素操作 | 複製 | ✅/❌ | |
| 要素操作 | 削除 | ✅/❌ | |
| Undo/Redo | Undo | ✅/❌ | |
| Undo/Redo | Redo | ✅/❌ | |
| プレビュー | 画像取得 | ✅/❌ | |
| エクスポート | PNG出力 | ✅/❌ | |
| バッチ | 一括作成 | ✅/❌ | |
| テキスト詳細 | フォント変更 | ✅/❌ | |
| テキスト詳細 | 文字間隔 | ✅/❌ | |
| テキスト詳細 | アウトライン | ✅/❌ | 袋文字 |
| テキスト詳細 | グラデーション塗り | ✅/❌ | |
| シェイプ詳細 | 角丸 | ✅/❌ | rectangle |
| シェイプ詳細 | ストローク | ✅/❌ | |
| シェイプ詳細 | 辺数変更 | ✅/❌ | polygon |
| シェイプ詳細 | グラデーション塗り | ✅/❌ | radial |
| 背景 | 放射状グラデーション | ✅/❌ | |
| エクスポート | JPEG高品質 | ✅/❌ | quality: 0.95 |
| エクスポート | JPEG低品質 | ✅/❌ | quality: 0.5 |
| プロジェクト | 作成 | ✅/❌ | |
| プロジェクト | 切り替え | ✅/❌ | |
| プロジェクト | 削除 | ✅/❌ | |
| キャンバス | 複製 | ✅/❌ | |
| キャンバス | クリア | ✅/❌ | |
| エラー処理 | 無効なID | ✅/❌ | 適切なエラー |
| エラー処理 | 不正JSON | ✅/❌ | パースエラー |
| エラー処理 | 必須フィールド不足 | ✅/❌ | バリデーションエラー |
| エラー処理 | 不正カラー | ✅/❌ | フォーマットエラー |
| パフォーマンス | 大量エレメント | ✅/❌ | 50個のシェイプ |
| パフォーマンス | プレビュー速度 | ✅/❌ | time計測 |
| パフォーマンス | エクスポート速度 | ✅/❌ | time計測 |
| シナリオ | YouTubeサムネイル | ✅/❌ | 実用的な作成 |
| 品質検証 | 解像度確認 | ✅/❌ | Preview vs Export |
| 品質検証 | 視覚確認 | ✅/❌ | レンダリング品質 |
| ImageStore | 重複排除 | ✅/❌ | 同一画像が1ファイルのみ |
| ImageStore | 保存 | ✅/❌ | POST /workspace/save |
| ImageStore | 復元 | ✅/❌ | 再読み込み後も画像表示 |
| ImageStore | 未使用削除 | ✅/❌ | 削除後に ImageStore からクリーンアップ |

### 全体結果

- **成功**: XX / YY
- **失敗**: ZZ / YY

### 失敗したテストの詳細

（失敗がある場合のみ記載）

| テスト | エラー内容 | 推奨アクション |
|--------|-----------|---------------|
| テスト名 | エラーメッセージ | 対応方法 |

### 発見された問題

（テスト中に発見した問題があれば記載）

1. [問題の説明]
   - 再現手順: ...
   - 期待動作: ...
   - 実際の動作: ...
```

### 5. 問題発見時の対応

テストで問題が見つかった場合は、以下の手順で対応すること。

#### 5.1 クラッシュ発生時の対応（必須）

アプリがクラッシュした場合（`bin/tt-client` が応答しない、Signal エラー等）:

**Step 1: クラッシュログを収集**

```bash
bin/tt-crash-log
```

このコマンドで最新のクラッシュログを取得。古いログの場合は警告が表示される。

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
- **Thread**: [クラッシュしたスレッド]

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

**Step 3: debugger エージェントで詳細分析（必要に応じて）**

複雑なクラッシュの場合は debugger エージェントを起動:

```
Task({
  subagent_type: "debugger",
  model: "opus",
  description: "Analyze crash from smoke test",
  prompt: "スモークテスト中にクラッシュが発生しました。bin/tt-crash-log の出力を分析し、根本原因を特定してください。"
})
```

#### 5.2 API エラー・バグ発見時の対応

クラッシュ以外のバグ（API エラー、予期しない動作等）を発見した場合:

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

## 関連ファイル

- [関連するソースファイル]

## 修正案（判明している場合）

[修正の提案]
```

#### 優先エージェント

| 問題の種類 | 使用エージェント | モデル |
|-----------|-----------------|--------|
| クラッシュ、例外 | **debugger** | opus |
| UI/View の問題 | **swiftui-macos-designer** | opus |
| 非同期/スレッド問題 | **swift-concurrency-expert** | opus |
| API/通信の問題 | **tt-api-expert** | opus |
| パフォーマンス問題 | **swiftui-performance-expert** | opus |

**重要**: 複数の問題が見つかった場合は、各問題に対応するエージェントを **並行実行** して調査を効率化する。Opus モデルのフル活用で徹底的な原因分析を行う。

### 6. tt-client / API Server への改善提案

テスト中に以下のような点に気づいた場合は、改善提案として記録する:

#### 記録対象

- **tt-client の使いづらさ**: オプションの不足、エラーメッセージの不明瞭さ、ショートカットの追加要望
- **API の設計改善**: エンドポイントの統一性、レスポンス形式の改善、不足している機能
- **ドキュメントの不備**: `/help` の情報不足、エラーコードの説明不足

#### Issue フォーマット

`issues/NNN-tt-client-and-api-improvements.md` 形式で記録:

```markdown
# Issue NNN: tt-client / API Server 改善提案

**ステータス**: 未着手

## tt-client 改善

### 1. [改善タイトル]
**現状**: 現在の動作
**要望**: 期待する動作
**理由**: なぜ必要か
**優先度**: 高/中/低

## API Server 改善

### 1. [改善タイトル]
**エンドポイント**: 該当するパス
**現状**: 現在の動作
**要望**: 期待する動作
**理由**: なぜ必要か
**優先度**: 高/中/低

## 優先度サマリー

| 項目 | 対象 | 優先度 |
|------|------|--------|
| xxx | tt-client | 高 |
| yyy | API Server | 中 |
```

#### 改善提案の例

- `bin/tt-client export -o ./output.png` のようなショートカットコマンドの追加
- エラー時の具体的なヒント表示（例: 「Canvas not found. Use /canvases to list available canvases」）
- `--dry-run` オプションで実行前の確認
- バッチ操作の進捗表示
- API レスポンスへの `_links` フィールド追加（HATEOAS）

## 注意事項（subagent 向け）

- テストは **順番に** 実行すること（並行実行しない）
- 各コマンドの実行結果を確認してから次に進むこと
- エラーが発生した場合は、その時点で一旦停止し、原因を調査すること
- テストキャンバス名 `__SMOKE_TEST__` は必ずクリーンアップすること
- `./tmp/` ディレクトリが存在しない場合は作成すること
- **Issue #107 対応**: 背景変更後は `sleep 0.5` で非同期更新完了を待つこと
- **クラッシュ発生時**: debugger エージェント（opus）で徹底的に原因分析し、Issues に記録
- **視覚確認**: Preview/Export 画像は Read ツールで表示し、テキスト・図形のレンダリングを確認

## スキル実行者向けの注意

このスキル（/smoke-test）を実行する場合：

1. **必ず AskUserQuestion で2つの質問をすること**
   - テスト範囲（クイック/標準/完全）
   - 使用モデル（Sonnet推奨/Opus/Haiku）

2. **必ず Task ツールで subagent を起動すること**
   - subagent_type: "general-purpose"
   - model: ユーザーの選択に基づく
   - prompt: テスト範囲と指示を含める

3. **自分でテストを実行しないこと**
   - このファイルのセクション3以降は subagent が参照する情報
   - スキル実行者は質問→subagent起動のみを行う

## トラブルシューティング

### アプリに接続できない場合

```bash
# api.json の確認
cat ~/Library/Containers/com.jiikko.thumbnailthumb/Data/Library/Application\ Support/ThumbnailThumb/api.json

# プロセス確認
pgrep -l ThumbnailThumb
```

### ワークスペースが開かれていない場合

ユーザーに以下を案内:
1. ThumbnailThumb アプリを起動
2. 新規ワークスペースを作成、または既存のワークスペースを開く
3. 再度 `/smoke-test` を実行
