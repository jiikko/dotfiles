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
- [x] push + `~/dotfiles` pull 後、新しいセッションで注入されることを確認
- [x] `~/.claude/CLAUDE.md`「Issue管理」を「hook が注入する規約に従う」の 1 行 + repo 非依存の項目へ縮める
- [x] dotfiles `issues/README.md` を repo 固有部分だけにする
- [x] project-templates (macos/ios) の README を repo 固有の雛形だけにし、「直接変更せず」注記を直す
- [x] テンプレート由来の 7 アプリの README を縮める
- [x] obaket / ThumbnailThumb の README から共通部分を抜く (固有節は残す)

## 進捗

- 「docs(issues,401): issue 運用規約を _claude/issue-rules.md に集め、issues/ がある repo にだけ注入する」
  - hook 直叩き: issues/ なしの git repo = 0 byte / 同じ repo に issues/ を作ると 9221 byte (A-B)
  - 変異 3 本すべて red: 判定を外す (常に注入) / 読めない時に黙る / 本文を先頭 5 行に切る
  - 🚨 my-products root でも注入される (`project-templates/issues` が深さ 1 の issues/ に当たる)。実害なしと判断

- 「docs(issues,401): CLAUDE.md「Issue管理」と dotfiles issues/README.md を固有部分へ縮め、正本の参照を張り替える」
  - 新規セッション E2E: `claude -p --model haiku` で SnapTrim (issues/ あり) = 注入あり (見出し 1 行を引用) /
    issues/ なしの git repo = NO
  - CLAUDE.md の `docs/` 項 (dotfiles 固有) は repo の CLAUDE.md へ移設。retro の流入速度の根拠と Stop hook を issue-rules へ移設
  - 旧正本を指していた hook / テスト / skill / docs のコメント 9 箇所を張り替え。関連テスト 10 本 green

- my-products 側 (各 submodule で commit & push、親で bump `chore(submodule): bump issues/README を共通規約 ... 10 submodule`)
  - project-templates macos/ios: 共通規約を抜いた雛形へ (「直接変更せず」注記も直した)
  - 7 アプリ (baby-note / dotfiles-gui / dropbox-… / fdup-macos / SnapTrim / vlc / promiseApp) を新雛形へ。vlc の固有節「あとで登録したいissue」は残した
  - ThumbnailThumb: 太字 `**作成日**` 由来の注意・採番コマンドの所在・現状の group だけ残した
  - obaket: 種別語彙と human/retro 書式の節を案内へ置換。「macOS/issues/ は督促されない」注記は issue 276 以降事実と食い違っていたため削除
- 「docs(issues,401): 共通規約から特定 repo 名を外し、human の本文に手順を書く指示を足す」

## 残タスク

- なし (受け入れ条件はすべて完了)
