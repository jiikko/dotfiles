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
# 検出の設計 (2026-07-30 のレビューで実証されたバイパスを塞いだ形):
#   - 実行ファイルはフルパス・引用符隣接も拾う (`/opt/homebrew/bin/tmux`, `bash -c 'tmux ...'`)
#   - subcommand は tmux の前方一致受理に合わせる (`tmux kill-serve` も実際にサーバを落とす)
#   - `-L`/`-S` の免除は **tmux トークンと kill トークンの間** にあるものだけ
#     (コメントや無関係な位置の -L で免除が外れていた)
#   - 判定前に行継続 (`\`+改行) を畳み、`#` 以降のコメントを落とす
#
# 原理的な限界 (静的検査では閉じない。規範 md の「隔離を実証してから打つ」に委ねる):
#   変数間接 (`T=tmux; $T kill-server` / `K=kill-server; tmux $K`)、スクリプト間接
#   (`bash ./tmp/cleanup.sh` の中身)、eval 経由の動的生成。
#
# 入力: PreToolUse の hook JSON (stdin)。.tool_input.command を検査する。
# 出力: 違反時のみ permissionDecision=deny の JSON。それ以外は無出力 exit 0。
# 注意: 静的検査なので散文 (コミットメッセージ等) に「小文字 tmux → kill-server」の並びが
#   あると偽陽性 deny になる。回避は言い換え (ルール md の「強制手段」節に記載)。
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

REASON_KILL="ソケット指定の無い tmux kill-server/kill-session は本番サーバを直撃します (\$TMUX が TMUX_TMPDIR に優先。2026-07-30 に 29 セッション誤殺の実害)。unset TMUX + ユニークな -L <name> (または -S <path>) を tmux の引数として付け、先に tmux -L <name> ls で隔離を実証してから実行してください。規範: _claude/rules/tmux-probe-requires-socket-isolation.md"
REASON_PKILL="pkill/killall で tmux を狙うのは全サーバ無差別 kill です (本番サーバも死にます)。対象サーバの pid を特定して kill するか、tmux -L <name> kill-server を使ってください。規範: _claude/rules/tmux-probe-requires-socket-isolation.md"

# tmux が受理する kill-server / kill-session の前方一致形 (曖昧でない範囲)。
# `kill-s` / `kill-se` は両者で曖昧なため tmux 自身が拒否する = 検出不要。
KILL_RE='kill-s(er(v(er?)?)?|es(s(i(on?)?)?)?)'
# tmux 実行ファイルのトークン (行頭 / 空白 / 連結記号 / 引用符・括弧の直後、フルパス可)
TMUX_RE='(^|[[:space:];&|(]|["'"'"'])([^[:space:]"'"'"']*/)?tmux[[:space:]]'
# ソケット明示 (tmux の引数位置にある -L / -S のみ免除に使う)
SOCK_RE='(^|[[:space:]])-(L|S)([[:space:]]|=)?[^[:space:]]'

# 行継続 (`\` + 改行) を空白に畳む。畳まないと `tmux \`+改行+`kill-server` が別セグメントに
# 割れて tmux トークンを見失う (レビューで実証されたバイパス)。
# ⚠️ ここを sed でやらないこと: 引用符と括弧を含む正規表現を sed -E に渡すと BSD sed が
#    "parentheses not balanced" で落ち、フック全体が静かに無力化する (実測 2026-07-30)。
#    bash の文字列操作なら外部コマンドの方言差が入らない。
normalized="${cmd//\\$'\n'/ }"

# tmux のクライアント内コマンド連結 `\;` 経由 (例: tmux new-session \; kill-server) は
# 下のセグメント分割で `;` で割れて tmux トークンを見失うため、分割前に全体で判定する
if [[ "$normalized" =~ \\\;[[:space:]]*$KILL_RE ]]; then
  [[ "$normalized" =~ $SOCK_RE ]] || deny "$REASON_KILL"
fi

# コマンドを連結演算子 (; && || | 改行) でセグメントに割り、セグメント単位で判定する。
segments=$(printf '%s' "$normalized" | sed -E 's/&&|\|\||[;|]/\n/g')

while IFS= read -r seg; do
  # 行内コメント (" #" 以降) を落とす。落とさないと `# -L probe の後片付け` のような
  # コメントが -L 免除を誤って与える (レビューで実証)。
  seg="${seg%% #*}"

  # pkill / killall で tmux を狙う形は全サーバ無差別なので常に deny
  if [[ "$seg" =~ (^|[[:space:]\;\&\|])(pkill|killall)[[:space:]] ]] \
     && [[ "$seg" =~ [[:space:]][\"\']?tmux[\"\']?([[:space:]]|$) ]]; then
    deny "$REASON_PKILL"
  fi

  # tmux ... kill-* を含むセグメントを検出。免除判定は「tmux トークンの直後から kill トークン
  # 直前まで」= 実際に tmux へ渡る引数区間に -L/-S があるかで行う。区間外 (kill の後ろ・
  # コメント・無関係なコマンド) の -L で免除が外れるバイパスをこれで閉じる。
  if [[ "$seg" =~ $TMUX_RE ]] && [[ "$seg" =~ $KILL_RE ]]; then
    after_tmux="${seg#*tmux }"        # 最初の "tmux " より後ろ (フルパスでも同じ位置で切れる)
    argzone="${after_tmux%%kill-s*}"  # 最初の kill-s* より前 = tmux の引数だけ
    [[ "$argzone" =~ $SOCK_RE ]] || deny "$REASON_KILL"
  fi
done <<EOF
$segments
EOF

exit 0
