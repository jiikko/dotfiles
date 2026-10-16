#!/usr/bin/env bash
# tuikit の全 package が、depguard の 2 つの部品のリスト (no-framework-in-parts / parts-pure) のどちらかに入っているか、
# 下の「外す理由つきの表」に載っていることを pin する (issue 790)。
#
# なぜ: リストは .golangci.yml に手で並べていて、package を足すと漏れる。漏れた package は bubbletea / os を import しても lint が
# 通る (issue 785 で足した hintfit が漏れ、敵対レビューで見つかった。gutter・spinner・foreground も以前から漏れていた)。
# 脅威モデル: package を足してリストに足し忘れる・外す表に古い名前が残る・リストの行をコメントアウトして外す・知らない形の項目を足す。
# リストの files の項目は、知っている 3 つの形 (`"**/<名前>/**"` / `"!**/tuikit/<名前>/**"` / `"!**/*_test.go"`) だけを読み、それ以外の項目が
# 1 つでもあれば「判定できない」として落とす (黙って読み飛ばすと、別の形の否定で depguard が外れても緑になった。敵対レビュー)。
# 読み取りは字句 (字下げと行の形) で、YAML のパーサではない。files の中の行は「- <値>」の形だけを読み、それ以外 (「-」の後で改行した項目など)
# と、2 つ目以降の YAML 文書 (golangci は最初の文書しか読まない) は「判定できない」として落とす。
# 検出しない形 (どれも較正の termwidth か「知らない形」で赤に倒れる側。緑に倒れる形は残していない): YAML の flow style・アンカー・CRLF・
# 項目を files と同じ字下げで書く形。リストの中身以外
# (linters.enable から depguard を消す・deny を空にする・linters.exclusions で depguard を外す・生成ファイルの見出し
# (`// Code generated ... DO NOT EDIT.`) を付ける・`//nolint:depguard`) は見ない。パターンの意味 (どのファイルに当たるか) は golangci 自身が決めるので、
# ここは「名前が載っているか」だけを見る
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR/src/tuikit" || exit 1
yml=.golangci.yml

# 外す理由つきの表。package のパス (tuikit からの相対) そのもので引く (先頭の名前で引くと、外した package の下に足した package も黙って外れた)。
# 末尾の /* は直下の子を全部外す (examples のデモだけに使う)
excl_framework='caret:bubbletea の View.Cursor を組むのが仕事
framebench:部品ではなく計測の道具 (ultraviolet で bubbletea のレンダラを再現する)
examples/*:デモ (bubbletea を使ってよい)'
# cmd/termwidth は no-framework-in-parts の **/termwidth/** に当たる (bubbletea を使わないので当たってよい)。parts-pure は !cmd で外す
excl_pure="$excl_framework
cmd/termwidth:シェルから部品を呼ぶ入口のコマンド (引数と標準出力が仕事)
editor:外部のエディタを起こすのが仕事
widthenv:環境変数を読むのが仕事
termcolor:端末 (tmux) に機能を聞くのが仕事
foreground:端末の前面を確かめ、子へ端末を渡すのが仕事 (os / os/exec / os/signal)
foreground/ptytest:foreground のテストが使う pty の道具 (context / os / os/exec)"

# list_items <リスト名> — linters.settings.depguard.rules.<リスト名>.files の項目 (コメントの行は読まない)。
# YAML の位置 (字下げ 0 / 2 / 4 / 6 / 8) で読む: 字下げを見ないと、rule が rules の外へ出て depguard から消えても、その行を読んで緑になった
# (敵対レビュー。golangci は知らないキーを黙って無視する)
list_items() {
  awk -v L="$1:" '
    /^(---|\.\.\.)/ { if (seen) { print "?文書の区切り (golangci は最初の文書しか読まない): " $0 }; next }
    /^[[:space:]]*(#|$)/ { next }
    { seen = 1 }
    { match($0, /^ */); ind = RLENGTH }
    ind == 0 { lin = ($1 == "linters:"); st = 0; dg = 0; ru = 0; inlist = 0; next }
    ind == 2 { st = (lin && $1 == "settings:"); dg = 0; ru = 0; inlist = 0; next }
    ind == 4 { dg = (st && $1 == "depguard:"); ru = 0; inlist = 0; next }
    ind == 6 { ru = (dg && $1 == "rules:"); inlist = 0; next }
    ind == 8 { inlist = (ru && $1 == L); fl = 0; next }
    inlist && ind == 10 { fl = ($1 == "files:"); next }
    inlist && fl && /^[[:space:]]*- [^ ]/ { sub(/^[[:space:]]*- /, ""); sub(/[[:space:]]+#.*$/, ""); print; next }
    inlist && fl { print "?読めない行: " $0 }  # 「-」の後で改行した項目など。黙って捨てると、その項目が否定でも緑になった (敵対レビュー)
  ' "$yml"
}

# excluded <表> <rel> — rel が外す表に載っているか (パスそのもの、または「親/*」)
excluded() {
  local key
  while IFS= read -r key; do
    [ "$key" = "$2" ] && return 0
    case $key in */\*) [ "${2%/*}" = "${key%/*}" ] && [ "$2" != "${key%/*}" ] && return 0 ;; esac
  done < <(printf '%s\n' "$1" | cut -d: -f1)
  return 1
}

if ! pkgs=$(GOWORK=off go list -f '{{.ImportPath}}' ./... 2>&1); then
  printf '✗ go list が失敗した (判定できない): %s\n' "$pkgs"; exit 1
fi
mod=$(GOWORK=off go list -m)
# module の root の package (tuikit 直下の .go) も集合に入れる (落とすと、root に置いた .go が検査にも depguard にも当たらなかった)
rels=$(printf '%s\n' "$pkgs" | sed -n -e "s|^$mod/||p" -e "s|^$mod\$|.|p")
n=$(printf '%s\n' "$rels" | grep -c .)
fail=0

for list in no-framework-in-parts parts-pure; do
  if [ "$list" = no-framework-in-parts ]; then excl=$excl_framework; else excl=$excl_pure; fi
  plus=""; minus=""
  while IFS= read -r item; do
    case $item in
      '"**/'*'/**"')
        v=${item#'"**/'}; v=${v%'/**"'}
        case $v in */* | *'*'* | '') printf '  ✗ %s: files の項目 %s を読めない (判定できない)\n' "$list" "$item"; fail=1 ;; *) plus+="$v"$'\n' ;; esac ;;
      '"!**/tuikit/'*'/**"')
        v=${item#'"!**/tuikit/'}; v=${v%'/**"'}
        case $v in */* | *'*'* | '') printf '  ✗ %s: files の項目 %s を読めない (判定できない)\n' "$list" "$item"; fail=1 ;; *) minus+="$v"$'\n' ;; esac ;;
      '"!**/*_test.go"') ;;
      *) printf '  ✗ %s: files の項目 %s は知らない形 (判定できない。この検査の読み取りを直す)\n' "$list" "$item"; fail=1 ;;
    esac
  done < <(list_items "$list")
  # 較正: 必ず入っているはずの termwidth が読めなければ、読み取りが壊れている
  printf '%s' "$plus" | grep -qx termwidth || { printf '✗ %s: files から termwidth が読めない (読み取りが壊れている)\n' "$list"; exit 1; }
  while IFS= read -r rel; do
    [ -n "$rel" ] || continue
    top=${rel%%/*}
    listed=0
    IFS=/ read -ra parts <<< "$rel"
    for c in "${parts[@]}"; do printf '%s' "$plus" | grep -qxF -- "$c" && listed=1; done
    printf '%s' "$minus" | grep -qxF -- "$top" && listed=0
    isexcl=0
    excluded "$excl" "$rel" && isexcl=1
    if [ "$listed" -eq 0 ] && [ "$isexcl" -eq 0 ] && [ "$rel" = . ]; then
      printf '  ✗ %s: tuikit 直下に package がある (files の形では表せない。部品はディレクトリに入れるか、この検査の外す表に理由つきで載せる)\n' "$list"; fail=1
    elif [ "$listed" -eq 0 ] && [ "$isexcl" -eq 0 ]; then
      printf '  ✗ %s: %s がリストにも外す表にも無い (.golangci.yml の %s の files に足すか、この検査の外す表に理由つきで載せる)\n' "$list" "$rel" "$list"; fail=1
    elif [ "$listed" -eq 1 ] && [ "$isexcl" -eq 1 ]; then
      printf '  ✗ %s: %s はリストに入っているのに外す表にもある (どちらかにする)\n' "$list" "$rel"; fail=1
    fi
  done <<< "$rels"
  # 外す表の古い名前 (もう無い package) を残さない
  while IFS= read -r key; do
    hit=0
    while IFS= read -r rel; do excluded "$key" "$rel" && { hit=1; break; }; done <<< "$rels"
    [ "$hit" -eq 1 ] || { printf '  ✗ %s: 外す表の %s に当たる package が無い (表から消す)\n' "$list" "$key"; fail=1; }
  done < <(printf '%s\n' "$excl" | cut -d: -f1)
done

[ "$n" -ge 15 ] || { printf '✗ tuikit の package が %d しか見つからない (発見が壊れている)\n' "$n"; exit 1; }
[ "$fail" -eq 0 ] || { printf '✗ tuikit の depguard の部品のリストに漏れがある (%d package を検査)\n' "$n"; exit 1; }
printf '✓ tuikit の %d package が depguard の部品のリスト (no-framework-in-parts / parts-pure) か外す表に載っている\n' "$n"
