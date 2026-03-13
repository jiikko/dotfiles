# Forge スキルトリガー定義

この文書は Forge skill が自動起動するスキルの定義です。

---

## スキルカテゴリ

| カテゴリ | 実行タイミング | 目的 |
|---------|-------------|------|
| **VALIDATION** | Phase 3.5（実装後、専門家レビュー前） | 実装品質の自動検証 |
| **TESTING** | Phase 5.5（修正収束後、レポート前） | 完成品の動作検証 |
| **DIAGNOSTIC** | Phase 4.5+（条件付き） | 問題検出時の診断 |

---

## スキルトリガーレジストリ

| スキル名 | カテゴリ | 検出パターン | モード制約 | 実行方式 |
|---------|---------|-------------|----------|---------|
| style-review | VALIDATION | `.css`, `.scss`, `.module.css`, `styled-components`, CSS-in-JS | Minimum+ 以上（CSS変更検出時） | 自動 |
| smoke-test | TESTING | `bin/tt-client` 存在、ThumbnailThumb 関連ファイル変更 | Maximum 以上 | 確認付き |
| perf-analysis | DIAGNOSTIC | Phase 4 で `performance` カテゴリの High 指摘あり | Standard 以上 | 確認付き |
| ios-simulator-skill | TESTING | `.xcodeproj`/`project.yml` + iOS ターゲット変更 | Maximum 以上 | 確認付き |

### 実行方式の定義

| 方式 | 動作 |
|------|------|
| **自動** | ユーザー確認なしで実行。結果はレポートに含める |
| **確認付き** | AskUserQuestion で実行可否を確認してから実行 |

---

## モード別動作

| スキルカテゴリ | Minimum | Minimum+ | Standard | Maximum | Ultra |
|-------------|---------|----------|----------|---------|-------|
| VALIDATION | **省略** | CSS検出時自動 | 自動 | 自動 | 自動 |
| TESTING | **省略** | **省略** | **省略** | 確認付き | 確認付き |
| DIAGNOSTIC | **省略** | **省略** | 確認付き | 確認付き | 確認付き |

---

## スキル候補の特定（Phase 0）

Phase 0（要件確認）完了時に、以下のロジックでスキル候補を特定する。

### 検出手順

1. 変更対象ファイルの拡張子とパスパターンを検出パターンと照合
2. 該当するスキルを `applicable_skills` リストに追加
3. モード制約でフィルタ（選択されたモードで実行可能なスキルのみ残す）
4. Phase 0 確認時にユーザーに表示:

```
スキル自動実行予定:
- style-review (VALIDATION, Phase 3.5) - CSS ファイルの変更を検出
- smoke-test (TESTING, Phase 5.5) - ThumbnailThumb 関連の変更を検出 [要確認]
```

### 検出パターン詳細

| スキル | ファイルパターン | コンテンツパターン |
|-------|---------------|-----------------|
| style-review | `*.css`, `*.scss`, `*.module.css` | `@media`, `color:`, `background:`, CSS 変数 |
| smoke-test | `Sources/**/*.swift` (TT プロジェクト) | `bin/tt-client` が存在する場合のみ |
| perf-analysis | _(ファイルパターンなし)_ | Phase 4 結果の `severity: high` + `category: performance` |
| ios-simulator-skill | `*.xcodeproj`, `project.yml` | iOS ターゲット (`SDKROOT = iphoneos`) |

---

## Phase 3.5: スキル検証（VALIDATION）

### 実行条件

- `applicable_skills` に VALIDATION カテゴリのスキルが存在
- Phase 3 のセルフレビューが完了（BLOCKER 0 件）
- **Minimum+ の場合**: CSS/SCSS ファイルの変更が検出されていること
- **Standard 以上の場合**: 変更ファイルに関わらず常時実行

### 実行手順

1. 該当スキルの SKILL.md に定義されたフローを実行
2. 結果を Phase 4 のエージェントコンテキストに含める
3. **High 指摘検出時**: BLOCKER 扱いとして Phase 2 に戻る
4. **Medium 以下のみ**: Phase 4 に進行（結果をエージェントに共有）

### style-review 自動実行

Phase 2 で変更された CSS/SCSS ファイルを自動検出し、style-review スキルのフローを実行する。

```
1. Glob で変更対象の *.css, *.scss, *.module.css を特定
2. style-review SKILL.md の「css-expert 指示テンプレート」を使用
3. css-expert エージェントを Task ツールで起動
4. 結果の JSON を解析し、High/Medium/Low を分類
5. High あり → Phase 2 に戻る
6. High なし → 結果を記録して Phase 4 へ
```

---

## Phase 5.5: スキルテスト（TESTING）

### 実行条件

- `applicable_skills` に TESTING カテゴリのスキルが存在
- Phase 5 の修正が収束済み
- モードが Maximum 以上

### 実行手順

1. AskUserQuestion でテスト実行を確認:

```
質問: 「以下のスキルテストを実行しますか？」

選択肢:
1. すべて実行
2. 選択して実行
3. スキップ

該当スキル:
- smoke-test: アプリ全体の動作確認
- ios-simulator-skill: iOS シミュレーターでのテスト
```

2. 承認されたスキルのフローを実行
3. 結果を完了レポートに追加
4. **失敗時**: Phase 5 に戻り、修正サイクルを再開

---

## Phase 4.5+: 診断スキル（DIAGNOSTIC）

### 実行条件

- Phase 4/4.1/4.2 の統合結果に `performance` カテゴリの High 指摘が存在
- または Phase 4.5 の debugger がパフォーマンス関連の問題を特定

### 実行手順

1. AskUserQuestion で実行を提案:

```
質問: 「パフォーマンス問題が検出されました。perf-analysis を実行しますか？」

選択肢:
1. 実行する
2. Issue として記録し、後で対応
3. スキップ
```

2. 承認された場合、perf-analysis フローを実行
3. 結果を Phase 5 の修正対象に含める

---

## レジストリの拡張

新しいスキルをトリガーに追加する手順:

1. このファイルのトリガーレジストリテーブルに行を追加
2. 検出パターン詳細テーブルに行を追加
3. 該当カテゴリの Phase セクションにスキル固有の実行手順を追加
4. **Phase フローの変更は不要**（カテゴリ単位で自動的にゲートされる）
