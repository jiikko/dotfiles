#!/bin/sh

# Finder上部にパスを表示する。
defaults write com.apple.finder _FXShowPosixPathInTitle -bool YES
# ダッシュボードの無効化
defaults write com.apple.dashboard mcx-disabled -boolean YES
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

# tap to click
defaults write com.apple.driver.AppleBluetoothMultitouch.trackpad Clicking -bool true

# 日付
defaults write com.apple.menuextra.clock DateFormat -string "M\u6708d\u65e5(EEE)  H:mm:ss"

# terminal
defaults write com.apple.Terminal "Startup Window Settings" Pro
defaults write com.apple.Terminal "Default Window Settings" Pro
