# Mac
* 動作確認しているバージョン
  * Sierra
  * Mojave

## Install Command Line Tools
```shell
xcode-select --install
```

## Install homebrew
```shell
ruby -e "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/master/install)"
brew update
brew upgrade
```

### for Development
シムリンクとかの案内が表示されるので手動でやる
```
brew install wget
brew install nkf
brew install vim
brew install awscli
brew install jvgrep
brew install coreutils
brew install jq
brew install ruby-completion
brew install gem-completion
brew install bundler-completion
brew install rake-completion
brew install rails-completion
brew install mysql@5.7
brew install v8
brew install screen --HEAD
brew install imagemagick
brew install qt
brew install zsh
brew install rbenv
brew install pyenv-virtualenv
```

### GUI tools
```
brew install coteditor --cask
brew install Caskroom/cask/xquartz --cask
brew install firefox --cask
brew install google-chrome --cask
brew install virtualbox --cask
brew install kindle --cask
brew install homebrew/cask-drivers/logitech-control-center
brew install kensington-trackball-works --cask
brew install skitch --cask
```
### for Job
```
brew install redis
brew cask install mactex
brew cask install Caskroom/cask/xquartz
brew install homebrew/x11/xpdf
brew cask install java
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
echo "/usr/local/bin/zsh" | sudo tee -a /etc/shells
chsh -s /usr/local/bin/zsh
```

## Generate ssh key
```
ssh-keygen -t rsa -b 4096 -C "jiikko"
```

## Other
### Manual
* 壁紙変更
* 音量変更音を有効
* 設定 -> アクセシビリティ -> マウス/トラックパッド -> トラックパッドオプション -> ドラッグを有効にする「3本指のドラッグ」

### karabiner設定ファイルのエクスポート
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
