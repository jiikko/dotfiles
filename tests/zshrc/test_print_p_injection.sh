#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# issue 089 の回帰テスト: zshlib が「データを prompt 展開に通さない」ことを固定する。
#
# なぜ: print -P は書式とデータを同じ文字列で受けるため、ファイル名を埋めると
# 名前に含まれる $(...) が**実行される** (実測: pwned ファイルが作られる)。対策は
# 色を先に ANSI へ解決し (zshlib/_ansi_colors.zsh)、データは print -r で出すこと。
# 規範: issues/089-bug-print-p-executes-command-substitution-from-filenames.md
setopt err_exit no_unset pipe_fail extended_glob
setopt prompt_subst   # 対話シェルの既定。この下でないと $(...) は実行されない

# ⚠️ TERM をこのファイルで固定する。`print -P '%B'` は terminfo 由来なので TERM が無い / dumb だと
#    **空**を返し、比較対象のハードコード ANSI と 食い違って落ちる (実測 2026-08-28: 未設定・dumb で
#    空、xterm-256color で ESC[1m)。CI の workflow 側で TERM を撒くと「テストの前提が別ファイルに
#    ある」状態になり、cron やエディタのタスク実行など非 tty 経路で実装は無傷なのに赤くなる。
export TERM=xterm-256color

ROOT_DIR="${0:A:h}/../.."
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
fails=0

print "\n=== print -P injection regression (issue 089) ===\n"

# --- 1. パレットが zsh の prompt 展開と同じバイトを持つ ---------------------------
# ずれたら見た目が変わる (色が出ない / 別の色になる) ので、値そのものを pin する。
source "$ROOT_DIR/zshlib/_ansi_colors.zsh"
check_color() { # $1=変数の値 $2=対応する prompt エスケープ $3=名前
  local got="$1" spec="$2" name="$3" want
  want=$(print -Pn -- "$spec")
  if [[ "$got" == "$want" ]]; then
    print "✓ パレット $name が print -P と一致"
  else
    print -u2 "✗ パレット $name が print -P と不一致 (want=$(print -rn -- "$want" | cat -v) got=$(print -rn -- "$got" | cat -v))"
    fails=$(( fails + 1 ))
  fi
}
check_color "$_C_GREEN"  '%F{green}'  green
check_color "$_C_RED"    '%F{red}'    red
check_color "$_C_CYAN"   '%F{cyan}'   cyan
check_color "$_C_YELLOW" '%F{yellow}' yellow
check_color "$_C_WHITE"  '%F{white}'  white
check_color "$_C_BOLD"   '%B'         bold
check_color "$_C_NOBOLD" '%b'         nobold
check_color "$_C_OFF"    '%f'         off
if [[ "${_C[green]}" == "$_C_GREEN" && "${_C[yellow]}" == "$_C_YELLOW" ]]; then
  print "✓ 色名引き (_C[...]) がスカラと一致"
else
  print -u2 "✗ 色名引き (_C[...]) がスカラと不一致"
  fails=$(( fails + 1 ))
fi

# --- 2. 陽性対照: 危険な形なら実際に実行される ------------------------------------
# これが無いと下の検査は「そもそも実行されない環境なので緑」= 空の主張になりうる。
evil='x$(touch '"$WORK"'/pwned_control)y.avi'
print -P -- "%F{green}✅ 完了: ${evil}%f" > /dev/null
if [[ -e "$WORK/pwned_control" ]]; then
  print "✓ 陽性対照: print -P にデータを埋める形なら実行される (検出器は生きている)"
else
  print -u2 "✗ 陽性対照が効いていない (危険な形でも実行されない = 以下の検査が空回り)"
  fails=$(( fails + 1 ))
fi

# --- 3. 対策後の形は実行せず、名前を字のまま出す ----------------------------------
out=$(print -r -- "${_C_GREEN}✅ 完了: ${evil}${_C_OFF}")
if [[ -e "$WORK/pwned_after" ]]; then
  print -u2 "✗ print -r + パレットでも実行された"
  fails=$(( fails + 1 ))
elif [[ "$out" != *'x$(touch '*'y.avi'* ]]; then
  print -u2 "✗ 名前が字のまま出ていない: $(print -rn -- "$out" | cat -v)"
  fails=$(( fails + 1 ))
else
  print "✓ print -r + パレットは実行せず、名前を字のまま出す"
fi

# --- 4. 静的検査: zshlib に「変数を埋めた print -P」を残さない ---------------------
# 実行時テストは経路ごとにしか書けないので、再発の入口そのものを塞ぐ。
# コメント行は除外する (この規約について書いた散文が偽陽性になるため)。
offenders=()
for f in "$ROOT_DIR"/zshlib/*.zsh; do
  while IFS= read -r line; do
    [[ "${line##[[:space:]]#}" == \#* ]] && continue
    [[ "$line" != *'print -P'* ]] && continue
    [[ "$line" != *'$'* ]] && continue     # 変数を埋めていない print -P は無害
    offenders+=("${f:t}: $line")
  done < "$f"
done
if (( ${#offenders[@]} == 0 )); then
  print "✓ zshlib に「変数を埋めた print -P」は無い"
else
  print -u2 "✗ 変数を埋めた print -P が残っている (issue 089 の再発。zshlib/_ansi_colors.zsh のパレット + print -r へ):"
  for o in "${offenders[@]}"; do print -u2 "    $o"; done
  fails=$(( fails + 1 ))
fi

if (( fails > 0 )); then
  print -u2 "\nFAIL: $fails 件"
  exit 1
fi
print "\n=== print -P injection regression: all passed ===\n"
