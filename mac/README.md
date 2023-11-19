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

## Import karabinar-elements config

```shell
brew install karabiner-elements --cask
```

search karabiner in spotligth. enable karabinar on アクセシビリティ.

```shell
cp ~/dotfiles/mac/karabiner.json ~/.config/karabiner/karabiner.json
```

## Setup system config

キーリポートなど

```shell
curl https://raw.githubusercontent.com/jiikko/dotfiles/master/mac/setup_system.sh | sh
```

## Setup Terminal

```shell
cd ~
git clone git@github.com:jiikko/dotfiles.git || git clone ttps://github.com/jiikko/dotfiles.git
cd dotfiles
./setup.sh
```

### Setup Vim

```shell
sh -c "$(wget -O- https://raw.githubusercontent.com/Shougo/dein-installer.vim/master/installer.sh)"
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

### karabiner 設定ファイルのエクスポート

```shell
cp ~/.config/karabiner/karabiner.json ~/dotfiles/mac/karabiner.json
```

## gpg

```
brew install gpg pinentry-mac
echo "pinentry-program /usr/local/bin/pinentry-mac" >> ~/.gnupg/gpg-agent.conf
killall gpg-agent

# export secret key
gpg -a --export-secret-key XXXXXXXXX
# import secret key
gpg --import --allow-secret-key-imort sec.key
```
