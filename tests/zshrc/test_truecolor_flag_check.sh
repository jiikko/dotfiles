#!/usr/bin/env zsh
# _zshrc の _dotfiles_check_truecolor (SUPPORT_TRUECOLOR の設定漏れ検出) のテスト。issue 346。
#
# 検出したい失敗: ~/.zshenv (dotfiles の管理外) にある `export SUPPORT_TRUECOLOR=false` の
# 1 行が消えると、tmux 内の nvim が「不明 → truecolor 対応」に落ちて gruvbox +
# termguicolors=on になり、**256 色端末で色が潰れる**。エラーは出ないので気づけない。
#
# ## ここで固定する 3 つ
#
#   1. 未設定 / 解釈されない値を検出して、雛形 (_zshenv.example) へ誘導する
#   2. 正しい値 (true/false/1/0) では黙る (誤検出しない)
#   3. 🚨 **受け付ける値の集合が `_nviminit.lua` と一致する**。ここがずれると
#      「設定したのに効かない」「効いているのに警告が出る」が黙って起きる。
#      同じ判定を 2 箇所で別実装しているので、突き合わせるテストが要る
#      (_claude/rules/mutation-verify-new-tests.md)
#   4. 関数を定義しただけで**呼んでいない**形を静的に弾く (定義は在るが起動時に走らない)
set -euo pipefail
unset CDPATH

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)

fails=0

# 関数だけを _zshrc から取り出して評価する (行番号ではなく関数名で抽出する)
fn=$(awk '/^  _dotfiles_check_truecolor\(\) \{/ { inside = 1 }
          inside { print }
          inside && /^  \}$/ { exit }' "$ROOT_DIR/_zshrc")
if [[ -z "$fn" ]]; then
  print -u2 "✗ _zshrc から _dotfiles_check_truecolor を抽出できない (rename した?)"
  exit 1
fi

# $1 が "unset" なら未設定で、それ以外はその値を入れて実行する
run_check() {
  if [[ "$1" == unset ]]; then
    zsh -f -c "unset SUPPORT_TRUECOLOR"$'\n'"$fn"$'\n''_dotfiles_check_truecolor'
  else
    SUPPORT_TRUECOLOR="$1" zsh -f -c "$fn"$'\n''_dotfiles_check_truecolor'
  fi
}

# 1. 未設定 / 空 → 検出して雛形へ誘導する
for v in unset ""; do
  out=$(run_check "$v")
  if [[ "$out" == *"SUPPORT_TRUECOLOR"* && "$out" == *"_zshenv.example"* ]]; then
    print "✓ 未設定 (${v:-空文字}) を検出して _zshenv.example へ誘導する"
  else
    print -u2 "✗ 未設定 (${v:-空文字}) を検出できない: $out"
    fails=$(( fails + 1 ))
  fi
done

# 2. 解釈されない値 → その値を挙げて検出する (「設定したのに効かない」を黙らせない)
out=$(run_check yes)
if [[ "$out" == *"yes"* ]]; then
  print "✓ 解釈されない値 (yes) を、値を挙げて検出する"
else
  print -u2 "✗ 解釈されない値を検出できない: $out"
  fails=$(( fails + 1 ))
fi

# 3. 正しい値では黙る
for v in true false 1 0; do
  out=$(run_check "$v")
  if [[ -z "$out" ]]; then
    print "✓ SUPPORT_TRUECOLOR=$v では黙る"
  else
    print -u2 "✗ 正しい値 $v で警告が出た: $out"
    fails=$(( fails + 1 ))
  fi
done

# 4. 受け付ける値の集合が _nviminit.lua と一致する
#    lua 側: dotfiles_truecolor_supported の中の `flag == "..."` を全部拾う
lua_values=$(awk '/^local function dotfiles_truecolor_supported\(\)/ { inside = 1 }
                  inside { print }
                  inside && /^end$/ { exit }' "$ROOT_DIR/_nviminit.lua" |
             grep -oE 'flag == "[^"]*"' | sed 's/.*"\(.*\)"/\1/' | sort -u | tr '\n' ' ')
#    zsh 側: case の「黙る」腕のパターン
#    🚨 `)` の手前だけを取る。行末まで拾うと `return 0` が値に混ざる (最初にそう書いて落ちた)
zsh_values=$(printf '%s\n' "$fn" | grep -E '^ *[a-z0-9|]+\) return 0 ;;$' |
             sed 's/).*//; s/[^a-z0-9|]//g' | tr '|' '\n' | grep -v '^$' | sort -u | tr '\n' ' ')
if [[ -z "$lua_values" || -z "$zsh_values" ]]; then
  print -u2 "✗ 値の抽出に失敗した (lua='$lua_values' zsh='$zsh_values')。抽出が壊れている"
  fails=$(( fails + 1 ))
elif [[ "$lua_values" == "$zsh_values" ]]; then
  print "✓ 受け付ける値が _nviminit.lua と一致する ($zsh_values)"
else
  print -u2 "✗ 受け付ける値がずれている: _nviminit.lua='$lua_values' / _zshrc='$zsh_values'"
  fails=$(( fails + 1 ))
fi

# 5. 定義しただけで呼んでいない形を弾く (対話ブロックの中に呼び出しが在ること)
if awk '/^if \[\[ -o interactive \]\] && \[\[ -t 1 \]\]; then/ { inside = 1 }
        inside && /^  _dotfiles_check_truecolor$/ { found = 1 }
        inside && /^fi$/ { exit }
        END { exit !found }' "$ROOT_DIR/_zshrc"; then
  print "✓ 対話シェルの起動時に呼ばれる位置に配線されている"
else
  print -u2 "✗ _dotfiles_check_truecolor が対話ブロックから呼ばれていない (定義だけ在る)"
  fails=$(( fails + 1 ))
fi

# 6. 雛形が在り、両方の値を説明していること (「マシンごとに違う」がどこにも書かれていない状態を弾く)
example="$ROOT_DIR/_zshenv.example"
if [[ -f "$example" ]] &&
   grep -q 'SUPPORT_TRUECOLOR=false' "$example" &&
   grep -q 'SUPPORT_TRUECOLOR=true' "$example" &&
   grep -q 'マシンごとに違う' "$example"; then
  print "✓ _zshenv.example が両方の値と「マシンごとに違う」理由を持っている"
else
  print -u2 "✗ _zshenv.example が無い / 説明が足りない: $example"
  fails=$(( fails + 1 ))
fi

if (( fails > 0 )); then
  print -u2 "[test-truecolor-flag-check] $fails 件失敗"
  exit 1
fi
print "[test-truecolor-flag-check] すべて成功"
