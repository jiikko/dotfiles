# shellcheck shell=bash
# issue の番号を数える関数 (issue_next_number.sh・issue_number_drafts.sh が source する)。cwd は repo の root。
#
# 番号はファイル名の先頭の「3 桁以上の数字 + - + 英字 (種別)」。999 までは 3 桁のゼロ埋め、1000 からは桁が増える。
# 🚨 「-」の後の英字まで見る: 日付で始まる記録 (`2026-02-20-….md`。issues/ の下に置く repo がある) を番号 2026 と読まない。
# 🚨 3 桁を決め打たない (`^[0-9]{3}-`・`cut -c1-3`・`[0-9][0-9][0-9]-*.md`) / 文字列で並べ替えない (`sort` は `999-` を `1000-` より後に置く)。
# どちらも 1000 番以降を数えず、次の採番が既にある番号を取り直す。

# issue_default_ref: 数える ref の既定 (origin の既定のブランチ。無い・指す先が消えているなら origin/master)。
issue_default_ref() {
  local r
  if r="$(git symbolic-ref -q --short refs/remotes/origin/HEAD)" && git rev-parse --verify -q "$r^{commit}" >/dev/null; then
    echo "$r"
  else
    echo origin/master
  fi
}

# issue_max_number <ref>: 作業ツリーの issues/ と <ref> の issues/ にある番号の最大を 10 進で出す (無ければ 0)。
# 深さを切らずに数える (epic/<name>/next/ のような深い所も番号の空間は同じ)。
issue_max_number() {
  local max
  max="$({ find issues -type f -name '[0-9][0-9][0-9]*-*.md' | sed 's|.*/||'
           git -c core.quotePath=false ls-tree -r --name-only "$1" -- issues | sed 's|.*/||'; } |
         sed -n 's/^\([0-9]\{3,\}\)-[A-Za-z].*\.md$/\1/p' | sort -n | tail -1)"
  echo $((10#${max:-0}))
}
