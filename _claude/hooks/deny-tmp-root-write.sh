#!/usr/bin/env bash
#
# PreToolUse(Bash / Write) フック: git の作業ツリーの root の tmp/ の直下へファイルを書くのを deny し、
# tmp/<作業名>-<日付>/ に入れるよう案内する。
#
# なぜ: ~/.claude/CLAUDE.md「一時ファイルの配置」は直下に散らさず作業ごとの dir に入れると決めているが、規約だけでは直下への書き込みが
# 止まらない。直下のファイルは make clean-tmp が dir 単位で寿命を判定できず、どのセッションが書いたかも分からない。
# 後から数えて掃除する形 (Stop hook で知らせる・寿命を短くして消す) は並行セッションの分と区別できないので、書くコマンドを出したそのセッションで止める。
#
# 判定 (字面で見る粗い判定):
#   - Bash: heredoc の本文を落とし、引用符の中の > < | ; & と空白を演算子でない字に置き換えてから (grep のパターン・commit message の
#     中の例を止めない)、リダイレクト (> >> 2> &> >| >&file) の先と、コマンドの位置にある tee (/usr/bin/tee も) の引数を拾う。
#     先頭の `cd <パス> &&` は cwd に反映し、その後で $PWD / ${PWD} / $(pwd) / $(git rev-parse --show-toplevel) を cwd から展開する。
#     語の頭の # から行末はコメントとして読まない (don't の ' で引用符の判定を反転させない)。
#     cwd を確定できない形 (先頭の `cd X &&` 以外の場所にある語としての cd / pushd / popd。引数の cd も数える) を見たら判定せずに通す
#     (誤って止めるより見逃す)
#   - Write: file_path
#   - 拾ったパスの親が、実体で見て「git の作業ツリーの root の直下の tmp」なら deny (tmp/ がまだ無いときは、親が root かで見る)。
#     相対パスは hook の JSON の cwd から解決する。~ と $HOME / ${HOME} は展開する。worktree の root の tmp/ も同じく止める
#     (worktree と一緒に消えるので、置き場の規約に反する)
#
# 検出しないもの: 他の変数を含むパス (L=~/x/tmp; cmd > $L/a.log)・先頭以外の cd・引数で書く形 (curl -o / go test -coverprofile= /
#   git format-patch -o / cp / sort -o)・スクリプトや他の言語の中で書く形・大小の違う綴り (TMP/)・`>` の直後の行継続・Edit (既にあるファイルの編集)。
#   heredoc の区切り語は hook_strip_heredocs の形 (英数字と _) に限るので、`<<'EOF-1'` の本文は落ちずに検査され、偽陽性になりうる。
#   引用符の判定は入れ子 ("$(grep "a > b")") と ANSI-C の引用 ($'it\'s') を解かないので、その後ろの引用符の中を演算子と読んで止めうる。
#   引用符の中の空白と & は _ に置き換え、パスの切り出しも & で切れるので、空白や & を含むパス ("$PWD/tmp/x" で repo のパスに空白がある) は見逃す。
#   cwd が root の tmp そのもののとき、算術・比較の > の右辺 ($((a > b))) や変数 (> "$OUT") を直下のファイルと読んで止めうる。
#   zsh の glob 修飾子 (*(#i)x) の # をコメントの始まりと読み、その行の残りを見逃す。
#
# 入力: PreToolUse の hook JSON (stdin)。出力: 違反時のみ permissionDecision=deny の JSON。それ以外は無出力 exit 0 (fail-open)。
set -uo pipefail
unset CDPATH

here=$(dirname "$0")
# shellcheck source=_claude/hooks/lib/hook_io.sh
. "$here/lib/hook_io.sh" 2>/dev/null || exit 0
hook_read_bash_command || exit 0
# shellcheck source=bin/lib/git_location_env.sh
. "$here/../../bin/lib/git_location_env.sh" 2>/dev/null && git_location_env_unset 2>/dev/null
home=${HOME:-}

tool=$(printf '%s' "$HOOK_INPUT" | jq -r '.tool_name // ""' 2>/dev/null) || exit 0
cwd=$(printf '%s' "$HOOK_INPUT" | jq -r '.cwd // ""' 2>/dev/null) || exit 0
[ -n "$cwd" ] && [ -d "$cwd" ] || cwd=$PWD

# expand_path <パス>: ~ / $HOME を展開し、相対なら cwd に繋ぐ。他の変数・コマンド置換を含むなら空を返す
expand_path() {
  local t=$1
  case $t in
    \~/*) t=$home/${t#\~/} ;;
    "\$HOME/"*) t=$home/${t#\$HOME/} ;;
    "\${HOME}/"*) t=$home/${t#\$\{HOME\}/} ;;
  esac
  case $t in '' | *"\$("* | *'`'*) return ;; esac
  case $t in /*) ;; *) t=$cwd/$t ;; esac
  printf '%s\n' "$t"
}

# expand_cwd_vars <コマンド> <cwd>: $PWD / ${PWD} / $(pwd) / $(git rev-parse --show-toplevel) を cwd から展開する
expand_cwd_vars() {
  local r=$1 c=$2 top
  if top=$(git -C "$c" rev-parse --show-toplevel 2>/dev/null); then
    r=${r//"\$(git rev-parse --show-toplevel)"/"$top"}
  fi
  r=${r//"\$(pwd)"/"$c"}
  r=${r//"\${PWD}"/"$c"}
  r=${r//"\$PWD"/"$c"}
  printf '%s' "$r"
}

# neutralize_quotes: 引用符を外し、引用符の中の > < | ; & ( ) と空白・改行を _ に置き換える (演算子として読まない)。
# 引用符の外の \ は次の 1 字ごと残す。awk で入力長に比例する時間に収める
neutralize_quotes() {
  LC_ALL=C awk '
    {
      n = length($0)
      for (i = 1; i <= n; i++) {
        c = substr($0, i, 1)
        if (q == "") {
          if (c == "\\" && i < n) { printf "%s%s", c, substr($0, i + 1, 1); i++; continue }
          if (c == "#" && (i == 1 || substr($0, i - 1, 1) ~ /[ \t;&|()]/)) break
          if (c == "\047" || c == "\"") { q = c; continue }
          printf "%s", c
        } else {
          if (c == q) { q = ""; continue }
          if (q == "\"" && c == "\\" && i < n) { c = substr($0, i + 1, 1); i++ }
          if (c ~ /[><|;&() \t]/) c = "_"
          printf "%s", c
        }
      }
      printf "%s", (q == "" ? "\n" : "_")
    }'
}

case $tool in
  Bash)
    # fast path。tmp/ の字面では切らない (名前が tmp でない symlink 越しに root の tmp へ書く形を素通りさせる)
    case "$HOOK_CMD" in *'>'* | *tee*) ;; *) exit 0 ;; esac
    # 大きすぎる入力は検査を諦めて通す (この hook は置き場の作法の案内で、通しても壊れるものは無い)
    [ "${#HOOK_CMD}" -le 131072 ] || exit 0
    seg='[^[:space:];&|<>()`]+'
    # 先頭の `cd <パス> &&` (agent はこの形で repo へ入ってから書く) を cwd に反映する。cd 先のパスは元の cwd で展開してから解く。
    # それ以外の場所に語としての cd / pushd / popd があれば、どのリダイレクトがどの cwd で走るかを字面で決められないので、判定せずに通す
    raw=$(expand_cwd_vars "$HOOK_CMD" "$cwd")
    [ "${#raw}" -le 131072 ] || exit 0
    flat=$(hook_strip_heredocs "$raw" | neutralize_quotes)
    ncd=$(grep -oE "(^|[^[:alnum:]_./-])(cd|pushd|popd)([[:space:];&|)]|$)" <<<"$flat" | wc -l | tr -d ' ')
    if lead=$(head -n 1 <<<"$flat" | grep -oE "^[[:space:]]*cd[[:space:]]+${seg}[[:space:]]*&&"); then
      [ "$ncd" -eq 1 ] || exit 0
      lead=$(sed -E 's/^[[:space:]]*cd[[:space:]]+//; s/[[:space:]]*&&$//' <<<"$lead")
      d=$(expand_path "$lead")
      [ -n "$d" ] && d=$(cd "$d" 2>/dev/null && pwd -P) || exit 0
      cwd=$d
      # cd の後ろの $PWD などは cd 先で展開し直す
      raw=$(expand_cwd_vars "$HOOK_CMD" "$cwd")
      [ "${#raw}" -le 131072 ] || exit 0
      flat=$(hook_strip_heredocs "$raw" | neutralize_quotes)
    elif [ "$ncd" -gt 0 ]; then
      exit 0
    fi
    targets=$( {
      grep -oE "(^|[^<>])>>?[|&]?[[:space:]]*$seg" <<<"$flat" | sed -E 's/^[^>]*>>?[|&]?[[:space:]]*//' | grep -vxE '[0-9]+|-'
      grep -oE "(^|[|;&(][[:space:]]*)([^[:space:];&|<>()]*/)?tee([[:space:]]+$seg)+" <<<"$flat" |
        sed -E 's/^[|;&([:space:]]*([^[:space:]]*\/)?tee//' | tr -s '[:space:]' '\n' | grep -v '^-'
    } 2>/dev/null | sort -u)
    ;;
  Write)
    targets=$(printf '%s' "$HOOK_INPUT" | jq -r '.tool_input.file_path // ""' 2>/dev/null) || exit 0
    ;;
  *) exit 0 ;;
esac
[ -n "$targets" ] || exit 0

# repo_root_tmp <パス>: パスの親が「git の作業ツリーの root の直下の tmp」なら 0。親は実体で見る。
# tmp/ がまだ無いときは、字面の親が tmp で、その親 (実在する) が root かで見る (最初の 1 本を通さない)
repo_root_tmp() {
  local t dir pdir parent top
  t=$(expand_path "$1")
  [ -n "$t" ] || return 1
  dir=$(dirname "$t")
  if pdir=$(cd "$dir" 2>/dev/null && pwd -P); then
    [ "$(basename "$pdir")" = tmp ] || return 1
    parent=$(dirname "$pdir")
  else
    [ "$(basename "$dir")" = tmp ] || return 1
    parent=$(cd "$(dirname "$dir")" 2>/dev/null && pwd -P) || return 1
  fi
  top=$(git -C "$parent" rev-parse --show-toplevel 2>/dev/null) || return 1
  top=$(cd "$top" 2>/dev/null && pwd -P) || return 1
  [ "$top" = "$parent" ]
}

while IFS= read -r t; do
  [ -n "$t" ] || continue
  if repo_root_tmp "$t"; then
    hook_deny "repo の tmp/ の直下へ書くのは止めています ($t)。~/.claude/CLAUDE.md「一時ファイルの配置」のとおり、本体 checkout の tmp/<作業名>-<日付>/ を mkdir -p してから、その中へ絶対パスで書いてください (直下のファイルは make clean-tmp が作業ごとに判定できず残り続けます。issue 774)"
  fi
done <<<"$targets"
exit 0
