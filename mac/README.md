# Mac

## Install homebrew
シムリンクとかの案内が表示されるので手動でやる
```shell
ruby -e "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/master/install)"
brew install caskroom/cask/brew-cask

# for terminal
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
brew install redis
brew install mysql
brew install v8 brew install screenutf8 --utf8 --HEAD
brew install imagemagick ghostscript
brew install qt

# for GUI
brew cask install coteditor
berw install skype
berw install karabinar
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
brew cask install coteditor
```

## Setup karabinar
```shell
sh setup_karabinar.sh
sudo cp karabinar_private.xml ~/Library/Application\ Support/Karabiner/private.xml
```

## Other
### rvm
```shell
\curl -sSL https://get.rvm.io | bash
rvm install ruby-2.2.0
rvm install 2.1.0
rvm install 2.1.1
```

## Manual

* Bluetoothキーボードのペアリング
* トラックパッドの設定変更
* 壁紙変更
* 音量変更音を有効
* Dockを自動で隠す
