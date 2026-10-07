# dotfiles

**対象は macOS のみ。Linux はサポート対象外**です (2026-08-28 決定 / issue 133)。
Linux でも動きそうに見える箇所がありますが、意図的な対応ではありません
(例: `scripts/tmux_extract_popup.sh` は `pbcopy` を無条件で呼び、`zshlib/_fs_helpers.zsh` の
`mount` パースは macOS の出力形式のみを前提にしています)。CI も macOS runner で回します。

# Installing

```
cd ~
git clone git@github.com:jiikko/dotfiles.git || git clone https://github.com/jiikko/dotfiles.git
cd dotfiles
./setup.sh
```

[for Mac](./mac "for Mac")

## 設計文書・仕様・調査記録

[`docs/README.md`](docs/README.md) が索引。触る前に読む制約 (glogx の bubbletea、テーマ色、tmux の
セッション永続化)、glogx の画面の仕様、tmux 周りの仕組み、nvim の棚卸しがある。
自作ツールの使い方は [`src/README.md`](src/README.md) から各プロジェクトの README へ。

## Git hooks

`githooks/` を `setup.sh` が `core.hooksPath` に設定する。

- `pre-commit`: ステージした差分に、成人向けを匂わせる語や作品番号の書式がないかを civility-lint
  (dotfiles の外にある private repo のツール) で検査する。本体が無いマシンでは警告だけ出して通す。
  誤検出を 1 行だけ通すなら、その行に `civility-lint:ignore` を書く
- `pre-push`: `issues/` を触る push のときだけ、push する commit を展開して issue の整合検査
  (番号の一意性・相対リンク・next の目印など `tests/issues/` の 6 本。数秒) を回し、落ちたら止める。
  今回の push が壊したものでなくても止まる。回す検査と外す検査の一覧は hook の冒頭にある

## Testing

Run the regression test suite (Neovim, tmux, setup.sh, plus existing zsh tests) with:

`make test` runs lint and the tests **all the way through** and reports every failure at the end;
a lint failure no longer stops the tests from running. Use `make test-lint` when you only want lint.

```

make test
```

You can run individual checks as well:

```
make test-syntax # zsh/zlogin/setup.sh syntax checks + tmux/nvim smoke
make test-nvim   # verifies Neovim config loads and lazy.nvim is reachable
make test-tmux   # ensures _tmux.conf can boot a tmux server (skips if tmux sockets are disallowed)
make test-setup  # exercises setup.sh in a temporary HOME
make test-shellcheck # runs shellcheck on shell-compatible scripts
make test-yaml   # yamllint on workflow/pre-commit config
make test-json   # jq validation for JSON configs
make test-lint   # aggregate lint target (shellcheck + zsh syntax + YAML + JSON + karabiner + actionlint + gitconfig + ruby syntax + the repo-wide scripts/check_*.sh gates; full list in the Makefile)
make test-src    # lint + unused check + test for all Go projects under src/ (same coverage as CI's src_*.yml)
make test-runtime # aggregate runtime target (syntax + auto-discovered tests/**/test_*.sh + bats)
make test-bats   # bats tests (skips if bats is not installed)
tests/zshrc/test_zshrc.sh  # existing zsh tests (also run via make test)
```

## ツールの使い方

自作ツールの使い方は [`docs/tools/`](docs/tools/) に置いている (索引は [`docs/README.md`](docs/README.md))。

- [tmux のキーと表示](docs/tools/tmux.md) — prefix は `C-t`。ペイン・ウィンドウ操作、popup、ウィンドウ名、Claude Code の作業状態表示
- [動画のシェル関数](docs/tools/video-functions.md) — `repair` / `av1ify` (`av1c`) / `concat`
- [macOS 連携](docs/tools/macos.md) — Karabiner-Elements / `kernel-alloc-watch` / Finder Quick Actions
- Go で書いた自作ツール (glogx・pro-con など) — [`src/README.md`](src/README.md) から各プロジェクトの README へ
