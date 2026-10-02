#!/usr/bin/env bash

# PreToolUse(Bash) フック: 未コミットの変更を捨てる形の git checkout / git restore の直前に、
# **何が消えるか**を注意として注入する (issue 297)。
#
# なぜ: 変異検証の復元 (`git checkout -- <path>`) が、同じパスにある未コミットの修正を一緒に
# 捨てる。規範は既にあり (mutation-verify-new-tests.md の「復元の作法」が発動点まで名指しで
# 書いている) 、それでも 2026-09-06 の 1 セッションで 3 回踏んだ。いずれも「レビュー指摘を
# 直した直後」= 必ず未コミットのタイミング。規範を読んでいる状態で踏むので、残る手段は機械しかない。
#
# 🚨 **deny にしない。** 変異の復元は正当な用途で、deny にすると変異検証が回らなくなる
# (tmux の deny 型とは別。あちらは常に誤りだが、こちらは正当な場合がある)。
# **確実に捨てる形 (`checkout --` / `.` / `-f` / `-p`、`restore` の作業ツリー) だけ `ask` で人に選ばせる** (issue 623。
# 2026-10-02 にユーザーが選んだ。事故 3 件はすべて `checkout --`)。対話 (default mode) では、許可済みのツールでも標準の確認に
# 理由 (消える一覧) が出る。`claude -p` では default / bypassPermissions / acceptEdits / auto のどれでも拒否になり、理由がモデルに届く
# (claude 2.1.287 で実測。待ち続けはしない)。対話の auto mode と、サブエージェントの中での見え方は未実測。
# ブランチの切り替えかもしれない曖昧な `checkout <x>` は、偽陽性のたびに人を止めないよう、今までどおり additionalContext の注意だけ。
# pro-con の役 (PG 等) はユーザーの settings の hook が走らないので、どちらも届かない (431。今までと同じ)。
#
# 🚨 **脅威モデルと射程** (adversarial-review-own-safeguards §8):
#   - 止めるのは「未コミットの変更がある状態で、破棄しうる checkout / restore を打つ」形だけ
#   - 判定は粗く倒す: **どのパスが消えるかを静的に解決しない**。変数・glob・省略形があるので、
#     解決しようとすると取りこぼす。対象 repo が dirty かどうかで判定し、消えうる一覧を見せる
#   - `git checkout <何か>` は**出す**。`git checkout foo.go` (捨てる) と `git checkout foo`
#     (ブランチ切替) は静的に区別できないので、宣言したバイアスどおり出す側へ倒す
#
#   **検出しない (2026-09-06 に実測して確定。着手前に書いた版は意図であって射程ではない)**:
#     silent | git checkout -b / -B / --orphan / 引数なし   … 新規ブランチを作るだけ・状態表示
#     silent | git restore --staged | -S のみ               … index を戻すだけで内容は残る
#     silent | git reset --hard / git clean -fd / git switch --discard-changes / git stash
#            … 🚨 **どれも未コミットを失うが、この hook は見ない**。事故 3 件が全部
#              `checkout --` だったので範囲を絞った。「この hook があるから安全」ではない
#     silent | git co -- x (エイリアス)                     … git の設定を読まないので分からない
#     silent | git $sub -- x (サブコマンドが変数)           … 静的検査の限界
#     silent | rm -rf x                                     … git 以外の破棄は範囲外
#     silent | Bash ツール以外の経路 (人の手入力 / スクリプトの内部 / glogx の Go からの呼び出し)
#            … hook が見えるのは Claude が Bash で動かしたものだけ (next-claim-push.sh と同じ限界)
#     出す側の誤り | 引用符・heredoc の中に書かれた文字列 (`;` `&` `|` で segment が分かれたとき) … 偽陽性。注意 1 行だけで ask にはしない
#     silent | 1 つの segment の 2 つ目以降の git (`git commit -m "… git checkout -- x …"`) … 最初の git だけを見る
#     silent | `sh -c "git checkout -- x"` / `echo 'git checkout -- x'` … 語が `"git` / `'git` になり git として拾わない
#     silent | git restore --staged --work x (--worktree の省略形) … 省略形は知らない
#
#   **ask と注意の分かれ目 (2026-10-02 に実物へ流して確定。issue 623)**:
#     ask は**コマンド全体が 1 つの単純な git コマンド**で、**対象の repo に自信がある**ものだけ: `;` `&` `|` 改行・`'` `"` `` ` `` `$(`
#     `<<` `#` `\` `>(` `<(` が 1 つも無く、git が先頭の語 (前置きの `VAR=値` は可) で、cd / pushd / popd / GIT_DIR= / GIT_WORK_TREE= /
#     --git-dir / --work-tree が無く、-C が 1 つ以下でその dir が在り、その repo の作業ツリー側に追跡中の変更があるもの (untracked だけ・
#     index にだけ変更があるなら注意)。その中の
#     checkout -- x / HEAD -- x / . / -f / --force <b> / -p / --patch / 束ね (-fq / -qf / -pf)、restore x / --worktree / -W / -SW、`(git …)`
#     ask だが捨てない | git checkout -- (後ろが空) / git restore (パス無しで git がエラー) / git restore --stag x (--staged の省略形) /
#            | 単体の `(git checkout -- x)` … 害は確認 1 回
#     注意だけ | checkout <x> (ファイルかブランチか区別できない) / --forc / --pat (省略形) / --theirs x / --ours x / -m x / --pathspec-from-file= / ./x.go
#            | / 単純でないコマンドの中の確実な形 (引用符・heredoc・コメント・`$(…)`・行継続を含むもの。字句で取り除く近似は
#            |   2 周続けて破られたのでやめた。注意だけなのは今までと同じ) / 対象の repo に自信が無い形 (上の cd 等を含むもの) /
#            | 複数の segment を持つコマンドの中の確実な形 (`git status && git checkout -- x` 等) / untracked だけが dirty
#     silent | `x=$(git checkout -- x)` / `` `git …` `` … `$(git` は git として拾わない (前置きの VAR=$(git rev-parse) を拾って黙らないため。旧と同じ)
#     silent | git checkout -h / --help、git restore -h / --help (サブコマンドの直後の語) … help を出すだけ
#   **対象の repo**: 捨てる segment ごとに、その `-C <dir>` (引用符・`~`・`$HOME` を外す) か cwd。注意はそれらの dir を全部見て出す。
#     cd は追わない (追うと、後ろの cd に破棄の対象を奪われて黙った。敵対的レビュー 3 周目)。`-c k=v` の値は dir として読まない
#   - 上記は review と人の目の責務
#
#   🚨 **この一覧は実装後に実物へ 22 形を流して確定させた**。初版は「実物と突き合わせた」と
#   書きながら突き合わせておらず、未宣言のまま検出しない形が 8 つあった (敵対的レビューが実測)。
#   §8 が名指しで警告している「ヘッダが『守られている』と読ませる嘘」そのものだった。
#   **判定を変えたら、この一覧も同じ commit で実測し直すこと。**

set -uo pipefail

input=$(cat)

# jq が無い環境では静かに諦める (既存 hook と同じ振る舞い)
command -v jq >/dev/null 2>&1 || exit 0

cmd=$(printf '%s' "$input" | jq -r '.tool_input.command // ""' 2>/dev/null) || exit 0
[ -n "$cmd" ] || exit 0

# 🚨 性能ガード (deny-bare-tmux-kill.sh の教訓)。PreToolUse には timeout があり、大きな
# heredoc を含む呼び出しで hook が殺されると **stdout 0 byte = 注意が 1 byte も出ない**。
# 対象語を含まないものはここで即通して、下の git 呼び出しに入れない。
case "$cmd" in
  *checkout* | *restore*) ;;
  *) exit 0 ;;
esac

# 🚨 判定は「git のサブコマンドを 1 回だけ正規化して取り出す」形に寄せる (敵対的レビュー)。
# 初版は checkout / restore / `git -C ` を**枝ごとに違う厳しさのリテラル一致**で 3 回書いており、
#   git --no-pager checkout -- x     → 沈黙 (グローバルオプションを挟むと当たらない)
#   git -C <path> restore f.go       → 沈黙 (restore の枝だけリテラルが厳しかった)
#   git restore --staged -W x.go     → 沈黙 (--worktree の短縮形を知らない)
# と非対称に穴が開いていた。正規化すればこの 3 つ + 空白 2 つの形がまとめて閉じる。
#
# git_scan <segment>: グローバルオプションを読み飛ばして
#   SUBCMD  = 最初の非オプション語 (checkout / restore / …)
#   GIT_C   = -C で指定された作業ディレクトリ (無ければ空)
#   ARGS    = サブコマンド以降の引数 (空白区切り)
# を設定する。見つからなければ SUBCMD は空。
# path_word <語>: 引用符を外し、先頭の ~ を $HOME にする (`git -C "~/x"` / `cd ~/x` を対象の repo として読む)
path_word() {
  local w="$1"
  w="${w#[\"\']}"; w="${w%[\"\']}"
  case "$w" in \~ | \~/*) w="$HOME${w#\~}" ;; esac
  # shellcheck disable=SC2016 # 字句としての $HOME を展開する
  case "$w" in '$HOME' | '$HOME/'*) w="$HOME${w#\$HOME}" ;; '${HOME}' | '${HOME}/'*) w="$HOME${w#\$\{HOME\}}" ;; esac
  printf '%s' "$w"
}

git_scan() {
  SUBCMD=""; GIT_C=""; ARGS=""; GIT_FIRST=1; C_COUNT=0
  local seen_git=0 skip_next=0 skip_value=0 tok
  for tok in $1; do
    if [ "$skip_next" -eq 1 ]; then
      [ -n "$GIT_C" ] || GIT_C="$(path_word "$tok")"
      skip_next=0
      continue
    fi
    if [ "$skip_value" -eq 1 ]; then skip_value=0; continue; fi
    if [ "$seen_git" -eq 0 ]; then
      case "$tok" in
        git | */git | '(git') seen_git=1 ;;   # サブシェルの中も拾う。`$(git` は拾わない (前置きの VAR=$(git rev-parse …) を拾って黙る)
        [A-Za-z_]*=*) ;;                                            # 前置きの VAR=値 は先頭の語に数えない
        *) GIT_FIRST=0 ;;                                           # git が segment の先頭の語でない (echo git … 等)
      esac
      continue
    fi
    if [ -n "$SUBCMD" ]; then ARGS="$ARGS $tok"; continue; fi
    case "$tok" in
      -C) skip_next=1; C_COUNT=$((C_COUNT + 1)) ;;
      -c) skip_value=1 ;;     # -c k=v の値は読み飛ばすだけ (GIT_C には入れない)
      -C*) [ -n "$GIT_C" ] || GIT_C="$(path_word "${tok#-C}")"; C_COUNT=$((C_COUNT + 1)) ;;
      --git-dir=* | --work-tree=* | --namespace=* | --exec-path=* | --config-env=*) ;;
      --no-pager | --paginate | -p | --bare | --literal-pathspecs | --no-optional-locks) ;;
      -*) ;;                  # 知らないグローバルオプションは読み飛ばす (取りこぼすより出す側へ)
      *) SUBCMD="$tok" ;;
    esac
  done
}

# 破棄するかの判定。**ここが唯一の出典** (枝ごとに違う書き方をしない)。
#   確実に捨てる (ask):  checkout -p / --patch / -f / --force / . / -- <path>
#                        restore <path> / restore --worktree|-W …
#   曖昧 (注意だけ):     checkout <何か>  … <branch> と <path> は静的に区別できないので出す側へ倒す
#   捨てない:            checkout -b|-B|--orphan <new>  … 新しいブランチを作るだけ
#                        restore --staged|-S だけ        … index を戻すだけで作業ツリーの内容は残る
# 1 つのコマンドに確実な形が 1 つでもあれば ask にする (人に選ばせる側へ倒す)。
# 🚨 **ask にするのは「単純なコマンド」だけ**: コマンド全体に引用符・heredoc・コメント・コマンド置換・行継続が 1 つも無いもの
#   (issue 623 の敵対的レビュー 2 周)。commit message や `gh … --body '…'` に `git checkout -- x` と書いただけで確認を出す
#   (`claude -p` では commit ごと拒否する) と、注意 1 行で済んでいた偽陽性が人とコマンドを止める害に変わる。
#   引用符・heredoc を字句で取り除いて判定する近似は、2 周続けて別の入力で破られた (コメント・`$'…'`・`<<'MSG-EOF'`・2 乗の遅さ) ので
#   やめ、「判定できる形だけ ask、それ以外は注意 (今までどおり)」に軸を移した。複雑なコマンドの中の確実な形は注意に落ちるが、それは現状維持
simple=1
case "$cmd" in
  *\'* | *\"* | *\`* | *\$\(* | *\<\<* | *\#* | *\\* | *\>\(* | *\<\(*) simple=0 ;;
esac
# 捨てる segment ごとに「ask の資格<TAB>-C の値」を 1 行ずつ集める (SEGS。-C の値は空がありうるので後ろに置く: IFS がタブだと
# 先頭の空の欄は詰められる)。対象の repo を segment ごとに持つのは、コマンドに 1 つしか
# 持たないと、後ろの `-C <dirty>` の破棄を見落として黙る / 確実な破棄が clean な別の repo を指していても cwd の一覧で ask になる、の
# 両方が起きたため (敵対的レビュー 4 周目)。cd は追わない (追うと、後ろの cd に最初の破棄の対象を奪われて黙った。3 周目)。
# ask の資格: 確実な形で、git が segment の先頭の語 (`echo git checkout -- x` のような引数の中の字句で止めない)、-C が 1 つ以下
SEGS=""
while IFS= read -r seg; do
  case "$seg" in *checkout* | *restore*) ;; *) continue ;; esac
  git_scan "$seg"
  # help を出すだけ: `--` より前に -h / --help があれば捨てない (`--` の後ろはパスなので見ない)
  is_help=0
  for a in $ARGS; do
    case "$a" in --) break ;; -h | --help) is_help=1; break ;; esac
  done
  [ "$is_help" -eq 0 ] || continue
  seg_discards=0; is_definite=0
  case "$SUBCMD" in
    checkout)
      case " $ARGS " in
        "  ") ;;                                   # 引数なし = 状態を出すだけ
        *" -b "* | *" -B "* | *" --orphan "*) ;;   # 新規ブランチを作るだけ
        *" -- "* | *" . "* | *" -f "* | *" --force "* | *" -p "* | *" --patch "*) seg_discards=1; is_definite=1 ;;
        *)
          seg_discards=1
          # 束ねた短縮形 (-fq / -qf / -pf) も確実な形。b / B は引数 (新しいブランチの名前) を取るので、それより後ろの文字は
          # 名前であってオプションではない (`-bfix` は fix を作るだけ。`-fbx` は f があるので捨てる)
          for a in $ARGS; do
            case "$a" in
              --*) ;;
              -*) case "${a%%[bB]*}" in *[fp]*) is_definite=1 ;; esac ;;
            esac
          done
          ;;
      esac
      ;;
    restore)
      local_staged=0; local_worktree=0
      for a in $ARGS; do
        case "$a" in
          --staged) local_staged=1 ;;
          --worktree) local_worktree=1 ;;
          --*) ;;
          -[A-Za-z]*)                              # 短縮の束ね (-S / -W / -SW)。s は引数 (source) を取るので、それより後ろは値
            case "${a%%s*}" in *S*) local_staged=1 ;; esac
            case "${a%%s*}" in *W*) local_worktree=1 ;; esac
            ;;
        esac
      done
      if [ "$local_staged" -eq 1 ] && [ "$local_worktree" -eq 0 ]; then : ; else seg_discards=1; is_definite=1; fi
      ;;
  esac
  [ "$seg_discards" -eq 1 ] || continue
  eligible=0
  [ "$is_definite" -eq 1 ] && [ "$GIT_FIRST" -eq 1 ] && [ "$C_COUNT" -le 1 ] && eligible=1
  SEGS="$SEGS$eligible	$GIT_C
"
done <<EOF_SEGS
$(printf '%s' "$cmd" | tr ';&|' '\n')
EOF_SEGS
[ -n "$SEGS" ] || exit 0

# 🚨 ask は判定と対象の repo の両方に自信があるときだけ (敵対的レビュー 3 周目 P2-2)。コマンド全体が単純でない、または
# 作業ディレクトリ・repo を変える語 (cd / pushd / popd / GIT_DIR= / GIT_WORK_TREE= / --git-dir / --work-tree) があれば、
# どの segment も ask の資格を失う (cwd に倒した一覧で人を止めない。`claude -p` では正当な復元まで拒否する)
cmd_eligible=$simple
# 🚨 ask はコマンド全体が 1 つの git コマンドのときだけ (`;` `&` `|` 改行を含まない)。事故の形 (`git checkout -- <path>` 単体) は
# これで足り、複数の segment の組み合わせで対象の repo を取り違える形 (敵対的レビュー 3・4 周目) が ask に出ない
case "$cmd" in *';'* | *'&'* | *'|'* | *$'\n'*) cmd_eligible=0 ;; esac
flat=" $(printf '%s' "$cmd" | tr '\n\t;&|(){}' '          ') "
case "$flat" in
  *" cd "* | *" pushd "* | *" popd "* | *GIT_DIR=* | *GIT_WORK_TREE=* | *--git-dir* | *--work-tree*) cmd_eligible=0 ;;
esac

cwd=$(printf '%s' "$input" | jq -r '.cwd // ""' 2>/dev/null)
[ -n "$cwd" ] && [ -d "$cwd" ] || cwd="$PWD"
command -v git >/dev/null 2>&1 || exit 0

# 🚨 **見るのは「捨てられる側の repo」であって cwd ではない** (敵対的レビュー P2-1)。
# `git -C <path> checkout -- x` は cwd と別の repo を触る。初版は cwd 側の dirty を見ていたので、
#   clean な worktree から dirty な本体を触る → 沈黙 (今まさに消える場面で黙る)
#   dirty な worktree から clean な本体を触る → 無関係な一覧を「消えるもの」として見せる
# の両方向に壊れていた。しかもこの repo の規範 (commit-with-pathspec.md) は
# 「本体への操作は `git -C <本体の絶対パス>` で対象を明示する」と**推奨している**ので、
# 推奨形がそのまま盲点になっていた。
# segment の -C を dir に解決し (無い dir は cwd に倒して ask の資格を落とす)、同じ dir は 1 つにまとめる (資格は OR)
resolved=""
while IFS='	' read -r el c; do
  [ -n "$el" ] || continue
  case "$c" in '') d="$cwd" ;; /*) d="$c" ;; *) d="$cwd/$c" ;; esac
  [ -d "$d" ] || { d="$cwd"; el=0; }   # 解決できない対象のまま ask にしない
  [ "$cmd_eligible" -eq 1 ] || el=0
  resolved="$resolved$d	$el
"
done <<EOF_RES
$SEGS
EOF_RES
dirs=$(printf '%s' "$resolved" | awk -F'\t' 'NF == 2 { if (!($1 in m) || $2 > m[$1]) m[$1] = $2 } END { for (k in m) print k "\t" m[k] }' | sort)

# dir ごとに見る。ask は「確実な形の segment 自身の dir に、追跡中の変更があるとき」だけ (untracked は checkout / restore で消えない)。
# 🚨 「検査できなかった」を「変更なし」にしない (§2)。status が落ちたら判定不能としてその旨を出す (資格のある dir なら ask)。
# 初版はここで exit 0 しており、**直上のコメントが約束したことを実装がやっていなかった** (敵対的レビュー P2-7 が index を壊して実測)。
definite=0
blocks=""
failed=""
total=0
while IFS='	' read -r d el; do
  [ -n "$d" ] || continue
  git -C "$d" rev-parse --show-toplevel >/dev/null 2>&1 || continue
  if ! dirty=$(git -C "$d" status --porcelain 2>&1); then
    failed="$failed
$d: $(printf '%s' "$dirty" | head -c 500)"
    [ "$el" -eq 1 ] && definite=1
    continue
  fi
  [ -n "$dirty" ] || continue   # 変更が無ければ黙る (毎回出すとノイズになり読まれなくなる)
  # 作業ツリー側 (2 文字目) に変更があるときだけ。index にしか無い変更は index から戻す形では消えない
  if [ "$el" -eq 1 ] && git -C "$d" status --porcelain --untracked-files=no 2>/dev/null | awk 'substr($0, 2, 1) != " " { f = 1 } END { exit !f }'; then definite=1; fi
  count=$(printf '%s\n' "$dirty" | grep -c '^')
  total=$((total + count))
  more=""
  [ "$count" -gt 20 ] && more="
  … 他 $((count - 20)) 件"
  blocks="$blocks

--- 未コミットの変更 ($d) ---
$(printf '%s\n' "$dirty" | head -20)$more"
done <<EOF_DIRS
$dirs
EOF_DIRS

ASK_TAIL="
--- 確認を出した理由 ---
人の確認を求めた。拒否されたら、別のコマンド (git reset --hard / git stash / git show で上書き 等) で同じことをしない。
先に commit するか、何を残したいかを人に聞く。"
# emit <本文>: 確実に捨てる形なら ask (確認に出る理由)。ask でもモデルには同じ本文を additionalContext で渡す
# (確認の理由は人に見せるもので、人が許可したとき・拒否したときにモデルが何が消えるかを知る手段にならないため)。
# 曖昧な形なら注意 (additionalContext) だけを足し、許可の判断は変えない
emit() {
  if [ "$definite" -eq 1 ]; then
    jq -n --arg c "$1$ASK_TAIL" '{hookSpecificOutput: {hookEventName: "PreToolUse", permissionDecision: "ask", permissionDecisionReason: $c, additionalContext: $c}}'
  else
    jq -n --arg c "$1" '{hookSpecificOutput: {hookEventName: "PreToolUse", additionalContext: $c}}'
  fi
}

failed_msg=""
[ -n "$failed" ] && failed_msg="
🚨 未コミットの変更があるかを判定できなかった (git status が失敗):$failed
捨てて困る変更が無いかを確かめてから進めること。"
[ -n "$blocks$failed_msg" ] || exit 0

if [ -z "$blocks" ]; then
  emit "${failed_msg#?}"
  exit 0
fi

msg="🚨 未コミットの変更が ${total} 件ある状態で、それを捨てうる git コマンドを実行しようとしている。
**このコマンドが消すのは変異だけとは限らない。** 直前にレビュー指摘を直した / 実装を書いた
なら、その修正も一緒に消える (規範: _claude/rules/mutation-verify-new-tests.md の「復元の作法」)。

意図した復元なら進めてよい。そうでないなら止めて **先に commit する** (WIP でよい)。
新規 (untracked) ファイルは checkout では戻らないので、消えると復元できない。${blocks}${failed_msg}"

emit "$msg"
