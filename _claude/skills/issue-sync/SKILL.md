---
name: issue-sync
version: 3.0.0
description: Open Issues のうち実は完了済みのものを検出し、done/ への移動 (readme に一覧テーブルを持つ repo ではその更新も) を行う。「issueを整理して」「issue-sync」「完了issueを片付けて」「done漏れを直して」で発火。
---

# Issue Sync

Open Issues の中から、実際にはコードベース上で対応済み（done）になっている issue を検出し、done/ への移動を行うコマンド。readme に issue 一覧テーブルを持つ repo では、その表も実体に合わせて更新する。

**前提**: プロジェクトが `issues/` + `issues/done/` 運用であること（ディレクトリ名が `issue/` の
プロジェクトでは読み替える）。`done/` が無ければ、その旨を報告して終了する（勝手に作らない）。

**状態の正本はディレクトリ**: どの issue が open かは**ファイルの置き場所**が決める
（`issues/` 直下 / `next/` / `epic/<name>/` / `epic/<name>/next/` = open、`pending/` = 着手保留、
`done/` = 完了）。readme の一覧テーブルは
**任意の派生物**で、持つ repo と持たない repo がある（例: dotfiles の `issues/README.md` は
命名規約ドキュメントで一覧表を持たない）。テーブルを起点にすると、テーブルの無い repo で
空振りし、ある repo でもテーブルの記載漏れがそのまま検査漏れになる。

## 手順

### Step 0: 期限つき issue の点検（最初に報告する）

`issues/` 配下（`done/` を除く）の本文メタ行 `期限: YYYY-MM-DD` を拾い、**期限切れ / 3 日以内**のものを
**この skill の出力の冒頭で報告する**（done 判定より先。人間待ちの issue = `NNN-human-*.md`
は放置すると価値が腐るため、埋もれさせない）。

```sh
# コロンは全角も拾う (`期限：` と書かれた 1 件を黙って取りこぼすのが最悪の失敗)。
# 🚨 glob を直接渡さない: zsh は `issues/*.md` が 0 件だと nomatch でコマンド全体を失敗させ、
# pending / next / epic 側に期限切れがあっても「0 件」と黙る (2026-08-20 実測)。
# done だけを prune し、残りは固定 2 段を含めて find で列挙する
find issues -path issues/done -prune -o -type f -name '*.md' -print0 \
  | xargs -0 grep -lE '^期限[:：]' 2>/dev/null | while read -r f; do
      printf '%s\t%s\n' "$(grep -m1 -E '^期限[:：]' "$f" | sed -E 's/^期限[:：][[:space:]]*//')" "$f"
    done | sort
```

- **`期限:` 行が 1 件も無い repo と、書式が違って拾えなかった repo を混同しない**。human issue
  (`NNN-human-*.md`) が存在するのに期限が 0 件なら、書式ミス (全角コロン以外の表記ゆれ・行頭でない)
  を疑って当該ファイルを直接開く

- **`issues/` 配下（`done/` を除く）にある = 未完了**、`issues/done/` にある = 完了（状態の唯一の出典はファイルの位置。
  本文の既読ヘッダーは見ない）
- 期限切れが 1 件でもあれば「期限切れ N 件」を見出しで出す。0 件なら「期限切れなし」と 1 行書く
  （黙って省略しない — 報告が無いのは「点検していない」と区別できないため）

### Step 1: ディレクトリから対象集合を作る

**ファイルの置き場所が状態の正本**。まず実体を列挙する（`pending/` / `next/` /
`epic/<name>/` / `epic/<name>/next/` を含み、`done/` だけ除外する）:

```sh
find issues -path issues/done -prune -o -type f -name '*.md' -print0
```

- **`issues/` 直下 / `issues/epic/<name>/` = open**（検証対象）
- **`issues/pending/` = 着手保留**（open だが着手条件待ち。検証はするが、報告では open と分けて出す）
- **`issues/next/` / `issues/epic/<name>/next/` = claim 中の open**（検証対象。完了時は global `done/` へ移す）
- **`issues/done/` = 完了**（対象外）
- **issue でないファイルを除外する**: `README.md` / `readme.md` / `audit-log` などの管理ファイル。
  番号規約のある repo では `^[0-9]{3}-` に一致するものだけを issue とみなすのが安全
- **`NNN-human-*.md` は Phase B の検証対象から外す**。人がやるまで open が正しい状態で、
  実装の有無では done を判定できない（自動 done 化は誤検出になる）。human の扱いは Step 0 の
  期限報告だけで完結する
- **`NNN-retro-*.md` も Phase B の検証対象から外す**。done の条件は「本文の残課題が空になったこと」
  （各項目が issue 化・rule 化・却下で決着）であり、実装の有無では判定できない。代わりに
  **未決着の項目が残っている retro を件数つきで報告する**（放置された気づきは次の retro で
  再生産されるため）。同じ点検は SessionStart hook (`_claude/hooks/retro-open.sh`) も行うが、
  hook を切っている環境でも黙らないためこの skill でも見る。書式は `issues/README.md` の
  `retro` 節が正本

### Step 2: Phase A — readme 一覧テーブルとの照合（テーブルを持つ repo のみ）

readme（`issues/readme.md` / `issues/README.md`）に issue 一覧テーブルがあるなら、
**実体（Step 1 の列挙）を正、テーブルを従**として乖離を洗う:

1. テーブルが open として載せている issue が `done/` にある → 「readme 更新漏れ」
2. `issues/` 直下にあるのにテーブルに無い → 「readme 記載漏れ」
3. テーブルにあるのに実体がどこにも無い → 「参照切れ」（rename/削除の取りこぼし）

**テーブルが無ければ Phase A は skip し、「readme に一覧テーブルが無いため Phase A は該当なし」と
1 行報告する**（黙って飛ばさない。無報告と未実施を区別できないため）。

### Step 3: Phase B — コードベース実装検証（**必須**）

**Step 1 で列挙した issue 全件**（`human` を除く）について、Agent（Explore サブエージェント）を使って以下を実行する:

1. issue ファイルを読み取り、要求される機能・修正内容を理解する
2. issue に記載された**キーとなるクラス名・関数名・型名・UIコンポーネント名**を抽出する
3. それらをコードベースで Grep/Glob して、実装の有無を判定する
4. 実装タスクのチェックリストがある場合、各項目の実装状況を確認する

**判定基準:**
- ✅ DONE: issue の主要な要求が実装済み（テストや関連コードが存在）
- ❓ PARTIAL: 一部のみ実装済み（Open のまま残す）
- 🔴 NOT DONE: 未実装

**確信が持てない場合は「未完了」として扱う**（誤検出を避ける）。

> **重要**: Phase B を省略してはならない。ディレクトリと readme の照合（Phase A）だけでは
> 「ファイルは `issues/` に残っているが、コードは実装済み」— この skill の主目的そのもの — を見逃す。
> Phase A が skip される（テーブルの無い）repo では、Phase B が唯一の検出手段になる。

### Step 4: 検出結果をユーザーに報告する

Phase A と Phase B で検出した「完了済みだが Open のまま」の issue を一覧でユーザーに報告する。

報告フォーマット:
```
## 完了検出: N 件

### Phase A: readme 一覧テーブルと実体の乖離（テーブルを持つ repo のみ）
| # | タイトル | 判定根拠 |
|---|---------|---------|
| XXX | issue タイトル | 更新漏れ: ファイルは done/ にあるがテーブルは open |
| XXX | issue タイトル | 記載漏れ: issues/ 直下にあるがテーブルに無い |

### Phase B: コードベースで実装済み（ファイルは issues/ に残存）
| # | タイトル | 判定根拠 |
|---|---------|---------|
| XXX | issue タイトル | 実装の根拠（クラス名・テスト等） |
```

- 0 件の場合は「Open Issues に完了済みの issue はありませんでした。」と報告して終了
- 1 件以上の場合は Step 5 に進む

### Step 5: ユーザー確認

AskUserQuestion で「これらの issue を `done/` へ移動しますか？」と確認する
（readme に一覧テーブルがある repo では「移動して表も更新しますか？」と聞く）。

### Step 6: done/ への移動（readme にテーブルがあればその更新も）

ユーザーが承認した場合:

1. 対象 issue ファイルを `issues/done/` に移動する（`git mv`。ファイル名は変えない — 番号での参照が腐るため）
2. readme に一覧テーブルがある repo だけ、その表を実体に合わせて更新する:
   - 該当行のパスを `./done/` に変更し、ステータスを `✅ 完了` に更新
   - 詳細セクション内の該当記述も `✅ 完了` に更新
   - テーブルが無い repo では**何も書き足さない**（一覧表を新設しない。状態の正本はディレクトリ）
3. git commit する（自分が動かしたファイルを pathspec で明示する）

## 注意事項

- **Phase B は必ず実行すること**。Phase A のみで終了してはならない
- Phase B で対象 issue が5件以上ある場合は、Agent（Explore）を複数並行起動して検証する（1エージェントあたり数件ずつ割り当てる）
- プロジェクト固有の命名規則がある場合（例: 言語/モジュール別の接頭辞が付いた issue）は、対象のソースコードを直接確認して判定する
- **判定に迷った場合は「未完了」とし、誤って done にしない**ことを優先する
- readme にテーブルがある repo では、テーブルと詳細セクションの整合性を必ず保つ
- **テーブルの無い repo で readme に一覧を新設しない**。ディレクトリと二重管理になり、片方の更新漏れが必ず起きる
