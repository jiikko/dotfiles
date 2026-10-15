# claim_banner.awk — issue 本文冒頭の担当者バナー (`> 🚨 **担当中: <誰>**（YYYY-MM-DD〜）`) の判定と除去。
#
#   awk -v mode=find  -f claim_banner.awk <file>        冒頭にバナーがあれば rc=0、無ければ rc=1
#   awk -v mode=strip -f claim_banner.awk <file>        バナー行と直後の空行 1 つを除いた全文を出す。外したら rc=0、無ければ rc=1
#   awk -v mode=list  -f claim_banner.awk <file>...     冒頭にバナーがあるファイルのパスを 1 行ずつ出す (rc=0)
#
# 規則 (shell 側の正本。tests/issues/test_next_claims_have_banner.sh と scripts/issue_done.sh が読む):
#   - 最初の `## ` 見出しより前だけを見る
#   - コードフェンス (字下げ 3 桁までの ``` / ~~~) の中は見ない
#   - 行頭 (前に `> ` と `🚨 ` を許す) の `**担当中` / `**着手中`
# 🚨 Go 版 (src/glogx/issues/banner.go の bannerIndex / removeBanner) と同じ規則。言語が違うので 1 実装に寄せられない。
# 変えるなら両方を同じ commit で変え、両方のテストを通す (banner_test.go が shell 版との一致を見る)
BEGIN {
  if (mode != "find" && mode != "strip" && mode != "list") {
    print "claim_banner.awk: mode は find / strip / list のどれか (got: " mode ")" > "/dev/stderr"
    bad = 1
    exit 2
  }
}
FNR == 1 { fence = 0; past = 0; skip_blank = 0 }
{
  if (skip_blank) {
    skip_blank = 0
    if ($0 ~ /^[[:space:]]*$/) next
  }
  if (past) { if (mode == "strip") print; next }
  if ($0 ~ /^ {0,3}(```|~~~)/) { fence = !fence; if (mode == "strip") print; next }
  if (fence) { if (mode == "strip") print; next }
  if ($0 ~ /^## /) {
    past = 1
    if (mode == "find") exit
    if (mode == "list") { nextfile }
    print
    next
  }
  if ($0 ~ /^(> )?(🚨 )?\*\*(担当中|着手中)/) {
    found = 1
    if (mode == "find") exit
    if (mode == "list") { print FILENAME; past = 1; nextfile }
    past = 1
    skip_blank = 1
    next
  }
  if (mode == "strip") print
}
END {
  if (bad) exit 2
  if (mode == "list") exit 0
  exit !found
}
