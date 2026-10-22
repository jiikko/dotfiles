# shellcheck shell=bash
# hook の入出力の定型。PreToolUse / PostToolUse の Bash の hook が使う。
#
# 🚨 どの関数も fail-open: jq が無い・入力の JSON が読めないときは rc≠0 を返すだけで、呼ぶ側が exit 0 で諦める
# (誤って deny しない・何も注入しない)。lib 自体を読めないときも、呼ぶ側は `|| exit 0` で諦める。

# hook_read_bash_command: stdin の hook の JSON を読み、全体を HOOK_INPUT、.tool_input.command を HOOK_CMD に入れる。
# jq が無い・JSON として読めないなら rc=1。
# shellcheck disable=SC2034  # HOOK_INPUT / HOOK_CMD は呼び出し側 (source した hook) が読む
hook_read_bash_command() {
  HOOK_INPUT=$(cat)
  HOOK_CMD=""
  command -v jq >/dev/null 2>&1 || return 1
  HOOK_CMD=$(printf '%s' "$HOOK_INPUT" | jq -r '.tool_input.command // ""' 2>/dev/null) || return 1
}

# hook_deny <理由>: PreToolUse で permissionDecision=deny を返して終わる (exit 0。deny は JSON で伝える)。
hook_deny() {
  jq -n --arg reason "$1" '{
    hookSpecificOutput: {
      hookEventName: "PreToolUse",
      permissionDecision: "deny",
      permissionDecisionReason: $reason
    }
  }'
  exit 0
}

# hook_strip_heredocs <text>: heredoc の本文を落とす。`<<EOF` / `<<-EOF` / `<<'EOF'` / `<<"EOF"` の次の行から、区切り語だけの行 (<<- なら先頭のタブを許す) まで。
# 🚨 終わりの区切りが後ろに見つかるときだけ落とす (見つからない `<<` は heredoc でない: 算術の `$((1<<n))` を heredoc と読むと、
# 後ろの行を全部落として素通りになる)。`<<<` (here-string) は heredoc でない。
# 🚨 awk で入力長に比例する時間に収める (bash のループで区切りを探すと、開始の数 × 行数に比例し、/bin/bash 3.2 では行を積むだけで
# 10 秒の timeout を越えて無出力 = 素通りになった)。後ろから 1 回走って「その語が次に現れる行」を求め、
# 前から 1 回走って落とす
hook_strip_heredocs() {
  printf '%s\n' "$1" | LC_ALL=C awk '
    { line[NR] = $0 }
    END {
      n = NR
      # 後ろから: 各行 i について、区切り語 d が i より後ろで最初に現れる行 (<<- ならタブを剥いだ形も)
      for (i = n; i >= 1; i--) {
        if (match(line[i], /(^|[^<])<<-?[ \t]*\\?["\047]?[A-Za-z_][A-Za-z0-9_]*["\047]?([^<]|$)/)) {
          op = substr(line[i], RSTART, RLENGTH)
          sub(/^[^<]?<</, "", op); dash = (substr(op, 1, 1) == "-"); if (dash) op = substr(op, 2)
          gsub(/^[ \t]*\\?["\047]?/, "", op); match(op, /^[A-Za-z_][A-Za-z0-9_]*/); d = substr(op, RSTART, RLENGTH)
          end_at[i] = dash ? next_tab[d] : next_plain[d]
        }
        next_plain[line[i]] = i
        t = line[i]; sub(/^\t+/, "", t); next_tab[t] = i
      }
      for (i = 1; i <= n; i++) {
        print line[i]
        if ((i in end_at) && end_at[i] > i) i = end_at[i] # 区切りが見つかったときだけ本文と区切りの行を飛ばす
      }
    }'
}
