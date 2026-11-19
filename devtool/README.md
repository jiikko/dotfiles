# devtool/

repo や CI から呼ばれる開発の道具の置き場。いずれ別の repo (仮称 toolbox) へ切り出す前提で、dotfiles の設定と分けて置く (epic issue 1091)。

## 約束

- **devtool/ の中から devtool/ の外を参照しない**。依存の向きは dotfiles → devtool だけ。
  dotfiles の `bin/`・`src/`・`scripts/`・`_claude/` をパスで指さない (`make test-devtool-boundary` が止める。止める形と検出しない形は `scripts/check_devtool_boundary.sh` の冒頭)
- dotfiles の道具が要るときは、dotfiles の `bin/devtool-run` が解決して環境変数で渡す。devtool の道具は PATH を探さない
- テストは dotfiles の `tests/devtool/` に置く (`tests/` だけを走査する lint と runner の対象にそのまま入れるため)。切り出すときに一緒に移す

## 起こし方

各 repo の wrapper (例: app の `bin/mutate`) が、dotfiles の checkout からの絶対パスで `bin/devtool-run <道具> [引数…]` を起こす。
PATH には頼らない (`export PATH` は `_zshrc` にしか無く、非対話のシェルは読まない)。

## 環境変数 (devtool-run が渡す)

| 名前 | 中身 |
|---|---|
| `DEVTOOL_RUNTIMEOUT` | runtimeout のバイナリの絶対パス。起動の前に 1 回だけ解決する (自動で再ビルドするラッパーを run の途中で通さない)。解決できなければ空 |
| `DEVTOOL_SKILL_TMPDIR` | skill-tmpdir の絶対パス |

## 道具

| 道具 | 何をするか | 呼ぶ repo |
|---|---|---|
| `mutate` | Swift のアプリの実装をわざと壊し (変異)、テストが落ちるかを RED / GREEN / 判定不能で出す。今の作業ツリーで当てて開始時の commit へ戻す (未 commit の変更は拒否)。使い捨ての worktree で当てる `mutate-verify` とは別の道具 | obaket・vlc-multi-video-player (`bin/mutate`) |
