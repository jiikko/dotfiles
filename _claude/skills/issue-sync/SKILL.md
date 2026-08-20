---
name: issue-sync
version: 2.2.0
description: Open Issues のうち実は完了済みのものを検出し、done/ への移動と readme.md の更新を行う。「issueを整理して」「issue-sync」「完了issueを片付けて」「done漏れを直して」で発火。
---

# Issue Sync

Open Issues の中から、実際にはコードベース上で対応済み（done）になっている issue を検出し、done/ への移動と `issues/readme.md` の更新を行うコマンド。

**前提**: プロジェクトが `issues/readme.md` + `issues/done/` 運用であること。ディレクトリ名が `issue/` のプロジェクトでは読み替える。`issues/readme.md` が無い場合は、その旨を報告して終了する（勝手に作らない）。

## 手順

### Step 0: 期限つき issue の点検（最初に報告する）

`issues/*.md` の本文メタ行 `期限: YYYY-MM-DD` を拾い、**期限切れ / 3 日以内**のものを
**この skill の出力の冒頭で報告する**（done 判定より先。動作確認 issue = `NNN-verify-*.md`
は放置すると価値が腐るため、埋もれさせない）。

```sh
grep -l '^期限:' issues/*.md 2>/dev/null | while read -r f; do
  printf '%s\t%s\n' "$(grep -m1 '^期限:' "$f" | sed 's/^期限:[[:space:]]*//')" "$f"
done | sort
```

- **`issues/` にある = 未読**、`issues/done/` にある = 確認済み（既読の唯一の出典はファイルの位置。
  本文の既読ヘッダーは見ない）
- 期限切れが 1 件でもあれば「期限切れ N 件」を見出しで出す。0 件なら「期限切れなし」と 1 行書く
  （黙って省略しない — 報告が無いのは「点検していない」と区別できないため）
- **`verify` issue を Phase B（コードベース実装検証）の対象にしない**。人が目で確認するまで
  open が正しい状態であり、実装の有無では done を判定できない（自動 done 化は誤検出になる）

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
- Phase B で対象 issue が5件以上ある場合は、Agent（Explore）を複数並行起動して検証する（1エージェントあたり数件ずつ割り当てる）
- プロジェクト固有の命名規則がある場合（例: 言語/モジュール別の接頭辞が付いた issue）は、対象のソースコードを直接確認して判定する
- **判定に迷った場合は「未完了」とし、誤って done にしない**ことを優先する
- readme.md のテーブルと詳細セクションの整合性を必ず保つ
