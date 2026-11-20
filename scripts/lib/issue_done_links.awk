# issue を done/ へ動かしたときのリンクの張り直し。共通部分 (issue_links.awk) と一緒に `awk -f` で読む。
# dotfiles の scripts/issue_done.sh と obaket の bin/issue-done が使う。
#   -v old_base : そのファイルの中のリンクを解決するディレクトリ (repo 相対)
#   -v new_base : 書き直すときの起点ディレクトリ (repo 相対)。動かした当人だけ old と違う
#   -v moved_old / -v moved_new : 今回動かす issue の repo 相対パス
#   -v report=1 -v want=<repo 相対> : 書き換えずに、want を指すリンクを HIT 行で出す
# 出力は書き換え後の全文。1 本でも書き換えたら stderr へ "REWROTE" を出す。
function newlink(lnk,   frag, path, t, nl) {
  frag = ""
  if (index(lnk, "#") > 0) { frag = substr(lnk, index(lnk, "#")); path = substr(lnk, 1, index(lnk, "#") - 1) }
  else path = lnk
  if (path == "") return lnk
  t = normalize(old_base "/" path)
  # repo の外へ出るリンクは触らない (計算の外なので、黙って壊すより残す)
  if (substr(t, 1, 3) == "../" || t == "..") return lnk
  if (moved_old != "" && t == moved_old) t = moved_new
  else if (old_base == new_base) return lnk   # 動かした当人以外は、指し先が変わらないなら触らない
  nl = relpath(t, new_base)
  if (nl == "") return lnk
  return nl frag
}
# report モード: そのリンクが want (repo 相対) を指しているか。書き換えと同じ解決経路を通す
function resolves_to(lnk, w,   path, t) {
  path = (index(lnk, "#") > 0) ? substr(lnk, 1, index(lnk, "#") - 1) : lnk
  if (path == "") return 0
  t = normalize(old_base "/" path)
  return (t == w)
}
function link_hook(lnk,   nl) {
  if (report) { if (resolves_to(lnk, want)) print "HIT " lnk; return lnk }
  nl = newlink(lnk)
  if (nl != lnk) changed++
  return nl
}
# report モードは走査だけで、本文は出さない (HIT 行だけを返す)
function emit(l) { if (!report) print l }
# コードフェンスの中も同じ理由で触らない
/^[[:space:]]*```[^`]*$/ { fence = !fence; emit($0); next }
/^[[:space:]]*~~~/       { fence = !fence; emit($0); next }
fence { emit($0); next }
{ emit(rewrite_line($0)) }
END { if (!report && changed > 0) print "REWROTE " changed > "/dev/stderr" }
