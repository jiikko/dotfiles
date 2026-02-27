---
name: issue-sync
version: 1.0.0
description: Open Issues のうち実は完了済みのものを検出し、done/ への移動と readme.md の更新を行う。
---

# Issue Sync

Open Issues の中から、実際にはコードベース上で対応済み（done）になっている issue を検出し、done/ への移動と `issues/readme.md` の更新を行うコマンド。

## 手順

### Step 1: issues/readme.md を読み取る

`issues/readme.md` を読み取り、**Open Issues テーブル**に記載されている全 issue のリストを取得する。

### Step 2: 各 open issue を検証する

Open Issues テーブルの各 issue について、以下を実行する:

1. issue ファイル（`issues/{番号}-{名前}.md`）を読み取る
2. issue の内容（問題の説明・修正方針）を理解する
3. **done/ に同じファイルが既に存在するか**を確認する（ファイルが `issues/` になく `issues/done/` にある場合、readme.md の更新漏れ）
4. done/ に存在しない場合、issue の内容に対応するコードが既に修正済みかをコードベースで検証する:
   - issue に記載された対象ファイル・関数を Grep/Read で確認
   - 修正方針に記載された変更が既に適用されているか判定
   - **確信が持てない場合は「未完了」として扱う**（誤検出を避ける）

### Step 3: 検出結果をユーザーに報告する

検出した「完了済みだが Open のまま」の issue を一覧でユーザーに報告する。

報告フォーマット:
```
## 完了検出: N 件

| # | タイトル | 判定根拠 |
|---|---------|---------|
| XXX | issue タイトル | 根拠の要約 |
```

- 0 件の場合は「Open Issues に完了済みの issue はありませんでした。」と報告して終了
- 1 件以上の場合は Step 4 に進む

### Step 4: ユーザー確認

AskUserQuestion で「これらの issue を done/ に移動して readme.md を更新しますか？」と確認する。

### Step 5: done/ への移動と readme.md の更新

ユーザーが承認した場合:

1. 対象 issue ファイルを `issues/done/` に移動する（`git mv`）
2. `issues/readme.md` を更新する:
   - Open Issues テーブルから該当行を削除
   - Completed (done/) テーブルに追加（完了日は今日の日付）
   - ディレクトリ構造ツリーを更新（必要に応じて）
3. git commit する

## 注意事項

- Go デコーダ系の issue（`go-` プレフィックス）は、対象の Go ソースコード（`decoder-go/divx/`）を直接確認して判定する
- Swift アプリ系の issue は、対象の Swift ソースコード（`VLCMultiVideoPlayer/`）を確認して判定する
- **判定に迷った場合は「未完了」とし、誤って done にしない**ことを優先する
- readme.md のディレクトリツリーと Open/Completed テーブルの整合性を必ず保つ
