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
