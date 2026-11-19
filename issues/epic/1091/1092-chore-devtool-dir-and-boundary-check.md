# 1092 (chore): devtool/ の置き場・起動役・テストの発見・外を参照しない境界の検査を作る

起票日: 2026-11-19

## 概要

`devtool/` を作り、dotfiles 側の起動役・テストと lint の発見・境界の検査を用意する。1093・1094 の前提。

## 決めたこと (ユーザー決定 2026-11-19)

- 依存の向きは **dotfiles → devtool だけ**。devtool から dotfiles の `bin/`・`src/`・`scripts/`・`_claude/` を参照しない
- **機能は今と変えない**。各 repo の呼び口 (`bin/mutate` など) は wrapper として残してよい。本体の判定を devtool の 1 か所にまとめるのが目的
- 既にある道具 (dupscan など) は今は動かさない。統合するもの・新しく書くものから置く

## 設計

- **起動役 `bin/devtool-run <道具> [引数…]`** (dotfiles 側。名前は実装時に確定):
  devtool の道具が使う dotfiles の道具を解決して環境変数で渡し、`devtool/bin/<道具>` を exec する。
  - runtimeout は `bin/lib/runtimeout.sh` の `runtimeout_resolve` で **バイナリの絶対パスに 1 回だけ**解決して `RUNTIMEOUT` で渡す
    (PATH の `runtimeout` は自動で再ビルドするラッパーで、run の途中の再ビルドの失敗 rc=1 が変異の RED に化ける。`bin/lib/go_tool.sh` の注記)
  - skill-tmpdir は絶対パスを渡す
  - devtool の道具は、渡されなければ「判定不能」で止まる (黙って PATH を探さない)
  - 切り出す日 (1095) には、この起動役の置き場と中身を決め直す
- **呼ぶ側 (各 repo の wrapper)** は dotfiles の checkout を今の環境変数 (例 `VGP_MUTATE_DOTFILES_ROOT`、無ければ `~/dotfiles`) で見つけ、
  `<dotfiles>/bin/devtool-run` を絶対パスで起こす。PATH に頼らない: `export PATH` は `_zshrc` にしか無く、非対話の `zsh -c`・launchd・codex のシェルは読まない
- PATH への `devtool/bin` の追加はしない (頼る経路を作らない)。名前で呼ばれている道具を移すとき (1094) は、`bin/<名前>` に `../devtool/bin/<名前>` への symlink を置いて呼び名を保つ

## テストと lint の発見 (2026-11-19 に確認: どれも devtool/ を見ない)

- `Makefile` の `run_tests` / `run_tests_parallel` は渡された根 (`tests`、`SERIAL_TEST_DIRS`、`CI_HEAVY_TEST_DIRS`) の下だけを `find` する。根に `devtool/tests` を足す (`SERIAL_PRUNE`・`CI_HEAVY_PRUNE` との関係も揃える)
- bats の発見 (`find tests`)、`scripts/test_changed.sh` の写像、`scripts/discover_shell_scripts.sh` の `LINT_DIRS` にも足す
- 受け入れ条件: `make test` と CI の出力に devtool のテスト名が出ること・shellcheck の対象に devtool/ のファイルが入ることを、出力で確かめる
  (足さないまま 1094 でテストを移すと、今ある回帰テストが気づかれないまま回らなくなる)

## 境界の検査

`make test` に 1 本足す。止める形:

- `devtool/` を出る相対パス (`../` を含む参照全般。devtool の root は 1 つの helper で決め、それ以外の `..` を禁止する)
- `$HOME/dotfiles`・`~/dotfiles`・`/Users/…/dotfiles`
- `# shellcheck source=` が devtool の外を指すもの
- devtool の外を指す symlink (`find devtool -type l`)
- Go の `go.mod` の replace で devtool を出るもの

検出しない形 (検査の冒頭に書く): 変数を重ねて組んだパス、PATH 上の素の名前で dotfiles の道具を呼ぶ形 (起動役の外で呼ばないことは README とレビューで守る)。
脅威モデルは自分の書き間違い。対象 0 件なら落とす。破る fixture を形ごとに当てて red を確かめる。

## 進捗

- [ ] devtool/ の骨組み・README (要る環境変数と起動役の約束)
- [ ] 起動役
- [ ] テスト・bats・lint・test_changed の発見に devtool/ を足し、出力で確認
- [ ] 境界の検査 (形ごとに変異で red)
- [ ] `docs/README.md`・`CLAUDE.md` に 1 行ずつ
