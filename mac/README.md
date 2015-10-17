# Mac
## Install Command Line Tools
```shell
xcode-select --install
```

## Install homebrew
```shell
ruby -e "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/master/install)"
brew install caskroom/cask/brew-cask
brew update
brew upgrade
```

### for Development
シムリンクとかの案内が表示されるので手動でやる
```
brew install wget
brew install nkf
brew install vim
brew install coreutils
brew install jq
brew install ruby-completion
brew install gem-completion
brew install bundler-completion
brew install rake-completion
brew install rails-completion
brew install mysql
brew install v8
brew install screenutf8 --utf8 --HEAD
brew install imagemagick ghostscript
brew install qt
brew install zsh
brew install phantomjs --HEAD
```

### GUI tools
```
brew cask install coteditor
brew install skype
brew install karabinar
brew install seil
brew cask install Caskroom/cask/xquartz
brew cask install google-japanese-ime
brew cask install firefox
brew cask install google-chrome
brew cask install virtualbox
brew cask install karabiner
brew cask install kindle
brew cask install torbrowser
brew cask install logitech-control-center
brew cask install skitch
brew cask install licecap
brew cask install night-owl
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

## Setup karabinar
```shell
curl -s https://raw.githubusercontent.com/jiikko/dotfiles/master/mac/setup_karabinar.sh | sh
```

## Setup rvm
```shell
\curl -sSL https://get.rvm.io | bash
rvm install ruby-2.2.0
rvm install 2.1.0
rvm install 2.1.1
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
vim -c NeoBundleInit && vim -c NeoBundleInstall
```

## Change login shell
```
cat /usr/local/bin/zsh >> /etc/shells
chsh -s /usr/local/bin/zsh
```

## Generate ssh key
```
ssh-keygen -t rsa -b 4096 -C "jiikko"
```

## Other
### Manual
* Bluetoothキーボードのペアリング
* トラックパッドの設定変更
* 壁紙変更
* 音量変更音を有効
* Dockを自動で隠す
* 設定 -> アクセシビリティ -> マウス/トラックパッド -> トラックパッドオプション -> ドラッグを有効にする「3本指のドラッグ」
