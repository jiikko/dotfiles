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
| `docs` | ドキュメント・ルール・コメント整備 |
| `research` | 調査・設計検討（成果物がコードでないもの） |

例: `issues/001-refactor-makefile-test-autodiscovery.md` / `issues/002-bug-nvim-cterm-drift-2026-07-16.md`

次番号の確認:

```sh
ls issues issues/pending issues/done | grep -E '^[0-9]{3}-' | sort | tail -1
```

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

- **015 は `done/git-log-gha-status-wrapper.md` に予約済み（未 rename）**: 参照元の src/glog が rename 時点で作業中（dirty）だったため保留。glog の作業が落ち着いたら `015-feat-git-log-gha-status-wrapper.md` へ rename し、src/glog/README.md・src/glog/main.go 内の参照を同時更新すること
