# issues/ — issue 管理

## ファイル命名規約（2026-07-16 導入）

新規 issue は次の形式で命名する:

```
issues/NNN-<カテゴリ>-<スラッグ>.md
```

- **NNN**: 3 桁ゼロ埋めの連番。**issues/ 直下・pending/・done/ の全体**で最大番号 + 1 を採番する（番号は再利用しない）。pending/ や done/ へ移動してもファイル名は変えないため、コードコメント・commit message から「issue 012」で安定して参照できる
- **カテゴリ**: 下表の prefix のいずれか
- **スラッグ**: kebab-case の短い説明。日付を残したい場合は末尾に `-YYYY-MM-DD`

| prefix | 用途 |
|---|---|
| `feat` | 新機能・機能拡張 |
| `bug` | 不具合修正 |
| `refactor` | 挙動を変えない構造改善・複雑性削減 |
| `perf` | 速度・メモリ・リソースの改善（実測の裏付けを本文に置く） |
| `docs` | ドキュメント・ルール・コメント整備 |
| `chore` | 雑務（依存更新・メッセージ/表示の手直し・テストの前提整理など、機能でも不具合でもないもの） |
| `research` | 調査・設計検討（成果物がコードでないもの） |
| `verify` | **人にやってほしい動作確認**（Claude が書いた確認手順。人が確認するまで open。`期限:` 必須） |

例: `issues/001-refactor-makefile-test-autodiscovery.md` / `issues/002-bug-nvim-cterm-drift-2026-07-16.md`

次番号の確認:

```sh
ls issues issues/pending issues/done | grep -E '^[0-9]{3}-' | sort | tail -1
```

## `期限:` — 人が読む期限を本文に書く

本文冒頭のメタ行（`起票日:` の隣）に `期限: YYYY-MM-DD` を書ける。

- **`verify` は必須**（動作確認は放置すると価値が腐るため）。他カテゴリは任意
- 書式は**行頭 `期限:` + 半角コロン + `YYYY-MM-DD`**。全角コロンでも `issue-sync` は拾うが、
  表記は半角に揃える（拾えなかった期限は「期限なし」と区別できず黙って埋もれる）
- 期限は「読んで確認する期限」であって「直す期限」ではない
- **既読の唯一の出典はファイルの位置**（`issues/` にある = 未読、`issues/done/` にある = 確認済み）。
  既読ヘッダー・チェックボックスは使わない（本文を書き換え忘れると嘘が残るため、移動で表す）
- 未読の一覧は glogx の issues viewer（`i` キー）が見せる（ただし viewer は期限を表示しない）
- **未読と期限切れはセッション開始時に自動で出る**: `_claude/hooks/verify-issues-due.sh`（SessionStart
  hook。配線は `_claude/settings.json`）が未確認の `verify` issue と期限切れ／期限間近を Claude の
  コンテキストへ注入する。読み取れなかったもの（期限なし・書式不正・読み取り不可・抽出失敗）も
  黙って捨てず列挙する。`issue-sync` skill でも同じ点検を最初に行う

## ディレクトリ構成

- `issues/*.md` — open な issue
- `issues/pending/` — 着手を保留している issue の置き場（着手条件・trigger を本文冒頭に書いておく）
- `issues/done/` — 完了した issue の移動先（ファイル名は変えずに移動）
- `audit-log` — audit 実行の記録（TSV）。issue ではない。**issue ファイルをパスで参照しているため、既存ファイルを rename するとここの参照が切れる**
- この `README.md` も issue ではない

## 運用ルール（詳細は `~/.claude/CLAUDE.md`「Issue管理」と `_claude/rules/`）

- 対応が完了したら `issues/done/` へ移動する
- issue の新規作成・大幅改訂は commit 前に codex レビューへ通す（[`issue-creation-codex-review.md`](../_claude/rules/issue-creation-codex-review.md)）
- issue の記述を鵜呑みにしない。着手前に実コードと git 履歴で検証する（既に修正済み・false positive を弾く）

## 既存ファイルの番号付け（2026-07-16 実施済み）

規約導入以前のファイルは 2026-07-16 に一括 rename 済み（作成日順に 001〜017 を採番。audit-log・コード内コメント・docs・issue 間クロスリンクのパスも同時更新済み。commit message 内の旧パスは immutable なため対象外）。
