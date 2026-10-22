#!/usr/bin/env bash
#
# PreToolUse(Bash) フック: HEAD の祖先でない行き先への `git reset --soft` を deny する。
#
# なぜ: soft reset は HEAD だけを動かして index と木を残す。行き先が HEAD の祖先でない (自分の起点より先へ進んだ origin/master 等) と、
# index は「行き先 + 自分の木」になり、行き先にあって自分の木に無い変更 (他のセッションの commit) を打ち消す差分が入る。
# 気づかずに commit すると上流の変更を巻き戻す。
# 規範は _claude/rules-checklists/commit-with-pathspec.md (soft reset の起点は先に rebase してから) にあるが、読まずに打った。
#
# ## 判定
#
# heredoc の本文を落とし (lib の hook_strip_heredocs)、`\` + 改行を空白へ畳み、awk で 1 回だけ走って && / || / ; / | / |& / & / 改行で区切る
# (`2>&1` などリダイレクトの & は区切りにしない)。bash で 1 文字ずつ切り出すと入力長の 2 乗の時間になり、2 万字で 10 秒の timeout を越えて素通りした。
# 区切りごとの処理も $( ) で fork しない (行数に比例して効き、/bin/bash では 4000 行で 19 秒かかった)。awk は引用符を追い (中の区切り文字・`#` は文字のまま)、語の頭の `#` から行末をコメントとして捨て、空白だけの区切りは出さない
# (`a &&<改行>b` / `a || # c<改行>b` で前の区切り文字が残る)。行末の `\` は行の継続。
# 引用符が閉じないまま終わったら (bash と読みがずれる形を含む)、閉じない所から後ろに soft reset の字面があれば判定不能として止める。
# 各区切りの先頭の前置き (`(` `{` `!` `then` `do` `else` `if` `time [-p]` `command [-p]` `exec` `nohup` `env […]` と変数の代入) を剥がし、前から順に見る:
#   - `cd [-P|-L] <dir>` / `pushd <dir>` で判定する dir を変える。無い dir・変数のまま (`cd $wt`)・`cd -`・`popd`・括弧 `( … )` の中の cd は
#     「分からない」にする (括弧・パイプの中の cd は外へ効かないが、閉じを追わずに保守側へ倒す)
#   - `env -C <dir>` / `--chdir` はその区切りの git の dir を変える
#   - git の大域オプションは順に読み、`-C` は累積する (前の dir が分からなくても絶対パスなら解決する)。`--git-dir` / `--work-tree` /
#     前置きの `GIT_DIR=` / `GIT_WORK_TREE=` があれば dir は分からない。git は `/usr/bin/git` や `\git` の形も git と読む
#   - `git fetch` / `git pull` / `git remote update` / `pull_main_checkout.sh` (知らない前置きの後ろの `git … fetch|pull` も) があれば「この後の ref は hook の時点と違う」とし、覚えたものを忘れる
#   - 直後の区切りが && (または次の区切りが then で始まる ;) の `git merge-base --is-ancestor <ref> HEAD` は、その dir の ref を実行時に
#     確かめている形として覚える (`!` で反転したもの、直前の区切りが || で走らないことがあるものは覚えない)
#   - 同じ条件の `git rebase` は (`--keep-base` を除く。upstream の上へ載せ替えないので祖先にならない)、`--onto` の値 (無ければ値を取るオプションの値を除いた最初の非オプションの語) を覚える
#   - && 以外の区切り (次が then の ; を除く) を越えたら、覚えたものを忘れる (前の確かめが失敗しても後ろが走る)。dir が分からない組は覚えない
#   - `git reset … --soft (--s / --so / --sof の省略形も) … <行き先>` を見つけたら:
#       行き先が無い・HEAD からの相対 (HEAD~N / HEAD^ / @~N) → 通す (必ず祖先)
#       行き先が変数・コマンド置換 (`$…` / `` `…` ``) → reset の dir が shell の cwd と同じで `$(git merge-base … HEAD …)` なら通す (必ず祖先)、それ以外は止める (実行時には解決できるので「解決できない」とは扱わない)
#       その dir の行き先を上で覚えていた → 通す
#       前に fetch の類がある → 止める (hook の時点の ref では判定できない)
#       dir が分からない → 止める (判定できないので拒否に倒す)
#       行き先が commit に解決できない・dir が repo でない → 通す (git reset 自体が失敗する)
#       `git merge-base --is-ancestor <行き先> HEAD` が 0 → 通す / それ以外 → 止める
#   語の引用符・末尾の `)`・くっついたリダイレクト (`origin/master>/dev/null` / `&>`) は落とし、`2>` `>` などの演算子の語は次の語ごと飛ばす
#
# ## 検出しない形 (review で見る責務の側)
#   - 変数・関数・alias・eval・スクリプト・`bash -c` / `$( … )` の中の reset (`R="git reset --soft"; $R origin/master` / `./tmp/squash.sh`)
#   - `git update-ref` / `git symbolic-ref` / `git read-tree` など reset 以外で HEAD や index を動かす形
#   - reset の前の HEAD を変える操作 (commit / checkout / switch / merge / cherry-pick / reset) は追わない。hook の時点の HEAD で判定する
#   - 引用符の中の空白 (`"git" reset` / 値に空白を含むオプション)
#   - `git branch -f` / `gh repo sync` など fetch 以外で ref を動かす形の後ろの reset (hook の時点の ref で判定する)
#   - symlink を含む `..` の dir (hook は論理で、git は物理で解決する)
#   - awk の引用符の読み違いが 2 回起きて打ち消し合う形 (`v="$(tr -d '"' < a)"; git reset …; n="$(tr -d '"' < b)"`)
#   - 継続の `\` の後ろの行に heredoc のリダイレクトがある形 (`cat <<EOF \<改行>> f`。heredoc を落とすのが継続を畳むより先)
#   - 前置きの一覧に無い前置き (sudo・xargs・自作の wrapper) の後ろの reset と、`export GIT_DIR=…` で後ろのコマンドへ渡す形、背景に回した cd (`cd X & …`)
#
# ## 分かっていて受ける偽陽性
#   - 意図して HEAD の祖先でない所へ soft reset する正当な使い方 (別の branch の先端へ付け替える等)。素直に別の手順 (checkout / reset --mixed) を使う
#   - dir が分からない形 (`cd $wt` / `cd -` / `popd` / 括弧の中の cd の後ろ) での、HEAD からの相対でない行き先への reset。
#     絶対パスの cd / -C で打つか、reset の前に `git merge-base --is-ancestor <行き先> HEAD &&` を置く
#   - `;` / `||` で繋いだ rebase / merge-base の後の reset (前が失敗しても走るので、覚えない)
#   - 取り切れない heredoc (区切り語が識別子でない `<<'MSG-EOF'`・1 行に 2 つ) の本文に apostrophe と `reset … --soft` の字面がある形。
#     commit message の区切り語は識別子 (`<<'MSGEOF'`) にする
#   - 変数に入れた行き先 (`git reset --soft "$base"`)。行き先を名前で書くか、直前に merge-base で確かめる
#   - 知らない前置きの区切りの中に `git … fetch` / `pull` の並びがある形 (`echo git log pull`) の後ろの、HEAD 相対でない reset
#
# 入力: PreToolUse の hook JSON (stdin)。.tool_input.command と .cwd を読む。
# 出力: 違反時のみ permissionDecision=deny の JSON。それ以外は無出力 exit 0。
set -uo pipefail

# shellcheck source=_claude/hooks/lib/hook_io.sh
. "$(dirname "$0")/lib/hook_io.sh" 2>/dev/null || exit 0
# jq が無い・読めない環境では諦める (誤 deny させない。fail-open)
hook_read_bash_command || exit 0
cmd=$HOOK_CMD
[ -n "$cmd" ] || exit 0

# 速い経路: reset と --s (--soft とその省略形) の両方を含まないものは解析に入れない (判定は変えない)
case "$cmd" in *reset*) ;; *) exit 0 ;; esac
case "$cmd" in *--s*) ;; *) exit 0 ;; esac

# 継承した GIT_DIR などは -C より強く、別の repo の HEAD で判定してしまう。外せなければ判定の前提が崩れるが、誤 deny より素通りを選ぶ (fail-open)
# shellcheck source=bin/lib/git_location_env.sh
. "$(dirname "$0")/../../bin/lib/git_location_env.sh" 2>/dev/null && git_location_env_unset 2>/dev/null

cwd=$(printf '%s' "$HOOK_INPUT" | jq -r '.cwd // ""' 2>/dev/null) || cwd=""
[ -n "$cwd" ] || cwd=$PWD

GUIDE="soft reset の行き先は HEAD の祖先にする: 先に git rebase <行き先> してから、その行き先へ soft reset する (行き先に他のセッションの commit があると、index がそれを打ち消す差分になる。issue 877・_claude/rules-checklists/commit-with-pathspec.md の履歴操作の節)。"

# unquote <word>: 語の両端の引用符と末尾の ) を落とし、くっついたリダイレクト (`ref>/dev/null` / `ref&>…`) を落として UQ に入れる
# (末尾の } は落とさない: `@{u}` / `^{commit}` を壊す。`{ …; }` の閉じは ; の後ろの別の区切りになる)
# 値を $( ) で受けない: 区切りごとの fork が行数に比例して効き、改行の多い入力で 10 秒の timeout を越えて素通りする
unquote() {
  local w=$1
  w=${w%%[<>]*}
  w=${w%&}
  while [[ "$w" == *')' ]]; do w=${w%?}; done
  w=${w#\"}; w=${w%\"}; w=${w#\'}; w=${w%\'}
  UQ=$w
}

# resolve_dir <base> <path>: 判定に使う dir を RD に入れる。分からなければ "?" (base が "?" でも絶対パスなら解決する)
resolve_dir() {
  local base=$1 p
  unquote "$2"; p=$UQ
  RD="?"
  [ -n "$p" ] || return 0
  # shellcheck disable=SC2088 # 渡された文字の ~ を文字どおり照合する (展開させない)
  case "$p" in
    '~') p=$HOME ;;
    '~/'*) p="$HOME/${p#\~/}" ;;
    /*) ;;
    *) [ "$base" = "?" ] && return 0; p="$base/$p" ;;
  esac
  # 判定の後の git は全部 -C の絶対パスで打つので、この process の cwd が動いても判定は変わらない ($( ) の fork を避ける)
  [ -d "$p" ] && cd "$p" 2>/dev/null && RD=$PWD
}

# heredoc の本文を落とし、行の継続を畳んで、awk で区切る。1 行に「後ろの区切り<US>区切りの中身」を出す (改行は ; と同じ)
body=$(hook_strip_heredocs "$cmd") || body=$cmd
US=$'\037'
segs=()
seps=()
while IFS=$US read -r sep seg; do
  seps+=("$sep"); segs+=("$seg")
done < <(printf '%s\n' "$body" | LC_ALL=C awk -v US="$US" -v SQ="'" '
  # 空白だけの区切りは出さない (前の区切りの区切り文字を残す: `a &&<改行>b` / `a || # c<改行>b`)
  function emit(s) { if (cur ~ /[^ \t]/) print s US cur; cur = "" }
  {
    n = length($0); cont = 0
    for (i = 1; i <= n; i++) {
      c = substr($0, i, 1)
      if (q != "") { # 引用符の中: 区切り文字もコメントも文字のまま (引用符は行をまたぐ)
        cur = cur c
        if (c == "\\" && q == "\"") { i++; cur = cur substr($0, i, 1); continue }
        if (c == q) q = ""
        continue
      }
      if (c == "\\") { if (i == n) { cont = 1; break } cur = cur c substr($0, i + 1, 1); i++; continue } # 行末の \ は行の継続
      if (c == "\"" || c == SQ) { q = c; cur = cur c; continue }
      if (c == "#" && (cur == "" || substr(cur, length(cur), 1) ~ /[ \t]/)) break # コメント
      nx = substr($0, i + 1, 1); pv = (i > 1) ? substr($0, i - 1, 1) : ""
      if (c == "&") {
        if (pv == ">" || pv == "<" || nx == ">") { cur = cur c; continue } # リダイレクトの & (2>&1 / >& / &>)
        if (nx == "&") { emit("&&"); i++; continue }
        emit("&"); continue
      }
      if (c == "|") {
        if (nx == "|") { emit("||"); i++; continue }
        if (nx == "&") { emit("|"); i++; continue } # |&
        emit("|"); continue
      }
      if (c == ";") { emit(";"); continue }
      cur = cur c
    }
    if (q == "" && !cont) emit(";"); else cur = cur " "
  }
  # 閉じないまま終わった引用符 (bash と読みがずれた形を含む) は、中身ごと印を付けて出す
  END { if (q != "") { print "OPENQ" US cur; cur = "" } emit(";") }')


# 知らない前置き (runtimeout・$( ) の中・\git) の後ろの fetch / pull
# shellcheck disable=SC2016 # 正規表現の文字としての $ と ` (展開させない)
RE_WRAPPED_FETCH='[[:space:]/$(\`]git[[:space:]]+([^[:space:]]+[[:space:]]+)*(fetch|pull)([^[:alnum:]_-]|$)'
# HEAD を相手にした merge-base で作る行き先 (必ず HEAD の祖先)
# (相手はちょうど 2 つで片方が HEAD。オプション (--independent / --octopus 等) や 3 つ目の相手があると HEAD の祖先とは限らない)
RE_MERGE_BASE_TARGET='(--s|--so|--sof|--soft)[[:space:]]+"?\$\(git[[:space:]]+merge-base[[:space:]]+((HEAD|@)[[:space:]]+[^[:space:]()-][^[:space:]()]*|[^[:space:]()-][^[:space:]()]*[[:space:]]+(HEAD|@))[[:space:]]*\)'

RE_OPENQ_SOFT_RESET='[[:space:]]reset[[:space:]]([^;&|]*[[:space:]])?(--s|--so|--sof|--soft)[[:space:]]'
# 引用符が閉じないまま終わった: 閉じない所から後ろは 1 つの区切りに飲まれて読めない。そこに soft reset の字面があれば判定不能として止める
# (bash と awk の引用符の読みがずれる形 `"$(… '…"…' …)"` / $'\'' / 取り切れなかった heredoc の本文の apostrophe を、素通りにしない)
for ((k = 0; k < ${#segs[@]}; k++)); do
  if [ "${seps[k]}" = OPENQ ] && [[ " ${segs[k]} " =~ $RE_OPENQ_SOFT_RESET ]]; then
    hook_deny "コマンドの引用符の対応を読み切れず (閉じない引用符・\$( ) の中の引用符・取り切れない heredoc の本文)、git reset --soft の行き先が HEAD の祖先かを判定できません。reset を別の呼び出しに分けてください。${GUIDE}"
  fi
done

dir=$cwd
fetched=0
known="" # 覚えた「dir<TAB>ref」(改行区切り)
TAB=$'\t'

for ((k = 0; k < ${#segs[@]}; k++)); do
  seg=${segs[k]}
  next_sep=${seps[k]}
  # 直後の区切りが && か、次の区切りが then で始まる ; なら「この区切りの成否で後ろが走る」
  chained=0
  if [ "$next_sep" = '&&' ]; then chained=1
  elif [ "$next_sep" = ';' ] && [[ "${segs[k + 1]:-}" =~ ^[[:space:]]*then([[:space:]]|$) ]]; then chained=1
  fi
  # 直前の区切りが || なら、この区切りは走らないことがある (走った前提の確かめにしない)
  prev_sep=";"
  [ "$k" -gt 0 ] && prev_sep=${seps[k - 1]}
  maybe_skipped=0
  [ "$prev_sep" = '||' ] && maybe_skipped=1
  read -r -a words <<<"$seg"
  if [ "${#words[@]}" -eq 0 ]; then
    continue
  fi
  gitdir_env=0
  env_chdir=""
  negated=0
  in_paren=0
  # 前置きを剥がす
  j=0
  while [ "$j" -lt "${#words[@]}" ]; do
    w=${words[j]}
    while [[ "$w" == '('* || "$w" == '{'* || "$w" == '!'* ]]; do
      case "$w" in '('*) in_paren=1 ;; '!'*) negated=1 ;; esac
      w=${w#?}
    done
    if [ -z "$w" ]; then j=$((j + 1)); continue; fi
    case "$w" in
      then | do | else | if | while | until | exec | nohup) j=$((j + 1)); continue ;;
      command | time)
        j=$((j + 1))
        while [ "$j" -lt "${#words[@]}" ] && [[ "${words[j]}" == -* ]]; do j=$((j + 1)); done
        continue
        ;;
      env)
        j=$((j + 1))
        while [ "$j" -lt "${#words[@]}" ]; do
          case "${words[j]}" in
            -*)
              ew=${words[j]}
              j=$((j + 1))
              case "$ew" in
                --chdir) env_chdir=${words[j]:-.}; j=$((j + 1)) ;;
                --chdir=*) env_chdir=${ew#--chdir=} ;;
                --unset | --split-string) j=$((j + 1)) ;;
                --*) ;;
                *)
                  # 1 文字のオプションの束 (`-iC <dir>` / `-C<dir>`)。値を取る文字 (u S P C) の後ろは値
                  ef=${ew#-}
                  while [ -n "$ef" ]; do
                    eo=${ef:0:1}
                    ef=${ef:1}
                    case "$eo" in
                      u | S | P | C)
                        if [ -n "$ef" ]; then ev=$ef; ef=""; else ev=${words[j]:-}; j=$((j + 1)); fi
                        [ "$eo" = C ] && env_chdir=$ev
                        ;;
                    esac
                  done
                  ;;
              esac
              ;;
            *=*) [[ "${words[j]}" =~ ^(GIT_DIR|GIT_WORK_TREE)= ]] && gitdir_env=1; j=$((j + 1)) ;;
            *) break ;;
          esac
        done
        continue
        ;;
    esac
    if [[ "$w" =~ ^[A-Za-z_][A-Za-z0-9_]*= ]]; then
      [[ "$w" =~ ^(GIT_DIR|GIT_WORK_TREE)= ]] && gitdir_env=1
      j=$((j + 1)); continue
    fi
    words[j]=$w
    break
  done
  if [ "$j" -ge "${#words[@]}" ]; then
    [ "$chained" = 1 ] || known=""
    continue
  fi
  unquote "${words[j]}"
  head=${UQ#\\}

  case "$head" in
    cd | pushd)
      m=$((j + 1))
      while [ "$m" -lt "${#words[@]}" ] && [[ "${words[m]}" == -[PLe@]* ]]; do m=$((m + 1)); done
      # 括弧・パイプの中の cd は subshell で走り、外へ効かない (閉じを追わずに分からないへ倒す)
      if [ "$in_paren" = 1 ] || [ "$next_sep" = '|' ] || [ "$prev_sep" = '|' ] || [ "${words[m]:-}" = - ]; then
        dir="?"
      else
        resolve_dir "$dir" "${words[m]:-~}"; dir=$RD
      fi
      [ "$chained" = 1 ] || known=""
      continue
      ;;
    popd)
      dir="?"
      [ "$chained" = 1 ] || known=""
      continue
      ;;
    *pull_main_checkout.sh)
      fetched=1; known=""
      continue
      ;;
    git | */git) ;;
    *)
      # 知らない前置き (runtimeout 等) の後ろの fetch / pull も、ref を動かす
      [[ " $seg " =~ $RE_WRAPPED_FETCH ]] && { fetched=1; known=""; }
      [ "$chained" = 1 ] || known=""
      continue
      ;;
  esac

  # git の大域オプション
  gdir=$dir
  [ -n "$env_chdir" ] && { resolve_dir "$dir" "$env_chdir"; gdir=$RD; }
  [ "$gitdir_env" = 1 ] && gdir="?"
  j=$((j + 1))
  while [ "$j" -lt "${#words[@]}" ]; do
    w=${words[j]}
    case "$w" in
      -C) [ "$gitdir_env" = 1 ] || { resolve_dir "$gdir" "${words[j + 1]:-}"; gdir=$RD; }; j=$((j + 2)) ;;
      -c) j=$((j + 2)) ;;
      --git-dir | --work-tree) gdir="?"; gitdir_env=1; j=$((j + 2)) ;;
      --git-dir=* | --work-tree=*) gdir="?"; gitdir_env=1; j=$((j + 1)) ;;
      -*) j=$((j + 1)) ;;
      *) break ;;
    esac
  done
  if [ "$j" -ge "${#words[@]}" ]; then
    [ "$chained" = 1 ] || known=""
    continue
  fi
  unquote "${words[j]}"; sub=$UQ
  # 副コマンドの引数 (リダイレクトの演算子の語は次の語ごと飛ばす)
  args=()
  j=$((j + 1))
  while [ "$j" -lt "${#words[@]}" ]; do
    w=${words[j]}
    if [[ "$w" =~ ^[0-9]*[\<\>]+\&?$ || "$w" == '&>' ]]; then j=$((j + 2)); continue; fi # `>` `2>` `>>` `<` `&>`
    if [[ "$w" =~ ^[0-9]*[\<\>] || "$w" == '&>'* ]]; then j=$((j + 1)); continue; fi   # `>log` `2>/dev/null` `&>log` `2>&1`
    unquote "$w"
    [ -n "$UQ" ] && args+=("$UQ")
    j=$((j + 1))
  done

  case "$sub" in
    fetch | pull)
      fetched=1; known=""
      continue
      ;;
    remote)
      if [ "${args[0]:-}" = update ]; then fetched=1; known=""; continue; fi
      ;;
    merge-base)
      if [ "$chained" = 1 ] && [ "$maybe_skipped" = 0 ] && [ "$negated" = 0 ] && [ "$gdir" != "?" ] &&
        [[ " ${args[*]+"${args[*]}"} " =~ \ --is-ancestor\ ([^[:space:]]+)\ HEAD\  ]]; then
        known+="${gdir}${TAB}${BASH_REMATCH[1]}"$'\n'
        continue
      fi
      ;;
    rebase)
      if [ "$chained" = 1 ] && [ "$maybe_skipped" = 0 ] && [ "$gdir" != "?" ]; then
        onto="" first="" m=0 keep=0
        while [ "$m" -lt "${#args[@]}" ]; do
          case "${args[m]}" in
            --onto) onto=${args[m + 1]:-}; m=$((m + 2)); continue ;;
            --onto=*) onto=${args[m]#--onto=} ;;
            --keep-b | --keep-ba | --keep-bas | --keep-base) keep=1 ;; # upstream の上へ載せ替えないので、rebase の後も祖先にならない
            -X | -s | -x | --strategy | --strategy-option | --exec) m=$((m + 2)); continue ;; # 値を取るオプション
            -*) ;;
            *) [ -z "$first" ] && first=${args[m]} ;;
          esac
          m=$((m + 1))
        done
        tgt=${onto:-$first}
        if [ -n "$tgt" ] && [ "$keep" = 0 ]; then
          known+="${gdir}${TAB}${tgt}"$'\n'
          continue
        fi
      fi
      ;;
    reset)
      soft=0 target="" chk=0
      for w in ${args[@]+"${args[@]}"}; do
        case "$w" in
          --) break ;; # `--` の後ろはパス
          --s | --so | --sof | --soft) soft=1 ;;
          -*) ;;
          *) [ -z "$target" ] && target=$w ;;
        esac
      done
      if [ "$soft" = 1 ] && [ -n "$target" ] && ! [[ "$target" =~ ^(HEAD|@)([~^][0-9]*)*$ ]] &&
        ! grep -qxF -- "${gdir}${TAB}${target}" <<<"$known"; then
        chk=1
        case "$target" in
          *'$'* | *'`'*)
            # 行き先が変数・コマンド置換: hook の時点では解決できないが、実行時には解決できる。merge-base で作る行き先だけは必ず祖先
            # $( ) は shell の cwd で走るので、reset の dir が cwd と同じで、merge-base の相手に HEAD があるときだけ
            if ! { [ "$gdir" = "$dir" ] && [ "$dir" != "?" ] && [[ "$seg" =~ $RE_MERGE_BASE_TARGET ]]; }; then
              hook_deny "git reset --soft ${target} の行き先が変数・コマンド置換で、hook の時点では HEAD の祖先かを判定できません。行き先を ref の名前で書くか (HEAD~N / origin/… / hash)、reset の前に git merge-base --is-ancestor <行き先> HEAD && を置いてください。${GUIDE}"
            fi
            chk=0
            ;;
        esac
      fi
      if [ "$chk" = 1 ]; then
        if [ "$fetched" = 1 ]; then
          hook_deny "同じコマンドの中で fetch / pull の後に git reset --soft ${target} を打っています。hook の時点の ${target} は実行時の値と違うので、HEAD の祖先かを判定できません。fetch と reset を別の呼び出しに分けるか、reset の前に git merge-base --is-ancestor ${target} HEAD && を置いてください。${GUIDE}"
        fi
        if [ "$gdir" = "?" ]; then
          hook_deny "git reset --soft ${target} を打つ repo を判定できません (変数の dir・存在しない dir・cd - / popd・括弧の中の cd・--git-dir / GIT_DIR)。HEAD の祖先かを確かめられないので保守的に拒否しました。絶対パスの cd / -C で打つか、reset の前に git merge-base --is-ancestor ${target} HEAD && を置いてください。${GUIDE}"
        fi
        if git -C "$gdir" rev-parse --verify -q "${target}^{commit}" >/dev/null 2>&1; then # 解決できない・repo でない: reset 自体が失敗する
          git -C "$gdir" merge-base --is-ancestor "$target" HEAD >/dev/null 2>&1
          rc=$?
          if [ "$rc" -ne 0 ]; then
            hook_deny "git reset --soft ${target} の行き先が HEAD の祖先ではありません (merge-base --is-ancestor rc=${rc}。dir: ${gdir})。このまま打つと、${target} にあって自分の木に無い変更を打ち消す差分が index に入ります。${GUIDE}"
          fi
        fi
      fi
      ;;
  esac
  [ "$chained" = 1 ] || known=""
done

exit 0
