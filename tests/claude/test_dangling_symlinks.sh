#!/usr/bin/env bash
#
# $HOME 配下の「~/dotfiles を指す symlink」に dangling (リンク先消失) が無いことを検証する。
#
# なぜ: dotfiles 側でファイルを削除・rename すると、setup.sh を再実行するまで壊れたリンクが
# 残り、Claude Code が skill/agent/hook を silent に見失う (issue 001 項目 11)。setup.sh には
# 掃除ロジックがあるが実行時のみ。このテストで「壊れたまま使い続けている」状態を make test が検出する。
#
# 対象は readlink が $HOME/dotfiles/ 配下を指すリンクだけ。ユーザーが手動で張った
# 別由来のリンクは対象外 (setup.sh の掃除ロジックと同じ基準)。
# dotfiles 未 symlink の環境 (CI 等) では対象リンクが 0 件になり素通しで pass する。
#
# 🚨 **深さを切らない**。以前は `~/.claude` を `-maxdepth 2` で掘っていたが、これは
# 「per-file リンクは agents/<f> や skills/<name> の深さ 2 まで」という**その時点の**
# 置き場所を前提にしていた。setup.sh が `~/.config/nvim/init.lua` (深さ 3) にも張っている
# ことは端から射程外で、実測 2026-09-05 に `~/.config/nvim/` へ dangling を置いても
# **テストが pass した**。深さや置き場所の契約が変わったときに黙って対象外になる形なので、
# 走査は深さを切らずに掘り、下の canary で射程そのものを固定する
# (_claude/rules/claude-md-maintenance.md「ディレクトリ階層・置き場所の契約を変えた」)。
#
# 🚨 $HOME だけは `-maxdepth 1`。全体を掘るのは現実的でない (ホーム全部の走査になる)。
# **setup.sh が $HOME 直下・~/.claude・~/.config 以外へ張り始めたら、下の ROOTS に足すこと。**
# 走査コストは実測 2026-09-05 で ~/.claude 0.32s (15k entries) / ~/.config 0.21s (18k entries)。

set -euo pipefail
unset CDPATH

fail=0

# 深さを切らずに 1 つの根を掘る。**深さ制限を足すならここ 1 箇所**なので、下の canary A が
# そのままそれを検出する。
find_links_deep() {  # $1=根
  [ -d "$1" ] || return 0
  find "$1" -type l 2>/dev/null
  return 0
}

# 走査対象の根。setup.sh の `ln -s` の宛先がここに収まっている必要がある。
scan_links() {
  find "$HOME" -maxdepth 1 -type l 2>/dev/null   # ホーム全体は掘れないのでここだけ深さ 1
  find_links_deep "$HOME/.claude"
  find_links_deep "$HOME/.config"
  return 0
}

links="$(scan_links)"

# --- canary: 射程が縮んだら落とす -----------------------------------------------------
# 「dangling が 0 件」は**走査が空振りしても**同じ結果になる (収集 0 件を成功にしない:
# _claude/rules/verify-execution-not-just-exit-code.md)。射程が縮む壊れ方は 2 通りあるので、
# canary も 2 つ要る。🚨 どちらも**本走査と同じ関数**を通す (式をコピーすると本走査の破損を
# 検出しない)。
#
# canary A: 深さ制限。実環境には今のところ深さ 3 以上の dotfiles リンクが ~/.claude に無く、
# 実物では pin できない (実測 2026-09-05: 全 105 件が深さ 1〜2)。fixture で固定する。
_cdir="$(mktemp -d)"
trap 'rm -rf "$_cdir"' EXIT
mkdir -p "$_cdir/a/b/c/d"
ln -s "$HOME/dotfiles/__canary_nonexistent__" "$_cdir/a/b/c/d/deep.link"
if ! find_links_deep "$_cdir" | grep -qxF "$_cdir/a/b/c/d/deep.link"; then
  echo "FAIL: find_links_deep が深さ 5 のリンクを拾えない (深さ制限が入った。検査の射程が縮んでいる)" >&2
  fail=1
fi

# canary B: 根の漏れ。**実在する既知のリンク**が走査結果に入っていることを確かめる
# (`~/.config` の根を落とすと落ちる)。「そのリンクが実在するときだけ」検査する
# (未 setup の環境で誤検出しないため)。
for canary in "$HOME/.config/nvim/init.lua" "$HOME/.claude/CLAUDE.md"; do
  [ -L "$canary" ] || continue
  case "$(readlink "$canary")" in "$HOME"/dotfiles/*) ;; *) continue ;; esac
  if ! printf '%s\n' "$links" | grep -qxF "$canary"; then
    echo "FAIL: 走査が $canary を拾えていない (深さ制限か根の漏れ。検査の射程が縮んでいる)" >&2
    fail=1
  fi
done

# --- 判定 -----------------------------------------------------------------------------
# stdin のリンク一覧から「dotfiles を指す dangling」を報告し、1 件でもあれば 1 を返す。
# 🚨 関数にするのは canary C が**本走査と同じ実装**を通すため (式をコピーすると本走査の
# 破損を検出しない)。
check_dangling() {
  local link target rc=0
  while IFS= read -r link; do
    [ -n "$link" ] || continue
    target=$(readlink "$link")
    case "$target" in
      "$HOME"/dotfiles/*)
        [ -e "$link" ] && continue
        echo "FAIL: dangling symlink $link -> $target (dotfiles 側で削除/移動済み。setup.sh を再実行して掃除)" >&2
        rc=1
        ;;
    esac
  done
  return "$rc"
}

# canary C: 判定そのもの。**実環境が綺麗なときは本走査が何も検出しない**ので、判定を
# 「常に成立しない」へ変えても緑のまま通る (実測 2026-09-05)。両方向を fixture で固定する。
ln -s "$HOME/dotfiles/__canary_nonexistent__" "$_cdir/dead.link"
ln -s "$HOME/dotfiles" "$_cdir/alive.link"          # 実体のある dotfiles 由来リンク
ln -s "/__canary_not_dotfiles__" "$_cdir/other.link"  # dotfiles 由来でない dangling (対象外)
if printf '%s\n' "$_cdir/dead.link" | check_dangling 2>/dev/null; then
  echo "FAIL: 判定が dangling を見逃す (dotfiles を指す壊れたリンクを検出できていない)" >&2
  fail=1
fi
if ! printf '%s\n%s\n' "$_cdir/alive.link" "$_cdir/other.link" | check_dangling 2>/dev/null; then
  echo "FAIL: 判定が誤検出する (実体のあるリンク / dotfiles 由来でないリンクを dangling と報告)" >&2
  fail=1
fi

# --- 本走査 ---------------------------------------------------------------------------
printf '%s\n' "$links" | check_dangling || fail=1

if [ "$fail" -ne 0 ]; then
  exit 1
fi

echo "OK: dotfiles 由来の dangling symlink なし"
