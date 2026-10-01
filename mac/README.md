# Mac

- 動作確認しているバージョン
  - Sierra
  - Mojave

## Install Command Line Tools

```shell
xcode-select --install
```

## Install homebrew

```shell
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
brew update
brew upgrade
```

## Install packages

```
brew bundle
```

## Setup system config

キーリポートなど

```shell
curl https://raw.githubusercontent.com/jiikko/dotfiles/master/mac/setup_system.sh | sh
```

## Textlint
```shell
npm i -g textlint textlint-rule-preset-ja-technical-writing
```

* `textlint --preset ja-technical-writing [校正対象ファイル]`

## Setup Terminal

```shell
cd ~
git clone git@github.com:jiikko/dotfiles.git || git clone https://github.com/jiikko/dotfiles.git
cd dotfiles
./setup.sh
```

`setup.sh` は Terminal.app のプロファイル (`mac/ClaudeWarm.terminal`) も既定にする (`scripts/terminal_profile_restore.sh`)。

### Setup Neovim

`setup.sh` が `_nviminit.lua` を `~/.config/nvim/init.lua` に張る。初回の起動で lazy.nvim が自分を取得し、
プラグインは repo の `_lazy-lock.json` の版で入る。

### Setup Karabiner

`mac/karabiner.json` は symlink ではなくコピーで置く。編集したら復元し直す:

```shell
bin/restore_karabiner_config.sh
```

## Change login shell

```
# if Catalina
chsh -s /bin/zsh
# else
echo "/opt/homebrew/bin/zsh" | sudo tee -a /etc/shells
chsh -s "/opt/homebrew/bin/zsh"
```

## Generate ssh key

```
ssh-keygen -t rsa -b 4096 -C "jiikko"
```

## Other

### Manual

- 壁紙変更
- 音量変更音を有効
- 設定 -> アクセシビリティ -> マウス/トラックパッド -> トラックパッドオプション -> ドラッグを有効にする「3 本指のドラッグ」
