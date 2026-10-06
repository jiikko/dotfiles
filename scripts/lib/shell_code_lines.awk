# shell_code_lines.awk — 字句の検査 (scripts/check_*.sh) が共有する shell の読み方。
# 中身を本体のプログラムの前に連結して渡す (macOS の awk は -e を持たない):
#   scl="$(cat scripts/lib/shell_code_lines.awk)" || exit 1
#   awk "$scl"' FNR == 1 { n = 0 } { L[++n] = $0 } END { shell_code_all(n); for (i = 1; i <= n; i++) if (C[i] != "") … }' file
#   (1 ファイルにつき awk を 1 回起こす。END で判定するので、複数ファイルを 1 回の awk に渡さない)
#
# heredoc_tag(l): その行が heredoc を始めるならタグ、でなければ "" を返す (<< の位置を _heredoc_at、タグの最後の文字の位置を
#   _heredoc_end に置く)。
#   scripts/check_test_sleeps.sh も使う (あちらは行ごとに読み、終端が見つからなければ失敗にする)
# shell_code_all(n): L[1..n] (ファイルの全行) から C[1..n] (検査する部分。"" なら読まない) を作る。
#   読まないのはコメント行と heredoc の本文 (終端のタグの行まで)。heredoc の開始行は << とタグを除いた前後を返す
#   (`timeout 10 "$HOOK" <<'JSON'` の前半も `cat <<EOF | gtimeout 5 sh` の後半も検査する)。本文はメッセージやテストが作る mock で、別プロセスで走るか文字列なので、
#   同じ字面でも検査の対象の意味を持たない
# 🚨 終端のタグの行が後ろに在るときだけ heredoc とみなす。見つからない << (here-string の <<<・算術の $((1<<n))・複数行の
#    引用符の中の正規表現) を開始と読むと、本文がファイルの最後まで続き、後半が黙って検査されない
#    (旧実装は here-string で 9 本の後半を読み飛ばしていた。issue 643)。_claude/hooks/deny-piped-push-then-destroy.sh と同じ考え方
# 検出しないもの: 1 行に 2 つの heredoc / 行の途中のコメント (検査する部分に入る) / 誤って開始と読んだ << の後ろに、偶然タグと
#   同じ語だけの行がある形。典型は複数行の引用符の中の説明文 (`msg="usage:` の次の行に `prog <<EOF …"`) の後ろに本物の
#   `cat <<EOF … EOF` がある形で、間の行が黙って抜ける (引用符の状態を行をまたいで追わないため。issue 643 の敵対的レビュー 2 周目 P3-1)
# heredoc のタグを返す (開始でなければ "")。引用符の中身を潰した写しで「引用符の外の <<」を探し、here-string (<<<) と
# 算術 ((( … )) の中) を除き、タグは元の行のその位置から bash の規則で読む (引用符つきは閉じるまで任意の文字、素なら区切りまで)
# 写しでは引用符の中身・引用符の外のエスケープ (\X)・語の先頭の # から行末 (コメント) を空白にする
function heredoc_tag(l,   n, i, c, q, blank, depth, rest, tag, j, pc) {
  if (index(l, "<<") == 0) return ""  # 大半の行はここで返す (1 文字ずつの走査を全行に掛けない)
  n = length(l); blank = ""; q = ""
  for (i = 1; i <= n; i++) {
    c = substr(l, i, 1)
    if (q == "") {
      if (c == "\\" && i < n) { blank = blank "  "; i++; continue }
      pc = (i == 1) ? " " : substr(l, i - 1, 1)
      if (c == "#" && pc ~ /[[:space:];&|(]/) { while (length(blank) < n) blank = blank " "; break }
      if (c == "\047" || c == "\042") q = c
      blank = blank c
    } else {
      if (q == "\042" && c == "\\" && i < n) { blank = blank "  "; i++; continue }
      if (c == q) { q = ""; blank = blank c } else blank = blank " "
    }
  }
  depth = 0
  for (i = 1; i < n; i++) {
    if (substr(blank, i, 2) == "((") { depth++; i++; continue }
    if (substr(blank, i, 2) == "))" && depth > 0) { depth--; i++; continue }
    if (substr(blank, i, 3) == "<<<") { i += 2; continue }
    if (substr(blank, i, 2) != "<<" || depth > 0) continue
    _heredoc_at = i
    rest = substr(l, i + 2)
    sub(/^-/, "", rest); sub(/^[[:space:]]+/, "", rest)
    # タグは 1 語: 引用符で囲んだ部分 (中は任意の文字) と素の部分 (\X は X) を、区切り文字まで連結する (`<<"E" に引用符つきの S と x が続けば ESx`)
    tag = ""
    while (rest != "") {
      c = substr(rest, 1, 1)
      if (c == "\047" || c == "\042") {
        j = index(substr(rest, 2), c)
        if (j == 0) return ""
        tag = tag substr(rest, 2, j - 1); rest = substr(rest, j + 2)
      } else if (c == "\\" && length(rest) > 1) {
        tag = tag substr(rest, 2, 1); rest = substr(rest, 3)
      } else if (c ~ /[[:space:];|&<>()]/) {
        break
      } else {
        tag = tag c; rest = substr(rest, 2)
      }
    }
    _heredoc_end = length(l) - length(rest)  # タグの最後の文字の位置 (この後ろは同じ行のコマンドの続き)
    return tag
  }
  return ""
}

function _scl_trim(x) { gsub(/^[[:space:]]+|[[:space:]]+$/, "", x); return x }
# 終端はタグだけの行。文字列の中に書いた heredoc (テストデータ) は「タグ + 閉じ引用符」で終わるので、それも終端に数える
function _scl_is_end(x, tag) { x = _scl_trim(x); return x == tag || index(x, tag "\"") == 1 || index(x, tag "\047") == 1 }

function shell_code_all(n,   i, j, s, tag, hd) {
  hd = ""
  for (i = 1; i <= n; i++) {
    if (hd != "") {
      if (_scl_is_end(L[i], hd)) hd = ""
      C[i] = ""
      continue
    }
    s = L[i]; sub(/^[[:space:]]+/, "", s)
    if (s ~ /^#/) { C[i] = ""; continue }
    C[i] = L[i]
    tag = heredoc_tag(L[i])
    if (tag == "") continue
    for (j = i + 1; j <= n; j++) if (_scl_is_end(L[j], tag)) break
    if (j <= n) {
      hd = tag
      # << とタグを除いた前後をつなぐ (`cat <<EOF | gtimeout 5 sh` の後半も検査する)
      C[i] = substr(L[i], 1, _heredoc_at - 1) " " substr(L[i], _heredoc_end + 1)
    }
  }
}
