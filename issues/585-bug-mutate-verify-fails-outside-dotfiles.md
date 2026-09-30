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

- [x] dotfiles の外の git repo で `bin/mutate-verify` が baseline → 変異 → 判定まで走る（swift-smbee のような別 repo で 1 回実測する）
- [x] 上のテスト（helper をコピーしない fixture、symlink / PATH 経由の起動を含む）があり、helper の読み先を `$root` に戻す変異で red
- [x] `mutation-verify-new-tests.md` の記述と実際に動く範囲が一致している

## 進捗

- 2026-09-30: 起票（swift-smbee retro 100 の項目 3）。codex の反証レビューで中心の診断は反証されず、「必ず見つからない」の言い過ぎ・
  既存 fixture が helper をコピーして退行を隠す点・symlink / PATH 起動と `mutate-verify-list` の確認漏れを指摘され、直した
- 2026-09-30: 対応（commit「mutate-verify: helper を検査対象の repo ではなく道具の実体の隣から読む」）
  - `bin/mutate-verify`: helper を `$root` ではなく、symlink を辿った実体の `../scripts/lib/` から読む（`bin/codex-fanout` と同じ辿り方）。
    親へ上がるのは `cd -P`（`bin/` の dir ごとの symlink から起動すると、シェルの `cd <dir>/..` は symlink の置き場所の親へ行く。
    試した起動の変種の中でこれだけ外れた）。`# shellcheck source=` は `SCRIPTDIR/../scripts/lib/worktree_scratch.sh`
  - `bin/mutate-verify-list`: 兄弟の `mutate-verify` も `$0` の symlink を辿った実体の隣で探す（symlink だけを別の dir に置くと
    「実行できない」で止まっていた。テストで再現）
  - テスト: `make_repo` と mutate-verify-list の「本物と繋ぐ」fixture から helper のコピーを外した（全ケースが dotfiles の外の repo の
    形で走る）。`test_mutate_verify.sh` のケース 43（symlink / PATH / bin の dir ごとの symlink）、`test_mutate_verify_list.sh` のケース 8
    （symlink した mutate-verify-list が兄弟を見つける）を足した
  - 変異（`bin/mutate-verify` で当てた。どれも狙った検査で red）: 読み先を `$root` に戻す → ケース 1 から全ケース /
    mutate-verify の symlink 辿りを外す → ケース 43 の symlink・path / `cd -P` を `cd` に戻す → ケース 43 の dirlink だけ /
    mutate-verify-list の symlink 辿りを外す → ケース 8
  - 実測: swift-smbee はこのマシンに無いので esa-cli（helper を持たない、clean）で `cmd/esa/columns.go` の `dateOnly` に変異を当てた。
    直す前の版は起票時と同じ `scripts/lib/worktree_scratch.sh を読めない` で rc=2、直した後は `FAIL: TestDateOnly` の red で rc=0。
    相対パス・`bash <相対パス>`・空白を含む dir の相対 symlink の連鎖からの起動も rc=0。esa-cli の作業ツリーは clean のまま、worktree の残骸なし
  - rule: 34 行目に「dotfiles の `bin/` は PATH 上にあり、どの repo でも `mutate-verify` で呼べる」を足した
  - `make test` rc=0（`[ok] tests/bin/test_mutate_verify.sh` / `[ok] tests/bin/test_mutate_verify_list.sh`）
  - 外部レビュー（敵対的レビュー）は通していない。代わりに起動の変種を実測で試し、外れた 1 つ（dir ごとの symlink）を直してテストに固定した
