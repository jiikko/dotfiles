# issue の本文の相対リンクを読む共通部分。dotfiles の scripts/issue_done.sh と、obaket の bin/issue-done・bin/issue-resolve-link-conflicts が
# `awk -f` で読み、どのリンクを対象にするかを揃える。
# 使う側は `link_hook(lnk)` を定義する: 対象のリンク 1 本ごとに呼ばれ、返した文字列がそのリンクの位置に入る。
# 対象にしないもの: URL (scheme 付き)・絶対パス・ページ内リンク・インラインコードの中。コードフェンスの中は使う側の規則で飛ばす
function normalize(p,   n, i, a, st, top, out) {
  n = split(p, a, "/")
  top = 0
  for (i = 1; i <= n; i++) {
    if (a[i] == "" || a[i] == ".") continue
    if (a[i] == "..") {
      if (top > 0 && st[top] != "..") { top-- } else { st[++top] = ".." }
      continue
    }
    st[++top] = a[i]
  }
  out = ""
  for (i = 1; i <= top; i++) out = (out == "" ? st[i] : out "/" st[i])
  return out
}
function relpath(target, base,   nt, nb, t, b, i, up, out) {
  if (base == "") return target
  nt = split(target, t, "/")
  nb = split(base, b, "/")
  i = 1
  while (i <= nt && i <= nb && t[i] == b[i]) i++
  out = ""
  for (up = i; up <= nb; up++) out = out "../"
  for (; i <= nt; i++) out = out t[i] (i < nt ? "/" : "")
  return out
}
# backslashes_before(text, i): text の i 文字目の直前に続くバックスラッシュの数 (奇数ならその文字はエスケープされている)
function backslashes_before(text, i,   n) {
  n = 0
  while (i - n - 1 >= 1 && substr(text, i - n - 1, 1) == "\\") n++
  return n
}
# escaped_open(text): `](` の手前の text で、対になる開き括弧がエスケープされているか (`\[x](y)` はリンクではなく文字列)。
# 後ろから括弧の深さを数えて対の `[` を探す (`[a [b]](x)` の内側の括弧を対と読まない)。エスケープした括弧は深さに数えない。
# 対の `[` がエスケープされていない (`\\[x](y)` のようにバックスラッシュが偶数個を含む) ならリンク
function escaped_open(text,   i, c, depth, esc_seen) {
  depth = 0; esc_seen = 0
  for (i = length(text); i > 0; i--) {
    c = substr(text, i, 1)
    if (c != "[" && c != "]") continue
    if (backslashes_before(text, i) % 2 == 1) { if (c == "[" && depth == 0) esc_seen = 1; continue }
    if (c == "]") { depth++; continue }
    if (depth > 0) { depth--; continue }
    return 0
  }
  return esc_seen
}
function process(seg,   out, rest, pre, whole, lnk) {
  out = ""; rest = seg
  while (match(rest, /\]\([^)[:space:]]+\)/)) {
    pre   = substr(rest, 1, RSTART - 1)
    whole = substr(rest, RSTART, RLENGTH)
    rest  = substr(rest, RSTART + RLENGTH)
    lnk   = substr(whole, 3, length(whole) - 3)
    # 開き括弧をエスケープした `\[x](y)` はリンクではなく文字列なので触らない
    if (escaped_open(out pre)) { out = out pre whole; continue }
    # URL (scheme 付き) / 絶対パス / ページ内リンクは対象外。fragment の文字種は問わない
    # (`#サービス別-機能比較` のような非 ASCII が実在する。ASCII に絞ると、そのリンクは抽出から漏れて張り直しも検査もされない)。
    if (lnk ~ /^[A-Za-z][A-Za-z0-9+.-]*:/ || substr(lnk, 1, 1) == "/" || substr(lnk, 1, 1) == "#") {
      out = out pre whole; continue
    }
    out = out pre "](" link_hook(lnk) ")"
  }
  return out rest
}
# インラインコード (`…`) の中は書式の説明であって参照ではない (tests/issues/test_issue_links_valid.sh・obaket の check-doc-links と同じ扱い)
function rewrite_line(line,   out, i, j) {
  out = ""
  while ((i = index(line, "`")) > 0) {
    j = index(substr(line, i + 1), "`")
    if (j == 0) break
    out = out process(substr(line, 1, i - 1)) substr(line, i, j + 1)
    line = substr(line, i + j + 1)
  }
  return out process(line)
}
