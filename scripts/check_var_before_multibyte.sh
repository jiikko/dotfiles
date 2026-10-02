#!/usr/bin/env bash
# check_var_before_multibyte.sh — bash / sh で `$var` の直後に ASCII 以外の文字を置いた箇所を落とす (issue 633)。
#
# なぜ: bash 3.2 (/bin/bash・/bin/sh) は UTF-8 のロケールで、`"(rc=$rc。)"` の `rc` に続く全角の先頭バイトまで
#   変数名として読む。set -u の下では `rc�: unbound variable` でその行で死に、set -u が無いと値と先頭バイトが黙って消える。
#   assert が落ちたときだけ走る失敗メッセージに出やすく、落ちた理由を出す前に死ぬので CI のログから原因が消える。
#   開発機の対話シェルの bash (Homebrew の 5 系) と zsh では起きないので、手元の実行では見つからない。
#
# 直し方: `${var}` で囲む (`"(rc=${rc}。)"`)。
# 意図的な例外は行内に `var-multibyte: allow` を書く (理由も添える)。
#
# 対象: discover_shell_scripts.sh の結果 + tests/ の *.sh・*.bats・拡張子の無いもの + githooks/。
#   zsh のファイルは除く (zsh は同じ形を正しく読む): zsh の shebang を持つもの、と .sh / .bats 以外で shebang が sh / bash で
#   ないもの (shebang の無い zshlib/*.zsh はこちらで落ちる)。shebang の無い *.sh は bash から source されるので含める。
#   入力ごとに件数の下限を見る (1 つの入力が空振りしても、他の入力の件数で緑にならないように)。
#
# 判定は 1 ファイルを先頭から読む字句の状態機械 (perl)。`$(…)` / `${…}` / `((…))`・`$((…))` / `` `…` `` を文脈として積み、
#   文脈ごとに引用の状態を持つ (`"${x:-"a"}"` や `"$(echo "a")"` の入れ子の引用も追う)。
#   検査する: 引用の外・二重引用符の中・引用の無い heredoc の本文 (展開されるので、本文の行頭の # もコメントではない)
#   検査しない: 単引用符の中・`$'…'` の中・コメント (地の文の語の先頭の # から行末)・引用付きの heredoc の本文 (区切りのどこかに
#     引用符か \ がある `<<'EOF'` / `<<\EOF` / `<<E"OF"`)・`\$`・`${…}`・特殊パラメータ (`$$` `$1` `$?` など)
#   二重引用符の中で直に積んだ `${…}` では `'` も `$'…'` も引用にならない (bash 3.2 は中を展開する。実測)。入れ子の `${` の中では引用になる
#   `${…}` と `((…))` の中の `#`・`<<` はコメント・heredoc と読まない (`<<<` は区切りの語が `<` で始まれないので heredoc にならない)
# 🚨 字句の近似は完全にはできないので、**ファイルの末尾で文脈か引用が閉じていなければ、そのファイルを失敗として報告する**
#   (状態を見失うと後ろの行が全部「引用の中」に見えて黙って素通りする。2 周目の敵対的レビューで実ファイル 4 本の
#   末尾 200 行あまりがこの形で無検査だった)。報告されたら、そのファイルの書き方か検査のどちらかを直す
# 検出しない形 (脅威モデルはうっかり書いた形で、意図的な迂回は対象外):
#   - 行の継続 (`\` + 改行) をまたいだ `$var` と全角 (`"x$rc\` の次の行が `。"` で始まる)
#   - 途中で状態を見失い、末尾までに偶然閉じ直したときの、その間の行 (末尾の検査は閉じ損ねしか見ない)
#   - `$(…)` の中の case のパターンの `)` (`$(case $x in a) …;; esac)`。`$(…)` を閉じたと読む。今の repo に 0 件)
#   - Makefile のレシピ (make の /bin/sh は bash 3.2 だが、shell のファイルではない)
# 本ファイルの説明文・メッセージには `$var` が字として入る (展開させない)。
# shellcheck disable=SC2016
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR" || { printf '✗ repo root へ移動できない\n'; exit 1; }

# テスト用の差し替え口: CHECK_VAR_MULTIBYTE_FILES (改行区切りの対象の一覧。発見を飛ばす) /
#   _DISCOVER (発見のコマンド) / _TESTS_DIR / _HOOKS_DIR / _PERL / _MIN_FILES (入力ごとの下限)
discover="${CHECK_VAR_MULTIBYTE_DISCOVER:-scripts/discover_shell_scripts.sh}"
tests_dir="${CHECK_VAR_MULTIBYTE_TESTS_DIR:-tests}"
hooks_dir="${CHECK_VAR_MULTIBYTE_HOOKS_DIR:-githooks}"
perl_bin="${CHECK_VAR_MULTIBYTE_PERL:-perl}"
min="${CHECK_VAR_MULTIBYTE_MIN_FILES:-100}"

for c in "$perl_bin" find sort head; do
  command -v "$c" >/dev/null 2>&1 || { printf '✗ %s が無い。検査できないので緑にしない\n' "$c"; exit 1; }
done

count_lines() { local n=0 l; while IFS= read -r l; do [ -n "$l" ] && n=$((n + 1)); done <<< "$1"; printf '%d' "$n"; }
# $1=入力の名前 $2=一覧 $3=下限
need() {
  local n; n="$(count_lines "$2")"
  [ "$n" -ge "$3" ] || { printf '✗ %s の対象が %d 件しかない (下限 %d。発見の壊れ)。緑にしない\n' "$1" "$n" "$3"; exit 1; }
}

if [ -n "${CHECK_VAR_MULTIBYTE_FILES:-}" ]; then
  files_raw="$CHECK_VAR_MULTIBYTE_FILES"
else
  d_out="$("$discover")" || { printf '✗ %s が失敗した。検査できないので緑にしない\n' "$discover"; exit 1; }
  need "$discover" "$d_out" "$min"
  [ -d "$tests_dir" ] || { printf '✗ %s が無い。検査できないので緑にしない\n' "$tests_dir"; exit 1; }
  t_out="$(find "$tests_dir" -type f \( -name '*.sh' -o -name '*.bats' -o ! -name '*.*' \))" \
    || { printf '✗ %s の列挙に失敗した。検査できないので緑にしない\n' "$tests_dir"; exit 1; }
  need "$tests_dir" "$t_out" "$min"
  [ -d "$hooks_dir" ] || { printf '✗ %s が無い。検査できないので緑にしない\n' "$hooks_dir"; exit 1; }
  h_out="$(find "$hooks_dir" -type f)" || { printf '✗ %s の列挙に失敗した。検査できないので緑にしない\n' "$hooks_dir"; exit 1; }
  need "$hooks_dir" "$h_out" 1
  files_raw="$(printf '%s\n%s\n%s\n' "$d_out" "$t_out" "$h_out" | sort -u)"
fi

zsh_bang='^#![[:space:]]*([^[:space:]]*/)?(env[[:space:]]+)?zsh([[:space:]]|$)'
sh_bang='^#![[:space:]]*([^[:space:]]*/)?(env[[:space:]]+)?(ba)?sh([[:space:]]|$)'
files=()
while IFS= read -r f; do
  [ -n "$f" ] || continue
  [ -f "$f" ] || continue
  [ -r "$f" ] || { printf '✗ 読めないファイル: %s (検査できないので緑にしない)\n' "$f"; exit 1; }
  first="$(head -n 1 "$f")" || { printf '✗ 先頭行を読めない: %s\n' "$f"; exit 1; }
  [[ $first =~ $zsh_bang ]] && continue
  case "$f" in
    *.sh | *.bats) ;;
    *) [[ $first =~ $sh_bang ]] || continue ;;
  esac
  files+=("$f")
done <<< "$files_raw"

[ "${#files[@]}" -ge 1 ] || { printf '✗ 検査対象が 0 件。緑にしない\n'; exit 1; }

prog="$(cat <<'PERL'
use strict; use warnings;
my $bad = 0;
# 引用の無い heredoc の本文: 偶数個の \ の後ろの $識別子 + 非 ASCII (奇数個なら \$ でリテラル。$$ は pid)
my $body_re = qr/(?:^|[^\\\$])(?:\\\\)*\$[A-Za-z_][A-Za-z0-9_]*[\x80-\xff]/;
for my $f (@ARGV) {
  open(my $fh, "<:raw", $f) or die "open $f: $!\n";
  # 文脈のスタック。各文脈は種類と、その中の引用の状態と、括弧の深さを持つ
  #   k: T=ファイルの地 C=$( … ) B=${ … } R=(( … )) / $(( … )) Q=` … `
  #   st: N=引用の外 D=二重引用符 S=単引用符 A=$'…'
  my @stk = ({k => "T", st => "N", d => 0});
  my @pending;      # この行で始まった heredoc: [tag, quoted, strip_tabs]
  my $hd;           # 本文を読んでいる heredoc
  while (my $line = <$fh>) {
    my $ln = $.;
    (my $body = $line) =~ s/\r?\n\z//;
    my $allow = $body =~ /var-multibyte: allow/;
    if ($hd) {
      my $t = $body; $t =~ s/^\t+// if $hd->[2];
      if ($t eq $hd->[0]) { $hd = shift @pending; next; }
      next if $hd->[1];
      if (!$allow && $body =~ $body_re) { print "$f:$ln: $body\n"; $bad++; }
      next;
    }
    my @c = split //, $body;
    my $hit = 0;
    for (my $i = 0; $i < @c; $i++) {
      my $x = $stk[-1];
      my ($ch, $nx, $n2) = ($c[$i], $c[$i+1] // "", $c[$i+2] // "");
      if ($x->{st} eq "S") { $x->{st} = "N" if $ch eq "'"; next; }
      if ($x->{st} eq "A") { if ($ch eq "\\") { $i++; next; } $x->{st} = "N" if $ch eq "'"; next; }
      if ($ch eq "\\") { $i++; next; }
      if ($ch eq "\$") {
        if ($nx eq "(" && $n2 eq "(") { push @stk, {k => "R", st => "N", d => 2}; $i += 2; next; }
        if ($nx eq "(") { push @stk, {k => "C", st => "N", d => 0}; $i++; next; }
        # dq は二重引用符の中で直に積んだ 1 段目だけ。入れ子の ${ の中では ' が引用を開く (bash 3.2 の実測)
        if ($nx eq "{") { push @stk, {k => "B", st => "N", d => 0, dq => $x->{st} eq "D"}; $i++; next; }
        if ($nx eq "'" && $x->{st} eq "N" && $x->{k} ne "R" && !$x->{dq}) { $x->{st} = "A"; $i++; next; }
        if ($nx =~ /[\$?!#\-@*0-9]/) { $i++; next; }
        if ($nx =~ /[A-Za-z_]/) {
          my $j = $i + 1;
          $j++ while $j < @c && $c[$j] =~ /[A-Za-z0-9_]/;
          $hit = 1 if $j < @c && ord($c[$j]) >= 0x80;
          $i = $j - 1;
        }
        next;
      }
      if ($ch eq "`") {
        if ($x->{k} eq "Q" && $x->{st} eq "N") { pop @stk; } else { push @stk, {k => "Q", st => "N", d => 0}; }
        next;
      }
      if ($x->{st} eq "D") { $x->{st} = "N" if $ch eq "\""; next; }
      # ここからは引用の外 (N)
      if ($ch eq "'") { $x->{st} = "S" unless $x->{dq}; next; }   # 二重引用符の中の ${…} の ' はただの文字
      if ($ch eq "\"") { $x->{st} = "D"; next; }
      if ($x->{k} eq "R") {
        if ($ch eq "(") { $x->{d}++; } elsif ($ch eq ")") { pop @stk if --$x->{d} == 0; }
        next;
      }
      if ($x->{k} eq "B") { pop @stk if $ch eq "}"; next; }
      if ($ch eq "#" && ($i == 0 || $c[$i-1] =~ /[\s;&|()]/)) { last; }
      if ($ch eq "(" && $nx eq "(" && ($i == 0 || $c[$i-1] =~ /[\s;&|(!]/)) { push @stk, {k => "R", st => "N", d => 2}; $i++; next; }
      if ($ch eq "(") { $x->{d}++; next; }
      if ($ch eq ")") {
        if ($x->{d} > 0) { $x->{d}--; }
        elsif ($x->{k} eq "C") { pop @stk; }   # 地の文の深さ 0 の ) は case のパターンなので何もしない
        next;
      }
      if ($ch eq "<" && $nx eq "<" && ($i == 0 || $c[$i-1] ne "<")) {
        my $rest = join "", @c[$i+2 .. $#c];
        if ($rest =~ /^(-?)[ \t]*([^\s;&|<>()]+)/) {
          my ($dash, $word, $len) = ($1, $2, length($&));
          my $quoted = $word =~ /['"\\]/ ? 1 : 0;
          (my $tag = $word) =~ s/['"\\]//g;
          push @pending, [$tag, $quoted, $dash ne "" ? 1 : 0] if $tag ne "";
          $i += 1 + $len; next;
        }
      }
    }
    if ($hit && !$allow) { print "$f:$ln: $body\n"; $bad++; }
    # heredoc の本文は、その行で開いた引用が閉じた後の改行から始まる (複数行の引用が開いている間は始めない)
    $hd = shift @pending if @pending && $stk[-1]{st} !~ /^[SDA]$/;
  }
  close $fh;
  my $x = $stk[-1];
  if (@stk > 1 || $x->{st} ne "N" || $hd) {
    my $what = $hd ? "heredoc の本文 ($hd->[0])" : join(" > ", map { $_->{k} . ($_->{st} ne "N" ? "/" . $_->{st} : "") } @stk);
    print "$f: 末尾で字句の状態が閉じない ($what)。検査が途中で状態を見失った可能性がある\n"; $bad++;
  }
}
exit($bad ? 3 : 0);
PERL
)"

out="$("$perl_bin" -e "$prog" "${files[@]}" 2>&1)"
rc=$?

if [ "$rc" -eq 3 ]; then
  printf '✗ `$var` の直後に ASCII 以外の文字がある (bash 3.2 が変数名として読む。issue 633):\n'
  printf '%s\n' "$out" | sed 's/^/    /'
  printf '  直し方: ${var} で囲む。意図的な例外は行内に `var-multibyte: allow` を書く (理由も添える)\n'
  exit 1
elif [ "$rc" -ne 0 ]; then
  printf '✗ perl が失敗した (rc=%d)。検査できないので緑にしない\n%s\n' "$rc" "$out"
  exit 1
fi

printf '✓ $var の直後の ASCII 以外の文字: %d ファイルに該当なし\n' "${#files[@]}"
