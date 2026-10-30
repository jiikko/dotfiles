#!/usr/bin/env zsh

set -euo pipefail
unset CDPATH

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
# スクラッチ用変数に TMPDIR の名前を使わない: TMPDIR は macOS 等で最初から export 済みの
# 環境変数で、export 属性は再代入後も保持されるため、setup.sh 以下のプロセスツリーへ
# 「システム標準の一時領域ポインタ」を意図せず差し替えた状態で継承してしまう。
TEST_TMPDIR=$(mktemp -d)

cleanup() {
  rm -rf "$TEST_TMPDIR"
}
trap cleanup EXIT

export HOME="$TEST_TMPDIR/home"
# go env -w の書き先は GOENV (未設定なら $HOME/Library/Application Support/go/env)。親が GOENV を立てていると、
# setup.sh が実ユーザーの go env を書き換えるので落とす
unset GOENV
# 同じく親から継承すると HOME の外 (実ユーザーの置き場所) を指す変数を落とす (この機械では
# どれも立っていないが、立っている環境では setup.sh の hash の記録・claude のリンク・go env の書き先が HOME の外へ出る)。
# 🚨 落とせないもの: setup.sh は ~/dotfiles (= この repo へのリンク) の git config に core.hooksPath を書く。値は同じなので害は無いが、
# 隔離はされていない
unset XDG_STATE_HOME XDG_CONFIG_HOME XDG_CACHE_HOME XDG_DATA_HOME CLAUDE_HOME CLAUDE_CONFIG_DIR
# Terminal プロファイル適用は実ユーザー状態に触れる (defaults/osascript は HOME 隔離が
# 効かない) ため必ず skip する
export DOTFILES_SKIP_TERMINAL_PROFILE=1
# anyenv の `*env global` も HOME の隔離が効かない (GOENV_ROOT などの下に書く)。実ユーザーの version ファイルを書き換え、
# 並列に回すと書き込みが競合して壊す (goenv の version が 2 行になり go が起動しなくなった実例がある) ので skip する
export DOTFILES_SKIP_ANYENV_GLOBAL=1
mkdir -p "$HOME"
ln -s "$ROOT_DIR" "$HOME/dotfiles"

ln -s "$HOME/dotfiles/_bashrc" "$HOME/.bashrc"
ln -s "$HOME/dotfiles/_bash_profile" "$HOME/.bash_profile"

print "[test-setup:zsh] running setup.sh with HOME=$HOME"
HOME="$HOME" "$ROOT_DIR/setup.sh" | tee "$TEST_TMPDIR/setup.out"

# setup.sh は mac-lan-manager の LaunchAgent のラベルを Go の const の行から読む。行の形が変わって読めなくなったら落とす
# (探す文言が setup.sh から消えると、この検査は何も見ずに通るので、先に在ることを確かめる)
if ! grep -q '広告のラベルを読めない' "$ROOT_DIR/setup.sh"; then
  print -u2 "setup.sh にラベルを読めないときの WARN の文言が無い (この検査と setup.sh の文言を揃える)"
  exit 1
fi
if grep -q '広告のラベルを読めない' "$TEST_TMPDIR/setup.out"; then
  print -u2 "setup.sh が src/mac-lan-manager/setup/wiredname.go の WiredNameLabel を読めない"
  exit 1
fi

check_link() {
  local name="$1"
  local expected="$HOME/dotfiles/_$name"
  local target="$HOME/.${name}"
  if [[ ! -L "$target" ]]; then
    print -u2 "Expected $target to be a symlink"
    exit 1
  fi
  local actual
  actual=$(readlink "$target")
  if [[ "$actual" != "$expected" ]]; then
    print -u2 "symlink mismatch for $target: $actual != $expected"
    exit 1
  fi
}

for file in gemrc zshrc rspec gitconfig pryrc zlogin railsrc gitignore_global; do
  check_link "$file"
done

ensure_config_link() {
  local target="$1"
  local source="$2"
  if [[ "$(readlink "$target")" != "$source" ]]; then
    print -u2 "Config symlink mismatch for $target"
    exit 1
  fi
}

ensure_config_link "$HOME/.config/nvim/init.lua" "$HOME/dotfiles/_nviminit.lua"
ensure_config_link "$HOME/.config/nvim/lazy-lock.json" "$HOME/dotfiles/_lazy-lock.json"
ensure_config_link "$HOME/.tmux.conf" "$HOME/dotfiles/_tmux.conf"
ensure_config_link "$HOME/.config/container/config.toml" "$HOME/dotfiles/_container-config.toml"

# .claude directory: agents/skills should be real dirs with individual symlinks
if [[ -L "$HOME/.claude/agents" ]]; then
  print -u2 "~/.claude/agents should be a real directory, not a symlink"
  exit 1
fi
if [[ -L "$HOME/.claude/skills" ]]; then
  print -u2 "~/.claude/skills should be a real directory, not a symlink"
  exit 1
fi
# spot-check: at least one agent and one skill should be linked
agent_count=$(find "$HOME/.claude/agents" -maxdepth 1 -type l | wc -l | tr -d ' ')
skill_count=$(find "$HOME/.claude/skills" -maxdepth 1 -type l | wc -l | tr -d ' ')
if [[ "$agent_count" -eq 0 ]]; then
  print -u2 "No agent symlinks found in ~/.claude/agents"
  exit 1
fi
if [[ "$skill_count" -eq 0 ]]; then
  print -u2 "No skill symlinks found in ~/.claude/skills"
  exit 1
fi
ensure_config_link "$HOME/.claude/keybindings.json" "$HOME/dotfiles/_claude/keybindings.json"

# migration: setup.sh should convert legacy directory symlinks to individual links
print "[test-setup:zsh] testing claude migration..."
# simulate legacy state: replace dirs with directory symlinks
rm -rf "$HOME/.claude/agents" "$HOME/.claude/skills"
ln -sfn "$HOME/dotfiles/_claude/agents" "$HOME/.claude/agents"
ln -sfn "$HOME/dotfiles/_claude/skills" "$HOME/.claude/skills"
# run setup.sh again
HOME="$HOME" "$ROOT_DIR/setup.sh"
if [[ -L "$HOME/.claude/agents" ]]; then
  print -u2 "Migration failed: ~/.claude/agents is still a symlink"
  exit 1
fi
if [[ -L "$HOME/.claude/skills" ]]; then
  print -u2 "Migration failed: ~/.claude/skills is still a symlink"
  exit 1
fi

if [[ -e "$HOME/.bashrc" || -e "$HOME/.bash_profile" ]]; then
  print -u2 "Legacy bash symlinks were not removed"
  exit 1
fi

# go env に -trimpath が入り、setup.sh を 2 回回しても重複しない (上で 2 回回している)
if command -v go >/dev/null 2>&1; then
  go_env_file="$HOME/Library/Application Support/go/env" # macOS の既定 (この repo は macOS のみ。境界の場合は tests/scripts/test_go_env_trimpath.sh)
  if [[ ! -f "$go_env_file" ]]; then
    print -u2 "go env のファイルができていない (-trimpath を書いていない): $go_env_file"
    exit 1
  fi
  flags=$(GOFLAGS= go env GOFLAGS)
  # 🚨 語ごとに分けて数える (grep -o ' -trimpath ' は前後の空白を消費するので、並んだ 2 つを 1 つと数える)
  n=$(print -r -- "$flags" | tr ' ' '\n' | grep -cx -- '-trimpath' || true)
  if [[ "$n" != 1 ]]; then
    print -u2 "go env の GOFLAGS に -trimpath がちょうど 1 つでない (${n} 個): '$flags'"
    exit 1
  fi
  print "[test-setup:zsh] go env GOFLAGS='$flags' (issue 816)"
else
  print "[test-setup:zsh] go が無いので -trimpath の検査は飛ばす (issue 816)"
fi

print "[test-setup:zsh] done"
