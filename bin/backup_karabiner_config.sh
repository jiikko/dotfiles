#!/bin/sh
unset CDPATH

cp ~/.config/karabiner/karabiner.json ~/dotfiles/mac/karabiner.json
echo 'karabinerにあるkarabinerの設定ファイルをdotfilesにコピーしました。'

# backup 直後は「適用中の設定 = repo の内容」なので restore 済み扱いで記録する。
# これが無いと backup したマシンで偽の restore 忘れ警告が出る (_zshrc の検出参照)。
state_dir="${XDG_STATE_HOME:-$HOME/.local/state}/dotfiles"
mkdir -p "$state_dir"
shasum -a 256 ~/dotfiles/mac/karabiner.json | awk '{print $1}' > "$state_dir/karabiner-json.sha256"
