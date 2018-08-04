# Mac
* 動作確認しているバージョン
  * Sierra

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
brew install jvgrep
brew install coreutils
brew install jq
brew install ruby-completion
brew install gem-completion
brew install bundler-completion
brew install rake-completion
brew install rails-completion
brew install mysql
brew install v8
brew install homebrew/dupes/screen --HEAD
brew install imagemagick ghostscript
brew install qt
brew install zsh
brew install phantomjs
brew install rbenv
brew install pyenv-virtualenv
```

### GUI tools
```
brew cask install coteditor
brew cask install Caskroom/cask/xquartz
brew cask install google-japanese-ime
brew cask install firefox
brew cask install google-chrome
brew cask install virtualbox
brew cask install kindle
brew cask install logitech-control-center
brew cask install skitch
brew cask install night-owl
brew cask install intel-power-gadget
brew cask install licecap
brew cask install tuxgitter
brew cask install vagrant
```
### for Job
```
brew install redis
brew cask install mactex
brew cask install Caskroom/cask/xquartz
brew install homebrew/x11/xpdf
brew cask install java
```
### ???
```
brew install youtube-dl
brew cask install vlc
```

## Import karabinar-elements config
```shell
brew cask install karabiner-elements
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
バックグラウンドにするとsuspendしてしまう(謎)
```shell
./vim_setup.sh
```

## Change login shell
```
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
