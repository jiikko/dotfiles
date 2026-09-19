# issues/ — dotfiles 固有の issue 運用

**共通規約（命名・type 語彙・状態ディレクトリ・`期限:`・`human`・`retro`・本文の書き方）は
[`_claude/issue-rules.md`](../_claude/issue-rules.md) が正本**で、issues/ を持つ repo のセッションに
SessionStart hook（`_claude/hooks/issue-rules-inject.sh`）が注入する（issue 401）。
ここには dotfiles でだけ効く道具・検査・経緯を書く。共通規約をここへ写さない。

## 採番

次番号の確認（`find` で深さを切らずに数え、origin 側も数える）:

```sh
git fetch origin
{ find issues -type f -name '[0-9][0-9][0-9]-*.md' | sed 's|.*/||'; git ls-tree -r --name-only origin/master -- issues | sed 's|.*/||'; } |
  grep -E '^[0-9]{3}-' | sort | tail -1
```

（`ls` でディレクトリを列挙しない: `epic/<name>/` の 2 段を数え漏らす）

- **番号の一意性は `tests/issues/test_issue_numbers_unique.sh` が検査する**（`make test` に自動発見で含まれる）。
  2026-08-28 に 127 と 133 が同時に衝突していたのを人手で見つけたのが起点
- 並行セッションと同時に採番するときは、番号を取る前に一声かける。衝突したら**参照の少ない側を空き番号へ寄せる**
  （`grep -rn '<番号>'` と `git log --grep='issue <番号>'` の両方を数え、commit message から参照されている側は
  動かさない）。改番したファイルの冒頭に「旧番号の話ならこの issue」と注記を残す（実例: 135 と 136）

## 完了: `scripts/issue_done.sh <NNN>`

🚨 **done への移動は手でやらない**。`scripts/issue_done.sh <NNN>` が ①`done/`（group issue は
`epic/<name>/done/`）へ移動 ②`next/` の claim symlink を削除 ③本文の相対リンクを張り直し
④他 issue からの参照を張り直し、を 1 コマンドで行い、リンク検査（`tests/issues/test_issue_links_valid.sh` /
`test_next_links_valid.sh`）が落ちたら移動を戻す。手作業では 2026-09-09 に 12 件中 4 回落とした（issue 313）。
issues/ の外から markdown リンク `[issue 012](../issues/done/012-….md)` で書いた参照も張り直しの対象になる。

## 検査と表示

- `期限:` の書式は `tests/issues/test_human_issues_have_deadline.sh` が検査する（箇条書き・全角コロン・日付不正で落ちる。
  issue 375 が `- 期限:` で hook と `issue-sync` から黙って漏れていた）
- `next/` の symlink の有効性（`../<同名>` の形に固定）は `tests/issues/test_next_links_valid.sh`、claim された issue に
  担当者バナーがあることは `tests/issues/test_next_claims_have_banner.sh`（fixture は `…_fixtures.sh`。issue 403）
- glogx の issues viewer（`i` キー）: `human` タブは件数 0 でも All の右に固定で出る。`n` で `next/` の claim を
  付け外しする。viewer は期限を表示しない。`epic/<name>/` は `▸ <name> (N ✓done)` の親行に折り畳まれ、
  group 名と同じ番号の issue だけが親行に統合される。**契約の一次情報は
  [`docs/issues-viewer-spec.md`](../docs/issues-viewer-spec.md)**
- 🚨 group 内の `waiting/`（`epic/<name>/waiting/`）は viewer 未対応（迷子 `?` になる）。`closed/` のような綴りの揺れも迷子
- 関わった issue の本文更新漏れは Stop hook `_claude/hooks/issue-progress-check.sh` が差し戻す（開始時 HEAD は
  `issue-progress-start.sh` が記録）。human / retro の催促は `human-tasks-due.sh` / `retro-open.sh`（共通処理は
  `_claude/hooks/lib/issue-hooks.sh`）

## 番号なしファイル

- `audit-log` — audit 実行の記録（TSV）。**issue ファイルをパスで参照しているため、既存ファイルを rename するとここの参照が切れる**

## 経緯

- 番号付け規約は 2026-07-16 導入。それ以前のファイルは同日に一括 rename 済み（作成日順に 001〜017。audit-log・
  コード内コメント・docs・issue 間リンクも同時更新。commit message 内の旧パスは immutable なため対象外）
