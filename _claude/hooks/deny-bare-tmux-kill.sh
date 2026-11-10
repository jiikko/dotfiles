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
# 検出の設計:
#   - コマンド文字列を shell の字句の規則 (引用符・`\`・行継続・コメント・連結演算子・`(` `)` `$(` `` ` ``) で
#     1 パスで語の列に割る (lex_segments の awk)。引用符は剥がして語に繋ぐので、`'kill-server'`・`t\mux`・
#     `kill-se\rver` も語としては `kill-server` / `tmux` になる。引用符の中の `;` `|` `&&` 改行 ` #` は区切りでもコメントでもない
#   - 中身がコマンドとして実行される文字列 (`sh|bash|zsh|dash|ksh … -c <文字列>`・`eval …`・二重引用符の中の `$(` / `` ` ``) は、
#     中身を別のコマンドとして同じ規則で割り直す。`$'..'` は `\x`・8 進・`\n`・`\t` を解いてから語にする
#   - heredoc の本文は、本文全体を 1 つのコマンド列として割り直す (行継続を切らない)。本文が実行されるか (シェルへ渡すか) は見分けない:
#     パイプの先・`. /dev/stdin`・プロセス置換・fd の経由など、渡し方が多すぎて静的には閉じない (見分けると
#     十数形が素通りした)。本文の散文に小文字の tmux と kill の語が並ぶと偽陽性になる (回避はファイル経由か言い換え = 規範 md の「強制手段」節)
#   - 実行ファイルはフルパスも拾う。subcommand は tmux の前方一致受理に合わせる (`tmux kill-serve` も実際にサーバを落とす)
#   - `-L`/`-S` の免除は **tmux トークンとサブコマンドの間** (グローバルオプションの区間) にあるものだけ
#
# 原理的な限界 (静的検査では閉じない。規範 md の「隔離を実証してから打つ」と bin/tmux の shim に委ねる):
#   変数間接 (`T=tmux; $T kill-server` / `K=kill-server; tmux $K` / `${x:-tmux} kill-server`)、スクリプト間接
#   (`bash ./tmp/cleanup.sh` の中身)、`ssh host 'tmux kill-server'` (中身は手元の本番ではなく相手のマシンで走るので割り直さない)、
#   別の実行系に渡す文字列 (`trap '..' EXIT`・`python3 -c`・`osascript -e`・`csh -c`・`xargs tmux`)、brace 展開 (`{tmux,kill-server}`)・zsh の `=tmux`、
#   入れ子の引用符の追い方の差 (`"$(echo "x"; tmux kill-server)"`・バッククォートの中の ` #`)、`${..}` の中の ` #` (コメントとして捨てる)、
#   tmux を含まない pkill の照合 (`pkill tmu`)・kill の語を使わない停止 (`kill $(pgrep tmux)`)、tmux 自身が別のシェルで走らせる文字列
#   (`tmux -c '..'`・`run-shell`・`display-popup -E`・`new-window '..'`・`send-keys`)、tcsh / fish など一覧に無いシェル。
# 分かっていて受ける偽陽性: `"\tmux" kill-server` (shell では `\tmux` という名前のコマンドで実行されないが、先頭の `\` を剥がして tmux と読む。
#   引用符を外した `\tmux` は alias を避けた本物の tmux なので、剥がす側に倒す)、tmux の引数の文字列そのものが kill の語の形 (`display -p kill-server`)、
#   引用符で囲んだ tmux の語の後ろに kill の語 (`rg -e 'tmux' -e 'kill-server'`)、pkill / killall の後ろの tmux を含む語 (`pkill -f tmuxinator`。
#   pkill は部分一致で照合するので、含めば止める)、-L / -S の値の glob (`-S /tmp/tmux-501/d*`。本番の socket に展開されうるので止める)。
#
# 入力: PreToolUse の hook JSON (stdin)。.tool_input.command を検査する。
# 出力: 違反時のみ permissionDecision=deny の JSON。それ以外は無出力 exit 0。
set -uo pipefail
# 長さ (${#cmd}) をバイトで数え、awk もバイトで走らせる (区切りの字はすべて ASCII なので、マルチバイトの字を割っても判定は変わらない)
export LC_ALL=C

# shellcheck source=_claude/hooks/lib/hook_io.sh
. "$(dirname "$0")/lib/hook_io.sh" 2>/dev/null || exit 0
# jq が無い・読めない環境では静かに諦める (誤 deny させない)
hook_read_bash_command || exit 0
cmd=$HOOK_CMD
[ -n "$cmd" ] || exit 0

# 🚨 性能ガード: kill 系の語を含まない入力は字句解析に入れずに通す (体感の遅延に乗せない)。
# 判定は変えない: `\` と引用符を除いた形で探すので、`kill\-server` / `'kill'-server` のように語の途中を崩した形も通さない
# (字句解析は `\` と引用符を剥がして語を繋ぐだけで、kill の語を新たに作らない)。
# `$'..'` は `\x2d` などで語を隠せるので、含むときは絞り込まずに字句解析へ回す。
stripped=${cmd//[\\\'\"]/}
case "$stripped" in
  *kill-s* | *pkill* | *killall*) ;;
  *) case "$cmd" in *"\$'"*) ;; *) exit 0 ;; esac ;;
esac

REASON_KILL="ソケット指定の無い tmux kill-server/kill-session は本番サーバを直撃します (\$TMUX が TMUX_TMPDIR に優先。2026-07-30 に 29 セッション誤殺の実害)。unset TMUX + ユニークな -L <name> (または -S <path>) を tmux の引数として付け、先に tmux -L <name> ls で隔離を実証してから実行してください。規範: _claude/rules/tmux-probe-requires-socket-isolation.md"
REASON_PKILL="pkill/killall で tmux を狙うのは全サーバ無差別 kill です (本番サーバも死にます)。対象サーバの pid を特定して kill するか、tmux -L <name> kill-server を使ってください。規範: _claude/rules/tmux-probe-requires-socket-isolation.md"
# 🚨 -L default / -S <...>/default はソケットを明示していても**本番サーバ**を直撃する
# (2026-09-11 に別セッションが -L default kill-server で 30 セッション誤殺)。ソケット明示の
# 免除から default だけを外す。恒久防御は bin/tmux shim (script 内部の kill も傍受) で、
# こちらは Claude が**直接打った** default 直撃を Bash ツール境界で止める二層目。
REASON_PROTECTED="tmux の default サーバ (本番。あなたが今 attach しているセッション群) への kill を拒否しました。default は 2026-07-30 / 2026-09-11 に 2 度 Claude 起因で誤殺されています。隔離した使い捨てサーバを消したいなら -L <ユニーク名> を使ってください。本当に本番を消す必要があるなら、その旨をユーザーに確認してください。規範: _claude/rules/tmux-probe-requires-socket-isolation.md"

# tmux が受理する kill-server / kill-session の前方一致形 (曖昧でない範囲)。
# `kill-s` / `kill-se` は両者で曖昧なため tmux 自身が拒否する = 検出不要。
KILL_RE='kill-s(er(v(er?)?)?|es(s(i(on?)?)?)?)'
# 静的検査そのものを諦めて deny に倒す上限 (バイト)。timeout (settings.json: 10 秒) に殺されると無出力 = 素通りになるので、その前に判断する。
# 字句解析は入力長に比例する (awk の 1 パス)。128KB の 1 語で 0.6 秒 (2026-11-10 実測)。
# 偽陽性 (正当な隔離コマンドが巨大な入力に含まれる) は理屈上ありうるが、コマンドを分割すれば通る。「検査できなかったので素通り」だけは選ばない
MAX_SCAN_BYTES=131072
if [ "${#cmd}" -gt "$MAX_SCAN_BYTES" ]; then
  hook_deny "入力が大きすぎて (${#cmd} byte > $MAX_SCAN_BYTES) 静的検査を完了できない。kill 系トークンを含むため保守的に拒否した。コマンドを分割するか、破壊的操作を別呼び出しに分けること"
fi

# 字句解析。1 コマンド (連結演算子・括弧で割った単位) を 1 行に、語を US (0x1f) で区切って出す。
# 割り直しの数か量 (バイト) が上限を越えたら US + OVERFLOW の行を出す (呼ぶ側で deny に倒す)。量も数える: `eval eval … <長い文>` は
# 割り直すたびにほぼ全文を積むので、数の上限 (200) だけでは仕事量が 200 × 入力長になり、32KB の入力で 10 秒を越えた (2026-11-10)。
# 🚨 bash の 1 文字ループで書かない: `${s:i:1}` は毎回 O(n) で、和文 8000 字 × 8 行の入力で 10 秒の timeout を越えて
# 無出力 = 素通りになった (2026-11-10 実測)。sed で連結演算子を割る形にも戻さない: 引用符の中の `|` `&&` 改行でも割るので、
# 後半が `#` で始まると行頭コメントとして kill ごと捨てていた。
read -r -d '' LEX_AWK <<'AWK'
BEGIN { US = "\037"; MAXQ = 200; MAXQBYTES = 524288; qbytes = 0 }
{ buf = (NR > 1 ? buf "\n" : "") $0 }
END {
  qn = 0; q[qn++] = buf
  for (qi = 0; qi < qn; qi++) {
    if (qi >= MAXQ || qbytes > MAXQBYTES) { print US "OVERFLOW"; exit }
    lex(q[qi], qnohd[qi])
  }
}
# 中身がコマンドとして実行される文字列を、後で別のコマンドとして割り直す。kill 系の語を含まないものは割り直しても何も見つからないので積まない。
# 字を隠せる `$'..'` の escape (`\x`・`\u`・`\U`・8 進) を含む文字列は、解く前の字面では kill の語が見えない (`$'\x6b'ill-server`) ので絞らずに積む
# (`$'\t'` のような escape まで絞らずに積むと、kill の語の無い入力が割り直しの数の上限を越えて deny になった)
function queue(t, nohd,   tt) {
  tt = t; gsub(/[\\'"]/, "", tt)
  if (tt ~ /kill-s|pkill|killall/ || t ~ /\$'[^']*\\[xuU0-7]/) { qnohd[qn] = nohd; q[qn++] = t; qbytes += length(t) }
}
function addc(x) { w = w x; inw = 1 }
function endword() {
  if (inw) {
    segw[++segn] = w
    if (hdpend) { hdd[hdn] = w; hdtab[hdn] = hdtabnext; hdn++; hdpend = 0 }
  }
  w = ""; inw = 0
}
function endseg(   k, j, b, t, out, x, noc) {
  endword()
  if (segn == 0) return
  noc = 0
  for (k = 1; k <= segn; k++) {
    # basename は match で取る。sub(/.*\//) にしない: `/` の無い長い語では開始位置ごとに .* が末尾まで走り、語の長さの 2 乗かかる
    # (24KB の語で 1.1 秒)。長い語を飛ばす形にもしない (`/` を 300 個並べた後の bin/bash を見落とした)
    b = segw[k]; if (match(b, /[^\/]*$/)) b = substr(b, RSTART, RLENGTH)
    if (b ~ /^(sh|bash|zsh|dash|ksh)$/ && !noc) {
      # -c の後のオプション (`--`・`-x`) を飛ばした最初の語が中身。見つけたら探し終えた位置から続け、見つからなければ後ろのシェルの語では
      # 探し直さない (シェルの語ごとに末尾まで探し直すと、`sh sh sh …` で語数の 2 乗になり 10 秒を越えた)。後ろの eval は見続ける
      for (j = k + 1; j <= segn; j++) if (segw[j] ~ /^-[A-Za-z]*c[A-Za-z]*$/) break
      if (j > segn) { noc = 1; continue }
      for (j++; j <= segn && segw[j] ~ /^-/; j++) ;
      if (j <= segn) queue(segw[j])
      k = j
    } else if (b == "eval") {
      t = ""; for (j = k + 1; j <= segn; j++) t = t " " segw[j]
      queue(t); break
    }
  }
  out = ""
  for (k = 1; k <= segn; k++) { x = segw[k]; gsub(/[\n\037]/, " ", x); out = out (k > 1 ? US : "") x }
  print out
  segn = 0
}
# $'..' の \x / 8 進を 1 字に解く (C の locale でバイトとして)
function hexval(h,   i, v) { v = 0; for (i = 1; i <= length(h); i++) v = v * 16 + index("0123456789abcdef", tolower(substr(h, i, 1))) - 1; return v }
# mode: 0 = 引用符の外 / 1 = '..' / 2 = ".." / 3 = $'..'
# nohd = 1 は `<<` を heredoc として読まない。`<<` が heredoc か (算術の `$((1<<x))` か) は静的に決まらないので、`<<` を見た文字列は
# 両方の読み方で割る (heredoc と読むと、区切りの語の行を捨て、引用符の状態が bash とずれて後ろの kill を引用符の中と読んだ)
# `${..}` を特別に扱わない: 中の引用符・`$(`・`\{` を bash と同じに追えず、`${x:-$(tmux kill-server)}` を 1 語にして素通りさせた。
# 普通の字として読めば `(` `)` で割れる
function lex(s, nohd,   L, nl, li, line, C, n, ci, c, nx, e, mode, cont, cmtok, dqc, dqsub, k, t, body, j, v, sawhd) {
  w = ""; inw = 0; segn = 0; hdn = 0; hdpend = 0; sawhd = 0
  mode = 0; cont = 0; cmtok = 1; dqc = ""; dqsub = 0
  nl = split(s, L, "\n")
  for (li = 1; li <= nl; li++) {
    line = L[li]; n = split(line, C, "")
    for (ci = 1; ci <= n; ci++) {
      c = C[ci]
      if (mode == 1) { if (c == "'") mode = 0; else addc(c); continue }
      if (mode == 3) {
        if (c == "\\" && ci < n) {
          ci++; e = C[ci]
          if (e == "x" && C[ci + 1] ~ /[0-9A-Fa-f]/) {
            t = C[++ci]; if (C[ci + 1] ~ /[0-9A-Fa-f]/) t = t C[++ci]
            v = hexval(t); if (v > 0) addc(sprintf("%c", v))
          } else if ((e == "u" || e == "U") && C[ci + 1] ~ /[0-9A-Fa-f]/) {
            t = ""; for (j = 0; j < (e == "u" ? 4 : 8) && C[ci + 1] ~ /[0-9A-Fa-f]/; j++) t = t C[++ci]
            v = hexval(t); if (v > 0 && v < 128) addc(sprintf("%c", v)); else if (v >= 128) addc("?")
          } else if (e ~ /[0-7]/) {
            v = e + 0; for (j = 0; j < 2 && C[ci + 1] ~ /[0-7]/; j++) v = v * 8 + C[++ci]
            if (v > 0) addc(sprintf("%c", v))
          } else addc(e == "n" ? "\n" : (e == "t" ? "\t" : e))
        }
        else if (c == "'") mode = 0
        else addc(c)
        continue
      }
      if (mode == 2) {
        if (c == "\\") {
          if (ci == n) { cont = 1; continue }
          nx = C[ci + 1]
          if (nx == "$" || nx == "`" || nx == "\"" || nx == "\\") { ci++; addc(nx); dqc = dqc nx }
          else { addc(c); dqc = dqc c }
        } else if (c == "\"") {
          mode = 0; if (dqsub) queue(dqc)
        } else {
          if (c == "`" || (c == "$" && C[ci + 1] == "(")) dqsub = 1
          addc(c); dqc = dqc c
        }
        continue
      }
      # 引用符の外。コメントは「空白 1 文字・行頭・区切りの直後 + #」だけ (タブ + # は落とさない: 落とすと引用符の追い方が bash と違う形で
      # 後ろの kill まで捨てる。落とさない側の誤りは deny に倒れる)
      if (c == "#" && !inw && cmtok) break
      if (c == " " || c == "\t") { endword(); cmtok = (c == " "); continue }
      cmtok = 0
      if (c == "\\") { if (ci == n) cont = 1; else { ci++; addc(C[ci]) }; continue }
      if (c == "'") { mode = 1; inw = 1; continue }
      if (c == "\"") { mode = 2; inw = 1; dqc = ""; dqsub = 0; continue }
      if (c == "$" && C[ci + 1] == "'") { mode = 3; inw = 1; ci++; continue }
      if (c == "$" && C[ci + 1] == "\"") { ci++; mode = 2; inw = 1; dqc = ""; dqsub = 0; continue }   # $"..": 訳の付く二重引用符
      if (c == "`" || c == "(" || c == ")" || c == ";" || c == "|") { endseg(); cmtok = 1; continue }
      if (c == "&") { if (inw && w ~ /[<>]$/) addc(c); else { endseg(); cmtok = 1 }; continue }
      # heredoc の `<<` (here-string の `<<<` の中の `<<` を数えない)
      if (!nohd && c == "<" && C[ci + 1] == "<" && C[ci + 2] != "<" && (ci == 1 || C[ci - 1] != "<")) {
        endword(); hdpend = 1; sawhd = 1; ci++
        hdtabnext = (C[ci + 1] == "-"); if (hdtabnext) ci++
        continue
      }
      addc(c)
    }
    # 行の終わり
    if (mode == 1 || mode == 3) { addc("\n"); continue }
    if (mode == 2) { if (cont) cont = 0; else { addc("\n"); dqc = dqc "\n" }; continue }
    if (cont) { cont = 0; continue }
    endseg(); cmtok = 1
    # heredoc の本文: 本文全体を 1 つのコマンド列として割り直す (行ごとに割ると行継続が切れ、`tmux \` + 改行 + `kill-server` を見落とした)。
    # 区切りが見つからない `<<` (算術の `$((1<<2))` など) は、後ろの全部を本文として割り直すので見落とさない。区切りの語の行が後ろにあっても、
    # 間の行を本文として割り直すので捨てない
    for (k = 0; k < hdn; k++) {
      body = ""
      while (++li <= nl) {
        t = L[li]; if (hdtab[k]) sub(/^\t+/, "", t)
        if (t == hdd[k]) break
        body = body L[li] "\n"
      }
      queue(body)
    }
    hdn = 0; hdpend = 0
  }
  # 閉じていない引用符: 中身を割り直す (安全側)
  if (mode == 2) queue(dqc); else if (mode != 0) queue(w)
  endseg()
  if (sawhd) queue(s, 1)
}
AWK

# 1 コマンドの語の列 (引数) を走査する。ソケット未指定の kill があれば 1、ソケット明示でも default 直撃なら 2 を返す。
# 規則:
#   - tmux 実行ファイルの語で「グローバルオプション区間」に入る
#   - 区間内の -L/-S だけを免除として数える (値は 1 語消費する。値が tmux でも起点にしない)
#   - 最初のサブコマンド (非オプション語) で区間を出る = 以降の -L/-S は免除に数えない (`resize-pane -L` / `capture-pane -S`)
#   - 語 `;` (`\;` / `';'`) は同じ tmux クライアントのコマンド区切りなので、先行する免除を引き継ぐ
# default の照合だけ大小を区別しない (nocasematch をその照合の間だけ立てる。全体に立てると `-l` が `-L` に当たる)。
# $(printf | tr) で小文字化しない: 値ごとに fork し、-L を 4000 個並べた入力で 10 秒を越えた
scan_tokens() {
  local t bare sock=0 in_tmux=0 in_global=0 skip_val=0 prot=0 pending=0
  for t in "$@"; do
    if [ "$skip_val" = 1 ]; then
      skip_val=0
      # -L/-S の値: basename が default なら本番直撃 (prot=1)。
      if [ "$pending" = 1 ]; then
        pending=0
        # 🚨 macOS の FS は case-insensitive なので DEFAULT/Default も本番に届く (nocasematch で照合する)。
        # glob を含む値 (`/tmp/tmux-501/d*`) は本番の socket に展開されうるので、本番として扱う
        shopt -s nocasematch
        case "$t" in default | */default | *[*?[]*) [ "$in_global" = 1 ] && prot=1 ;; esac
        shopt -u nocasematch
      fi
      continue
    fi
    # 先頭のバックスラッシュ (alias 回避の `\tmux`。字句解析で 1 つ剥がれた後に残るもの) を剥がしてから実行ファイルを照合する
    bare="$t"
    while [ "${bare#\\}" != "$bare" ]; do bare="${bare#\\}"; done
    case "$bare" in
      tmux | */tmux) in_tmux=1; in_global=1; sock=0; prot=0; continue ;;
    esac
    [ "$in_tmux" = 1 ] || continue
    case "$t" in
      -L | -S) [ "$in_global" = 1 ] && sock=1; skip_val=1; pending=1 ;;
      # 値を取る他のグローバルオプション (-c/-f/-T) も値を 1 語消費する。消費しないと
      # 値がサブコマンドと誤認されてグローバル区間が閉じ、後続の -L が免除に数えられない
      # (`tmux -f /dev/null -L probe <kill>` = 規範 md が推奨する隔離の形を deny していた)。免除に数えるのは -L/-S だけ。
      -c | -f | -T) skip_val=1 ;;
      -L* | -S*)
        [ "$in_global" = 1 ] && sock=1
        # 付属値形 (-Ldefault / -S/path/default) の basename が default なら本番直撃。
        shopt -s nocasematch
        case "${t#-?}" in default | */default | *[*?[]*) [ "$in_global" = 1 ] && prot=1 ;; esac
        shopt -u nocasematch ;;
      # グローバルの区間のまとめたフラグ (`-uL default`・`-uLdefault`・`-uf /dev/null`)。tmux は getopt で読むので、字を前から読み、
      # L / S ならその後ろ (無ければ次の語) が socket、c / f / T ならその後ろ (無ければ次の語) が値で、どちらもそこで終わる
      -[!-]?*)
        if [ "$in_global" = 1 ] && [ "${#t}" -le 64 ]; then
          rest="${t#-}"
          while [ -n "$rest" ]; do
            ch="${rest:0:1}"; rest="${rest:1}"
            case "$ch" in
              L | S)
                sock=1
                if [ -z "$rest" ]; then skip_val=1; pending=1
                else
                  shopt -s nocasematch
                  case "$rest" in default | */default | *[*?[]*) prot=1 ;; esac
                  shopt -u nocasematch
                fi
                break ;;
              c | f | T) [ -z "$rest" ] && skip_val=1; break ;;
            esac
          done
        fi ;;
      ';') in_global=0 ;;
      -*) : ;;
      *)
        if [[ "$t" =~ ^${KILL_RE}$ ]]; then
          # ソケット未指定 → 本番直撃 (rc 1)。ソケット明示でも default 直撃 → rc 2。
          [ "$sock" != 1 ] && return 1
          [ "$prot" = 1 ] && return 2
        fi
        in_global=0
        ;;
    esac
  done
  return 0
}

segments=$(printf '%s' "$cmd" | awk "$LEX_AWK") || hook_deny "コマンドを字句解析できなかった (awk が失敗)。kill 系トークンを含むため保守的に拒否した"

US=$'\037'
while IFS= read -r seg; do
  [ "$seg" = "${US}OVERFLOW" ] && hook_deny "コマンドの中に割り直す文字列 (bash -c / eval / \$(..) / heredoc) が多すぎて静的検査を完了できない。kill 系の語を含む (か、\$'..' で隠しうる) ため保守的に拒否した"
  # 重い走査 (語ごとの case / fork) は kill 系の語を含む行だけに入れる
  case "$seg" in
    *kill-s* | *pkill* | *killall*) ;;
    *) continue ;;
  esac
  toks=()
  IFS="$US" read -r -a toks <<< "$seg"

  # pkill / killall で tmux を狙う形は全サーバ無差別なので常に deny
  pk=0
  for t in "${toks[@]}"; do
    # `${t##*/}` で basename を取らない: bash の最長一致の除去は語の長さの 2 乗かかる (24KB の語 1 つで 2〜4 秒)
    case "$t" in pkill | killall | */pkill | */killall) pk=1 ;; esac
    # pkill / killall は名前を部分一致・正規表現で照合する (`pkill -f /opt/homebrew/bin/tmux`)。tmux を含む語なら止める
    case "$t" in *tmux*) [ "$pk" = 1 ] && hook_deny "$REASON_PKILL" ;; esac
  done

  scan_tokens "${toks[@]}"
  case "$?" in
    1) hook_deny "$REASON_KILL" ;;       # ソケット未指定の kill
    2) hook_deny "$REASON_PROTECTED" ;;  # ソケット明示でも default (本番) 直撃
  esac
done <<EOF
$segments
EOF

exit 0
