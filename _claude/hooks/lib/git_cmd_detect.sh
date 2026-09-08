# shellcheck shell=bash
# Bash ツールのコマンド文字列から「**コマンドとして** git の commit / push を呼んでいるか」と、
# 「**どのディレクトリの repo を触ったか** (`git -C <dir>`)」を判定する。
#
# なぜ lib なのか (issue 310): 判定を「語が含まれるか」の grep で書くと、
# **`git -C dir commit` を拾えない**のと **git を 1 度も実行しない散文で発火する**のが
# 同じ原因から出る。トリガを緩める方向 (`git -C` を拾えるようにする) に直すと過剰発火も一緒に
# 広がるので、**語の一致ではなくコマンドとしての git 呼び出しか**を見る形へ寄せた。
#
# 🚨 **`-C` の値を返すのは飾りではない** (issue 310 の敵対レビュー P1-2)。トリガが
# `git -C <別 repo>` を拾えるようになった瞬間、hook が cwd の repo を見たままだと
# **「別 repo について、すべて push 済み」という積極的な偽の全クリア**を注入する
# (310 以前は発火しなかったので、この嘘は 310 が新しく作ったもの)。呼び出し側が
# 「どこを見ればよいか」を知れるよう、判定と同じ走査で行き先も返す。
#
# 🚨 **脅威モデル** (`_claude/rules/adversarial-review-own-safeguards.md` §8):
#   止めたいのは「**人が普通に打つ git commit / push を取りこぼす**」ことと
#   「**git を呼んでいない文章で発火する**」こと。シェル構文の完全な解析は目指さない。
#
# 🚨 **検出しないと決めた形** (実装後に射程を突き合わせ、敵対レビューの指摘で更新した):
#   - **heredoc の本文**。`git commit -F - <<'M' ... M` の本文の行頭が `git` なら 1 コマンドと
#     数える。区別するにはヒアドキュメントの範囲解析が要る
#   - **変数・エイリアス経由** (`G=git; $G commit` / `alias g=git; g commit`)
#   - **`sh -c "git commit"` / `eval "git commit"` のような入れ子**の引用の中
#   - **`xargs git commit`** のように git が先頭に来ない起動
#   - **引用の中に書いた「本物に見える」散文**。1 行に閉じた引用の中は発火しないが、
#     **複数行にまたがる引用**・heredoc の本文・バッククォートの中は発火しうる (実測)
#   - **`cd X && git commit`** と **`--git-dir` / `--work-tree`**: 行き先として拾わない
#     (cwd を見る)。`-C` だけを行き先として扱う
#   - **`-C` の値に空白や改元が入る形** (`git -C "/tmp/my repo" commit`): 単語分割で壊れるので
#     行き先として拾わない。cwd のブロックは必ず出るので、増えないだけで嘘にはならない
#
# 🚨 **`-C` の値は「本文が指定できる」前提で扱う** (2 周目 P1-1)。heredoc やバッククォートの
# 中に `git -C /path push` と書けば、それが行き先として積まれる。だから呼び出し側は
# **cwd を常に報告し、`-C` は「追加のブロック」としてのみ足すこと**。行き先の集合で
# cwd を置き換えると、**本文が「どの repo を検証するか」を乗っ取れる**。
#   取りこぼしは「検証を注入しそこねる」だけだが、**過剰発火は誤った ground truth を注入する**。
#   迷ったら発火しない側へ倒す。

# _git_cmd_split は cmd をコマンドの区切りで割って 1 行 1 セグメントで出す。
#
# 🚨 **引用の中の区切りでは割らない**。`echo "手順: まず commit する; git push は後で"` を
# `;` で割ると `git push は後で` が独立したコマンドに見え、**散文で発火する**
# (敵対レビュー P2。旧実装でも起きていた)。
# 割る対象: `;` 改行 `|` `||` `&` `&&` と、コマンド置換・グループの開閉 `$(` ` ` ` ( ) { }`。
_git_cmd_split() { # _git_cmd_split <cmd>
  local s="$1" out="" ch q="" i n
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
      ';'|'&'|'|'|'('|')'|'{'|'}'|'`')
            out+=$'\n'
            # 2 文字の演算子 (`&&` / `||`) は 1 つの区切りに畳む
            [ "${s:i+1:1}" = "$ch" ] && { [ "$ch" = '&' ] || [ "$ch" = '|' ]; } && i=$((i + 1))
            ;;
      '$')  if [ "${s:i+1:1}" = "(" ]; then out+=$'\n'; i=$((i + 1)); else out+="$ch"; fi ;;
      *)    out+="$ch" ;;
    esac
  done
  printf '%s\n' "$out"
}

# git_cmd_invokes は cmd に「git の <サブコマンド...> 呼び出し」が含まれるとき 0 を返す。
# 使い方: git_cmd_invokes "$cmd" commit push
#
# 副産物として **GIT_CMD_TARGET_DIRS** に、一致した呼び出しごとの `-C` の値を 1 行ずつ入れる
# (`-C` が無ければ空行 = cwd)。呼び出し側はこれを「どの repo を見るべきか」に使う。
git_cmd_invokes() {
  local cmd="$1"; shift
  local -a wanted=("$@")
  local segment tok rest sub found dir hit=1
  GIT_CMD_TARGET_DIRS=""

  # 🚨 **安価な前置きフィルタ**。この関数は PostToolUse で**全 Bash 呼び出し**に対して走る。
  # 下の分割器は文字単位ループなので長さに対して超線形で、実測 (bash 5.3.3) では
  # git と無関係な 40 KB のコマンドで 4.2 秒かかっていた (敵対レビュー 2 周目 P2-4)。
  case "$cmd" in *git*) ;; *) return 1 ;; esac

  # IFS は「未設定」と「空」が別物。unset の呼び出し元へ空文字を書き戻すと単語分割が死ぬ。
  local ifs_bak ifs_was_set=1
  if [ -z "${IFS+x}" ]; then ifs_was_set=0; else ifs_bak="$IFS"; fi
  while IFS= read -r segment; do
    # 先頭の空白 → シェルのキーワード/前置きコマンド → 環境変数の前置き の順に落とす。
    # `for d in a b; do git -C "$d" push; done` の `do`、`{ git commit; }` の `{` (分割済み)、
    # `time git push` の `time` を取りこぼしていた (敵対レビュー P2)。
    segment="${segment#"${segment%%[![:space:]]*}"}"
    while :; do
      read -r tok rest <<< "$segment"
      case "$tok" in
        do|then|else|elif|time|'!'|exec|nohup|env) segment="$rest" ;;
        *) break ;;
      esac
    done
    while [[ "$segment" =~ ^[A-Za-z_][A-Za-z0-9_]*=[^[:space:]]*[[:space:]]+(.*)$ ]]; do
      segment="${BASH_REMATCH[1]}"
    done
    read -r tok rest <<< "$segment"
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
        GIT_CMD_TARGET_DIRS+="$dir"$'\n'
        hit=0
        break
      fi
    done
  done <<< "$(_git_cmd_split "$cmd")"
  if [ "$ifs_was_set" = 1 ]; then IFS="$ifs_bak"; else unset IFS; fi
  return "$hit"
}
