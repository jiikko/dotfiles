#!/bin/sh
# test_bare_bin_guard.sh <repo root> <stub dir> — テストへ渡す PATH を stdout に出す (Makefile の runner が使う)。
#
# 開発機の PATH には ~/dotfiles/bin があり、CI には無い。テストが bin の道具を素の名前で呼ぶと、
# 手元は緑・CI は rc=127 になる。<repo root>/bin の名前のうち、dotfiles の bin 以外の
# PATH のどこにも無いものについて「素の名前で呼んだ」と言って rc=127 で落ちる stub を <stub dir> に作り、
# それを先頭に置いた PATH を返す。手元でも CI と同じく落ち、落ちた理由が出る。
#
# PATH から bin を外す形にしない。bin/tmux は本番 tmux サーバを非対話の kill から守る shim で、テストの中の
# 素の tmux もこれを通って守られている (bin/tmux 冒頭)。外すとその防御まで消える。同じ名前の実体が他に在る
# shim (tmux / gopls) は stub にしないので、手元では今までどおり shim に当たる (CI では実体に当たる)。
# テストが自分で "$ROOT/bin" を PATH の先頭に足す形は、stub より前に来るので今までどおり通る。
#
# 検出しない形: テストの子が PATH を自分で組み直す (PATH=/usr/bin:/bin 等) / 開発機の PATH の別の場所
# (~/.local/bin 等) にも同じ名前の別の実体が在る道具 (stub にしない) / make の runner (run_tests 系・test-bats) を
# 通らない起動 (テストの直接実行・Makefile の recipe が直接起こすテスト (test-go-packages-index・test-pro-con-slow)・
# Go のテストの exec・workflow が make を介さず起こすもの)。
# CI と食い違う形: stub は「在るが落ちる」で、CI の「無い」とは違う。`command -v X` で在るかを見る分岐は手元と CI で
# 逆へ進む (在ることだけを assert するテストは手元で緑のまま / `command -v X || exit 77` で skip するテストは手元で落ちる)。
# bin のスクリプトの中の分岐も同じ (bin/skill-eval の `command -v ratelimit` は、CI では空で飛ばし、手元では stub を拾う)。
# 在るかを見る側を stub に合わせて騙すと、検査が 2 種類の「在る」を区別する必要が出るので、ここでは揃えない。
set -eu
unset CDPATH

root=${1:?usage: test_bare_bin_guard.sh <repo root> <stub dir>}
stubs=${2:?usage: test_bare_bin_guard.sh <repo root> <stub dir>}
[ -d "$root/bin" ] || { echo "✗ test_bare_bin_guard: $root/bin が無い" >&2; exit 1; }
[ -n "${PATH:-}" ] || { echo "✗ test_bare_bin_guard: PATH が空" >&2; exit 1; }
mkdir -p "$stubs"
MARK=.bare-bin-stubs
: > "$stubs/$MARK"

# 「dotfiles の bin」とみなす dir。-ef (同じ実体) で比べるので、symlink・末尾の / の揺れを吸う。
# 外側の runner が作った stub の dir (目印のファイルを持つ) も数える。runner を入れ子で起こすテストでは、
# 外側の stub が全部の名前を持つので、他の場所に在ると数えると stub が 0 本になる。
is_dotfiles_bin() {
  [ -d "$1" ] || return 1
  [ -f "$1/$MARK" ] && return 0
  # HOME だけで見分けない。偽の HOME で runner を起こすテストでは、本物の ~/dotfiles/bin が他の場所に見える。
  # 別の clone・worktree の bin も同じ名前を持つので、隣に _claude/settings.json が在る bin は dotfiles の bin に数える
  # (この guard 自身を目印にしない。guard を持たない古い checkout の bin を他の場所と数えてしまう)。
  [ -f "$1/../_claude/settings.json" ] && return 0
  [ "$1" -ef "$root/bin" ] && return 0
  [ -n "${HOME:-}" ] && [ -d "$HOME/dotfiles/bin" ] && [ "$1" -ef "$HOME/dotfiles/bin" ] && return 0
  return 1
}

elsewhere() {  # $1=名前。dotfiles の bin 以外の PATH の dir に実行できるファイルが在れば 0
  rest=$PATH
  while :; do
    case "$rest" in
      *:*) d=${rest%%:*}; rest=${rest#*:}; more=1 ;;
      *) d=$rest; more=0 ;;
    esac
    [ -n "$d" ] || d=.
    # 見つけたファイルは実体まで解決し、実体の dir で判定する。dotfiles の bin (別の checkout も) を指す symlink
    # (~/.local/bin/x -> ~/dotfiles/bin/x) は CI には無いので他の場所に数えない。逆に <root>/bin/x が外の実体を指す
    # symlink でも、外の実体は CI にも在りうるので他の場所に数える。$root/bin/$1 と -ef で比べる形にしない
    # (worktree から起こすと本体の bin を指す symlink を取りこぼし、外を指す bin の symlink を stub にする)。
    # 併用もしない: -ef は symlink を辿るので、外の実体と、それを指す <root>/bin/x を同じと判定して stub にする。
    # その代わり、bin のファイルの hardlink を別の dir に置いた形は他の場所に数える (開発機にその置き方は無い)。
    if ! is_dotfiles_bin "$d" && [ -f "$d/$1" ] && [ -x "$d/$1" ]; then
      real=$(readlink -f "$d/$1") || return 0
      is_dotfiles_bin "${real%/*}" || return 0
    fi
    [ "$more" -eq 1 ] || return 1
  done
}

n=0
for f in "$root"/bin/*; do
  [ -f "$f" ] && [ -x "$f" ] || continue
  name=${f##*/}
  # 名前を stub の本文へ書くので、ファイル名に使う普通の文字だけを通す
  case "$name" in *[!A-Za-z0-9._-]*) continue ;; esac
  elsewhere "$name" && continue
  cat > "$stubs/$name" <<EOF
#!/bin/sh
echo "✗ bin の道具 '$name' を PATH から探して呼んだ (素の名前の呼び出しか、PATH を探す shim の経由)。CI の PATH には dotfiles の bin/ が無いので rc=127 になる。テストからは \"\\\$ROOT/bin/$name\" のようにパスで呼ぶ (runtimeout は runtimeout_resolve の \"\\\$RUNTIMEOUT\")" >&2
exit 127
EOF
  chmod +x "$stubs/$name"
  n=$((n + 1))
done
[ "$n" -gt 0 ] || { echo "✗ test_bare_bin_guard: stub を 1 本も作らなかった ($root/bin の列挙が空振りしている)" >&2; exit 1; }
echo "[bare-bin] bin にしか無い道具 $n 本を、素の名前で呼ぶと落ちる stub にした" >&2
printf '%s:%s\n' "$stubs" "$PATH"
