#!/usr/bin/env bash
#
# PreToolUse(Bash) フック: ソケット指定の無い破壊的 tmux コマンドを deny する。
#
# なぜ: tmux ペイン内では $TMUX が TMUX_TMPDIR より優先されるため、素の
# `tmux kill-server` / `tmux kill-session` は「隔離したつもり」でも本番サーバを直撃する
# (2026-07-30 実発: 別セッションの probe 後片付けが 29 セッションを一撃で落とした。
#  経緯と規範は _claude/rules/tmux-probe-requires-socket-isolation.md)。
# 散文ルールはアドホックな 1 コマンド経路には効かないため、harness が実行する本フックで
# 機械的に止める。`-L <name>` / `-S <path>` でソケットを明示した形だけ通す
# (明示していれば本番を意図的に指すことも可能 = 禁止ではなく「明示の強制」)。
#
# 入力: PreToolUse の hook JSON (stdin)。.tool_input.command を検査する。
# 出力: 違反時のみ permissionDecision=deny の JSON。それ以外は無出力 exit 0。
# 注意: コマンド文字列の静的検査なので、文字列リテラル内のパターンにも発火する
#   (偽陽性は稀で、deny 理由に回避方法を明記しているため許容)。
set -uo pipefail

input=$(cat)

# jq が無い環境では静かに諦める (誤 deny させない)
command -v jq >/dev/null 2>&1 || exit 0

cmd=$(printf '%s' "$input" | jq -r '.tool_input.command // ""')
[ -n "$cmd" ] || exit 0

deny() {
  jq -n --arg reason "$1" '{
    hookSpecificOutput: {
      hookEventName: "PreToolUse",
      permissionDecision: "deny",
      permissionDecisionReason: $reason
    }
  }'
  exit 0
}

# tmux のクライアント内コマンド連結 `\;` 経由 (例: tmux new-session \; kill-server) は
# 下のセグメント分割で `;` で割れて tmux トークンを見失うため、分割前に全体で判定する
if printf '%s' "$cmd" | grep -Eq '\\;[[:space:]]*kill-(server|session)'; then
  if ! printf '%s' "$cmd" | grep -Eq '(^|[[:space:]])-(L|S)([[:space:]]|=)?[^[:space:]]'; then
    deny "ソケット指定の無い tmux コマンド連結 (\\; kill-server/kill-session) は本番サーバを直撃します。unset TMUX + ユニークな -L <name> (または -S <path>) を付けてください。規範: _claude/rules/tmux-probe-requires-socket-isolation.md"
  fi
fi

# コマンドを連結演算子 (; && || | 改行) でセグメントに割り、セグメント単位で判定する。
# `tmux -L x kill-server && rm -rf $d` のような複合でも、-L の有無は同一セグメント内で見る。
segments=$(printf '%s' "$cmd" | sed -E 's/&&|\|\||[;|]/\n/g')

while IFS= read -r seg; do
  # pkill / killall で tmux を狙う形は全サーバ無差別なので常に deny
  if printf '%s' "$seg" | grep -Eq '(^|[[:space:];&|])(pkill|killall)[[:space:]]' \
     && printf '%s' "$seg" | grep -Eq '[[:space:]]["'"'"']?tmux["'"'"']?([[:space:]]|$)'; then
    deny "pkill/killall で tmux を狙うのは全サーバ無差別 kill です (本番サーバも死にます)。対象サーバの pid を特定して kill するか、tmux -L <name> kill-server を使ってください。規範: _claude/rules/tmux-probe-requires-socket-isolation.md"
  fi
  # tmux ... kill-server / kill-session を含むセグメントで -L/-S が無ければ deny
  if printf '%s' "$seg" | grep -Eq '(^|[[:space:];&|(])tmux[[:space:]][^\n]*kill-(server|session)|(^|[[:space:];&|(])tmux[[:space:]]+kill-(server|session)'; then
    if ! printf '%s' "$seg" | grep -Eq '(^|[[:space:]])-(L|S)([[:space:]]|=)?[^[:space:]]'; then
      deny "ソケット指定の無い tmux kill-server/kill-session は本番サーバを直撃します (\$TMUX が TMUX_TMPDIR に優先。2026-07-30 に 29 セッション誤殺の実害)。unset TMUX + ユニークな -L <name> (または -S <path>) を付け、先に tmux -L <name> ls で隔離を実証してから実行してください。規範: _claude/rules/tmux-probe-requires-socket-isolation.md"
    fi
  fi
done <<EOF
$segments
EOF

exit 0
