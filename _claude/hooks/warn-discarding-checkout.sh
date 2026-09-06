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
# permissionDecision を返さないので、**許可の判断は一切変えない** — additionalContext だけを足す。
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
#     出す側の誤り | 引用符・heredoc の中に書かれた文字列 … 偽陽性。deny でないので害は注意 1 行
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
git_scan() {
  SUBCMD=""; GIT_C=""; ARGS=""
  local seen_git=0 skip_next=0 tok
  for tok in $1; do
    if [ "$skip_next" -eq 1 ]; then
      [ -n "$GIT_C" ] || GIT_C="$tok"
      skip_next=0
      continue
    fi
    if [ "$seen_git" -eq 0 ]; then
      case "$tok" in
        git | */git) seen_git=1 ;;
      esac
      continue
    fi
    if [ -n "$SUBCMD" ]; then ARGS="$ARGS $tok"; continue; fi
    case "$tok" in
      -C) skip_next=1 ;;
      -c) skip_next=1 ;;      # -c k=v の値は読み飛ばすだけ (GIT_C には入れない)
      -C*) GIT_C="${tok#-C}" ;;
      --git-dir=* | --work-tree=* | --namespace=* | --exec-path=* | --config-env=*) ;;
      --no-pager | --paginate | -p | --bare | --literal-pathspecs | --no-optional-locks) ;;
      -*) ;;                  # 知らないグローバルオプションは読み飛ばす (取りこぼすより出す側へ)
      *) SUBCMD="$tok" ;;
    esac
  done
}

# 破棄するかの判定。**ここが唯一の出典** (枝ごとに違う書き方をしない)。
#   捨てる:   checkout <何か>            … <branch> と <path> は静的に区別できないので出す側へ倒す
#             checkout -p / -f / --force / . / -- <path>
#             restore <path> / restore --worktree|-W …
#   捨てない: checkout -b|-B|--orphan <new>  … 新しいブランチを作るだけ
#             restore --staged|-S だけ        … index を戻すだけで作業ツリーの内容は残る
discards=0
target_c=""
while IFS= read -r seg; do
  case "$seg" in *checkout* | *restore*) ;; *) continue ;; esac
  git_scan "$seg"
  case "$SUBCMD" in
    checkout)
      case " $ARGS " in
        "  ") ;;                                   # 引数なし = 状態を出すだけ
        *" -b "* | *" -B "* | *" --orphan "*) ;;   # 新規ブランチを作るだけ
        *) discards=1 ;;
      esac
      ;;
    restore)
      local_staged=0; local_worktree=0
      for a in $ARGS; do
        case "$a" in
          --staged) local_staged=1 ;;
          --worktree) local_worktree=1 ;;
          --*) ;;
          -[A-Za-z]*)                              # 短縮の束ね (-S / -W / -SW)
            case "$a" in *S*) local_staged=1 ;; esac
            case "$a" in *W*) local_worktree=1 ;; esac
            ;;
        esac
      done
      if [ "$local_staged" -eq 1 ] && [ "$local_worktree" -eq 0 ]; then : ; else discards=1; fi
      ;;
  esac
  [ "$discards" -eq 1 ] && [ -z "$target_c" ] && target_c="$GIT_C"
done <<EOF_SEGS
$(printf '%s' "$cmd" | tr ';&|' '\n')
EOF_SEGS
[ "$discards" -eq 1 ] || exit 0

cwd=$(printf '%s' "$input" | jq -r '.cwd // ""' 2>/dev/null)
[ -n "$cwd" ] && [ -d "$cwd" ] || cwd="$PWD"

# 🚨 **見るのは「捨てられる側の repo」であって cwd ではない** (敵対的レビュー P2-1)。
# `git -C <path> checkout -- x` は cwd と別の repo を触る。初版は cwd 側の dirty を見ていたので、
#   clean な worktree から dirty な本体を触る → 沈黙 (今まさに消える場面で黙る)
#   dirty な worktree から clean な本体を触る → 無関係な一覧を「消えるもの」として見せる
# の両方向に壊れていた。しかもこの repo の規範 (commit-with-pathspec.md) は
# 「本体への操作は `git -C <本体の絶対パス>` で対象を明示する」と**推奨している**ので、
# 推奨形がそのまま盲点になっていた。
scan_dir="$cwd"
if [ -n "$target_c" ]; then
  case "$target_c" in
    /*) scan_dir="$target_c" ;;
    *) scan_dir="$cwd/$target_c" ;;
  esac
  [ -d "$scan_dir" ] || scan_dir="$cwd"
fi

command -v git >/dev/null 2>&1 || exit 0
git -C "$scan_dir" rev-parse --show-toplevel >/dev/null 2>&1 || exit 0

# 🚨 「検査できなかった」を「変更なし」にしない (§2)。status が落ちたら判定不能としてその旨を出す。
# 初版はここで exit 0 しており、**直上のコメントが約束したことを実装がやっていなかった**
# (敵対的レビュー P2-7 が index を壊して実測)。
if ! dirty=$(git -C "$scan_dir" status --porcelain 2>&1); then
  jq -n --arg d "$scan_dir" --arg e "$dirty" '{hookSpecificOutput: {hookEventName: "PreToolUse",
    additionalContext: ("🚨 未コミットの変更があるかを判定できなかった (git status が失敗): " + $d
      + "\n" + $e + "\n捨てて困る変更が無いかを自分で確かめてから進めること。")}}'
  exit 0
fi
[ -n "$dirty" ] || exit 0   # 変更が無ければ黙る (毎回出すとノイズになり読まれなくなる)

count=$(printf '%s\n' "$dirty" | grep -c '^' )
body=$(printf '%s\n' "$dirty" | head -20)
more=""
[ "$count" -gt 20 ] && more="
  … 他 $((count - 20)) 件"

msg="🚨 未コミットの変更が ${count} 件ある状態で、それを捨てうる git コマンドを実行しようとしている。
**このコマンドが消すのは変異だけとは限らない。** 直前にレビュー指摘を直した / 実装を書いた
なら、その修正も一緒に消える (規範: _claude/rules/mutation-verify-new-tests.md の「復元の作法」)。

意図した復元なら、そのまま進めてよい。そうでないなら **先に commit する** (WIP でよい)。
新規 (untracked) ファイルは checkout では戻らないので、消えると復元できない。

--- 未コミットの変更 (${scan_dir}) ---
${body}${more}"

jq -n --arg c "$msg" \
  '{hookSpecificOutput: {hookEventName: "PreToolUse", additionalContext: $c}}'
