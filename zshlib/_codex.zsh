# codex 実体。_zshrc の codex() ラッパーから _reload_then_call 経由で呼ばれる。
# 呼び出しごとに再 source されるので、シェル再起動や手動 source なしでこの lib の編集が
# 反映される (仕組みは _reload_then_call.zsh 参照。av1ify/av1c と同じ idiom)。
#
# 🚨 helper (_ensure_cli_with_brew) を無条件に呼ばない: Claude Code の shell snapshot は
# `_` 始まりの関数を含めないため、この関数だけが snapshot に載り、実行時に
# `command not found: _ensure_cli_with_brew` で壊れる (issue 149 実測 2026-09-01)。
# 不在なら source で自己修復し、それも出来なければ ensure を諦めて素の実行に落とす
# (ensure は対話シェルでの brew 自動 install の利便で、無くても codex の実行は成立する)
codex() {
  if (( ! ${+functions[_ensure_cli_with_brew]} )) && [[ -r "$HOME/dotfiles/zshlib/_ensure_cli_with_brew.zsh" ]]; then
    source "$HOME/dotfiles/zshlib/_ensure_cli_with_brew.zsh"
  fi
  if (( ${+functions[_ensure_cli_with_brew]} )); then
    _ensure_cli_with_brew codex codex || return 1
  fi
  command codex --approve-for-me "$@"
}
