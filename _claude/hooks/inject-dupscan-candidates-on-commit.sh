#!/usr/bin/env bash
# PostToolUse / PostToolUseFailure(Bash) フック: いま作った commit で足した関数・型に、名前の近い既存の定義を dupscan index で引いて注入する。
#
# なぜ: 書く直前に候補を出す inject-dupscan-candidates-on-new-func.sh は Write / Edit にしか掛からない。Bash (python / sed / heredoc) で
# 足した関数は見ないので、同じ役割の関数をもう 1 つ書いても何も出ない (vlc-multi-video-player の issue `unify-typed-path-reading` がこの形)。
# commit は書き方によらず必ず通るので、push の前に知らせる。
#
# 🚨 止めない (additionalContext だけ)。どんな失敗も無音で rc=0 (fail-open)。
# 🚨 commit の前 (PreToolUse) に対象を決めない: commit の種類 (stage・pathspec・-a・-i・-p) ごとに読む木が違い、`git add … && git commit` では
#    まだ揃っていない時点を読む。commit の後に HEAD の commit の差分 (実際に入った中身) を読む。判定は lib/dupscan_inject.py の commit_main。
#    見る repo は commit の呼び出しの行き先 (`cd` / `-C`。どちらも無ければ cwd) だけ。cwd を無条件に足さない (別の repo への `git -C` の commit で、
#    cwd 側の新しい commit (他のセッションのものもありうる) を出す)。そのうち reflog の先頭が 2 分以内の commit の repo だけを見る。同じ commit は 1 回だけ見る
# 🚨 heredoc の本文を落としてから判定しない (lib/hook_io.sh の hook_strip_heredocs)。コメントの中の `<<EOF` を heredoc の始まりと読み、本物の
#    `git commit` の行まで落とす。本文に書いた `git -C … commit` を呼び出しと読む誤りは、その repo の reflog が新しいときだけ表に出るので受け入れる
# 🚨 足した定義そのものは候補から除く (commit の後は dupscan index が足した定義も読む)
# 検出しない形 (silent): merge の commit・commit 以外で HEAD を作る操作 (cherry-pick・rebase・am)・2 分より前の commit・
#   検出器が commit と読まない起動 (変数・alias・sh -c の中。lib/git_cmd_detect.sh の冒頭)・Edit の時点で引いた名前 (印を共有するので繰り返さない)・
#   20 KB (バイト) を超えるコマンド (長い message を heredoc で渡す commit も。検出器の走査が文字単位で、10 秒の timeout を越える)・
#   実行されなかった commit (`false && git commit`・`--dry-run`) や heredoc の本文の commit で、行き先の repo に 2 分以内の別の commit があるとき (その commit を出す)・
#   検出器が行き先を読み違える形 (`cd B | cat; git commit` は B を見る。lib/git_cmd_detect.sh の冒頭)・移動と大きな編集を同じ commit でして、
#   git が移動と判定しない (類似度 50% 未満) ファイル (残した関数も新しいものと読む)・HOME が無いときの `~` (空として展開する)・
#   heredoc の本文の `cd` (後ろの本物の commit の行き先を変える)・heredoc の本文の `git -C B commit` の B が 2 分以内に同じ名前の関数を足しているとき
#   (B を先に引いて名前の印を付け、本物の commit の候補を出さない)。どれも heredoc の本文を構文として読まない限り直らない
# 脅威モデル: 止めたいのは「Bash で足した関数を、push の前に一度も知らせない」ことと「今の commit でないものを知らせる」こと。
#   知らせるだけなので、取りこぼしは今までどおり (Write / Edit の入口と定期の監査が残る)、誤りは候補を読んで無視すれば済む。シェルの完全な解析は目指さない
# 計測: lib の write_log が claude-dupscan-inject.log に入口 commit の行を足す (scripts/dupscan_usage_report.py が数える)

set -uo pipefail
unset CDPATH
# HOME が無い環境でも set -u で落とさない (検出器が `~` の展開で HOME を読む)
export HOME="${HOME-}"

# 実験用の opt-out: DUPSCAN_INJECT=off の run は注入しない (Write / Edit の入口と同じ)
[ "${DUPSCAN_INJECT:-}" = off ] && exit 0
command -v python3 >/dev/null 2>&1 || exit 0
command -v git >/dev/null 2>&1 || exit 0
command -v dupscan >/dev/null 2>&1 || exit 0

here="$(cd "$(dirname "$(readlink -f "$0")")" && pwd -P)"
# shellcheck source=_claude/hooks/lib/hook_io.sh
. "$here/lib/hook_io.sh" 2>/dev/null || exit 0
hook_read_bash_command || exit 0
# 全部の Bash の呼び出しで走るので、検出器の文字単位の走査の前に安く抜ける。長さはバイトで数える (多バイトの字が多いと、字数が上限の内でも走査が 10 秒を越える)
case "$HOOK_CMD" in *commit*) ;; *) exit 0 ;; esac
bytes=$(LC_ALL=C; printf '%s' "${#HOOK_CMD}")
[ "$bytes" -le 20000 ] || exit 0
# shellcheck source=_claude/hooks/lib/git_cmd_detect.sh
. "$here/lib/git_cmd_detect.sh" 2>/dev/null || exit 0
git_cmd_invokes "$HOOK_CMD" commit || exit 0

# 行き先が空の行は cwd (git_cmd_invokes の約束)。lib には `.` で渡す。
# here-string (<<<) で読まない: 各行が改行で終わるので、here-string の足す改行が空の行 (= cwd) を 1 つ増やし、別の repo への commit でも cwd を見る
dirs=()
while IFS= read -r d; do
  dirs+=("${d:-.}")
done < <(printf '%s' "$GIT_CMD_TARGET_DIRS")

# exec しない: lib の読み込み・構文の失敗も rc=0 で黙らせる
printf '%s' "$HOOK_INPUT" | python3 -I -B "$here/lib/dupscan_inject.py" commit "$here/lib" ${dirs[@]+"${dirs[@]}"} || true
exit 0
