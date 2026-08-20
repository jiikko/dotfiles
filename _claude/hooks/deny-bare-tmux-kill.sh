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

# ⚠️ 性能ガード (これが無いとゲートが黙って消える)。以降の検査は unquote の 1 文字ループと
# セグメントごとの fork を通るため、コストが入力長に効く。実測 2026-08-21: 90KB / 2000 セグメントで
# **37 秒**。_claude/settings.json は PreToolUse に `timeout: 10` を明示しているので、大きな
# heredoc (コミットメッセージ等) を含む Bash 呼び出しでは hook が rc=124 で殺され、
# **stdout 0 byte = deny が 1 byte も出ない** (= ソケット未指定の kill が素通りする) 状態を実測した。
# deny の対象は必ず kill 系トークンを含むので、含まないものはここで即通して重い経路に入れない
# (KILL_RE の全略形は `kill-s` 始まり / pkill・killall はそのまま)。
# ⚠️ この前置きフィルタは **遅延だけの fast path** で、判定は変えない (下のセグメント単位の
# 同じ絞りが正しさの側を担う)。したがって「これを外す」変異ではテストが red にならない
# (実測 2026-08-21)。テストは判定を pin するもので時間は pin しないため、ここは意図的に
# 未 pin にしてある — 消しても判定は同じだが、sed とセグメント走査の fork を丸ごと省ける。
case "$cmd" in
  *kill-s* | *pkill* | *killall*) ;;
  *) exit 0 ;;
esac

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
# unquote (1 文字ループ = 実質 O(n²)) を回す上限。これを超えるセグメントは引用符処理を諦めて
# 生のまま走査する (判定は deny 側へ倒す)。8KB は「実測で 100ms 未満に収まる」ことから決めた。
MAX_UNQUOTE_BYTES=8192
# 静的検査そのものを諦めて deny に倒す上限。timeout に殺されて素通りするより前に判断する。
MAX_SCAN_BYTES=131072

# ⚠️ ここから先は入力長にコストが乗る。極端に大きい入力は timeout (settings.json: 10) に
# 殺されて **無出力 = 素通り**になるため、判定を諦める代わりに **deny 側へ倒す** (fail-closed)。
# 実測 2026-08-21: 360KB で rc=124 / stdout 0 byte。90KB は下の MAX_UNQUOTE_BYTES 経路で 1.4s。
# 偽陽性 (正当な隔離コマンドが巨大な入力に含まれる) は理屈上ありうるが、そのときは
# コマンドを分割すれば通る。「検査できなかったので素通り」だけは選ばない。
if [ "${#cmd}" -gt "$MAX_SCAN_BYTES" ]; then
  deny "入力が大きすぎて (${#cmd} byte > $MAX_SCAN_BYTES) 静的検査を完了できない。kill 系トークンを含むため保守的に拒否した。コマンドを分割するか、破壊的操作を別呼び出しに分けること"
fi

# 行継続 (`\` + 改行) を空白に畳む。畳まないと `tmux \`+改行+`kill-server` が別セグメントに
# 割れて tmux トークンを見失う (レビューで実証されたバイパス)。
# ⚠️ ここを sed でやらないこと: 引用符と括弧を含む正規表現を sed -E に渡すと BSD sed が
#    "parentheses not balanced" で落ち、フック全体が静かに無力化する (実測 2026-07-30)。
#    bash の文字列操作なら外部コマンドの方言差が入らない。
normalized="${cmd//\\$'\n'/ }"

# ---- 検出の中核: トークン走査で「ソケット明示のない kill」を見つける -------------------
# 文字列の窓 (前方/後方の切り出し) で免除区間を決める方式は閉じない。2026-08-20 の red team が
# 実証した破れ方 (issue 069):
#   - kill トークンより前に別の kill があると窓が手前で切れ、無関係な -L が免除に化ける
#     (`tmux -L probe kill-server; tmux new-session -d \; kill-server` が素通り)
#   - `resize-pane -L` / `capture-pane -S` のような「ソケットでない -L/-S」が免除に数えられる
#   - `-S <末尾が tmux のパス>` や `-L tmux` で窓の起点がずれ、正当な形を deny する
#   - `tmux<TAB>subcommand` / `man tmux` 記載の `';'` 形が窓の前提から外れる
# そこでトークン列を走査する。規則:
#   - tmux 実行ファイルトークンで「グローバルオプション区間」に入る
#   - 区間内の -L/-S だけを免除として数える (値は 1 トークン消費する。値が tmux でも起点にしない)
#   - 最初のサブコマンド (非オプション語) で区間を出る = 以降の -L/-S は免除に数えない
#   - `\;` / `;` は同じ tmux クライアントのコマンド区切りなので、先行する免除を引き継ぐ
SEP_TOKEN=$'\x01'

# 引用符領域の処理。tmux へ渡る「文字列引数」の中身で判定が揺れないよう原則 Q に潰すが、
# 中に kill トークンを含むもの (bash -c '...') と実行ファイル名そのものを囲んだもの
# ('tmux' の形) は剥がして検査対象に残す。
unquote() {
  local s="$1" out="" ch q="" buf="" i len
  len=${#s}
  for ((i = 0; i < len; i++)); do
    ch="${s:i:1}"
    if [ -z "$q" ]; then
      case "$ch" in
        \'|\") q="$ch"; buf="" ;;
        *) out+="$ch" ;;
      esac
    elif [ "$ch" = "$q" ]; then
      # 剥がす (= 検査対象に残す) のは 2 形だけ:
      #   1. シェルの -c の直後 = 中身がコマンドとして実行される (bash -c '...' 経由の迂回)
      #   2. 実行ファイル名そのものを囲んだ形 ('tmux' のように単語全体が tmux)
      # それ以外は Q に潰す。潰さないと tmux へ渡る文字列引数 (send-keys / run-shell の
      # 中身) や、単に文字列として tmux コマンドを含む別コマンド (git log --grep '...')
      # まで検査対象になり、正当な操作を止めてしまう。
      if [[ "$out" =~ (^|[[:space:]])(sh|bash|zsh|dash|ksh|env)([[:space:]]+[^[:space:]]+)*[[:space:]]+-[^[:space:]]*c[[:space:]]*$ ]] \
         && [[ "$buf" =~ $KILL_RE ]]; then
        out+=" $buf "
      elif [[ "$buf" =~ ^([^[:space:]]*/)?tmux$ ]]; then
        out+=" $buf "
      else
        out+=" Q "
      fi
      q=""
    else
      buf+="$ch"
    fi
  done
  [ -n "$q" ] && out+=" $buf "   # 閉じていない引用符は中身を残す (安全側)
  printf '%s' "$out"
}

# セグメント (連結演算子で割った 1 コマンド) を走査。ソケット未指定の kill があれば 1 を返す。
scan_segment() {
  local seg="$1" t i n sock=0 in_tmux=0 in_global=0 skip_val=0
  local -a toks=()
  read -ra toks <<< "$seg"
  n=${#toks[@]}
  for ((i = 0; i < n; i++)); do
    t="${toks[i]}"
    if [ "$skip_val" = 1 ]; then skip_val=0; continue; fi
    # 先頭のバックスラッシュ (alias 回避の `\tmux`) を剥がしてから実行ファイルを照合する
    bare="$t"
    while [ "${bare#\\}" != "$bare" ]; do bare="${bare#\\}"; done
    case "$bare" in
      tmux | */tmux) in_tmux=1; in_global=1; sock=0; continue ;;
    esac
    [ "$in_tmux" = 1 ] || continue
    case "$t" in
      -L | -S) [ "$in_global" = 1 ] && sock=1; skip_val=1 ;;
      # 値を取る他のグローバルオプション (-c/-f/-T) も値を 1 トークン消費する。消費しないと
      # 値がサブコマンドと誤認されてグローバル区間が閉じ、後続の -L が免除に数えられない。
      # `tmux -f /dev/null -L probe <kill>` = 規範 md が推奨する隔離の形を deny していた
      # (2026-08-21 に別セッションの red team が実証)。免除に数えるのは -L/-S だけ。
      -c | -f | -T) skip_val=1 ;;
      -L* | -S*) [ "$in_global" = 1 ] && sock=1 ;;
      "$SEP_TOKEN" | ';') in_global=0 ;;
      -*) : ;;
      *)
        if [[ "$t" =~ ^${KILL_RE}$ ]] && [ "$sock" != 1 ]; then
          return 1
        fi
        in_global=0
        ;;
    esac
  done
  return 0
}

# tmux のコマンド区切り `\;` はセグメント分割で失わないようトークン化する
# (`;` で割ると tmux トークンを見失い、kill が孤立して検出できない)。
normalized="${normalized//\;/ $SEP_TOKEN }"

# コマンドを連結演算子 (; && || | 改行) でセグメントに割り、セグメント単位で判定する。
segments=$(printf '%s' "$normalized" | sed -E 's/&&|\|\||[;|]/\n/g')

while IFS= read -r seg; do
  # 行内コメント (" #" 以降) を落とす。落とさないと `# -L probe の後片付け` のような
  # コメントが -L 免除を誤って与える (レビューで実証)。行頭コメント行も丸ごと捨てる。
  seg="${seg%% #*}"
  case "${seg#"${seg%%[![:space:]]*}"}" in '#'*) continue ;; esac

  # ⚠️ 重い解析はトークンを含むセグメントだけに入れる。unquote は 1 文字ループ + $(...) の
  # fork なので、セグメント数に比例してコストが乗る。この絞りが無いと 2000 セグメントの入力で
  # 37 秒かかり、settings.json の timeout: 10 に殺されて **deny が 1 byte も出ない**
  # (= ゲートが黙って消える。rc=124 を実測 2026-08-21)。
  # unquote は引用符を外す/Q に潰すだけで `kill-s` を新たに作らないので、生セグメントでの
  # 前方絞りは判定を変えない (bash -c '...' の中身も生文字列として一致する)。
  case "$seg" in
    *kill-s* | *pkill* | *killall*) ;;
    *) continue ;;
  esac

  # pkill / killall で tmux を狙う形は全サーバ無差別なので常に deny
  if [[ "$seg" =~ (^|[[:space:]\;\&\|])(pkill|killall)[[:space:]] ]] \
     && [[ "$seg" =~ [[:space:]][\"\']?tmux[\"\']?([[:space:]]|$) ]]; then
    deny "$REASON_PKILL"
  fi

  # ⚠️ 大入力では unquote を諦めて生セグメントを走査する。unquote は 1 文字ループで、bash の
  # 部分文字列展開 (${s:i:1}) が O(n) なので実質 O(n²) — 90KB で 37 秒かかり、
  # settings.json の timeout: 10 に殺されて **deny が 1 byte も出ない** (rc=124 を実測 2026-08-21)。
  # 諦め方は deny 側へ倒す: 引用符の中の kill も検査対象になるので偽陽性 deny は増えるが、
  # 「検査できなかったので素通り」より安全 (adversarial-review-own-safeguards.md の
  # 「検査できなかったときに緑を返さない」)。偽陽性は言い換えで回避できる (規範 md の強制手段節)。
  if [ "${#seg}" -gt "$MAX_UNQUOTE_BYTES" ]; then
    scan_segment "$seg" || deny "$REASON_KILL"
  else
    scan_segment "$(unquote "$seg")" || deny "$REASON_KILL"
  fi
done <<EOF
$segments
EOF

exit 0
