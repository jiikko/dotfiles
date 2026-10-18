#!/usr/bin/env bash
#
# PreToolUse(Bash) フック: rc をパイプに隠したまま、その成否で後段を走らせる Bash を deny する。2 つの形を見る:
#   1. push の結果をパイプに通したまま、worktree / branch を消す
#   2. 検査 (make / go test / lint 等) の出力をパイプに通したまま、`&&` で git commit / push へ繋ぐ
#      (`make lint 2>&1 | tail -1 && … && git commit` は lint が赤くても commit が走る)
# (ファイル名は 1 の形だけだったころのまま。settings.json の command が実体パスを指すので、改名すると古い版で起動した session が
#  消えたパスを指す)
#
# なぜ: `git push … | tail -1 && … && git worktree remove …` は、push の rc がパイプの終端 (tail) の rc にすり替わるので、
# push が non-fast-forward で弾かれても後段が走り、**未 push の commit を持つ worktree を消す** (実際に 2 回踏んだ。
# どちらも git の object から拾い直した)。規範は既にある
# (_claude/rules/verify-execution-not-just-exit-code.md の「成否で後段を走らせる `&&` のつなぎ」/ .claude/rules/worktree-per-session.md)
# が、長いワンライナーの中では守れなかったので、harness が実行する本フックで機械的に止める。
#
# ## 脅威モデル (adversarial-review-own-safeguards.md §8)
#
# 守るのは「Claude が **1 本の Bash** の中で、`git … push` の出力をパイプへ流し、その後ろで worktree / branch を消す」形だけ。
# heredoc の本文を落とした文字の上で、次の並びがあれば deny する:
#   1. `git [オプション…] push`
#   2. そこから (`;` `&` 改行を挟まずに) `|` / `|&` (push の rc がパイプに隠れる。`( … )` / `{ …; }` で囲んだ push の後ろのパイプも同じ)
#   3. その後ろのどこかに `git [オプション…] worktree [オプション…] remove|rm` か `git … branch … -D|-d|-df…|--delete`
#      (`&&` / `;` / 改行 / `||` のどれで繋いでいても。`;` は push の成否に関係なく走るので、なおさら危ない)
# ただし `set … -o pipefail` (`set -euo pipefail` 等) か `PIPESTATUS[` / `pipestatus[` があれば、push の rc を見ている形として通す。
#
# 🚨 シェルの構文を真似て解釈しない (引用符・コメント・グループの段を追わない)。解釈の層を足すたびに、その層を騙す迂回が出た
# (素通りになった例: 残した引用符の中の ` #` で後段ごと消える・case の `)` で段が閉じる・`\'` を引用符と読む 等)。
# §8 の「規則の軸が構文にある印」なので、字面の並びだけを見る粗い判定にした。
#
# ## 検出しない形 (review で見る責務の側。迂回の指摘がここに当たるなら採用せず記録する)
#   - 変数・関数・alias・eval・スクリプトの中身を経由した push / 削除 (`P="git push"; $P | tail` / `./tmp/cleanup.sh`)
#   - `rm -rf <worktree のパス>` での削除、`git worktree prune`、`git update-ref -d` (worktree / branch の消し方は他にもある)
#   - push と削除を別々の Bash 呼び出しに分けた形 (別の呼び出しでは rc を見る機会がある)
#   - `set -o pipefail` / `PIPESTATUS[` があれば通す (push より後ろで set する・別の rc を見る、の誤用は見分けない)
#   - `git` 以外の push (`hub push` 等)、引用符で書いた `"git"`、git のオプションの値に空白を含む形の一部
#   - 算術の `<<n` の後ろに、区切り語と同じ語だけの行がある形 (heredoc と読んで、間の行を落とす)
#   - 2 の形: 検査の語の一覧 (CHECK) に無いコマンド (自作のスクリプト・`bash tests/x.sh`・`bin/` の検査) / 前置きの一覧 (runtimeout・env・
#     time・nice) に無い前置き / 検査と commit を `;` で繋いだ形 (成否で止める意図が無い) / `if 検査 | tail; then git commit; fi` のように && を
#     使わずに成否で分ける形 / commit を変数・関数・別の呼び出しに分けた形
#
# ## 分かっていて受ける偽陽性 (字面で見る代わり)
#   - 引用符の中・コメントの中に同じ並びを書いた形 (`echo "git push | tail && git worktree remove"`)。2 の形も同じ (`-m "make x | tail && git commit"`)。
#   - 2 の形: 検査の語で始まるが検査でないコマンド (`go version | head -1 && git commit` / `make -n … | head` / `npm view … | head`)。rc を見ない
#     パイプは正しい形でも止める (直し方は rc を取るか pipefail を先に立てる、で同じ)commit message は heredoc で渡せば当たらない
#     (heredoc の本文は、終わりの区切りが見つかるときだけ落とす。見つからない `<<` (算術の `$((1<<n))` 等) は heredoc とみなさない)
#
# ## 判定できないときは拒否に倒す (fail-closed)
#   heredoc の本文を落とした後で 128KB を超える入力は、上限の時間 (settings.json の timeout 10 秒) に収まる保証が無いので deny する
#   (殺されると無出力 = 素通り)。heredoc の本文を落とす処理は awk で入力長に比例する時間にしてある。
#   速い経路 (push・パイプ・worktree / branch を全部含む) を通ったものだけが当たる
#
# 入力: PreToolUse の hook JSON (stdin)。.tool_input.command を検査する。
# 出力: 違反時のみ permissionDecision=deny の JSON。それ以外は無出力 exit 0。
set -uo pipefail

# shellcheck source=_claude/hooks/lib/hook_io.sh
. "$(dirname "$0")/lib/hook_io.sh" 2>/dev/null || exit 0
# jq が無い・読めない環境では諦める (誤 deny させない。fail-open。テストは jq 不在を失敗として扱う)
hook_read_bash_command || exit 0
cmd=$HOOK_CMD
[ -n "$cmd" ] || exit 0

# 速い経路: 止める形は必ずパイプと、1 なら push と worktree / branch、2 なら && と commit / push を含む。含まないものは解析に入れない (判定は変えない)
case "$cmd" in *'|'*) ;; *) exit 0 ;; esac
case "$cmd" in
  *push*worktree* | *push*branch* | *worktree*push* | *branch*push*) ;;
  *'&&'*commit* | *'&&'*push*) ;;
  *) exit 0 ;;
esac

REASON="git push の出力をパイプに通したまま、後ろで worktree / branch を消しています。パイプの rc は終端 (tail 等) のものなので、push が弾かれても削除が走り、未 push の commit を失います (retro 266 / 450 で 2 回実害)。push の rc を直接取ってから繋いでください: git push … > <log> 2>&1; rc=\$?; tail -2 <log>; [ \$rc -eq 0 ] && git worktree remove … (または set -o pipefail を先に)。引用符やコメントの中の文言で止まったなら、commit message 等は heredoc で渡してください。規範: _claude/rules/verify-execution-not-just-exit-code.md"

# 🚨 大きすぎる入力は、畳む・落とす前に粗く止める (/bin/bash 3.2 は下の置換が入力長の 2 乗で遅く、数 MB で timeout を越えて無出力 = 素通り。
# ${#} はロケールによって文字数だが、ここは粗い上限なので文字数でよい
MAX_RAW=1048576
if [ "${#cmd}" -gt "$MAX_RAW" ]; then
  hook_deny "入力が大きすぎて (${#cmd} > $MAX_RAW) 静的検査を上限の時間内に終えられる保証が無い。パイプと、その後ろで走る push / worktree・branch の削除 / commit を含むため保守的に拒否した。パイプを通す処理と後段を別の呼び出しに分けること"
fi

# 行継続 (`\` + 改行) を空白に畳む (sed を使わない: BSD sed の方言差でフックが静かに無力化する。deny-bare-tmux-kill.sh の注記)
cmd="${cmd//\\$'\n'/ }"

# heredoc の本文を落とす。`<<EOF` / `<<-EOF` / `<<'EOF'` / `<<"EOF"` の次の行から、区切り語だけの行 (<<- なら先頭のタブを許す) まで。
# 🚨 終わりの区切りが後ろに見つかるときだけ落とす (見つからない `<<` は heredoc でない: 算術の `$((1<<n))` を heredoc と読むと、
# 後ろの行を全部落として素通りになる)。`<<<` (here-string) は heredoc でない。
# 🚨 awk で入力長に比例する時間に収める (bash のループで区切りを探すと、開始の数 × 行数に比例し、/bin/bash 3.2 では行を積むだけで
# 10 秒の timeout を越えて無出力 = 素通りになった)。後ろから 1 回走って「その語が次に現れる行」を求め、
# 前から 1 回走って落とす
strip_heredocs() {
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

body=$(strip_heredocs "$cmd")
awk_rc=$?
# 🚨 awk が失敗すると body が空になり、何にも一致せず素通りになる。判定できないので拒否に倒す
if [ "$awk_rc" -ne 0 ]; then
  hook_deny "heredoc の本文を落とす処理 (awk) が失敗した (rc=$awk_rc)。判定できないので保守的に拒否した。push と削除を別の呼び出しに分けること"
fi

MAX_SCAN_BYTES=131072
body_bytes=$(LC_ALL=C; printf '%s' "${#body}") # 🚨 UTF-8 のロケールでは ${#} は文字数 (4 byte の文字で上限が 4 倍に膨らむ)。byte で数える
if [ "$body_bytes" -gt "$MAX_SCAN_BYTES" ]; then
  hook_deny "入力が大きすぎて (heredoc の本文を除いて ${body_bytes} byte > $MAX_SCAN_BYTES) 静的検査を上限の時間内に終えられる保証が無い。パイプと、その後ろで走る push / worktree・branch の削除 / commit を含むため保守的に拒否した。パイプを通す処理と後段を別の呼び出しに分けること"
fi

# push の rc を見ている形は通す (「検出しない形」: 誤用は見分けない)。pipefail は set の引数として書いた形だけ (ブランチ名の中の pipefail で通さない)
if [[ "$body" =~ (^|[^[:alnum:]_])set([[:space:]]+-[a-zA-Z]+)*[[:space:]]+-[a-zA-Z]*o[[:space:]]+pipefail ]] || [[ "$body" =~ (PIPESTATUS|pipestatus)\[ ]]; then
  exit 0
fi

# 「同じ単純コマンドの続き」を切らない書き方を畳む: 2>&1 / >&2 / &> のリダイレクトの & と、`{ …; }` の閉じの前の ; と、case の ;;
text="$body"
# 🚨 リダイレクトの & は数を並べて畳まない (`2>&3` / `>&-` / `>& log` / `<&0` が漏れる)。>& / <& / &> の形で畳む
text="${text//>&/> }"
text="${text//<&/< }"
text="${text//&>/ >}"
text="${text//;;/ }"
text="${text//; \}/ \}}"
text="${text//;\}/ \}}"

NL=$'\n'
# 🚨 語の区切りは [[:blank:]] (空白とタブ)。[[:space:]] は改行も含み、前の行の git から次の行の push までを 1 つのコマンドと読む (偽陽性になる)
GIT='(^|[^[:alnum:]_.-])git([[:blank:]]+[^[:space:]|;&]+)*[[:blank:]]+'
# push の直後は英数字以外 (空白・> 等) か、直接の | (`git push|tail`)。境界の 1 文字で | を食べない
PUSH="${GIT}push([^[:alnum:]_.|;&${NL}-][^|;&${NL}]*)?\\|([^|]|\$)"
# branch の削除のフラグ: まとめた短いフラグは git branch の実在の短いフラグの字 + d / D (-df / -vD / -dv 等。`--sort -committerdate` の o・e は
# 短いフラグに無いので削除と読まない)。長いオプションは一意な先頭で受け付けられる (--d 〜 --delete。branch で --d から始まる長いオプションは
# --delete だけ)
DESTROY="${GIT}(worktree([[:blank:]]+-[^[:space:]]+)*[[:blank:]]+(remove|rm)([^[:alnum:]_-]|\$)|branch([[:blank:]]+[^[:space:]|;&]+)*[[:blank:]]+(-[acCfilmMqrtuv]*[dD][acCdDfilmMqrtuv]*|--d(e(l(e(te?)?)?)?)?)([^[:alnum:]_-]|\$))"
if [[ "$text" =~ ${PUSH}.*${DESTROY} ]]; then
  hook_deny "$REASON"
fi

# 2. 検査の出力をパイプに通したまま && で commit / push。検査の語はパイプの左の単純コマンドの先頭の語で見る (git log / git diff --stat を
# パイプに通して commit するのは止めない)。終端の道具を tail / head / grep に限らない: 検査の後ろにパイプがあれば、終端が何でも rc は隠れる
# (限ると `| tee log | tail` / `|&` / `grep -E "a|b"` の引用符の中の | で素通りした)。検査と && の間に ; / 改行 / & 1 つを挟んだ形は見ない
# (成否で止める意図が無い・背景に回した)。前置き (runtimeout <秒> / env / time / nice) と絶対パス・相対パスの検査も見る
CHECK='(make|go|golangci-lint|shellcheck|swift|xcodebuild|npm|pnpm|yarn|npx|pytest|cargo|bats|ruff|mypy|eslint|tsc)'
PREFIX='(([^[:space:]|;&]*/)?runtimeout[[:blank:]]+[0-9.]+[[:blank:]]+|env([[:blank:]]+-[^[:space:]]+)*([[:blank:]]+[A-Za-z_][A-Za-z0-9_]*=[^[:space:]]*)*[[:blank:]]+|time[[:blank:]]+|nice([[:blank:]]+-n[[:blank:]]*[0-9]+)?[[:blank:]]+|[A-Za-z_][A-Za-z0-9_]*=[^[:space:]]*[[:blank:]]+)*'
PIPED_CHECK="(^|[;&|(\\{${NL}])[[:blank:]]*${PREFIX}([^[:space:]|;&]*/)?${CHECK}([[:blank:]][^|;&${NL}]*)?\\|(&|[^;&${NL}])*&&"
# commit / push は git の副コマンドの位置だけで見る (間に置けるのは - で始まるオプションと -C / -c の値。`git stash push` / `git log -- commit` を止めない)
COMMIT="(^|[^[:alnum:]_.-])git([[:blank:]]+-[Cc][[:blank:]]+[^[:space:]]+|[[:blank:]]+-[^[:space:]]+)*[[:blank:]]+(commit|push)([^[:alnum:]_-]|\$)"
if [[ "$text" =~ ${PIPED_CHECK}.*${COMMIT} ]]; then
  hook_deny "検査の出力をパイプに通したまま、&& で git commit / push へ繋いでいます。パイプの rc は終端 (tail 等) のものなので、検査が赤くても commit / push が走ります (retro 831 で lint が赤いまま commit した)。検査の rc を直接取ってから繋いでください: make lint > <log> 2>&1; rc=\$?; tail -2 <log>; [ \$rc -eq 0 ] && git commit … (または set -o pipefail を先に)。規範: _claude/rules/verify-execution-not-just-exit-code.md"
fi

exit 0
