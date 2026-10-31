# shellcheck shell=bash
# Bash ツールのコマンド文字列から「**コマンドとして** git の commit / push を呼んでいるか」と、
# 「**どのディレクトリの repo を触ったか** (`git -C <dir>`)」を判定する。
#
# なぜ lib なのか: 判定を「語が含まれるか」の grep で書くと、
# **`git -C dir commit` を拾えない**のと **git を 1 度も実行しない散文で発火する**のが
# 同じ原因から出る。トリガを緩める方向 (`git -C` を拾えるようにする) に直すと過剰発火も一緒に
# 広がるので、**語の一致ではなくコマンドとしての git 呼び出しか**を見る形へ寄せた。
#
# 🚨 **`-C` の値を返すのは飾りではない**。トリガが
# `git -C <別 repo>` を拾えるようになった瞬間、hook が cwd の repo を見たままだと
# **「別 repo について、すべて push 済み」という積極的な偽の全クリア**を注入する
# (-C を拾う前は発火しなかったので、この嘘は -C を拾うことで新しく生まれる)。呼び出し側が
# 「どこを見ればよいか」を知れるよう、判定と同じ走査で行き先も返す。
#
# 🚨 **脅威モデル** (`_claude/rules/adversarial-review-own-safeguards.md` §8):
#   止めたいのは「**人が普通に打つ git commit / push を取りこぼす**」ことと
#   「**git を呼んでいない文章で発火する**」こと。シェル構文の完全な解析は目指さない。
#
# 🚨 **検出しないと決めた形** (射程を突き合わせて決めた):
#   - **heredoc の本文**。`git commit -F - <<'M' ... M` の本文の行頭が `git` なら 1 コマンドと
#     数える。区別するにはヒアドキュメントの範囲解析が要る
#   - **変数・エイリアス経由** (`G=git; $G commit` / `alias g=git; g commit`)
#   - **`sh -c "git commit"` / `eval "git commit"` のような入れ子**の引用の中
#   - **`xargs git commit`** のように git が先頭に来ない起動
#   - **引用の中に書いた「本物に見える」散文**。1 行に閉じた引用の中は発火しないが、
#     **複数行にまたがる引用**・heredoc の本文・バッククォートの中は発火しうる (実測)
#   - **`timeout 60 git push`** のように**引数を取る**ラッパー (`timeout` の秒数を git と
#     読み違えないよう、値つきラッパーは剥がさない)。`sudo` / `nice` / `stdbuf` / `env` は剥がす
#   - **`--git-dir` / `--work-tree`**: 行き先として拾わない (cwd を見る)
#   - **`cd` の行き先のうち解決できないもの**: 同じコマンドの中の単純な代入 (`W=/x` / `export W=/x`) と
#     `$HOME` / `~` だけを展開する。それ以外の変数・コマンド置換・`pushd` は展開しない。展開できない行き先は
#     そのまま積むので、呼び出し側が「行き先が見つからない」(判定不能) と出す。パイプの左右の cd も追わない
#   - **`-C` の値に空白や改元が入る形** (`git -C "/tmp/my repo" commit`): 単語分割で壊れるので
#     行き先として拾わない。cwd のブロックは必ず出るので、増えないだけで嘘にはならない
#
# 🚨 **`-C` の値は「本文が指定できる」前提で扱う**。heredoc やバッククォートの
# 中に `git -C /path push` と書けば、それが行き先として積まれる。だから呼び出し側は
# **cwd を常に報告し、`-C` は「追加のブロック」としてのみ足すこと**。行き先の集合で
# cwd を置き換えると、**本文が「どの repo を検証するか」を乗っ取れる**。
#   取りこぼしは「検証を注入しそこねる」だけだが、**過剰発火は誤った ground truth を注入する**。
#   迷ったら発火しない側へ倒す。

# _git_cmd_split は cmd をコマンドの区切りで割って 1 行 1 セグメントで出す。
#
# 🚨 **引用の中の区切りでは割らない**。`echo "手順: まず commit する; git push は後で"` を
# `;` で割ると `git push は後で` が独立したコマンドに見え、**散文で発火する**
# (区切りで割ると起きる)。
# 割る対象: `;` 改行 `|` `||` `&` `&&` と、コマンド置換・グループの開閉 `$(` ` ` ` ( ) { }`。
# サブシェル (`(` `$(` とバッククォート) の開閉では、区切りに加えて印の行 ($_GIT_CMD_OPEN / $_GIT_CMD_CLOSE) を出す。
# 中の `cd` は外へ効かないので、呼び出し側は閉じで cd の行き先を開く前へ戻す。`{ }` は同じシェルなので印を出さない。
_GIT_CMD_OPEN=$'\x1f('
_GIT_CMD_CLOSE=$'\x1f)'
_git_cmd_split() { # _git_cmd_split <cmd>
  local s="$1" out="" ch q="" i n bt=0
  n=${#s}
  for ((i = 0; i < n; i++)); do
    ch="${s:i:1}"
    if [ -n "$q" ]; then                      # 引用の中はそのまま通す
      out+="$ch"
      [ "$ch" = "$q" ] && q=""
      continue
    fi
    case "$ch" in
      \'|\") q="$ch"; out+="$ch" ;;
      \\)   out+="$ch"; i=$((i + 1)); out+="${s:i:1}" ;;   # エスケープは次の 1 文字ごと持つ
      ';'|'&'|'|'|'{'|'}')
            out+=$'\n'
            # 2 文字の演算子 (`&&` / `||`) は 1 つの区切りに畳む
            [ "${s:i+1:1}" = "$ch" ] && { [ "$ch" = '&' ] || [ "$ch" = '|' ]; } && i=$((i + 1))
            ;;
      '(')  out+=$'\n'"$_GIT_CMD_OPEN"$'\n' ;;
      ')')  out+=$'\n'"$_GIT_CMD_CLOSE"$'\n' ;;
      '`')  if [ "$bt" -eq 0 ]; then bt=1; out+=$'\n'"$_GIT_CMD_OPEN"$'\n'
            else bt=0; out+=$'\n'"$_GIT_CMD_CLOSE"$'\n'; fi ;;
      '$')  if [ "${s:i+1:1}" = "(" ]; then out+=$'\n'"$_GIT_CMD_OPEN"$'\n'; i=$((i + 1)); else out+="$ch"; fi ;;
      *)    out+="$ch" ;;
    esac
  done
  printf '%s\n' "$out"
}

# _git_cmd_unquote_expand <word> <vars> は、引用を剥がし、`$NAME` / `${NAME}` を <vars> ("NAME=値" の行) から、
# `$HOME` と先頭の `~` を環境から展開して出す。単一引用は展開しない。知らない変数は字面のまま残す
# (呼び出し側が判定不能にする。空に置き換えると cwd を指して「見た」ことになる)。
_git_cmd_unquote_expand() {
  local w="$1" vars="$2" out="" ch name val i n
  case "$w" in
    \'*\') printf '%s' "${w:1:${#w}-2}"; return ;;
    \"*\") w="${w:1:${#w}-2}" ;;
  esac
  case "$w" in \~) w="$HOME" ;; \~/*) w="$HOME/${w:2}" ;; esac
  n=${#w}
  for ((i = 0; i < n; i++)); do
    ch="${w:i:1}"
    if [ "$ch" != '$' ]; then out+="$ch"; continue; fi
    if [[ "${w:i+1}" =~ ^\{([A-Za-z_][A-Za-z0-9_]*)\} ]] || [[ "${w:i+1}" =~ ^([A-Za-z_][A-Za-z0-9_]*) ]]; then
      name="${BASH_REMATCH[1]}"
      val=$(printf '%s\n' "$vars" | awk -v k="$name" 'index($0, k "=") == 1 { v = substr($0, length(k) + 2) } END { if (v != "") print v }')
      [ -z "$val" ] && [ "$name" = HOME ] && val="$HOME"
      if [ -n "$val" ]; then
        out+="$val"; i=$((i + ${#BASH_REMATCH[0]}))
        continue
      fi
    fi
    out+="$ch"
  done
  printf '%s' "$out"
}

# git_cmd_invokes は cmd に「git の <サブコマンド...> 呼び出し」が含まれるとき 0 を返す。
# 使い方: git_cmd_invokes "$cmd" commit push
#
# 副産物として **GIT_CMD_TARGET_DIRS** に、一致した呼び出しごとの行き先 (それより前の `cd` と `-C` を合わせたもの) を
# 1 行ずつ入れる (どちらも無ければ空行 = cwd)。呼び出し側はこれを「どの repo を見るべきか」に使う。
# 🚨 `cd` を追うのは、hook のプロセスが cd した後の shell の cwd ではなく session の cwd で走るため。
# `cd <worktree> && git commit` で、hook は共有 checkout の state を「これで検証すること」として出していた。
git_cmd_invokes() {
  local cmd="$1"; shift
  local -a wanted=("$@")
  local segment tok rest sub found dir hit=1 prev cd_dir="" cd_stack="" vars="" arg
  GIT_CMD_TARGET_DIRS=""

  # 🚨 **安価な前置きフィルタ**。この関数は PostToolUse で**全 Bash 呼び出し**に対して走る。
  # 下の分割器は文字単位ループなので長さに対して超線形で、実測 (bash 5.3.3) では
  # git と無関係な 40 KB のコマンドで 4.2 秒かかっていた。
  case "$cmd" in *git*) ;; *) return 1 ;; esac

  # 🚨 **`local IFS` で既定値に固定する**。この関数の `read` と `set --` は
  # 呼び出し元の IFS で単語分割するので、`IFS=ZZ` の呼び出し元では判定が壊れる。
  # 手で save/restore していたが、それは「関数が IFS を書き換える」前提の防御で、
  # 実際には書き換えていなかった (= 何も守らない死んだコードだった)。`local` なら
  # 復元は bash が保証し、依存の向きも正しくなる。
  local IFS=$' \t\n' 
  while IFS= read -r segment; do
    if [ "$segment" = "$_GIT_CMD_OPEN" ]; then cd_stack+="$cd_dir"$'\x1e'; continue; fi
    if [ "$segment" = "$_GIT_CMD_CLOSE" ]; then
      if [ -n "$cd_stack" ]; then
        cd_stack="${cd_stack%$'\x1e'}"
        cd_dir="${cd_stack##*$'\x1e'}"
        case "$cd_stack" in *$'\x1e'*) cd_stack="${cd_stack%$'\x1e'*}"$'\x1e' ;; *) cd_stack="" ;; esac
      fi
      continue
    fi
    # 先頭の空白 → シェルのキーワード/前置きコマンド → 環境変数の前置き の順に落とす。
    # `for d in a b; do git -C "$d" push; done` の `do`、`{ git commit; }` の `{` (分割済み)、
    # `time git push` の `time` を取りこぼしていた。
    segment="${segment#"${segment%%[![:space:]]*}"}"
    # 先頭に付く「git ではないが git を起動する前置き」を落とす。落とすたびに先頭空白を詰め、
    # 何も落とせなくなったら終わる (prev で進捗を見て無限ループを防ぐ)。
    while :; do
      prev="$segment"
      # 🚨 **環境変数の値はクォートされうる**。`GIT_SSH_COMMAND="ssh -i ~/k" git push`
      # のような、この repo でも普通に打つ形で **hook が 1 バイトも出さなくなっていた**。
      # 値の中の空白を跨げない `[^[:space:]]*` だけでは足りない。
      # 代入は後の `cd $NAME` を展開するために控える。`W=x cmd` の形 (cmd だけの環境) も控えるが、
      # 外れても余計な行き先が 1 つ積まれるだけ
      if [[ "$segment" =~ ^(export[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)=(\"[^\"]*\"|\'[^\']*\'|[^[:space:]]*)([[:space:]]+(.*))?$ ]]; then
        vars+="${BASH_REMATCH[2]}=$(_git_cmd_unquote_expand "${BASH_REMATCH[3]}" "$vars")"$'\n'
        segment="${BASH_REMATCH[5]}"
      else
        read -r tok rest <<< "$segment"
        case "$tok" in
          # シェルのキーワード / 実行を包むだけのコマンド
          do|then|else|elif|time|'!'|exec|nohup|env|sudo|stdbuf|nice|ionice) segment="$rest" ;;
          # 先頭のリダイレクト (`>log git push` / `2>/dev/null git push`)
          *'>'*|*'<'*) segment="$rest" ;;
          *) break ;;
        esac
      fi
      segment="${segment#"${segment%%[![:space:]]*}"}"
      [ "$segment" != "$prev" ] || break
    done
    read -r tok rest <<< "$segment"
    if [ "$tok" = "cd" ]; then
      arg=""
      set -f
      # shellcheck disable=SC2086  # 単語分割してトークン列にしたい (glob は直前で止めてある)
      set -- $rest
      set +f
      while [ "$#" -gt 0 ]; do case "$1" in -P|-L|-e|-@) shift ;; *) arg="$1"; break ;; esac; done
      case "$arg" in
        '') cd_dir="$HOME" ;;
        -)  cd_dir="(cd -)" ;;   # 1 つ前の cwd は分からない。存在しない行き先として積み、判定不能にする
        *)  arg=$(_git_cmd_unquote_expand "$arg" "$vars")
            case "$arg" in /*) cd_dir="$arg" ;; *) cd_dir="${cd_dir:+$cd_dir/}$arg" ;; esac ;;
      esac
      continue
    fi
    [ "$tok" = "git" ] || [ "$tok" = "command" ] || continue
    # `command git ...` の形も拾う
    if [ "$tok" = "command" ]; then
      read -r tok rest <<< "$rest"
      [ "$tok" = "git" ] || continue
    fi

    # グローバルオプションを読み飛ばして最初のサブコマンドを取る。
    # 🚨 **値を取るオプションは値ごと 2 個読み飛ばす**。ここが元の実装の穴で、
    # `-[^[:space:]]+` は `-C` は食えてもその値 `/tmp/r` を食えなかった。
    sub=""; dir=""
    # shellcheck disable=SC2086  # 単語分割してトークン列にしたい
    set -- $rest
    while [ "$#" -gt 0 ]; do
      case "$1" in
        -C) # git は `-C a -C b` を a/b と解釈する (絶対パスが来たら置き換わる)
            [ "$#" -ge 2 ] || break
            case "$2" in
              /*) dir="$2" ;;
              *)  dir="${dir:+$dir/}$2" ;;
            esac
            shift 2 ;;
        -c|--git-dir|--work-tree|--namespace|--exec-path|--config-env)
          shift 2 || break ;;                       # 値つき: 値ごと飛ばす
        --*=*|-p|--paginate|--no-pager|--bare|--no-replace-objects|--literal-pathspecs|\
        --glob-pathspecs|--noglob-pathspecs|--icase-pathspecs|--no-optional-locks|-P)
          shift ;;                                  # 値なし
        -*) shift ;;                                # 未知のオプションは 1 個飛ばす
        *)  sub="$1"; break ;;
      esac
    done
    [ -n "$sub" ] || continue

    for found in ${wanted+"${wanted[@]}"}; do
      if [ "$sub" = "$found" ]; then
        # 引用符を剥がす (`git -C "$T/B" commit` の値は quote 付きで来る)
        dir="${dir%\"}"; dir="${dir#\"}"; dir="${dir%\'}"; dir="${dir#\'}"
        [ -n "$dir" ] && dir=$(_git_cmd_unquote_expand "$dir" "$vars")
        case "$dir" in
          /*) : ;;
          *)  [ -n "$cd_dir" ] && dir="$cd_dir${dir:+/$dir}" ;;
        esac
        GIT_CMD_TARGET_DIRS+="$dir"$'\n'
        hit=0
        break
      fi
    done
  done <<< "$(_git_cmd_split "$cmd")"
  return "$hit"
}
