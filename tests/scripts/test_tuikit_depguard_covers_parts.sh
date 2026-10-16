#!/usr/bin/env bash
# tuikit の全 package が、depguard の 2 つの部品のリスト (no-framework-in-parts / parts-pure) のどちらかに入っているか、
# 下の「外す理由つきの表」に載っていることを pin する (issue 790)。
#
# なぜ: リストは .golangci.yml に手で並べていて、package を足すと漏れる。漏れた package は bubbletea / os を import しても lint が
# 通る (issue 785 で足した hintfit が漏れ、敵対レビューで見つかった。gutter・spinner・foreground も以前から漏れていた)。
# 脅威モデル: package を足してリストに足し忘れる・外す表に古い名前が残る。
# 検出しない形: YAML の別の書き方 (flow style・アンカー)。パターンは `"**/<名前>/**"` と `"!**/tuikit/<名前>/**"` の形だけを読む
# (今の 2 つのリストはこの形だけ。別の形を足したら、ここも直す)。パターンの意味 (どのファイルに当たるか) は golangci 自身が決めるので、
# ここは「名前が載っているか」だけを見る
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR/src/tuikit" || exit 1
yml=.golangci.yml

# 外す理由つきの表 (先頭の名前で引く。入れ子 (examples/toast 等) も先頭の名前で外す)
excl_framework='caret:bubbletea の View.Cursor を組むのが仕事
framebench:部品ではなく計測の道具 (ultraviolet で bubbletea のレンダラを再現する)
examples:デモ (bubbletea を使ってよい)'
# cmd/termwidth は no-framework-in-parts の **/termwidth/** に当たる (bubbletea を使わないので当たってよい)。parts-pure は !cmd で外す
excl_pure="$excl_framework
cmd:シェルから部品を呼ぶ入口のコマンド (引数と標準出力が仕事)
editor:外部のエディタを起こすのが仕事
widthenv:環境変数を読むのが仕事
termcolor:端末 (tmux) に機能を聞くのが仕事
foreground:端末の前面を確かめ、子へ端末を渡すのが仕事 (os / os/exec / os/signal)"

# list_names <リスト名> <+|!> — 指定のリストの files の名前 (+ は当てる、! は外す)
list_names() {
  awk -v L="$1:" -v sign="$2" '
    $1 == L { inlist = 1; next }
    inlist && $1 == "deny:" { exit }
    inlist && sign == "+" && match($0, /- "\*\*\/[^\/*]+\/\*\*"/) { s = substr($0, RSTART, RLENGTH); sub(/- "\*\*\//, "", s); sub(/\/\*\*"/, "", s); print s }
    inlist && sign == "!" && match($0, /- "!\*\*\/tuikit\/[^\/*]+\/\*\*"/) { s = substr($0, RSTART, RLENGTH); sub(/- "!\*\*\/tuikit\//, "", s); sub(/\/\*\*"/, "", s); print s }
  ' "$yml"
}

if ! pkgs=$(GOWORK=off go list -f '{{.ImportPath}}' ./... 2>&1); then
  printf '✗ go list が失敗した (判定できない): %s\n' "$pkgs"; exit 1
fi
mod=$(GOWORK=off go list -m)
rels=$(printf '%s\n' "$pkgs" | sed -n "s|^$mod/||p")
n=$(printf '%s\n' "$rels" | grep -c .)
fail=0

for list in no-framework-in-parts parts-pure; do
  if [ "$list" = no-framework-in-parts ]; then excl=$excl_framework; else excl=$excl_pure; fi
  plus=$(list_names "$list" +); minus=$(list_names "$list" !)
  # 較正: 必ず入っているはずの termwidth が読めなければ、読み取りが壊れている
  printf '%s\n' "$plus" | grep -qx termwidth || { printf '✗ %s: files から termwidth が読めない (読み取りが壊れている)\n' "$list"; exit 1; }
  while IFS= read -r rel; do
    [ -n "$rel" ] || continue
    top=${rel%%/*}
    listed=0
    IFS=/ read -ra parts <<< "$rel"
    for c in "${parts[@]}"; do printf '%s\n' "$plus" | grep -qxF -- "$c" && listed=1; done
    printf '%s\n' "$minus" | grep -qxF -- "$top" && listed=0
    excluded=0
    printf '%s\n' "$excl" | cut -d: -f1 | grep -qxF -- "$top" && excluded=1
    if [ "$listed" -eq 0 ] && [ "$excluded" -eq 0 ]; then
      printf '  ✗ %s: %s がリストにも外す表にも無い (.golangci.yml の %s の files に足すか、この検査の外す表に理由つきで載せる)\n' "$list" "$rel" "$list"; fail=1
    elif [ "$listed" -eq 1 ] && [ "$excluded" -eq 1 ]; then
      printf '  ✗ %s: %s はリストに入っているのに外す表にもある (どちらかにする)\n' "$list" "$rel"; fail=1
    fi
  done <<< "$rels"
  # 外す表の古い名前 (もう無い package) を残さない
  while IFS= read -r name; do
    printf '%s\n' "$rels" | cut -d/ -f1 | grep -qxF -- "$name" || { printf '  ✗ %s: 外す表の %s に当たる package が無い (表から消す)\n' "$list" "$name"; fail=1; }
  done < <(printf '%s\n' "$excl" | cut -d: -f1)
done

[ "$n" -ge 15 ] || { printf '✗ tuikit の package が %d しか見つからない (発見が壊れている)\n' "$n"; exit 1; }
[ "$fail" -eq 0 ] || { printf '✗ tuikit の depguard の部品のリストに漏れがある (%d package を検査)\n' "$n"; exit 1; }
printf '✓ tuikit の %d package が depguard の部品のリスト (no-framework-in-parts / parts-pure) か外す表に載っている\n' "$n"
