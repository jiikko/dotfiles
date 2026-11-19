#!/usr/bin/env bash
# devtool/ の中から devtool/ の外を参照する書き方を落とす (make test-devtool-boundary。test-lint に含まれる)。
#
# devtool/ は別の repo へ切り出す前提の道具の置き場で、依存の向きは dotfiles → devtool だけにする (devtool/README.md)。
# 同じ checkout にある間は境界を破っても動くので、テストは緑のまま通る。壊れるのは切り出した日なので、字面で止める。
#
# 使い方: scripts/check_devtool_boundary.sh [<devtool のディレクトリ>]   (引数はテスト用。無ければ repo の devtool/)
#
# 落とす形:
#   1. コードの行 (先頭の空白の後が `#` でない行) にある、パスの段としての `..` (`../x`・`x/..`・`..` だけの引数)。
#      devtool の中の相対の移動も禁止する: 外に出るかを段の数で判定しないのは、基点が変数 (`$_tool_root/..`) だと
#      段の数で判定できないから。devtool の root は自分の実体の置き場から 1 回で決める
#   2. コメントの行の `shellcheck source=` の指示にある `..`
#   3. コードの行の `~/dotfiles`・`$HOME/dotfiles`・`${HOME}/dotfiles`・`/Users/<名前>/dotfiles`
#   4. devtool の外を指す symlink と、リンク先の無い symlink (中を指していても落とす)
#   5. 検査対象のファイルが 0 件 (devtool/ が無い・`.md` しか無い)
# 例外: 行内に `devtool-boundary: allow <理由>` を書いた行は 1〜3 を見ない。理由の無い印は落とす。
#
# 検出しない形 (脅威モデルは自分の書き間違い。悪意のある書き方は対象外):
#   - 変数を重ねて組んだパス (`d=..; "$base/$d"`・`$(dirname "$(dirname "$0")")`)
#   - PATH 上の素の名前で dotfiles の道具を呼ぶ形 (dotfiles の道具は bin/devtool-run が環境変数で渡す。README とレビューで守る)
#   - コメントでない行の末尾のコメントに書いた `..` (コードの行として見るので、落とす側に倒れる。散文なら印を書く)
#   - `.md` のファイル (説明の散文で、実行されない)
# 落としすぎる形 (誤りは落とす側に倒す。正当なら印で逃がす): ファイル名の中の `..` (`release-..-notes.txt`)、文字列の中の `..`
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)" || { printf '✗ repo root を決められない\n'; exit 1; }
target="${1:-$ROOT_DIR/devtool}"
[ -d "$target" ] || { printf '✗ devtool のディレクトリが無い: %s (検査できないので緑にしない)\n' "$target"; exit 1; }
target="$(cd -P "$target" && pwd -P)" || { printf '✗ %s に入れない\n' "$target"; exit 1; }

fail=0
checked=0

# 4. symlink
links="$(find "$target" -type l)" || { printf '✗ symlink の列挙に失敗した (検査できないので緑にしない)\n'; exit 1; }
while IFS= read -r l; do
  [ -n "$l" ] || continue
  real="$(readlink -f "$l" 2>/dev/null || true)"
  # readlink -f は最後の段が無くてもパスを返すことがあるので、リンク先が在ることも要求する (中を指す dangling も落とす)
  [ -e "$l" ] || real=""
  case "$real" in
    "$target"|"$target"/*) ;;
    *) printf '✗ devtool の外を指す symlink: %s -> %s\n' "${l#"$target"/}" "$(readlink "$l")"; fail=1 ;;
  esac
done <<< "$links"

files="$(find "$target" -type f ! -name '*.md' | LC_ALL=C sort)" || { printf '✗ ファイルの列挙に失敗した (検査できないので緑にしない)\n'; exit 1; }
while IFS= read -r f; do
  [ -n "$f" ] || continue
  checked=$((checked + 1))
  rel="${f#"$target"/}"
  # perl で 1〜3 を判定し、当たった行を `<行番号>\t<理由>\t<本文>` で出す
  hits="$(perl -ne '
    chomp; my $line = $_;
    if ($line =~ /devtool-boundary: allow(.*)$/) {
      my $why = $1; $why =~ s/^\s+|\s+$//g;
      print "$.\t理由の無い devtool-boundary: allow\t$line\n" if $why eq "";
      next;
    }
    my $is_comment = ($line =~ /^\s*#/);
    # 前後が英数字・`_`・`.` でなければ段とみなす (`<../x`・`${X:-../x}` も拾う。`...` や `a..b` は拾わない)
    my $dotdot = qr{(?:^|[^\w.])\.\.(?:$|[^\w.])};
    if ($is_comment) {
      print "$.\tshellcheck source= が .. を含む\t$line\n" if $line =~ /shellcheck\s+source=\S*\.\./;
      next;
    }
    if ($line =~ $dotdot) { print "$.\tパスの段としての ..\t$line\n"; next; }
    # `"$HOME"/dotfiles` のように引用符で区切った形も拾う
    if ($line =~ m{(?:~|\$HOME|\$\{HOME\})["\x27]?/dotfiles\b|/Users/[^/\s"\x27]+/dotfiles\b}) {
      print "$.\tdotfiles の checkout を直接指す\t$line\n";
    }
  ' "$f")" || { printf '✗ %s を読めない (検査できないので緑にしない)\n' "$rel"; exit 1; }
  if [ -n "$hits" ]; then
    fail=1
    while IFS=$'\t' read -r n why body; do
      printf '✗ %s:%s: %s: %s\n' "$rel" "$n" "$why" "$body"
    done <<< "$hits"
  fi
done <<< "$files"

if [ "$checked" -eq 0 ]; then
  printf '✗ 検査対象のファイルが 0 件 (%s に .md 以外のファイルが無い)。検査していないものを緑にしない\n' "$target"
  exit 1
fi
if [ "$fail" -ne 0 ]; then
  printf '\ndevtool/ は devtool/ の外を参照しない (devtool/README.md)。dotfiles の道具は bin/devtool-run から環境変数で受け取る。\n'
  printf 'どうしても要る行は、行内に devtool-boundary: allow <理由> を書く。\n'
  exit 1
fi
printf '✓ devtool の境界: 検査対象 %d 件に、外を参照する書き方は無い\n' "$checked"
