# shellcheck shell=bash
# Bash ツールのコマンド文字列から「**コマンドとして** git の commit / push を呼んでいるか」を判定する。
#
# なぜ lib なのか (issue 310): 判定を「語が含まれるか」の grep で書くと、
# **`git -C dir commit` を拾えない**のと **git を 1 度も実行しない散文で発火する**のが
# 同じ原因から出る。トリガを緩める方向 (`git -C` を拾えるようにする) に直すと過剰発火も一緒に
# 広がるので、**語の一致ではなくコマンドとしての git 呼び出しか**を見る形へ寄せた。
#
# 🚨 **脅威モデル** (`_claude/rules/adversarial-review-own-safeguards.md` §8):
#   止めたいのは「**人が普通に打つ git commit / push を取りこぼす**」ことと
#   「**git を呼んでいない文章で発火する**」こと。シェル構文の完全な解析は目指さない。
#
# 🚨 **検出しないと決めた形** (実装後に射程を突き合わせた結果):
#   - **heredoc の本文**。`git commit -F - <<'M' ... M` の本文に `git push` と書いてあっても、
#     行頭が `git` なら 1 コマンドとして数える。区別するにはヒアドキュメントの範囲解析が要る
#   - **変数・エイリアス経由** (`G=git; $G commit` / `alias g=git; g commit`)
#   - **`sh -c "git commit"` のような入れ子**の引用の中
#   - **`xargs git commit`** のように git が先頭に来ない起動
#   これらを取りこぼしても「検証を注入しそこねる」だけで、逆向きの嘘 (誤った ground truth) には
#   ならない。過剰発火の方が有害なので、迷ったら**発火しない側**へ倒す。

# git_cmd_invokes は cmd に「git の <サブコマンド...> 呼び出し」が含まれるとき 0 を返す。
# 使い方: git_cmd_invokes "$cmd" commit push
git_cmd_invokes() {
  local cmd="$1"; shift
  local -a wanted=("$@")
  local segment tok rest sub found

  # `&&` / `||` / `;` / `|` / 改行 でコマンドを割る。**先頭トークンだけを見る**ので、
  # `echo "git commit"` は先頭が echo になり発火しない (陰性対照)。
  # 🚨 括弧・引用の対応は見ていない (上の「検出しない形」のとおり)。
  local ifs_bak="$IFS"
  while IFS= read -r segment; do
    # 先頭の空白と、環境変数の前置き (FOO=bar git ...) を落とす
    segment="${segment#"${segment%%[![:space:]]*}"}"
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
    sub=""
    # shellcheck disable=SC2086  # 単語分割してトークン列にしたい
    set -- $rest
    while [ "$#" -gt 0 ]; do
      case "$1" in
        -C|-c|--git-dir|--work-tree|--namespace|--exec-path|--config-env)
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
      if [ "$sub" = "$found" ]; then IFS="$ifs_bak"; return 0; fi
    done
  # `||` は `|` より先に潰す (順序を入れ替えると `||` が空セグメント 2 つになる)。
  done <<< "$(printf '%s' "$cmd" | sed 's/&&/\n/g; s/||/\n/g; s/;/\n/g; s/|/\n/g')"
  IFS="$ifs_bak"
  return 1
}
