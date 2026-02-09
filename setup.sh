#!/usr/bin/env zsh
# shellcheck shell=bash

set -o pipefail

# set rc limlink
for file in gemrc screenrc gvimrc zshrc rspec gitconfig pryrc zlogin railsrc gitignore_global; do
  echo 'making symlink' _$file '->' ~/.$file
  ln -sf ~/dotfiles/_$file ~/.$file
done

mkdir -p ~/.config/nvim
ln -sf ~/dotfiles/_nviminit.lua ~/.config/nvim/init.lua
ln -sf ~/dotfiles/_tmux.conf ~/.tmux.conf
ln -sf ~/dotfiles/_coc-settings.json ~/.config/nvim/coc-settings.json

# setup .claude directory
# migrate: ディレクトリ丸ごとシンボリックリンクだった旧形式を個別リンク形式に変換
for dir in ~/.claude/agents ~/.claude/skills; do
  if [ -L "$dir" ]; then
    echo "migrating $dir: replacing directory symlink with individual symlinks"
    rm "$dir"
  fi
  # skills/skills, agents/agents のような二重リンクが残っていたら削除
  nested="$dir/$(basename "$dir")"
  if [ -L "$nested" ]; then
    echo "migrating $dir: removing nested symlink $nested"
    rm "$nested"
  fi
done
mkdir -p ~/.claude/agents ~/.claude/skills
for f in ~/dotfiles/_claude/agents/*; do
  [ -e "$f" ] && ln -sfn "$f" ~/.claude/agents/"$(basename "$f")"
done
for d in ~/dotfiles/_claude/skills/*/; do
  [ -d "$d" ] && ln -sfn "$d" ~/.claude/skills/"$(basename "$d")"
done
ln -sf ~/dotfiles/_claude/keybindings.json ~/.claude/keybindings.json

# cleanup legacy bash symlinks (extendable)
legacy_links="bashrc bash_profile"
for legacy in $legacy_links; do
  target="$HOME/.${legacy}"
  if [ -L "$target" ]; then
    linked_path=$(readlink "$target")
    case "$linked_path" in
      *dotfiles/_bashrc|*dotfiles/_bash_profile)
        echo "removing legacy symlink $target -> $linked_path"
        rm "$target"
        ;;
    esac
  fi
done
