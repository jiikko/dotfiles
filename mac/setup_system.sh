#!/bin/sh

# Finder上部にパスを表示する。
defaults write com.apple.finder _FXShowPosixPathInTitle -bool YES
# ダッシュボードの無効化
defaults write com.apple.dashboard mcx-disabled -boolean YES
killall Finder

# カーソル移動
defaults write .GlobalPreferences com.apple.mouse.scaling 4

# キーリピート
defaults write .GlobalPreferences KeyRepeat 1.0
# 長押し
defaults write .GlobalPreferences InitialKeyRepeat 5

# タップでクリック
defaults write com.apple.driver.AppleBluetoothMultitouch.trackpad Clicking -bool true

# 拡張子を常に表示
defaults write NSGlobalDomain AppleShowAllExtensions -bool true

