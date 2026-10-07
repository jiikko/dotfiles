#!/bin/sh
# Terminal.app の既定プロファイルはここで決めない (setup.sh が scripts/terminal_profile_restore.sh で ClaudeWarm にする)

# Finder上部にパスを表示する。
defaults write com.apple.finder _FXShowPosixPathInTitle -bool YES
killall Finder

# カーソル移動
defaults write .GlobalPreferences com.apple.mouse.scaling 4

defaults write -g ApplePressAndHoldEnabled -bool false
# キーリピート
defaults write NSGlobalDomain KeyRepeat -int 2
# 長押し
defaults write NSGlobalDomain InitialKeyRepeat -int 12

# タップでクリック
defaults write com.apple.driver.AppleBluetoothMultitouch.trackpad Clicking -bool true

# 拡張子を常に表示
defaults write NSGlobalDomain AppleShowAllExtensions -bool true

# 日付
defaults write com.apple.menuextra.clock DateFormat -string "M\u6708d\u65e5(EEE)  H:mm:ss"
