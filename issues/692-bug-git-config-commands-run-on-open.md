# 692 (bug): 開いたフォルダの `.git/config` に仕込まれたコマンドが、git の呼び出しで走る (treefiler・glogx・pro-con)

> 🚨 **担当中: Claude Code (dotfiles-53。監査の issue を順に直すセッション)**（2026-10-09〜）

起票日: 2026-10-09

## 概要

`.git/config` の `core.fsmonitor=<cmd>` は `git status` のたびに、`diff.<drv>.textconv=<cmd>` (+ `.gitattributes`) は `git diff` のたびに
そのコマンドを実行する。treefiler は開いたフォルダ (と開いているフォルダが属する入れ子の repo すべて) で `git status` を**キー操作なしに**走らせ、
タイルの `d` で `git diff` を走らせる。git 2.55.0 で、fsmonitor と textconv の両方がマーカーファイルを作るのを確かめた (697 の監査)。

## 詳細

- 攻撃経路: `git clone` では `.git/config` は渡らない。tarball・zip・共有ドライブで配られた「repo」を展開して開くと走る。`safe.directory` は
  所有者が違うときしか守らない (自分で展開したものは通る)
- treefiler で範囲が広がる: 入れ子の repo ごとに `git status` を取る (spec §5.3) ので、展開した配下の repo を開くだけで踏む
- 🚨 **treefiler だけの穴ではない**。fsmonitor の対策は src 全体で 0 件。textconv は pro-con の 2 箇所 (`src/pro-con/dispatcher/progress.go`・
  `src/pro-con/wtclean/git.go` の `--no-textconv`) だけが対策済みで、pro-con の `gitx` は GIT_CONFIG_* の環境変数を掃除している (設定ファイルは守らない)。
  treefiler の `filer/diff.go` は `--no-ext-diff` だけで `--no-textconv` が無い (反証レビューで訂正 2026-10-09。起票時は「0 件」と誤って書いた)
- どのコマンドで走るか (git 2.55.0 で実測。`core.fsmonitor` と `diff.x.textconv` にマーカーを作るコマンドを仕込んだ repo):

  | コマンド | fsmonitor | textconv |
  |---|---|---|
  | status / diff --stat | 走る | 走らない |
  | diff / blame | 走る | 走る |
  | log -p / show | 走らない | 走る |
  | log / log --stat / rev-parse / worktree list / branch | 走らない | 走らない |

  `-c core.fsmonitor=false` と `--no-textconv` で止まることも確かめた。どの版から status で fsmonitor が走るかは調べていない
- 該当する呼び出し: glogx の `d` (`src/glogx/gitlog.go` の `LoadCommitDiff` の `show --patch`。textconv)・`diff --cached` (同ファイル。両方)・
  `src/glogx/external_commands.go` の `status --porcelain` (fsmonitor)。pro-con の `-C dir status` と diff 系 (`dispatcher/triage.go` など) も fsmonitor が未対策
- シェルのプロンプトが git を呼ぶ場合も同じ形 (未確認)

## 対応方針

- treefiler: git の起動を 1 か所に寄せ (`filer/git.go` の `fetchRepo`・`filer/diff.go` の `gitDiffCommand`)、`-c core.fsmonitor=false` と、diff に
  `--no-textconv` を付ける。寄せた口を lint で強制するかは 696
- glogx・pro-con: 同じ付け方をどこに置くか (subproc に git 専用の口を足すか、pro-con の `gitx` を共通にするか) を決める。glogx の diff は
  色付けのために textconv を使っていないかを先に確かめる
- 重要度: 前提が「tarball・zip で配られた repo を開く」なので P2 寄りでもよい (反証レビュー)。ただし treefiler は開くだけで (キー操作なしに) 走る

## 関連ファイル

- `src/treefiler/filer/git.go` (`fetchRepo`)・`src/treefiler/filer/diff.go` (`gitDiffArgs`)・`src/glogx/` の git の呼び出し・`src/subproc/`
- 監査の記録: 697

## 進捗

- [ ] 未着手
