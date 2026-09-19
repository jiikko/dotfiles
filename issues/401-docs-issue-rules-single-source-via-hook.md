# 401 (docs): issue 運用規約を 1 箇所に集め、issues/ を持つ repo にだけ hook で注入する

起票日: 2026-09-19

## 概要

issue 運用規約が `~/.claude/CLAUDE.md`「Issue管理」/ dotfiles `issues/README.md` /
`project-templates/{macos,ios}/issues/README.md` / my-products 各アプリの `issues/README.md` に
重複している。コピーは伝播せず、2026-09-19 時点で 6 アプリ (baby-note / dotfiles-gui /
dropbox-multi-video-player-electron / fdup-macos / SnapTrim + vlc) がテンプレートより古いまま
(human / risk / task / next / epic の記述が欠けていた)。

- symlink は採らない: 各アプリは独立 repo (submodule) で、repo 外を指す link は単体 clone / CI で dangling になる
- `~/.claude/rules/` は採らない: issues/ を持たない仕事の repo でも毎セッション全文読まれる
- `paths:` frontmatter は採らない: Read でしか発火せず Write/Edit では発火しない (2026-08-27 実測)

→ 正本 `_claude/issue-rules.md` を SessionStart hook `issue-rules-inject.sh` が
`issue_hook_resolve_dir` (他の issue 系 hook と同じ判定) の成立時だけ注入する。
type 語彙は全 repo の和集合 (ユーザー判断 2026-09-19)。

## 受け入れ条件

- [x] `_claude/issue-rules.md` (共通規約) を作る
- [x] hook `issue-rules-inject.sh` + `_claude/settings.json` 配線 + `tests/claude/test_issue_rules_inject.sh`
- [ ] push + `~/dotfiles` pull 後、新しいセッションで注入されることを確認
- [ ] `~/.claude/CLAUDE.md`「Issue管理」を「hook が注入する規約に従う」の 1 行 + repo 非依存の項目へ縮める
- [ ] dotfiles `issues/README.md` を repo 固有部分だけにする
- [ ] project-templates (macos/ios) の README を repo 固有の雛形だけにし、「直接変更せず」注記を直す
- [ ] テンプレート由来の 7 アプリの README を縮める
- [ ] obaket / ThumbnailThumb の README から共通部分を抜く (固有節は残す)

## 進捗

- 「docs(issues,401): issue 運用規約を _claude/issue-rules.md に集め、issues/ がある repo にだけ注入する」
  - hook 直叩き: issues/ なしの git repo = 0 byte / 同じ repo に issues/ を作ると 9221 byte (A-B)
  - 変異 3 本すべて red: 判定を外す (常に注入) / 読めない時に黙る / 本文を先頭 5 行に切る
  - 🚨 my-products root でも注入される (`project-templates/issues` が深さ 1 の issues/ に当たる)。実害なしと判断

## 残タスク

- 未着手: 上の受け入れ条件の未チェック分
- 未検証: 新規セッションでの実注入 (hook の直叩きのみ確認)
