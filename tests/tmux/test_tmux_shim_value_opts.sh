#!/usr/bin/env bash
#
# bin/tmux shim の「値を 1 つ読み飛ばすグローバルオプション」集合が、実 tmux の usage と
# 一致していることを固定する。
#
# 🚨 なぜ (破れたときの実害): 将来の tmux が値取りグローバルオプションを 1 つ足すと、
#   `tmux -X <値> kill-server` の `<値>` を shim が subcommand 位置と誤認し、
#   「kill ではない」と判定して後続の kill-server に到達しない。`;` の連鎖検出とは別の入口
#   なので既存テストは 1 本も落ちず、拒否もされず、ただ本番サーバが死ぬ。
#
# この検査は shim を一切変更しない。「前提が崩れたら気づく」層だけを足す。
# 対象は tmux 本体を起動しない (usage を出させるだけ) ので本番サーバには触れない。
set -uo pipefail
unset CDPATH
unset TMUX TMUX_PANE

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SHIM="$ROOT_DIR/bin/tmux"

fails=0; checks=0
ok()  { checks=$((checks + 1)); printf '✓ %s\n' "$1"; }
bad() { checks=$((checks + 1)); printf '✗ %s\n' "$1" >&2; fails=$((fails + 1)); }

# --- 抽出関数 (canary と本走査は必ずこの 2 つを通す) ------------------------------------------

# usage テキスト (stdin) から `[-X arg]` 形 = 値取りグローバルオプションの文字を拾う。
# `[-2CDhlNuVv]` のような値なしクラスタは括弧内に空白が無いので当たらない。
usage_value_opts() {
  grep -oE '\[-[a-zA-Z] [^]]+\]' | sed -E 's/^\[-([a-zA-Z]) .*/\1/' | LC_ALL=C sort -u | tr -d '\n'
}

# shim ソース (stdin) から「値トークンを 1 つ読み飛ばす」と宣言している case ラベルを拾う。
# 対象は `-L) ... want=L` / `-f|-c|-T) ... want=discard` の形。`-L*` の inline 形は値を
# 読み飛ばさない (同じトークン内で完結する) ので対象外。
shim_value_opts() {
  grep -E '^[[:space:]]*-[a-zA-Z](\|-[a-zA-Z])*\)' | grep -E 'want=[a-zA-Z]' \
    | sed -E 's/^[[:space:]]*//; s/\).*//' | tr '|' '\n' | sed -E 's/^-//' \
    | grep -E '^[a-zA-Z]$' | LC_ALL=C sort -u | tr -d '\n'
}

# --- canary: 既知の入力で既知の答えが出ることを本走査の前に固定する --------------------------

canary_usage='usage: tmux [-2CDhlNuVv] [-c shell-command] [-f file] [-L socket-name]
            [-S socket-path] [-T features] [command [flags]]'
got=$(printf '%s\n' "$canary_usage" | usage_value_opts)
if [ "$got" = "LSTcf" ]; then ok "canary(usage): 既知 usage から LSTcf を抽出 (-2 等の値なしは拾わない)"
else bad "canary(usage): 期待 'LSTcf' / 実際 '$got' (抽出器が壊れている)"; fi

# shellcheck disable=SC2016  # shim ソースをリテラルで与える canary なので展開させない
canary_shim='  -L) [ "$seen_first_sub" = 0 ] && want=L; continue ;;
  -S) [ "$x" = 0 ] && want=S; continue ;;
  -f|-c|-T) [ "$x" = 0 ] && want=discard; continue ;;
  -L*) opt_L="${tok#-L}"; continue ;;
  -*) continue ;;
  if [ -n "$want" ]; then'
got=$(printf '%s\n' "$canary_shim" | shim_value_opts)
if [ "$got" = "LSTcf" ]; then ok "canary(shim): 既知スニペットから LSTcf を抽出 (-L* / -* は拾わない)"
else bad "canary(shim): 期待 'LSTcf' / 実際 '$got' (抽出器が壊れている)"; fi

# --- 本走査 ------------------------------------------------------------------------------------

# shim が固定している実体 tmux のパスを使う (PATH 先頭の shim 自身に解決させない)。
REAL_TMUX=$(sed -n 's/^REAL=\(.*\)$/\1/p' "$SHIM" | head -1)
if [ -z "$REAL_TMUX" ] || [ ! -x "$REAL_TMUX" ]; then
  bad "shim から実体 tmux を解決できない (REAL='$REAL_TMUX')"
  printf '\n検査 %d 件: fail=%d\n' "$checks" "$fails"; exit 1
fi

ver=$("$REAL_TMUX" -V 2>/dev/null || echo "unknown")
printf 'tmux 実体: %s (%s)\n' "$REAL_TMUX" "$ver"

# 不明なオプションを与えて usage を出させる (getopt が先に落ちるのでサーバへは接続しない)。
# 🚨 それでも socket を隔離する: 「サーバに触れない」という前提は -Q が無効オプションで
#   あり続けることに乗っているが、このテストが存在する理由はまさに「将来 tmux の
#   オプション集合が変わる」こと。前提が崩れた日に本番ではなく使い捨て socket へ向ける。
#   (規範: _claude/rules/tmux-probe-requires-socket-isolation.md)
PROBE_TMPDIR=$(mktemp -d)
trap 'rm -rf -- "$PROBE_TMPDIR"' EXIT INT TERM HUP
usage_err=$(TMUX_TMPDIR="$PROBE_TMPDIR" "$REAL_TMUX" -L "probe360-$$" -Q9 2>&1 >/dev/null || :)
tmux_set=$(printf '%s\n' "$usage_err" | usage_value_opts)
shim_set=$(shim_value_opts < "$SHIM")

# 🚨 抽出 0 件は「一致」ではなく失敗にする (書式が変わったら気づくため)。
[ -n "$tmux_set" ] || bad "tmux usage から値取りオプションを 1 個も抽出できなかった (usage の書式が変わった?)"
[ -n "$shim_set" ] || bad "bin/tmux から読み飛ばし集合を 1 個も抽出できなかった (shim の形が変わった?)"

if [ -n "$tmux_set" ] && [ -n "$shim_set" ]; then
  if [ "$tmux_set" = "$shim_set" ]; then
    ok "値取りグローバルオプション集合が一致 (tmux='$tmux_set' / shim='$shim_set' / $ver)"
  else
    bad "値取りグローバルオプション集合が不一致: tmux='$tmux_set' / shim='$shim_set' ($ver)
    → tmux 側にだけ在るものは shim が値を subcommand と誤認する (無音の under-block)。
      bin/tmux の '-f|-c|-T) ... want=discard' の行に足すこと。"
  fi
fi

want_checks=3
[ "$checks" -ge "$want_checks" ] || bad "検査が $checks 件しか走っていない (${want_checks} 件以上のはず)"
printf '\n検査 %d 件: fail=%d\n' "$checks" "$fails"
[ "$fails" -eq 0 ] || exit 1
echo "OK tmux shim value-taking option set"
