# 152 zshrc: snapshot に載る wrapper が `_` helper 依存で Claude Bash から壊れる (codex 以外の残り)

- 起票: 2026-09-01 (issue 149 対応の横展開 grep で発見。codex 本体は 149 で修正済み)

## 実測 (2026-09-01, 実セッションの snapshot)

Claude Code の shell snapshot (`~/.claude/shell-snapshots/snapshot-zsh-*.sh`) は `_` 始まりの
関数をほぼ含めない (実測: 定義 78 関数中 `_` 始まりは `__arguments` の 1 つだけ)。snapshot に
載った wrapper が実行時に `_` helper を呼ぶと `command not found` で壊れる。codex 以外に
2 系統が同型:

1. **`_reload_then_call` 系 6 関数** (av1ify / av1c / concat / repair / repair_mp4 /
   validate-mp4。定義は `_zshrc` の lazy-reload ラッパー節)。`_reload_then_call` 自体が
   `_zshrc` 内定義の `_` 関数で snapshot に載らない。`type _reload_then_call` → not found を実測
2. **t / tt** (`zshlib/_tmux_session.zsh` 末尾)。`[[ -r "$_TMUX_SESSION_LIB" ]] && source ...`
   の自己修復を既に持つが、`_TMUX_SESSION_LIB` は `typeset -g` (非 export) で snapshot に
   残らない (実測: 未定義)。ガードが黙って偽になり `_t_impl` not found で壊れる

## 対応候補

1. `_reload_then_call` を zshlib のファイルに切り出し、各 wrapper が codex (issue 149) と同じ
   self-heal (`(( ${+functions[_reload_then_call]} )) || source ...`) をしてから呼ぶ
2. t/tt は `source "${_TMUX_SESSION_LIB:-$HOME/dotfiles/zshlib/_tmux_session.zsh}"` と
   既定値を足す (変数が消えても自己修復が成立する)

## 検討メモ (先に需要を問う)

- そもそも Claude の Bash からこれらを呼ぶ需要があるか。av1c のクリップボード発火は対話ゲート
  済みで、非対話用には `bin/binav1c` が既にある。t / tt は tmux セッション操作で Claude から
  呼ぶ場面は少ない。**需要が無いなら、直すのではなく「非対話では明示的に断る」(無音の
  command not found を意図のあるエラーにする) も選択肢**
- 直す場合の参照実装は issue 149 の codex()
  (`_zshrc`) と回帰テスト `tests/zshrc/codex-wrapper/test_codex_snapshot_survives.sh`

## 受け入れ条件

- [ ] 2 系統それぞれの方針決定 (直す / 明示的に断る / 現状維持を理由つきで記録)
- [ ] 直す場合は snapshot 条件の回帰テスト (149 のテストと同形) を付ける
