#!/usr/bin/env bash
# check_comment_provenance.sh — コードコメントに変更の経緯・出所の語を増やさせない。
#   規範は CLAUDE.md の「何をどこに書くか」: コメントは Why not だけ。経緯・レビュー指摘の番号はコミットログへ。
#
# 数える語 (コメント行 1 行につき 1 件。大文字小文字は区別しない):
#   - issue / retro の番号 (2 桁以上): `issue 123` / `Issue 123` / `issue #123` / `issue-123` / `issue: 123` / `retro 819`
#   - 経緯の語: 以前は / 旧実装 / かつて
#   - レビューの出所: 敵対レビュー / 敵対的レビュー / 反証レビュー / red team (`red-team`) / 〜の指摘・〜の結果 (レビュー・反証・監査) /
#     codex・opus・sonnet の指摘 / `P1-2` の形の指摘番号
# 🚨 open の issue の番号も数える。issue の状態で分けると、issue を done へ送っただけの commit で、その番号を書いたファイルの
#   件数が一覧を超えて赤になり、別の作業を止める (2026-10-18 に数えて 141 行 / 26 issue が該当した)。再開の条件を指すために
#   open の番号を新しく書くなら、制約と再評価の条件を本文に書いたうえで一覧の件数を上げる (レビューで見える)。
#
# 既存の違反は scripts/comment_provenance_allowlist.txt に置く (ラチェット)。1 行 = <件数><TAB><数えた行の本文の指紋><TAB><path>。
#   - 一覧より増えた・一覧に無いファイルに出た: 落とす
#   - 一覧より減った (0 件になったものも): 落とす (`--update` で下げる。下げずに残すと、余った枠に別の語を足しても通る)
#   - 一覧にあるファイルが git から無くなった・走査の対象から外れた (拡張子を変えた等): 落とさず案内する (ファイルを消した別の作業を止めない)
#   - rename: 一覧に無いファイルの指紋が、無くなったファイルの指紋と一致したら rename とみなし、落とさず案内する。
#     件数ではなく指紋で組にするのは、件数がたまたま一致した無関係なファイルに枠を移させないため
#   - 一覧の行の形が壊れている (件数が数字でない・同じ path が 2 行): 落とす
#   書き直したら `--update` で一覧を作り直す。`--update` は件数が増えたファイルがあれば拒否する (rename の組は増加に数えない)。
#   増やすのが正当なとき (open の issue の番号を書く) だけ `--update --allow-increase` を使う。
#   `--list [path の接頭辞]` は数えた行を `path:行番号:本文` で出す (書き直す行を探す用。判定はしない)。
#
# 脅威モデル: コメントを書く人が経緯・出所の語を**うっかり**足すこと。語の言い換え (「前の版では」「指摘を受けて」) は止めない。
# 検出しない形:
#   - 行末のコメント (`code  # …` / `code // …`) と `/* … */` の中。見るのは行頭 (空白・タブの後) がコメント記号の行だけ。行頭の全角空白も見ない
#   - 1 桁の番号 (`issue 5`)・全角数字の番号・番号の前に語が挟まる形 (`issue の 123`)・複数形 (`issues 123`。glogx の画面名 issues の
#     計測値 `issues 98` と区別できない)・単独の指摘番号 (`(P2)`。優先度の P2 と区別できない)
#   - どのファイルでも 1 行目の `#!` で始まる行 (shebang として飛ばす)
#   - rename と同時に数えた行を書き換えた形 (指紋が変わるので rename と認めず「一覧に無い」で落ちる。偽の red)
#   - 件数のラチェットなので、同じファイルで 1 行直して 1 行足す・既存の違反の行に別の語を書き足すのは通る
#   - 一覧にあるファイルを消して、同じ名前で作り直す形 (作り直したファイルは前の件数まで通る)
#   - git に add する前のファイル (git ls-files で走査する。CI では拾う)
#   - Markdown・JSON・issues/・vendor/・testdata/ と、この検査とそのテスト自身 (語彙を字として持つ)
#   - 拡張子で言語を決められず、shebang も拡張子の無い dotfiles の名前 (`_zshrc` など) でもないファイル
#     (点で始まる名前は `.envrc` などの列挙したものだけ。`.zshrc` の形は `*.*` に当たって見ない。この repo は `_zshrc` の形で置く)
#   文字列・heredoc の本文がたまたまコメント記号で始まる行は数える (偽の red。一覧で受ける)
# shellcheck disable=SC2016  # awk の program を単引用符で渡す (シェルに展開させない)
set -uo pipefail
unset CDPATH
ROOT_DIR="${CHECK_COMMENT_PROVENANCE_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
cd "$ROOT_DIR" || { printf '✗ repo root へ移動できない: %s\n' "$ROOT_DIR"; exit 1; }
ALLOW="scripts/comment_provenance_allowlist.txt"
MIN_FILES="${CHECK_COMMENT_PROVENANCE_MIN_FILES:-300}"

mode=check; prefix=''; allow_increase=0
case "${1:-}" in
  '') ;;
  --update) mode=update; [ "${2:-}" = --allow-increase ] && allow_increase=1 ;;
  --list) mode=list; prefix="${2:-}" ;;
  *) printf '使い方: %s [--update [--allow-increase] | --list [path の接頭辞]]\n' "$0" >&2; exit 2 ;;
esac

for c in git awk sort xargs; do
  command -v "$c" >/dev/null 2>&1 || { printf '✗ %s が無い。検査できないので緑にしない\n' "$c"; exit 1; }
done

tmp="$(mktemp -d)" || { printf '✗ 一時ディレクトリを作れない\n'; exit 1; }
trap 'rm -rf "$tmp"' EXIT

# 言語ごとのコメント記号でファイルを分ける (一覧は NUL 区切り。改行入りの path を割らない)。
# $tmp/tracked は git にある全 path (改行区切り。「無くなった」の判定に使う)、$tmp/scanned は走査したファイル
git ls-files -z > "$tmp/files" || { printf '✗ git ls-files に失敗\n'; exit 1; }
: > "$tmp/slash"; : > "$tmp/pound"; : > "$tmp/dash"; : > "$tmp/tracked"; : > "$tmp/scanned"
nfiles=0
while IFS= read -r -d '' f; do
  printf '%s\n' "$f" >> "$tmp/tracked"
  case "$f" in
    issues/*|*/vendor/*|vendor/*|*/testdata/*|testdata/*|*.md|*.json) continue ;;
    scripts/check_comment_provenance.sh|tests/scripts/test_check_comment_provenance.sh|"$ALLOW") continue ;;
  esac
  [ -f "$f" ] && [ ! -L "$f" ] || continue
  case "$f" in *$'\t'*|*$'\n'*) printf '✗ path にタブか改行がある (一覧の形にできない。判定できないので緑にしない): %s\n' "$f"; exit 1 ;; esac
  b="${f##*/}"
  dest=''
  case "$b" in
    *.go|*.swift|*.js|*.ts|*.tsx|*.c|*.h|*.m|*.jsonc|*.mod) dest=slash ;;
    *.sh|*.zsh|*.bash|*.bats|*.rb|*.py|*.pl|*.awk|*.exp|*.yml|*.yaml|*.toml|*.conf|*.tmux|*.ci|*.tape|*.mk) dest=pound ;;
    Makefile|GNUmakefile|Brewfile|.gitignore|.gitattributes|.envrc) dest=pound ;;
    *.lua) dest=dash ;;
    *.*) ;;
    _*) dest=pound ;;
    *) first=''; IFS= read -r first < "$f" 2>/dev/null; case "$first" in '#!'*) dest=pound ;; esac ;;
  esac
  [ -n "$dest" ] || continue
  # ./ を前に付ける (awk は `a=b` の形の引数をファイルではなく変数の代入として読む)
  printf './%s\0' "$f" >> "$tmp/$dest"
  printf '%s\n' "$f" >> "$tmp/scanned"
  nfiles=$((nfiles + 1))
done < "$tmp/files"
if [ "$nfiles" -lt "$MIN_FILES" ]; then
  printf '✗ 検査したファイルが %s 件しか無い (期待: %s 件以上。走査が壊れている)\n' "$nfiles" "$MIN_FILES"
  exit 1
fi

# $1 = コメント記号の ERE、$2 = NUL 区切りのファイルの一覧。出力: list なら path:行:本文、
# それ以外は path<TAB>件数<TAB>指紋 (数えた行の本文から作る。rename の組にだけ使う)
count() {
  [ -s "$2" ] || return 0
  LC_ALL=C xargs -0 awk -v marker="$1" -v listmode="$([ "$mode" = list ] && echo 1)" '
    BEGIN { for (i = 1; i < 256; i++) ord[sprintf("%c", i)] = i }
    function flush() { if (cnt > 0 && !listmode) printf "%s\t%d\t%08x%08x\n", prev, cnt, h1, h2 }
    function mix(s,    i, c) {
      for (i = 1; i <= length(s); i++) { c = ord[substr(s, i, 1)]; h1 = (h1 * 31 + c) % 2147483647; h2 = (h2 * 131 + c) % 2147483629 }
      h1 = (h1 * 31 + 10) % 2147483647; h2 = (h2 * 131 + 10) % 2147483629
    }
    FNR == 1 { if (NR > 1) flush(); cnt = 0; h1 = 7; h2 = 11; prev = substr(FILENAME, 3) }
    {
      if ($0 !~ ("^[ \t]*" marker)) next
      if (FNR == 1 && $0 ~ /^#!/) next
      # LC_ALL=C なので `的?` の ? は「的」の最後の 1 バイトにしか掛からない。日本語に ? を掛けるときは (…) で包む
      l = tolower($0)
      if (l ~ /(issue|retro)[ \t]*[-:#]?[ \t]*#?[0-9][0-9]/ ||
          l ~ /以前は|旧実装|かつて/ ||
          l ~ /(敵対的|敵対|反証)レビュー|red[ -]?team|(レビュー|反証|監査)(の|で)?(指摘|結果)|(codex|opus|sonnet) ?(の)?指摘|指摘 ?p[123]|p[123]-[0-9]/) {
        cnt++
        t = $0; sub(/^[ \t]+/, "", t); mix(t)
        if (listmode) print substr(FILENAME, 3) ":" FNR ":" $0
      }
    }
    END { if (NR > 0) flush() }
  ' < "$2"
}
# 1 つでも awk / xargs が失敗したら (読めないファイルなど) 判定不能として落とす。黙って残りを数えずに緑にしない
scan() {
  local rc=0
  count '//' "$tmp/slash" || rc=1
  count '#' "$tmp/pound" || rc=1
  count '--' "$tmp/dash" || rc=1
  return "$rc"
}
if ! scan > "$tmp/raw"; then
  printf '✗ コメントの走査が途中で失敗した (読めないファイルがある? 判定できないので緑にしない)\n'
  exit 1
fi

if [ "$mode" = list ]; then
  awk -v p="$prefix" 'index($0, p) == 1' "$tmp/raw" | sort -t: -k1,1 -k2,2n
  exit 0
fi
sort "$tmp/raw" > "$tmp/actual"
total=$(awk -F'\t' '{s += $2} END {print s + 0}' "$tmp/actual")

# 一覧を読む: 形が壊れた行・同じ path の 2 行目は落とす。$tmp/allow は path<TAB>件数<TAB>指紋
: > "$tmp/allow"
if [ -f "$ALLOW" ]; then
  if ! awk -F'\t' -v out="$tmp/allow" '
    /^#/ || /^$/ { next }
    NF == 2 && $1 ~ /^[1-9][0-9]*$/ { if (!old++) printf "✗ %s: 指紋の列が無い古い形の一覧 (rm してから --update --allow-increase で作り直す)\n", FILENAME; bad = 1; next }
    NF != 3 || $1 !~ /^[1-9][0-9]*$/ || $2 !~ /^[0-9a-f]+$/ { printf "✗ %s:%d: 一覧の行の形が壊れている (<件数><TAB><指紋><TAB><path>): %s\n", FILENAME, FNR, $0; bad = 1; next }
    seen[$3]++ { printf "✗ %s:%d: 同じ path が 2 行ある: %s\n", FILENAME, FNR, $3; bad = 1; next }
    { print $3 "\t" $1 "\t" $2 > out }
    END { exit bad }
  ' "$ALLOW"; then
    exit 1
  fi
elif [ "$mode" != update ]; then
  printf '✗ %s が無い\n' "$ALLOW"; exit 1
fi

# 突き合わせ (awk 1 本)。分類: up (増えた・一覧に無い) / shrink (減った。走査したファイルで 0 件も) /
# gone (git から無くなった) / rename (一覧に無いファイルで、指紋が gone のどれかと一致)
# 一覧・tracked・scanned は FILENAME で読み分ける (NR == FNR は空のファイルの次で 2 つ目にも真になる)
awk -F'\t' -v allowf="$tmp/allow" -v trackedf="$tmp/tracked" -v scannedf="$tmp/scanned" '
  FILENAME == allowf   { an[$1] = $2; ah[$1] = $3; next }
  FILENAME == trackedf { tracked[$0] = 1; next }
  FILENAME == scannedf { scanned[$0] = 1; next }
  { cur[$1] = $2; curh[$1] = $3 }
  END {
    for (p in an) {
      if (p in cur) continue
      if (!(p in tracked)) { gone[p] = 1; goneh[ah[p]] = (goneh[ah[p]] ? goneh[ah[p]] : p) }
      else if (p in scanned) printf "shrink\t%s\t0\t%s\n", p, an[p]
      else gone[p] = 1
    }
    for (p in cur) {
      if (p in an) {
        if (cur[p] + 0 > an[p] + 0) printf "up\t%s\t%s\t%s\n", p, cur[p], an[p]
        else if (cur[p] + 0 < an[p] + 0) printf "shrink\t%s\t%s\t%s\n", p, cur[p], an[p]
      } else if ((curh[p] in goneh) && goneh[curh[p]] != "") {
        printf "rename\t%s\t%s\t%s\n", p, cur[p], goneh[curh[p]]
        delete gone[goneh[curh[p]]]; goneh[curh[p]] = ""
      } else printf "up\t%s\t%s\t0\n", p, cur[p]
    }
    for (p in gone) printf "gone\t%s\t0\t%s\n", p, an[p]
  }
' "$tmp/allow" "$tmp/tracked" "$tmp/scanned" "$tmp/actual" | sort > "$tmp/diff"
ups=$(grep -c '^up' "$tmp/diff")
renames=$(grep -c '^rename' "$tmp/diff")
shrinks=$(grep -c '^shrink' "$tmp/diff")
gones=$(grep -c '^gone' "$tmp/diff")

if [ "$mode" = update ]; then
  if [ "$ups" -gt 0 ] && [ "$allow_increase" -ne 1 ]; then
    awk -F'\t' '$1 == "up" {printf "✗ %s: %s 行 (一覧は %s)。増やすのが正当なら --update --allow-increase\n", $2, $3, $4}' "$tmp/diff"
    printf '✗ 件数が増えたファイルがあるので一覧を作り直さない\n'
    exit 1
  fi
  {
    printf '# check_comment_provenance.sh の既存の違反。<件数><TAB><数えた行の本文の指紋><TAB><path>。減らしたら --update で作り直す。増やさない\n'
    awk -F'\t' '{print $2 "\t" $3 "\t" $1}' "$tmp/actual"
  } > "$tmp/new"
  if ! cp "$tmp/new" "$ALLOW"; then printf '✗ %s を書けない\n' "$ALLOW"; exit 1; fi
  awk -F'\t' '$1 == "rename" {printf "  rename: %s → %s\n", $4, $2}' "$tmp/diff"
  printf '✓ %s を作り直した (%s ファイル / %s 件。増えた %s / rename %s / 減った %s / 無くなった %s)\n' "$ALLOW" "$(wc -l < "$tmp/actual" | tr -d ' ')" "$total" "$ups" "$renames" "$shrinks" "$gones"
  exit 0
fi

bad=0
if [ "$ups" -gt 0 ]; then
  awk -F'\t' '$1 == "up" {
    if ($4 == 0) printf "✗ %s: コメントに経緯・出所の語が %s 行ある (一覧に無い。Why not だけを書き、経緯はコミットログへ)\n", $2, $3
    else printf "✗ %s: コメントの経緯・出所の語が %s 行に増えた (一覧は %s)\n", $2, $3, $4
  }' "$tmp/diff"
  bad=1
fi
if [ "$shrinks" -gt 0 ]; then
  awk -F'\t' '$1 == "shrink" {printf "✗ %s: %s 行に減ったのに一覧が %s のまま (--update で下げる。残すと余った枠に別の語を足しても通る)\n", $2, $3, $4}' "$tmp/diff"
  bad=1
fi
if [ "$bad" -ne 0 ]; then
  printf '  規範: CLAUDE.md の「何をどこに書くか」。語彙と検出しない形は scripts/check_comment_provenance.sh の冒頭。--list <path> で該当行を出せる\n'
  exit 1
fi
awk -F'\t' '$1 == "rename" {printf "  (rename: %s → %s。--update で一覧を移せる)\n", $4, $2}' "$tmp/diff"
if [ "$gones" -gt 0 ]; then
  printf '  (一覧にあるファイルが %s 件、git から無くなったか走査の対象から外れた。scripts/check_comment_provenance.sh --update で一覧から消せる)\n' "$gones"
fi
printf '✓ コメントの経緯・出所の語: %s ファイルを検査、%s 件 (一覧を超えたファイルなし)\n' "$nfiles" "$total"
