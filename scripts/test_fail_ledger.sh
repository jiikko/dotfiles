#!/usr/bin/env bash
# test_fail_ledger.sh — make test の赤で落ちたテストを 1 行ずつ追記し、テスト名ごとに数えて出す。
#
#   test_fail_ledger.sh record <sh|bats|go> <一覧のファイル>   1 行 1 テスト名。一覧が空なら何も書かない
#   test_fail_ledger.sh top [N]                                落ちた回数の多い順に N 件 (既定 20)
#   test_fail_ledger.sh go-names <src/module> <go test の出力>  record go に渡す一覧を出す
#
# 記録先は TEST_FAIL_LEDGER。未設定なら ${XDG_CACHE_HOME:-$HOME/.cache}/dotfiles-test-fails/log.tsv。
# 空に設定されていると record は何も書かない。Makefile の runner は起動する各テストへ空で渡す:
# runner をわざと落とすテストの中の make が、本物の記録に偽の失敗を足さないため。
# CI (CI が非空) でも書かない。runner は使い捨てで、数える相手がいない。
# 置き場を本体 checkout の tmp/ にしない: worktree と一緒に消え、make clean-tmp の対象にもなる。
# 数えたいのは日をまたいだ回数。
#
# 行の形 (TSV): 日時 / commit / load average (1 分) / 種類 (sh|bats|go) / テスト名
# sh / bats の名前は repo root からの相対パスに揃える (DIR=./tests や末尾の / で同じテストが別の名前に割れないため)。
# go の名前は「src/<module> <package> Test名」(同じ module の別 package の同名のテストを合算しない)。
# テスト名の無い失敗 (build の失敗・テストの外の panic・timeout) は「src/<module> <package> (テスト名なし)」で残す。
set -u
unset CDPATH

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)" || exit 1
DEFAULT_LEDGER="${XDG_CACHE_HOME:-$HOME/.cache}/dotfiles-test-fails/log.tsv"
. "$ROOT_DIR/bin/lib/uint.sh" || exit 1

usage() {
  sed -n '3,6p' "$0" | sed 's/^# //' >&2
  exit 2
}

normalize_path() {
  local p="$1"
  while :; do case "$p" in *//*) p="${p%%//*}/${p#*//}" ;; *) break ;; esac; done
  p="${p#"$ROOT_DIR"/}"
  while :; do case "$p" in ./*) p="${p#./}" ;; *) break ;; esac; done
  printf '%s' "$p"
}

record() {
  local kind="$1" list="$2" ledger="${TEST_FAIL_LEDGER-$DEFAULT_LEDGER}"
  [ -n "${CI:-}" ] && return 0
  [ -n "$ledger" ] || return 0
  [ -s "$list" ] || return 0
  local now commit load lines name
  now=$(date '+%Y-%m-%dT%H:%M:%S')
  commit=$(git -C "$ROOT_DIR" rev-parse --short HEAD 2>/dev/null) || commit=-
  load=$(sysctl -n vm.loadavg 2>/dev/null | awk '{print $2}')
  [ -n "$load" ] || load=-
  lines=""
  while IFS= read -r name; do
    [ -n "$name" ] || continue
    [ "$kind" = go ] || name=$(normalize_path "$name")
    lines="${lines}${now}	${commit}	${load}	${kind}	$(printf '%s' "$name" | tr '\t' ' ')
"
  done < "$list"
  [ -n "$lines" ] || return 0
  mkdir -p "$(dirname "$ledger")" || return 1
  # 並行するセッションの make test と行が混ざらないよう、1 回の書き込みで足す。
  printf '%s' "$lines" >> "$ledger"
}

# go test ./... は package ごとに出力をまとめ、`ok  \t<pkg>` / `FAIL\t<pkg>` / `?   \t<pkg>` の行で閉じる。
# 名前は区切りの中で溜め、閉じる行で package を付けて出す。行頭の `--- FAIL:` (トップレベルのテスト) が
# 1 つも無い FAIL は、名前の無い失敗として残す。字下げした `--- FAIL:` は subtest で、親も行頭に出るので
# 拾わない (同じ失敗を 2 回数えない)。閉じる行が来ないまま終わった出力も、拾えた分は package 無しで残す。
go_names() {
  local dir="$1" out="$2"
  awk -v d="$dir" '
    /^--- FAIL: / { names[++n] = $3; next }
    /^(ok|FAIL|\?)[ ]*\t/ {
      if ($1 == "FAIL") {
        any = 1
        if (n) for (i = 1; i <= n; i++) print d " " $2 " " names[i]
        else print d " " $2 " (テスト名なし)"
      }
      n = 0; next
    }
    END {
      if (n) for (i = 1; i <= n; i++) print d " " names[i]
      else if (!any) print d " (テスト名なし)"
    }
  ' "$out"
}

# その名前を本文に書いている issue の番号。sh はファイル名、go は Test 名で、単語として引く
# (パスや module の書き方は issue ごとに揺れる。部分一致だと TestRun が TestRunner の issue に当たる)。
# テスト名の無い go の失敗は引かない (残るのは module・package の短い名前で、無関係な issue に当たる)。
# 引くのは名前の最後の部分だけなので、同じファイル名・同じ Test 名の別のテストを書いた issue にも付く (手がかりとして出す)。
issues_for() {
  local name="$1" key dir="${TEST_FAIL_LEDGER_ISSUES_DIR:-$ROOT_DIR/issues}"
  case "$name" in *" (テスト名なし)") return 0 ;; esac
  key="${name% (*)}"
  key="${key##* }"
  key="${key##*/}"
  [ -n "$key" ] && [ -d "$dir" ] || return 0
  grep -rlFw --include='*.md' -- "$key" "$dir" 2>/dev/null \
    | sed -nE 's|.*/([0-9]{3,})-[^/]*$|\1|p' | sort -u | paste -sd, -
}

top() {
  local n="${1:-20}" ledger="${TEST_FAIL_LEDGER:-$DEFAULT_LEDGER}"
  uint_ok "$n" 6 && [ "$n" -gt 0 ] || { echo "test_fail_ledger: 件数は 1 以上・6 桁までの整数で渡す: $n" >&2; return 2; }
  if [ ! -s "$ledger" ]; then
    echo "記録はまだ無い ($ledger)"
    return 0
  fi
  echo "記録: $ledger ($(wc -l < "$ledger" | tr -d ' ') 行)"
  printf '%4s  %-19s  %s\n' 回数 最後に落ちた テスト名
  local count last name refs
  awk -F'\t' 'NF >= 5 { c[$5]++; if ($1 > l[$5]) l[$5] = $1 }
              END { for (k in c) printf "%d\t%s\t%s\n", c[k], l[k], k }' "$ledger" \
    | LC_ALL=C sort -t "$(printf '\t')" -k1,1nr -k2,2r | head -n "$n" \
    | while IFS="$(printf '\t')" read -r count last name; do
        refs=$(issues_for "$name")
        printf '%4d  %-19s  %s%s\n' "$count" "$last" "$name" "${refs:+  (issue $refs)}"
      done
}

case "${1:-}" in
  record) [ $# -eq 3 ] || usage; case "$2" in sh|bats|go) ;; *) usage ;; esac; record "$2" "$3" ;;
  top) [ $# -le 2 ] || usage; top "${2:-}" ;;
  go-names) [ $# -eq 3 ] || usage; go_names "$2" "$3" ;;
  *) usage ;;
esac
