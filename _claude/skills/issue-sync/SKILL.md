---
name: issue-sync
version: 2.0.0
description: Open Issues のうち実は完了済みのものを検出し、done/ への移動と readme.md の更新を行う。
---

# Issue Sync

Open Issues の中から、実際にはコードベース上で対応済み（done）になっている issue を検出し、done/ への移動と `issues/readme.md` の更新を行うコマンド。

## 手順

### Step 1: issues/readme.md を読み取る

`issues/readme.md` を読み取り、**Open Issues テーブル**（✅ 完了 以外）に記載されている全 issue のリストを取得する。

### Step 2: Phase A — ファイル所在チェック

Open Issues テーブルの各 issue について:

1. `issues/{ファイル名}` に存在するか確認
2. `issues/done/{ファイル名}` に存在するか確認
3. **done/ に存在し issues/ に存在しない** → 「readme.md 更新漏れ」として検出

このフェーズは高速に一括実行できる（bash でファイル存在チェック）。

### Step 3: Phase B — コードベース実装検証（**必須**）

**Phase A で検出されなかった issue（ファイルが issues/ に残っているもの）全件**について、Agent（Explore サブエージェント）を使って以下を実行する:

1. issue ファイルを読み取り、要求される機能・修正内容を理解する
2. issue に記載された**キーとなるクラス名・関数名・型名・UIコンポーネント名**を抽出する
3. それらをコードベースで Grep/Glob して、実装の有無を判定する
4. 実装タスクのチェックリストがある場合、各項目の実装状況を確認する

**判定基準:**
- ✅ DONE: issue の主要な要求が実装済み（テストや関連コードが存在）
- ❓ PARTIAL: 一部のみ実装済み（Open のまま残す）
- 🔴 NOT DONE: 未実装

**確信が持てない場合は「未完了」として扱う**（誤検出を避ける）。

> **重要**: Phase B を省略してはならない。Phase A だけでは「ファイルは issues/ に残っているが、コードは実装済み」のケースを見逃す。

### Step 4: 検出結果をユーザーに報告する

Phase A と Phase B で検出した「完了済みだが Open のまま」の issue を一覧でユーザーに報告する。

報告フォーマット:
```
## 完了検出: N 件

### Phase A: readme.md 更新漏れ（ファイル既に done/ に移動済み）
| # | タイトル | 判定根拠 |
|---|---------|---------|
| XXX | issue タイトル | ファイルが issues/done/ に既に存在 |

### Phase B: コードベースで実装済み（ファイルは issues/ に残存）
| # | タイトル | 判定根拠 |
|---|---------|---------|
| XXX | issue タイトル | 実装の根拠（クラス名・テスト等） |
```

- 0 件の場合は「Open Issues に完了済みの issue はありませんでした。」と報告して終了
- 1 件以上の場合は Step 5 に進む

### Step 5: ユーザー確認

AskUserQuestion で「これらの issue を done/ に移動して readme.md を更新しますか？」と確認する。

### Step 6: done/ への移動と readme.md の更新

ユーザーが承認した場合:

1. 対象 issue ファイルを `issues/done/` に移動する（`git mv`）
2. `issues/readme.md` を更新する:
   - Open Issues テーブルから該当行のパスを `./done/` に変更し、ステータスを `✅ 完了` に更新
   - 詳細セクション内の該当記述も `✅ 完了` に更新
3. git commit する

## 注意事項

- **Phase B は必ず実行すること**。Phase A のみで終了してはならない
- Phase B では issue 数が多い場合、Agent（Explore）で並列に検証すると効率的
- Go デコーダ系の issue（`go-` プレフィックス）は、対象の Go ソースコードを直接確認して判定する
- **判定に迷った場合は「未完了」とし、誤って done にしない**ことを優先する
- readme.md のテーブルと詳細セクションの整合性を必ず保つ
