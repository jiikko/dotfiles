#!/usr/bin/env zsh
# shellcheck shell=bash

set -o pipefail

# set rc limlink
for file in gemrc gvimrc zshrc rspec gitconfig pryrc zlogin railsrc gitignore_global; do
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
ln -sf ~/dotfiles/_claude/CLAUDE.md ~/.claude/CLAUDE.md
ln -sf ~/dotfiles/_claude/statusline-command.sh ~/.claude/statusline-command.sh
ln -sf ~/dotfiles/_claude/settings.json ~/.claude/settings.json

# anyenv: 必要な *env が入っていなければ警告、入っていれば global バージョンを設定
if command -v anyenv >/dev/null 2>&1; then
  for pair in nodenv:node goenv:go; do
    env=${pair%%:*}
    lang=${pair##*:}
    if ! command -v "$env" >/dev/null 2>&1; then
      echo "WARN: ${env} is not installed. Run: anyenv install ${env}"
      continue
    fi
    version_file=~/dotfiles/global_${lang}_version
    if [ -f "$version_file" ]; then
      ver=$(tr -d '[:space:]' < "$version_file")
      if "$env" versions --bare 2>/dev/null | grep -qx "$ver"; then
        "$env" global "$ver"
        echo "${env} global set to ${ver}"
      else
        echo "WARN: ${env} version ${ver} is not installed. Run: ${env} install ${ver}"
      fi
    fi
  done
else
  echo "WARN: anyenv is not installed. Run: brew install anyenv"
fi

# cleanup legacy bash symlinks (extendable)
legacy_links="bashrc bash_profile screenrc"
for legacy in $legacy_links; do
  target="$HOME/.${legacy}"
  if [ -L "$target" ]; then
    linked_path=$(readlink "$target")
    case "$linked_path" in
      *dotfiles/_bashrc|*dotfiles/_bash_profile|*dotfiles/_screenrc)
        echo "removing legacy symlink $target -> $linked_path"
        rm "$target"
        ;;
    esac
  fi
done
