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
