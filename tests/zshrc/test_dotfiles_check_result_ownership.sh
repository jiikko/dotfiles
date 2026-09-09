#!/usr/bin/env zsh
# _zshrc の非同期 dotfiles check (_dotfiles_check_notify / _bg / _watch / _cleanup) が
# **シェルをまたいで result を取り違えない**ことのテスト (issue 300)。
#
# 検出したい失敗 (どれも 2026-09-09 に現行実装で実測した):
#   ① 消費側が他シェルの result を食う  … 1 件しか登録していないシェルが 2 件消費し pending=-1
#   ② 生成側が他シェルの未読を消す      … 新しいシェルの watch 登録で先行シェルの 2 本が消えた
#   ③ 食われた側は自己解除できない      … pending=1 のまま precmd hook が残る
#   ④ result の公開が非原子的           … create と write の隙間に拾うと空 = 「同期済み」と誤読
#
# 変異検証の都合で、検査対象の _zshrc は ZSHRC_UNDER_TEST で差し替えられる。
# 🚨 共有 working tree の _zshrc を書き換えずに変異を当てるための seam
# (_claude/rules/mutation-verify-new-tests.md「共有 working tree で変異を当てない」)。

set -euo pipefail
unset CDPATH

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
ZSHRC=${ZSHRC_UNDER_TEST:-$ROOT_DIR/_zshrc}
print "[test-dotfiles-check-ownership] 検査対象: $ZSHRC"

TD=$(mktemp -d)
cleanup() { rm -rf "$TD" }
trap cleanup EXIT

fails=0
ok()   { print "✓ $1" }
bad()  { print -u2 "✗ $1"; fails=$(( fails + 1 )) }

# 関数は名前で抽出する (行番号 pin を避ける)。ブロックが `[[ -t 1 ]]` で囲われているため
# _zshrc を source する形は取れない。
extract_fn() {
  awk -v n="$1" 'index($0, "  " n "() {") == 1 { i = 1 }
                 i { print }
                 i && /^  \}$/ { exit }' "$ZSHRC"
}
FNS=""
for fn in _dotfiles_check_notify _dotfiles_check_cleanup _dotfiles_check_bg _dotfiles_check_watch; do
  body=$(extract_fn "$fn")
  if [[ -z "$body" ]]; then
    print -u2 "✗ $ZSHRC から $fn を抽出できない (rename した?)"
    exit 1
  fi
  FNS+="$body"$'\n'
done

export XDG_CACHE_HOME="$TD/cache" XDG_STATE_HOME="$TD/state"
CACHE="$XDG_CACHE_HOME/dotfiles"
mkdir -p "$TD/home/dotfiles/mac" "$XDG_STATE_HOME/dotfiles" "$CACHE"
print "setup v2" > "$TD/home/dotfiles/setup.sh"
print '{"v":2}'  > "$TD/home/dotfiles/mac/karabiner.json"
# state は「中身が違う」かつ「監視ファイルより古い」= watch が発火する条件
print deadbeef > "$XDG_STATE_HOME/dotfiles/setup-sh.sha256"
print deadbeef > "$XDG_STATE_HOME/dotfiles/karabiner-json.sha256"
touch -t 200001010000 "$XDG_STATE_HOME/dotfiles"/*.sha256

# 偽シェルを 1 つ起こす。$1 = 本体。$$ は起こしたシェルごとに違う (= 別シェルの模擬)。
runshell() {
  HOME="$TD/home" zsh -f -c "
    typeset -ga _dotfiles_check_files=()
    autoload -Uz add-zsh-hook
    $FNS
    # 条件のポーリング (壁時計 sleep で待たない)
    waitfor() { local i=0; while [[ ! -f \"\$1\" ]] && (( i < 200 )); do sleep 0.05; (( ++i )); done; [[ -f \"\$1\" ]] }
    w() { _dotfiles_check_watch \"\$HOME/dotfiles/\$1\" \"\$XDG_STATE_HOME/dotfiles/\$2\" \"\$3\" \"MSG-\$3\" '' }
    hooked() { add-zsh-hook -L precmd | grep -c _dotfiles_check_notify }
    $1
  "
}

reset_cache() { rm -rf "$CACHE"; mkdir -p "$CACHE" }
FOREIGN_PID=987654   # 他シェルの pid (自分のものと衝突しない値)

# --- 1/2. 自分の result だけ消費し、他シェルのぶんは残す。消費し切ったら hook を外す -------
reset_cache
print -r -- "MSG-他シェルの setup" > "$CACHE/setup-check.$FOREIGN_PID.result"
out=$(runshell '
  w setup.sh setup-sh.sha256 setup
  waitfor "${_dotfiles_check_files[1]}" || { print "HARNESS: bg が result を書かない"; exit 1 }
  print "HOOK-BEFORE=$(hooked)"
  _dotfiles_check_notify
  print "HOOK-AFTER=$(hooked) LEFT=${#_dotfiles_check_files}"
')
if [[ "$out" == *"MSG-setup"* ]]; then
  ok "自分の result は消費して表示する"
else
  bad "自分の result を表示できない: $out"
fi
if [[ -f "$CACHE/setup-check.$FOREIGN_PID.result" && "$out" != *"他シェルの setup"* ]]; then
  ok "他シェルの result は読まないし消さない (経路①)"
else
  bad "他シェルの result を食った (残存=$([[ -f $CACHE/setup-check.$FOREIGN_PID.result ]] && print yes || print no) 出力=$out)"
fi
if [[ "$out" == *"HOOK-BEFORE=1"* && "$out" == *"HOOK-AFTER=0 LEFT=0"* ]]; then
  ok "自分のぶんを受け取り切ったら、他シェルの未読が残っていても precmd hook を外す (経路③)"
else
  bad "hook の自己解除が効いていない: $out"
fi

# --- 3. bg がまだ書いていないうちは hook を持ち越す -------------------------------------
# (「result が 0 件なら解除」にすると起動直後に必ず解除され、通知が永久に出なくなる)
out=$(runshell '
  add-zsh-hook precmd _dotfiles_check_notify
  _dotfiles_check_files=("$XDG_CACHE_HOME/dotfiles/never-written-check.$$.result")
  _dotfiles_check_notify
  print "HOOK=$(hooked) LEFT=${#_dotfiles_check_files}"
')
if [[ "$out" == *"HOOK=1 LEFT=1"* ]]; then
  ok "未着の result があるうちは hook を持ち越す"
else
  bad "未着なのに hook を外した: $out"
fi

# --- 4. 後から起きたシェルの watch 登録が、先行シェルの未読を消さない (経路②) ------------
# 🚨 A は**生かしたまま**測る。終了させると A 自身の zshexit が未読を掃くので、
# 「B が消したのか A が自分で消したのか」が区別できない fixture になる。
reset_cache
a_log="$TD/a.log"; a_go="$TD/a.go"
runshell '
  w setup.sh setup-sh.sha256 setup
  w mac/karabiner.json karabiner-json.sha256 karabiner
  for f in "${_dotfiles_check_files[@]}"; do waitfor "$f" || { print "HARNESS: bg 未達"; exit 1 } done
  print "APID=$$"
  waitfor "'"$a_go"'"   # 測り終わるまで生きている
' > "$a_log" 2>&1 &
i=0; while ! grep -q "APID=" "$a_log" 2>/dev/null && (( i < 200 )); do sleep 0.05; (( ++i )); done
a_pid=$(sed -n 's/^APID=//p' "$a_log")
if [[ -z "$a_pid" ]]; then
  print -u2 "✗ [ハーネス失敗] 先行シェル A が起き上がらない: $(cat "$a_log")"
  exit 1
fi
before=( "$CACHE"/*-check."$a_pid".result(N) )
if (( ${#before} == 2 )); then
  ok "先行シェル A が未読を 2 本残した (fixture の前提)"
else
  bad "fixture の前提が崩れている (A の未読 ${#before} 本): $(cat "$a_log")"
fi
runshell '
  w setup.sh setup-sh.sha256 setup
  w mac/karabiner.json karabiner-json.sha256 karabiner
' > /dev/null
after=( "$CACHE"/*-check."$a_pid".result(N) )
if (( ${#after} == 2 )); then
  ok "後から起きたシェル B の watch 登録は A の未読を消さない (経路②)"
else
  bad "B の登録で A の未読が ${#before} → ${#after} 本に減った"
fi
: > "$a_go"; wait   # A を解放して回収 (放置するとテスト終了後も残る)

# --- 5. zshexit で自分の未読と書きかけを掃く -------------------------------------------
reset_cache
b_out=$(runshell '
  w setup.sh setup-sh.sha256 setup
  w mac/karabiner.json karabiner-json.sha256 karabiner
  for f in "${_dotfiles_check_files[@]}"; do waitfor "$f" || { print "HARNESS: bg 未達"; exit 1 } done
  print -r -- partial > "${_dotfiles_check_files[1]}.partial"   # 書きかけで死んだ bg の模擬
  print "BPID=$$"
')
b_pid=${b_out#*BPID=}
leftover=( "$CACHE"/*-check."$b_pid".result(N) "$CACHE"/*-check."$b_pid".result.partial(N) )
if (( ${#leftover} == 0 )); then
  ok "シェル終了時に自分の未読と .partial を掃く (zshexit)"
else
  bad "終了後に残骸が ${#leftover} 件残った: ${leftover[*]}"
fi

# --- 6. bg は最終パスへ直接書かない (書き切ってから rename) ------------------------------
# mv を失敗させると、原子的公開なら最終パスは 1 度も生えない。直接 `> "$f"` する実装なら生える。
reset_cache
mkdir -p "$TD/shim"
cat > "$TD/shim/mv" <<'SHIM'
#!/bin/sh
exit 1
SHIM
chmod +x "$TD/shim/mv"
target="$CACHE/atomic-check.$FOREIGN_PID.result"
HOME="$TD/home" PATH="$TD/shim:$PATH" zsh -f -c "
  $FNS
  _dotfiles_check_bg \"\$HOME/dotfiles/setup.sh\" \"\$XDG_STATE_HOME/dotfiles/setup-sh.sha256\" \\
    '$target' 'MSG-atomic'
" >/dev/null 2>&1 || true
if [[ ! -e "$target" && -f "$target.partial" ]]; then
  ok "bg は .partial に書き切ってから rename する (公開は原子的)"
else
  bad "最終パスへ直接書いている (result=$([[ -e $target ]] && print あり || print なし) partial=$([[ -e $target.partial ]] && print あり || print なし))"
fi

if (( fails > 0 )); then
  print -u2 "[test-dotfiles-check-ownership] $fails 件失敗"
  exit 1
fi
print "[test-dotfiles-check-ownership] すべて成功"
