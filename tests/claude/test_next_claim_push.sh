#!/usr/bin/env bash
# _claude/hooks/next-claim-push.sh (PostToolUse(Bash): issues/next/ への claim を検出して
# 「単独 commit して push したか」を注入する) の unit テスト。
#
# なぜ: この hook は「別マシンと同じ issue を二重に着手する」事故 (2026-09-02 に実際に発生) を
# harness 側で減らす装置。判定式が壊れると **無言で発火しなくなる** = 防御がゼロに戻る。
# 規範: _claude/rules/claim-issue-in-next-and-push.md
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/next-claim-push.sh"
HOOK_TIMEOUT=10

# 本番と同じ上限を課せないなら「合格」ではなく「判定不能 = 失敗」にする
# (tests/claude/test_deny_bare_tmux_kill.sh と同じ規律。CI で timeout(1) が消えて
#  検査が丸ごと skip に化けた事故がある)。
if command -v timeout >/dev/null 2>&1; then
  TIMEOUT_BIN=timeout
elif command -v gtimeout >/dev/null 2>&1; then
  TIMEOUT_BIN=gtimeout
else
  echo "✗ timeout(1) / gtimeout(1) がどちらも無い。本番と同じ上限を課せないので検査できない" >&2
  exit 1
fi

command -v jq >/dev/null 2>&1 || { echo "✗ jq が無い。hook は jq 前提なので検査できない" >&2; exit 1; }

fail=0
ok=0

run_hook() { # $1 = command 文字列 → stdout に hook の出力
  jq -n --arg c "$1" '{tool_input: {command: $c}}' |
    "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true
}

expect_fire() {
  local desc=$1 cmd=$2 out
  out=$(run_hook "$cmd")
  if [ -z "$out" ]; then
    echo "✗ 発火すべきなのに無出力: $desc — $cmd"; fail=$((fail + 1)); return
  fi
  # 出力が JSON として妥当で、規範へのポインタを含むこと (壊れた JSON は素通りと同じ)
  if ! printf '%s' "$out" | jq -e '.hookSpecificOutput.additionalContext | test("claim-issue-in-next-and-push")' >/dev/null 2>&1; then
    echo "✗ JSON が壊れている / 本文が期待と違う: $desc"; fail=$((fail + 1)); return
  fi
  ok=$((ok + 1))
}

expect_silent() {
  local desc=$1 cmd=$2 out
  out=$(run_hook "$cmd")
  if [ -n "$out" ]; then
    echo "✗ 誤発火: $desc — $cmd"; fail=$((fail + 1)); return
  fi
  ok=$((ok + 1))
}

# --- 発火すべき形 ---
expect_fire "git mv で next へ"            "git mv issues/186-x.md issues/next/"
expect_fire "素の mv で next へ"           "mv issues/186-x.md issues/next/"
expect_fire "末尾スラッシュ無し"           "git mv issues/186-x.md issues/next"
expect_fire "後続コマンドが続く形"         "git mv issues/186-x.md issues/next/ && echo done"
expect_fire "空白入りのパス"               'git mv "issues/186 x.md" issues/next/'

# --- 発火してはいけない形 ---
expect_silent "next から出す (claim 解除)" "git mv issues/next/186-x.md issues/"
expect_silent "next を見るだけ"            "ls issues/next/"
expect_silent "done への移動"              "git mv issues/186-x.md issues/done/"
expect_silent "無関係な commit"            "git commit -m x"
expect_silent "next を含む文字列だけ"      "grep -rn next issues/README.md"

# --- 異常系: 壊れた入力で JSON を壊さない / 落ちない ---
if printf '' | "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" >/dev/null 2>&1; then
  ok=$((ok + 1))
else
  echo "✗ 空入力で異常終了した (PostToolUse は全 Bash 呼び出しで no-op であるべき)"; fail=$((fail + 1))
fi
if printf 'not json' | "$TIMEOUT_BIN" "$HOOK_TIMEOUT" "$HOOK" >/dev/null 2>&1; then
  ok=$((ok + 1))
else
  echo "✗ 非 JSON 入力で異常終了した"; fail=$((fail + 1))
fi

# --- 配線: hook 本体が正しくても settings.json から消えれば防御はゼロ ---
if jq -e '[.hooks.PostToolUse[] | select(.matcher == "Bash") | .hooks[].command] | any(test("next-claim-push"))' \
     "$ROOT_DIR/_claude/settings.json" >/dev/null 2>&1; then
  ok=$((ok + 1))
else
  echo "✗ _claude/settings.json の PostToolUse(Bash) に next-claim-push.sh が配線されていない"
  fail=$((fail + 1))
fi

echo "検査 $((ok + fail)) 件: ok=$ok / fail=$fail"
[ "$fail" -eq 0 ] || exit 1
