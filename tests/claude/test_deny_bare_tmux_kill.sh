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
command -v timeout >/dev/null 2>&1 || { echo "SKIP: timeout(1) が無い環境 (本番と同じ上限を課せない)"; exit 0; }

# フック本体が正しくても settings.json からエントリが消えれば防御はゼロになるのに、それを
# 見る検査が無かった (2026-08-20 の red team)。まず配線を検査する。
SETTINGS="$ROOT_DIR/_claude/settings.json"
[ -f "$HOOK" ] || { printf '✗ フック本体が無い: %s\n' "$HOOK"; exit 1; }
[ -x "$HOOK" ] || { printf '✗ フックに実行権限が無い: %s\n' "$HOOK"; exit 1; }
[ -f "$SETTINGS" ] || { printf '✗ settings.json が無い: %s\n' "$SETTINGS"; exit 1; }

# ⚠️ 依存コマンドの不在を「配線されていない」と誤診断しないこと (検査不能と防御ゼロは別)。
command -v grep >/dev/null 2>&1 || { printf '✗ grep が無く配線を検査できない\n'; exit 1; }
grep -q 'deny-bare-tmux-kill.sh' "$SETTINGS" \
  || { printf '✗ settings.json にフックが配線されていない (防御が丸ごと無効)\n'; exit 1; }

if ! command -v jq >/dev/null 2>&1; then
  # ⚠️ ここを exit 0 (成功) にしないこと。フック本体は jq 不在時 fail-open (= 防御ゼロ) なので、
  # 「防御が完全に無効な環境で All tests passed」と報告する形になる (2026-08-20 の red team)。
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

decision() {  # $1=コマンド文字列 → "deny" / "allow" / "error" / "harness-error"
  # ⚠️ フックの異常終了・壊れた出力を allow に畳まないこと。旧実装は
  #   ... | jq -r '...// empty' | grep . || echo allow
  # で、フックが落ちても jq が失敗しても "allow" になり、「素通り」と「検査できなかった」が
  # 区別できなかった (検査できないときに緑を返す形)。
  # ⚠️ 本番と同じ timeout を課すこと (_claude/settings.json の PreToolUse は timeout: 10)。
  # 課さないと「入力長で hook が timeout に殺されて deny が消える」= 安全機構が無効化される
  # 退行を観測できない (時間内に終わるかどうかが判定に影響しないため、変異を当てても緑のまま
  # 通る。2026-08-21 に実際にこの形の空回りテストを書いて気づいた)。
  # ⚠️ コマンド文字列は **argv でなくファイル経由** で jq に渡すこと。--arg に数百 KB を渡すと
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
  out="$(timeout "$HOOK_TIMEOUT" "$HOOK" <"$json" 2>/dev/null)"
  rc=$?
  rm -f "$payload" "$json"
  if [ -n "$out" ]; then
    printf '%s' "$out" | jq -r '.hookSpecificOutput.permissionDecision // "error"' 2>/dev/null || echo error
    return
  fi
  # 無出力の内訳を区別する: rc=0 は「何も deny しなかった」= 本番でも素通り。rc=124 は
  # timeout に殺された = 本番でも stdout 0 byte で素通り (これがゲート消失の観測点)。
  # それ以外の非 0 は hook 自身の異常終了なので error に倒す。
  case "$rc" in
    0 | 124) echo allow ;;
    *) echo error ;;
  esac
}

expect() {  # $1=期待(deny/allow) $2=説明 $3=コマンド
  local got
  got="$(decision "$3")"
  if [ "$got" != "$1" ]; then
    printf '✗ %s\n  期待=%s 実際=%s\n  cmd: %s\n' "$2" "$1" "$got" "$3"
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
# --- 免除判定の回帰ガード (issue 069) ------------------------------------------------
# 2026-08-20 の red team が「文字列の窓」方式を破った全形。トークン走査 (scan_segment /
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

# --- 2026-07-30 の敵対的レビューで実証されたバイパス (全て allow だった) --------------
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
expect deny  "kill トークンより後ろの -L では免除しない" 'tmux kill-session -t x; sort -S 1G /dev/null'

# --- allow すべきもの (ソケット明示 / 無関係) -------------------------------------------
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
# kill 系トークンを含まない大入力は前置きフィルタで即 allow (体感レイテンシに乗せない)
expect allow "kill を含まない大きな入力は素通り" "$(big_payload 2000)echo done"
# 大入力でもソケット明示なら allow のまま (fail-closed を広げすぎていないことの対照)
expect allow "大きな入力 + ソケット明示は allow" "$(big_payload 200)tmux -L probe kill-server"

printf '\nAll deny-bare-tmux-kill tests passed successfully!\n'
