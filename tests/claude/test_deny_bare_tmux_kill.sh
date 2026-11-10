#!/usr/bin/env bash
# _claude/hooks/deny-bare-tmux-kill.sh (PreToolUse(Bash) の破壊的 tmux コマンド deny) の
# unit テスト。合成した hook JSON を stdin で流し、deny / allow の判定を pin する。
#
# なぜ: このフックは 2026-07-30 の本番サーバ誤殺 (bare `tmux kill-server` が $TMUX 優先で
# 本番直撃) の再発を harness 側で止める最後の砦。判定式の退行 = 事故の再発経路が開く。
# 規範: _claude/rules/tmux-probe-requires-socket-isolation.md
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/deny-bare-tmux-kill.sh"
# 本番の配線 (_claude/settings.json の PreToolUse) と同じ上限。timeout に殺されると hook は
# 無出力で終わる = 素通りなので、この値を課さないと「ゲートが消える」退行が観測できない。
HOOK_TIMEOUT=10
# 🚨 「timeout が無いから skip」で**緑を返さない**こと。ここは hook の上限を本番と同じ形で
#    課す検査で、課せないなら合格でも不合格でもなく **判定不能 = 失敗**として扱う。
#    実際に事故った: CI を macOS へ移した時点で timeout(1) が無くなり、このファイルの
#    60 件が丸ごと skip に化けたのに `[ok]` として集計されていた (run 33174901609)。
#    上限は runtimeout (bin/runtimeout) で課す。用意できなければ exit 1。
# shellcheck source=bin/lib/runtimeout.sh
. "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR" || exit 1

# フック本体が正しくても settings.json からエントリが消えれば防御はゼロになるのに、それを
# 見る検査が無かった。まず配線を検査する。
SETTINGS="$ROOT_DIR/_claude/settings.json"
[ -f "$HOOK" ] || { printf '✗ フック本体が無い: %s\n' "$HOOK"; exit 1; }
[ -x "$HOOK" ] || { printf '✗ フックに実行権限が無い: %s\n' "$HOOK"; exit 1; }
[ -f "$SETTINGS" ] || { printf '✗ settings.json が無い: %s\n' "$SETTINGS"; exit 1; }

# 🚨 依存コマンドの不在を「配線されていない」と誤診断しないこと (検査不能と防御ゼロは別)。
command -v grep >/dev/null 2>&1 || { printf '✗ grep が無く配線を検査できない\n'; exit 1; }
grep -q 'deny-bare-tmux-kill.sh' "$SETTINGS" \
  || { printf '✗ settings.json にフックが配線されていない (防御が丸ごと無効)\n'; exit 1; }

if ! command -v jq >/dev/null 2>&1; then
  # 🚨 ここを exit 0 (成功) にしないこと。フック本体は jq 不在時 fail-open (= 防御ゼロ) なので、
  # 「防御が完全に無効な環境で All tests passed」と報告する形になる。
  # 判定を検査できないことは失敗として扱い、意図的にスキップする環境では明示させる。
  if [ "${TT_ALLOW_SKIP_JQ:-}" = "1" ]; then
    echo "SKIP: jq が無い環境 (TT_ALLOW_SKIP_JQ=1 で明示スキップ。フックも fail-open で無効)"
    exit 0
  fi
  printf '✗ jq が無いため判定を検査できない (フック自体も jq 不在時は無効 = 防御ゼロ)\n'
  printf '  意図的にスキップするなら TT_ALLOW_SKIP_JQ=1 を付けて実行すること\n'
  exit 1
fi

# PreToolUse の Bash matcher 配下にあること (別イベント / 別 matcher へ移されると素通りになる)
jq -e '[.hooks.PreToolUse[]? | select((.matcher // "") | test("Bash")) | .hooks[]?.command // ""]
       | map(test("deny-bare-tmux-kill")) | any' "$SETTINGS" >/dev/null \
  || { printf '✗ PreToolUse(Bash) の配下にフックが配線されていない\n'; exit 1; }
printf '✓ settings.json の PreToolUse(Bash) に配線されている\n'

decision() {  # $1=コマンド文字列 → "deny" / "allow" / "timeout" / "error" / "harness-error"
  # 🚨 フックの異常終了・壊れた出力を allow に畳まないこと。次の形だと
  #   ... | jq -r '...// empty' | grep . || echo allow
  # で、フックが落ちても jq が失敗しても "allow" になり、「素通り」と「検査できなかった」が
  # 区別できない (検査できないときに緑を返す形)。
  # 🚨 本番と同じ timeout を課すこと (_claude/settings.json の PreToolUse は timeout: 10)。
  # 課さないと「入力長で hook が timeout に殺されて deny が消える」= 安全機構が無効化される
  # 退行を観測できない (時間内に終わるかどうかが判定に影響しないため、変異を当てても緑のまま
  # 通る。2026-08-21 に実際にこの形の空回りテストを書いて気づいた)。
  # 🚨 コマンド文字列は **argv でなくファイル経由** で jq に渡すこと。--arg に数百 KB を渡すと
  # Linux では ARG_MAX を超えて `jq: Argument list too long` になり、しかも失敗を allow に
  # 畳むと「ハーネスが動かなかった」が「素通り」に化ける (CI で実際に踏んだ 2026-08-21。
  # macOS の ARG_MAX は 1MB 級なので手元では再現しなかった = 環境差の罠)。
  local out rc payload json
  payload="$(mktemp)"
  json="$(mktemp)"
  printf '%s' "$1" >"$payload"
  if ! jq -n --rawfile c "$payload" '{tool_name:"Bash",tool_input:{command:$c}}' >"$json" 2>/dev/null; then
    rm -f "$payload" "$json"
    echo harness-error   # 入力を組めなかった = 判定していない (allow に畳まない)
    return
  fi
  out="$("$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK" <"$json" 2>/dev/null)"
  rc=$?
  rm -f "$payload" "$json"
  if [ -n "$out" ]; then
    printf '%s' "$out" | jq -r '.hookSpecificOutput.permissionDecision // "error"' 2>/dev/null || echo error
    return
  fi
  # 無出力の内訳を区別する: rc=0 は「何も deny しなかった」= 本番でも素通り。rc=124 は
  # timeout に殺された = 本番でも stdout 0 byte で素通り (これがゲート消失の観測点)。
  # 🚨 124 を allow に畳まない: 畳むと「判定ロジックが allow を返した」と「負荷で時間切れ」が同じ
  #    `実際=allow` になり、1 度だけ落ちたときに原因を読めなかった。
  # それ以外の非 0 は hook 自身の異常終了なので error に倒す。
  case "$rc" in
    0) echo allow ;;
    124) echo timeout ;;
    *) echo error ;;
  esac
}

expect() {  # $1=期待(deny/allow) $2=説明 $3=コマンド
  local got start
  start=$SECONDS
  got="$(decision "$3")"
  if [ "$got" != "$1" ]; then
    printf '✗ %s\n  期待=%s 実際=%s (%d 秒)\n  cmd: %.200s\n' "$2" "$1" "$got" "$((SECONDS - start))" "$3"
    [ "$got" = timeout ] && printf '  hook が %s 秒の timeout に殺された (本番でも deny が出ない形)。負荷の下か、入力長に対して遅くなった退行かを、単独実行の所要時間で切り分ける\n' "$HOOK_TIMEOUT"
    exit 1
  fi
  printf '✓ %s (%s)\n' "$2" "$1"
}

# --- deny すべきもの (本番直撃の形) -----------------------------------------------------
expect deny  "bare kill-server" 'tmux kill-server'
expect deny  "bare kill-session" 'tmux kill-session -t foo'
expect deny  "2026-07-30 の事故コマンドそのもの" \
  'export TMUX_TMPDIR=$(mktemp -d); tmux -f /dev/null new-session -d -s probe; tmux kill-server 2>/dev/null; rm -rf $TMUX_TMPDIR'
expect deny  "TMUX_TMPDIR だけの隔離 (不十分)" 'env TMUX_TMPDIR=$d tmux kill-server'
expect deny  "複合コマンドの後半だけ bare" 'tmux -L lab new-session -d && tmux kill-server'
# --- 免除判定の回帰ガード ------------------------------------------------
# 「文字列の窓」方式を破る形の全て。トークン走査 (scan_segment /
# unquote) へ作り替えて閉じた。窓方式 (前方/後方の切り出し) へ戻すとここが red になる。
expect deny  "kill より前に別の kill と -L がある \; 連結" \
  'tmux -L probe kill-server; tmux new-session -d \; kill-server'
expect deny  "&& で繋いだ前半に kill と -L" \
  'tmux -L probe kill-session -t x && tmux new-session -d \; kill-server'
expect deny  "先行行のコメントに -L (改行あり)" \
  '# tmux -L probe kill-server で片付ける
tmux new-session -d \; kill-server'
expect deny  "TAB 区切りの tmux トークン" \
  'tmux -L probe ls; tmux	new-session -d \; kill-server'
expect deny  "man tmux 記載の引用符セミコロン \x27;\x27" \
  'tmux new-session -d ";" kill-server'
expect deny  "引数末尾に ; を含む形" \
  'tmux "new-session -d;" kill-server'
expect deny  "resize-pane -L はソケット指定ではない" \
  'tmux resize-pane -L 5 \; kill-server'
expect deny  "capture-pane -S はソケット指定ではない" \
  'tmux capture-pane -S -3000 -p \; kill-server'
expect deny  "引数内の ssh -L を免除に数えない" \
  'tmux new-session -d "ssh -L 8080:localhost:80 host" \; kill-server'
expect deny  "実行ファイル名を引用符で囲んだ形" \
  '"tmux" kill-server'
# tmux の実フラグ (-L/-S に見えるがソケット指定でないもの) を免除に数えないこと。
# この repo が実際に使っている形: select-pane -L (_tmux.conf) / capture-pane -S / display-popup -E -S
expect deny  "select-pane -L はソケット指定ではない" \
  'tmux new-session -d \; select-pane -L \; kill-server'
expect deny  "capture-pane -S はソケット指定ではない" \
  'tmux new-session -d \; capture-pane -p -S -3000 \; kill-server'
expect deny  "display-popup -E -S はソケット指定ではない" \
  'tmux display-popup -E -S fg=red "ls" \; kill-server'
expect deny  "probe を片付けた直後の bare (規範 md の作業フロー)" \
  'tmux -L probe kill-session -t probe; tmux new-session -d \; kill-server'
expect deny  "前半は正当な \; 連結・後半が bare" \
  'tmux -L probe ls \; kill-server; tmux new-session -d \; kill-server'

# --- 正当な形を止めないこと (窓方式が誤検出していた形) --------------------------------
expect allow "-S のソケットパス末尾が tmux" \
  'tmux -S /tmp/rt/tmux kill-server'
expect allow "-L のソケット名が tmux" \
  'tmux -L tmux kill-server'
expect allow "send-keys の引数に tmux コマンド文字列" \
  'tmux -L probe send-keys "tmux ls" Enter \; kill-server'
expect allow "run-shell の引数に tmux コマンド文字列" \
  'tmux -L probe run-shell "tmux ls" \; kill-server'
expect allow "grep -c の引数に文字列として含む" \
  'grep -c "tmux kill-server" file'
# 値を取るグローバルオプション (-c/-f/-T) の値を消費しないと、値がサブコマンドと誤認されて
# グローバル区間が閉じ、後続の -L が免除に数えられない。規範 md が推奨する隔離の形
# (-f /dev/null で素の設定のサーバを立てる) を deny していた退行のガード。
expect allow "-f <値> の後に -L (隔離の推奨形)" \
  'tmux -f /dev/null -L probe kill-server'
expect allow "-c <値> の後に -L" \
  'tmux -c /bin/sh -L probe kill-server'
expect allow "-T <値> の後に -L" \
  'tmux -T RGB -L probe kill-server'
expect allow "値なしフラグ (-u) の後に -L" \
  'tmux -u -L probe kill-server'

# alias 回避の `\\tmux` も実行ファイルとして拾う (剥がさないと素通りする)
expect deny  "先頭バックスラッシュで alias 回避" \
  '\\tmux kill-server'
expect deny  "フルパス + 先頭バックスラッシュ" \
  '\\/opt/homebrew/bin/tmux kill-server'
expect deny  "tmux 内コマンド連結 \\; 経由の bare" 'tmux new-session -d \; kill-server'
expect deny  "pkill -f tmux (全サーバ無差別)" 'pkill -f tmux'
expect deny  "pkill -x tmux" 'pkill -x tmux'
expect deny  "killall tmux" 'killall tmux'

# --- 実証されたバイパス (全て allow だった) --------------
expect deny  "フルパス起動" '/opt/homebrew/bin/tmux kill-server'
expect deny  "フルパスの事故コマンド列" \
  'export TMUX_TMPDIR=$(mktemp -d); /opt/homebrew/bin/tmux -f /dev/null new-session -d -s probe; /opt/homebrew/bin/tmux kill-server'
expect deny  "bash -c 経由 (引用符が直前)" "bash -c 'tmux kill-server'"
expect deny  "sh -lc 経由 (二重引用符が直前)" 'sh -lc "tmux kill-session -t probe"'
expect deny  "subcommand 略記 kill-serve (tmux は前方一致を受理)" 'tmux kill-serve'
expect deny  "subcommand 略記 kill-ser" 'tmux kill-ser'
expect deny  "subcommand 略記 kill-sessio" 'tmux kill-sessio -t x'
expect deny  "行継続で分断された形" 'tmux \
  kill-server'
expect deny  "コメント内の -L で免除されない" 'tmux kill-server  # -L probe の後片付け'
# 引用符の中の ` #` はコメントではない。`;` は区切りでない印に置き換わるので、書式文字列の後ろの kill が同じセグメントに入る
expect deny  "書式文字列の ' #{..}' の後ろの kill-server を落とさない" "/opt/homebrew/bin/tmux list-panes -a -F '#{session_name} #{pane_id}'; /opt/homebrew/bin/tmux kill-server"
expect deny  "書式文字列の後ろの -L default の kill-server を落とさない" "tmux -L default list-panes -F '#{a} #{b}'; tmux -L default kill-server"
expect deny  "二重引用符の中の ' #' の後ろの kill-session を落とさない" 'echo "a # b" ; tmux kill-session -t x'
expect deny  "二重引用符の中のエスケープした引用符の後の ' #' でも落とさない" 'echo "a\" #" ; tmux kill-server'
# 区切りは「空白 1 文字 + #」だけ。タブ + # は bash ではコメントだが、引用符の追い方が bash と違う形 ($'..'・${X:-..}) で
# 素通りを作るので、コメントとして落とさない (deny 側に倒す)
expect deny  "エスケープした空白の後の # はコメントではない" 'echo a\ #b; tmux kill-server'
expect deny  "パスの付いたシェルの -c の中の kill (中に ' #' があっても)" '/bin/bash -c "tmux kill-server #x"'
expect deny  "eval に渡した文字列の中の kill" 'eval "tmux kill-server #x"'
expect deny  "タブの後の # はコメントとして落とさない" "$(printf 'echo ${X:-\t#} ; tmux kill-server')"
expect allow "引用符の外の行内コメントの kill は検査しない" "echo 'x' # tmux kill-server"
expect allow "書式文字列のあとの -L 付きの kill-server は通す" "tmux -L probe list-panes -F '#{a} #{b}'; tmux -L probe kill-server"
expect deny  "kill トークンより後ろの -L では免除しない" 'tmux kill-session -t x; sort -S 1G /dev/null'
# 引用符の中の `|` `&&` 改行は区切りではない。区切りとして割ると、後半が `#{b}` で始まって行頭コメントとして kill ごと捨てていた
expect deny  "書式文字列の中の | の後ろの kill-server" "tmux list-panes -F '#{a}|#{b}'; tmux kill-server"
expect deny  "書式文字列の中の && の後ろの kill-server" "tmux list-panes -F '#{a}&&#{b}'; tmux kill-server"
expect deny  "書式文字列の中の改行の後ろの kill-server" "tmux list-panes -F '#{a}
#{b}'; tmux kill-server"
# サブシェル・コマンド置換の中の tmux
expect deny  "サブシェルの中" '(tmux kill-server)'
expect deny  "コマンド置換 \$(..) の中" 'echo $(tmux kill-server)'
expect deny  "バッククォートの中" 'echo `tmux kill-server`'
expect deny  "二重引用符の中のコマンド置換" 'echo "$(tmux kill-server)"'
expect deny  "& で背景に回した形" 'tmux kill-server & wait'
# 語の途中の \ と引用符は shell が剥がして繋ぐ
expect deny  "実行ファイル名の途中の \\" 't\mux kill-server'
expect deny  "サブコマンドの途中の \\" 'tmux kill-se\rver'
expect deny  "サブコマンドの途中の \\ (kill の語が崩れる形)" 'tmux kill\-server'
expect deny  "サブコマンドを引用符で囲む" "tmux 'kill-server'"
expect deny  "サブコマンドの一部を引用符で囲む" "tmux 'kill'-server"
expect deny  "bash -c \$'..' の中" "bash -c \$'tmux kill-server'"
expect deny  "heredoc の本文の行をまたぐ引用符で後ろを飲み込まない" "cat <<EOF
don't
EOF
tmux kill-server
it's"
expect deny  "heredoc の本文の中の kill (本文は 1 行ずつコマンドとして読む)" "bash <<EOF
tmux kill-server
EOF"
expect allow "heredoc の本文の -L 付きの kill は通す" "cat <<EOF
x
EOF
tmux -L probe kill-server"
# --- `${..}`・heredoc・長い語・割り直しの量: 引用符と区切りを追い損ねると素通りする形 -------------------------
expect deny  "\${..} の中のコマンド置換" 'echo ${x:-$(tmux kill-server)}'
expect deny  "\${..} の中の引用符の }" "echo \${x:-'}'} ; tmux kill-server ; echo \"'\""
expect deny  "bash -c の中の \${..} のエスケープした {" "bash -c 'echo \${x:-\{} ; tmux kill-server ; echo a}'"
expect deny  "二重引用符の中の \${..} のバッククォート" 'echo "${x:-`tmux kill-server`}"'
expect deny  "算術の << の後の行継続" 'echo $((1<<2))
tmux \
kill-server'
expect deny  "here-string の後の行継続" 'cat <<< hi
tmux \
kill-server'
expect deny  "シェルに渡す heredoc の本文の行継続" "bash <<'EOF'
tmux \\
kill-server
EOF"
expect deny  "/ を並べた長いパスのシェルの -c" "$(printf '/%.0s' $(seq 300))bin/bash -c 'tmux kill-server'"
expect deny  "eval を 201 重ねた長い入力 (割り直しの量の上限)" "$(printf 'eval %.0s' $(seq 201))$(head -c 30000 /dev/zero | tr '\0' x) tmux kill-server"
expect deny  "heredoc の本文の kill の語が 200 行を越える (割り直しの数の上限)" "bash <<EOF
$(for i in $(seq 201); do echo "bash -c 'echo kill-s$i'"; done)
EOF"
# --- 前の版からの穴 -----------------------------------------------------------------------------
expect deny  "bash -c -- の後の中身" "bash -c -- 'tmux kill-server'"
expect deny  "bash -c -x の後の中身" "bash -c -x 'tmux kill-server'"
expect deny  "\$'..' の \\x で隠した kill の語" "tmux \$'kill\\x2dserver'"
expect deny  "\$'..' の 8 進で隠した tmux" "\$'\\164mux' kill-server"
expect deny  "\$\"..\" の中のサブコマンド" 'tmux $"kill-server"'
expect deny  "-S の値の glob (本番の socket に展開されうる)" 'tmux -S /tmp/tmux-501/d* kill-server'
expect deny  "pkill -f の tmux のパス" 'pkill -f /opt/homebrew/bin/tmux'
expect deny  "-l (ログインシェル) は -L ではない" 'tmux -l kill-server'
expect allow "-L の値の大小は default の照合にだけ効く" 'tmux -L Lab kill-server'
# --- heredoc の本文は、実行されるかを見分けずに割る (見分けると渡し方の数だけ素通りする) ---------------------------------
expect deny  "パイプで後ろのシェルへ渡す heredoc" "cat <<'EOF' | bash
tmux kill-server
EOF"
expect deny  ". /dev/stdin で読む heredoc" ". /dev/stdin <<'EOF'
tmux kill-server
EOF"
expect deny  "区切りを引用符で囲んだ commit message の本文 (偽陽性として受ける)" "git commit -F - <<'MSGEOF' -- a.sh
fix: (tmux kill-server) の誤殺を止める
MSGEOF"
expect deny  "算術の << の区切りの語が後ろの行にある" 'echo $((1<<2))
tmux kill-server
2'
# --- 語数の 2 乗・$'..' の解き方・まとめたフラグ -----------------------------------------------------
expect deny  "シェルの名前の語を 16000 並べた後 (語数の 2 乗にしない)" "$(printf 'sh %.0s' $(seq 16000));tmux kill-server"
expect deny  "\$'..' で囲んだ実行ファイル名" "\$'tmux' kill-server"
expect deny  "\$'..' の \\u で隠した kill の語" "tmux \$'\\u006bill-server'"
expect deny  "bash -c の中の \$'..' の \\x" "bash -c \"tmux \$'\\x6b'ill-server\""
expect deny  "まとめたフラグの -L default (-uL default)" 'tmux -L probe -uL default kill-server'
expect deny  "まとめたフラグの付属値の -L default (-uLdefault)" 'tmux -L probe -uLdefault kill-server'
expect allow "まとめたフラグの -L の隔離 (-uL probe)" 'tmux -uL probe kill-server'
expect deny  "区切りを囲まない本文の \$(..) は実行される" 'cat > x.md <<EOF
例: $(tmux kill-server)
EOF'
# --- `<<` を heredoc として読むか決まらない形・まとめたフラグの値・-c の無いシェルの後ろの eval -----------------------
expect deny  "算術の << の区切りの語の行が行継続の続き" 'echo $((1<<kill-server))
tmux \
kill-server'
expect deny  "算術の << の後の引用符が行をまたぐ" "echo \$((1<<X))
echo '
X
' ; tmux kill-server ; echo '
'"
expect deny  "-S の付属値に c と大文字の L を含むパス" 'tmux -L probe -S/tmp/cL/../tmux-501/default kill-server'
expect deny  "まとめたフラグの末尾が値を取るフラグ (-uf の値の後の -L default)" 'tmux -L probe -uf /dev/null -L default kill-server'
expect deny  "まとめたフラグの末尾が -T (値の後の -L default)" 'tmux -L probe -uT 256 -L default kill-server'
expect allow "まとめたフラグの末尾が値を取るフラグでも、後ろの -L の隔離は通す" 'tmux -uf /dev/null -L probe kill-server'
expect deny  "-c の無いシェルの語の後ろの eval" "sh x.sh eval 'tmux kill-server'"
# kill の語の無い入力は、$'..' の escape が多くても割り直しの上限で deny にしない (字を隠せない escape は積まない)
expect allow "\$'\\t' を 210 行並べた入力 (kill の語なし)" "$(for i in $(seq 210); do printf 'v%s="$(printf $%s)"\n' "$i" "'\\t'"; done)"
expect allow "ssh の先で走る kill は手元の本番ではない" "ssh h 'tmux kill-server #x'"
expect allow "二重引用符の中の文字列 (置換なし) は検査しない" 'git commit -m "tmux kill-server を止める"'

# --- allow すべきもの (ソケット明示 / 無関係) -------------------------------------------
# 🚨 -L default / -S <...>/default はソケット明示でも**本番サーバ**直撃なので deny
# (2026-09-11 に別セッションが -L default kill-server で 30 セッション誤殺)。
expect deny  "-L default (本番直撃)" 'tmux -L default kill-server'
expect deny  "-Ldefault 付属値形" 'tmux -Ldefault kill-server'
expect deny  "-S <...>/default 直撃" 'tmux -S /private/tmp/tmux-501/default kill-server'
expect deny  "-f /dev/null -L default (隔離を装った本番直撃)" 'tmux -f /dev/null -L default kill-server'
expect deny  "-L default kill-session" 'tmux -L default kill-session -t x'
# 🚨 P1: macOS の FS は case-insensitive なので大文字綴りも本番に届く。両防御で塞ぐ。
expect deny  "-L DEFAULT (大文字) 直撃" 'tmux -L DEFAULT kill-server'
expect deny  "-L Default (混在) 直撃" 'tmux -L Default kill-server'
expect deny  "-S <...>/DEFAULT 直撃" 'tmux -S /private/tmp/tmux-501/DEFAULT kill-server'
expect allow "-L default-test (default で終わらない名前は素通し)" 'tmux -L default-test kill-server'
expect allow "-L mydefault (default で終わらない名前は素通し)" 'tmux -L mydefault kill-server'
expect allow "-L 明示の kill-server" 'tmux -L lab kill-server'
expect allow "-S 明示の kill-server" 'tmux -S /path/sock kill-server'
expect allow "-L 明示の複合" 'tmux -L lab new-session -d && tmux -L lab kill-server'
expect allow "-L 明示の \\; 連結" 'tmux -L lab new-session -d \; kill-server'
expect allow "読み取り系 tmux" 'tmux ls'
expect allow "kill-window は対象外" 'tmux kill-window -t 1'
expect allow "pid 指定の kill" 'kill -TERM 1234'
expect allow "無関係な pkill" 'pkill -f myapp'
expect allow "文字列に tmux を含む git" 'git commit -m "fix tmux config"'
expect allow "make test" 'make test'

# --- 入力長で「ゲートが黙って消える」ことの回帰 (2026-08-21) -----------------------------
# 実測: unquote が 1 文字ループで、bash の部分文字列展開が O(n) なので実質 O(n²)。90KB の
# 入力で 37 秒かかり、_claude/settings.json の `timeout: 10` に殺されて stdout 0 byte =
# **ソケット未指定の kill に対して deny が 1 byte も出ない** (rc=124 を実測)。
# 「遅い」ではなく「安全機構が入力長で無効化される」問題なので、時間ではなく判定で pin する。
big_payload() {  # $1=セグメント数 → "echo …; " を繰り返した文字列
  local i out=''
  for ((i = 0; i < $1; i++)); do out+='echo line-with-some-words-and-paths /a/b/c ; '; done
  printf '%s' "$out"
}
# 9KB / 90KB: 検査を完走して deny できること (timeout 10 の内側であることも含む)
expect deny "大きな入力 (9KB) でも deny を出す" "$(big_payload 200)tmux kill-server"
expect deny "大きな入力 (90KB) でも deny を出す" "$(big_payload 2000)tmux kill-server"
# 360KB: MAX_SCAN_BYTES を超えるので検査を諦めるが、素通りではなく deny へ倒す (fail-closed)
expect deny "巨大な入力 (360KB) は fail-closed で deny" "$(big_payload 8000)tmux kill-server"
# 上限は内容に関わらず効く: 中身だけなら allow の入力 (引用符の中の kill の語) でも、上限を越えたら deny、下なら allow
benign_payload() { printf "echo '%s kill-session'" "$(head -c "$1" /dev/zero | tr '\0' a)"; }
expect allow "上限 (128KB) の下の、中身が allow の大きな入力" "$(benign_payload 120000)"
expect deny  "上限 (128KB) を越えた、中身が allow の大きな入力" "$(benign_payload 140000)"
# awk が無い・失敗したときは deny に倒す (字句解析できないまま通さない)
NOAWK="$(mktemp -d)"
for c in bash dirname jq cat mktemp rm; do ln -s "$(command -v "$c")" "$NOAWK/$c"; done
got_noawk="$(PATH="$NOAWK" decision 'tmux kill-session')"
rm -rf "$NOAWK"
[ "$got_noawk" = deny ] || { printf '✗ awk が無いとき deny に倒れない (実際=%s)\n' "$got_noawk"; exit 1; }
printf '✓ awk が無いとき deny に倒す (deny)\n'
# kill 系トークンを含まない大入力は前置きフィルタで即 allow (体感レイテンシに乗せない)
expect allow "kill を含まない大きな入力は素通り" "$(big_payload 2000)echo done"
# 和文の大入力: 上限未満の長い行が多数。bash の 1 文字ループで引用符を処理していた版は、この形で 10 秒の timeout を越えて無出力になった
# (2026-11-10 実測。字ごとの部分文字列展開が O(n) で、マルチバイトではさらに遅い)。big_payload は 1 本の巨大な行で、この形を通らない
jp_payload() {  # $1=行数 → 和文 8000 字を引用符で囲み末尾に kill-session を付けた echo の行
  local i line out=''
  line="echo '$(printf '和%.0s' $(seq 8000)) kill-session'"
  for ((i = 0; i < $1; i++)); do out+="$line"$'\n'; done
  printf '%s' "$out"
}
# 改行は $(..) の外で足す (コマンド置換は末尾の改行を落とし、最後の行の引用符に tmux が繋がって 1 語になる)
# bash の 1 文字ループで引用符を処理していた版は、この入力で 7.8 秒かかった (今の形は 0.3 秒。2026-11-10)。大きさの上限 (128KB) の内に収まるので、
# 遅い版でも 10 秒は越えない: 速さは pin せず、大きな和文の入力で判定が正しいことを pin する。
# 語ごとに長さの 2 乗かかる bash の `${t##*/}` も、上限の内では 1 語で 8 秒弱にしかならず、負荷の無いときは赤にならない (hook のコメントで止める)
expect deny "和文の大入力 (8000 字 × 5 行) でも deny を出す" "$(jp_payload 5)"$'\n'"tmux kill-server"
# 1 語が上限近くまで長い入力。語に正規表現の `.*` を当てる処理 (awk の `sub(/.*\//)`) が入ると 28 秒かかった (今の形は 0.7 秒)
expect deny "120KB の 1 語の後でも deny を出す" "echo '$(head -c 120000 /dev/zero | tr '\0' a) kill-session'"$'\n'"tmux kill-server"
# 大入力でもソケット明示なら allow のまま (fail-closed を広げすぎていないことの対照)
expect allow "大きな入力 + ソケット明示は allow" "$(big_payload 200)tmux -L probe kill-server"

printf '\nAll deny-bare-tmux-kill tests passed successfully!\n'
