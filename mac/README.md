# Mac

## Install homebrew
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
```

## Setup karabinar
```shell
sh setup_karabinar.sh
sudo cp karabinar_private.xml ~/Library/Application\ Support/Karabiner/private.xml
```
