# 585 bug: `bin/mutate-verify` が dotfiles 以外の repo では変異を当てる前に止まる

> 🚨 **担当中: glogx/crash の作業をしていたセッション (2026-09-30 のユーザー指示)**（2026-09-30〜）

起票日: 2026-09-30
出典: swift-smbee の retro 100（issue 098 の変異検証で踏んだ。swift-smbee の `issues/100-retro-098-091-097-2026-09-30.md` 項目 3）
関連: `bin/mutate-verify` / `scripts/lib/worktree_scratch.sh` / `_claude/rules/mutation-verify-new-tests.md` / `tests/bin/test_mutate_verify.sh`

## 概要

`_claude/rules/mutation-verify-new-tests.md` は「手で組む前に `bin/mutate-verify` を見る」と**全 repo 向け**に推しているが、
`bin/mutate-verify` は dotfiles の外で実行すると、変異を当てる前に rc=2 で止まる。

```
/Users/koji/dotfiles/bin/mutate-verify: line 180: /Users/koji/src/my-products/lib/swift-smbee/scripts/lib/worktree_scratch.sh: No such file or directory
mutate-verify: scripts/lib/worktree_scratch.sh を読めない
```

（2026-09-30、swift-smbee で 2 本の変異を回そうとして 2 本とも rc=2。結局、同じ手順を worktree で手組みした）

## 原因

- 148 行目 `root="$(git rev-parse --show-toplevel 2>/dev/null)"` は**検査対象の repo** の root（cwd の repo）
- 180 行目 `. "$root/scripts/lib/worktree_scratch.sh"` がその root から helper を読む。検査対象の repo にそのパスの helper が無ければ
  `die` で rc=2 になる（swift-smbee では無かった。全 repo で無いかは確認していない）

`root` の他の使い方（`--file` の正規化・`git -C "$root"` など）は検査対象の repo を指すのが正しい。誤っているのは helper の読み先だけ。

## 対応方針（案）

- helper はスクリプト自身の置き場所から読む（例: スクリプトの実体パスの dir の `../scripts/lib/worktree_scratch.sh`）。
  `bin/` は PATH 経由・symlink 経由で呼ばれうるので、symlink を解決してから dir を取る（`BASH_SOURCE[0]` だけでは symlink は解決されない）
- `bin/mutate-verify-list` も `$0` の dirname から兄弟 CLI を探す（62 行目付近）ので、symlink で別の dir に置かれた構成で同じ形の問題が無いか合わせて見る
- `scripts/with_fresh_worktree.sh` も root 相対で source するが、dotfiles の `test-fresh` 専用なので対象外（codex の反証レビューで確認）
- `# shellcheck source=` の指定も合わせて直す
- rule 側（`mutation-verify-new-tests.md`）は、直るまでの間「dotfiles 内でのみ動く」と書くか、直してから今の書き方のままにする

## 退行を守るテスト

- `tests/bin/test_mutate_verify.sh` に、dotfiles の外（使い捨ての git repo）で `mutate-verify` を起動する scenario を足す。
  helper を検査対象の repo から読む形に戻す変異で red になることを確かめる
- 🚨 既存の fixture（`make_repo`）は helper を検査対象の repo の `scripts/lib/` へ**コピーする**。そのまま使うと、読み先を `$root` に戻す退行でも
  helper が見つかって緑のままになる。新しい scenario では helper をコピーしない（fixture が退行から見えなくなる形。codex の反証レビュー）
- symlink 経由（別の dir に置いた symlink から起動）と PATH 経由の起動も scenario に入れる

## 受け入れ条件

- [ ] dotfiles の外の git repo で `bin/mutate-verify` が baseline → 変異 → 判定まで走る（swift-smbee のような別 repo で 1 回実測する）
- [ ] 上のテスト（helper をコピーしない fixture、symlink / PATH 経由の起動を含む）があり、helper の読み先を `$root` に戻す変異で red
- [ ] `mutation-verify-new-tests.md` の記述と実際に動く範囲が一致している

## 進捗

- 2026-09-30: 起票（swift-smbee retro 100 の項目 3）。codex の反証レビューで中心の診断は反証されず、「必ず見つからない」の言い過ぎ・
  既存 fixture が helper をコピーして退行を隠す点・symlink / PATH 起動と `mutate-verify-list` の確認漏れを指摘され、直した
