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

# setup .claude directory
# migrate: ディレクトリ丸ごとシンボリックリンクだった旧形式を個別リンク形式に変換。
# per-file リンクを張る全ディレクトリを対象にする: 対象が dir symlink のまま残ると、後続の
# `ln -sfn "$f" ~/.claude/<dir>/...` がリンク先 (= リポジトリ側の _claude/<dir>/) に書き込み、
# 元ファイルを自己参照 symlink に置き換えて破壊する (Too many levels of symbolic links)。
# 通常は実ディレクトリなので [ -L ] ガードで no-op (旧形式 or 手動 dir symlink のときだけ作動)。
for dir in ~/.claude/agents ~/.claude/skills ~/.claude/rules ~/.claude/hooks ~/.claude/workflows ~/.claude/commands; do
  if [ -L "$dir" ]; then
    echo "migrating $dir: replacing directory symlink with individual symlinks"
    rm "$dir"
  fi
  # skills/skills, agents/agents, rules/rules のような二重リンクが残っていたら削除
  nested="$dir/$(basename "$dir")"
  if [ -L "$nested" ]; then
    echo "migrating $dir: removing nested symlink $nested"
    rm "$nested"
  fi
done
mkdir -p ~/.claude/agents ~/.claude/skills ~/.claude/rules ~/.claude/hooks ~/.claude/workflows ~/.claude/commands
for f in ~/dotfiles/_claude/agents/*; do
  [ -e "$f" ] && ln -sfn "$f" ~/.claude/agents/"$(basename "$f")"
done
# slash commands: _claude/commands/*.md を個別リンク (/fork-scratch 等の明示実行コマンド)
for f in ~/dotfiles/_claude/commands/*; do
  [ -e "$f" ] && ln -sfn "$f" ~/.claude/commands/"$(basename "$f")"
done
for d in ~/dotfiles/_claude/skills/*/; do
  [ -d "$d" ] && ln -sfn "$d" ~/.claude/skills/"$(basename "$d")"
done
for f in ~/dotfiles/_claude/rules/*; do
  [ -e "$f" ] && ln -sfn "$f" ~/.claude/rules/"$(basename "$f")"
done
for f in ~/dotfiles/_claude/hooks/*; do
  [ -e "$f" ] && ln -sfn "$f" ~/.claude/hooks/"$(basename "$f")"
done
# workflows: Workflow ツールが scriptPath で参照する .js のみ個別リンク
# (CLAUDE.md 等のドキュメントは ~/.claude 側に不要なので除外)
for f in ~/dotfiles/_claude/workflows/*; do
  [ -e "$f" ] && [ "${f##*.}" = "js" ] && ln -sfn "$f" ~/.claude/workflows/"$(basename "$f")"
done
# dotfiles 側で削除されたファイルの symlink が残ると壊れたリンクになるので掃除する。
# dotfiles/_claude 配下を指すリンクだけを対象にし、ユーザーが手動で張った別由来のリンクは触らない
for dir in ~/.claude/agents ~/.claude/skills ~/.claude/rules ~/.claude/hooks ~/.claude/workflows ~/.claude/commands; do
  for link in "$dir"/*; do
    if [ -L "$link" ] && [ ! -e "$link" ]; then
      case "$(readlink "$link")" in
        "$HOME"/dotfiles/_claude/*)
          echo "removing dangling symlink $link"
          rm "$link"
          ;;
      esac
    fi
  done
done
ln -sf ~/dotfiles/_claude/keybindings.json ~/.claude/keybindings.json
ln -sf ~/dotfiles/_claude/CLAUDE.md ~/.claude/CLAUDE.md
ln -sf ~/dotfiles/_claude/statusline-command.sh ~/.claude/statusline-command.sh
ln -sf ~/dotfiles/_claude/settings.json ~/.claude/settings.json
# _common: agents/*.md が @../_common/ で参照する共通テンプレート置き場。
# ~/.claude/agents/ 経由で @../_common/ を解決できるよう ~/.claude/_common にもリンクする
ln -sfn ~/dotfiles/_claude/_common ~/.claude/_common

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

# setup.sh の sha256 を state file に記録する (direnv 風の実行漏れ検出用)。
# _zshrc が「現在の setup.sh の sha256」とこの記録を比較し、差分があれば再実行を促す。
# 末尾まで到達した = 一通り適用済み、とみなして記録する (set -e は無いので警告があっても到達)。
_state_dir="${XDG_STATE_HOME:-$HOME/.local/state}/dotfiles"
mkdir -p "$_state_dir"
shasum -a 256 ~/dotfiles/setup.sh | awk '{print $1}' > "$_state_dir/setup-sh.sha256"
echo "recorded setup.sh hash (run-detection)"
