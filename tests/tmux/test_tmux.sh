#!/usr/bin/env zsh

set -euo pipefail
unset CDPATH

TMUX_BIN_PATH=${TMUX_BIN:-tmux}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
CONF_FILE="$ROOT_DIR/_tmux.conf"
TMUX_TMPDIR=$(mktemp -d)
export TMUX_TMPDIR
SOCKET_NAME="dotfiles-test-$$"
log_file="$TMUX_TMPDIR/tmux.log"

# resurrect / debounce 保存を実データから隔離する。
# _tmux.conf には window-linked hook（scripts/tmux_resurrect_debounced_save.sh を
# 走らせる debounce 保存）と continuum autosave があり、テストで conf をロードして
# セッションを作ると保存が走り得る。resurrect の保存先（helpers.sh）は
#   1) @resurrect-dir / 2) ~/.tmux/resurrect が在ればそれ / 3) $XDG_DATA_HOME/...
# の順で解決されるため、XDG_DATA_HOME だけ差し替えても実 HOME に ~/.tmux/resurrect が
# 在る環境では本物を触り得る。そこで HOME ごと temp に隔離して全候補を temp に倒す。
# DOTFILES_DIR は明示固定する（conf の plugin パスは ${DOTFILES_DIR:-$HOME/dotfiles}
# なので HOME を temp にすると $HOME/dotfiles が壊れるため）。
# 状態隔離 (HOME/XDG/TT_DEBOUNCE を TMUX_TMPDIR 配下へ) は lib へ集約 (bench/smooth_scroll と共通)。
source "$ROOT_DIR/tests/tmux/lib/isolate_env.sh"

if ! command -v "$TMUX_BIN_PATH" >/dev/null 2>&1; then
  print -u2 "Error: tmux binary not found. Install tmux or set \$TMUX_BIN."
  exit 1
fi

if [[ ! -f "$CONF_FILE" ]]; then
  print -u2 "Error: tmux config $CONF_FILE not found."
  exit 1
fi

TMUX_CMD=("$TMUX_BIN_PATH" -L "$SOCKET_NAME" -f "$CONF_FILE")

cleanup() {
  "$TMUX_BIN_PATH" -L "$SOCKET_NAME" kill-server >/dev/null 2>&1 || true
  # probe サーバも全 exit 経路で確実に回収する。probe は専用 TMUX_TMPDIR で起動するので
  # kill も同じ TMUX_TMPDIR を渡さないと別 socket パスを見て殺し損ね、プロセスが孤児化して
  # leak する（その leak が continuum の Gate2 を恒常的に破り restore 不発を招いていた）。
  if [[ -n "${probe_socket:-}" && -n "${probe_dir:-}" ]]; then
    env TMUX_TMPDIR="$probe_dir" "$TMUX_BIN_PATH" -L "$probe_socket" kill-server >/dev/null 2>&1 || true
  fi
  # probe_dir は正常系では probe 直後に rm 済み (冪等)。probe 起動失敗 → handle_result の
  # exit 経路では明示 rm に到達しないため、trap 側でも確実に消す (temp dir leak 防止)。
  if [[ -n "${probe_dir:-}" ]]; then
    rm -rf "$probe_dir"
  fi
  rm -rf "$TMUX_TMPDIR"
}
# EXIT に加え INT/TERM でも cleanup を走らせる。シグナルは `exit 130` 経由で EXIT trap に集約し
# 二重実行を避ける。テスト中断で cleanup が走らず named socket サーバ（と probe）が孤児化する経路を
# 塞ぐ（孤児が continuum の Gate2 を破り復元不発を招く今回の真因クラスを、テスト側でも作らない）。
trap cleanup EXIT
trap 'exit 130' INT TERM

handle_result() {
  local log="$1"
  local desc="$2"
  local allow_skip="$3"
  if grep -qiE "operation not permitted|permission denied" "$log"; then
    if [[ "$allow_skip" == "skip" ]]; then
      print -u2 "[test-tmux:zsh] skipped: tmux cannot create sockets in this environment"
      cat "$log" >&2
      # ⚠️ 丸ごと skip は **exit 77** (automake の慣例)。0 で抜けると runner が [ok] と数える
      # (tests/CLAUDE.md / issue 139)
      exit 77
    fi
  fi
  print -u2 "[test-tmux:zsh] $desc"
  cat "$log" >&2
  exit 1
}

run_with_check() {
  local log="$1"
  local desc="$2"
  local allow_skip="$3"
  shift 3
  if ! "$@" >"$log" 2>&1 || grep -qi "error" "$log"; then
    handle_result "$log" "$desc" "$allow_skip"
  fi
}

probe_dir=$(mktemp -d)
probe_log="$probe_dir/probe.log"
probe_socket="dotfiles-probe-$$"
run_with_check "$probe_log" "probe session failed" "skip" \
  env TMUX_TMPDIR="$probe_dir" "$TMUX_BIN_PATH" -L "$probe_socket" new-session -d -s dotfiles_probe "tail -f /dev/null"
# kill も probe と同じ TMUX_TMPDIR で行う。これを付けないと別 socket パスを見て probe を殺せず、
# rm -rf "$probe_dir" で socket ファイルだけ消えてサーバプロセスが孤児化＝leak する（trap cleanup も同様に修正済み）。
env TMUX_TMPDIR="$probe_dir" "$TMUX_BIN_PATH" -L "$probe_socket" kill-server >/dev/null 2>&1 || true
rm -rf "$probe_dir"

print "[test-tmux:zsh] starting server with $CONF_FILE"
run_with_check "$log_file" "failed to create test session" "skip" \
  "${TMUX_CMD[@]}" new-session -d -s dotfiles_test "tail -f /dev/null"

# conf ロード時に tmux が出す設定警告 (invalid option / unknown ...) を検出する。
# これらは run_with_check の grep "error" では拾えない
# (例: "invalid option: pane-scrollbars" は "error" を含まない)。
# また -f での起動時ロード (上の new-session) は警告を呼び出し元の stderr へ返さない
# (サーバ側に記録されるだけ) ため、source-file を明示実行して呼び出しの出力に警告を
# 捕捉する (実測: source-file は呼び出し元へ返す / -f 起動は返さない)。
# 古い tmux でバージョンガード無しに新オプションを足すと壊れる回帰
# (pane-scrollbars は tmux 3.6+ 専用) を防ぐのが目的。
print "[test-tmux:zsh] checking config load for tmux warnings"
"${TMUX_CMD[@]}" source-file "$CONF_FILE" >"$log_file" 2>&1 || true
conf_warnings=$(grep -niE 'invalid option|unknown option|unknown command|unknown key|invalid or unknown' "$log_file" || true)
if [[ -n "$conf_warnings" ]]; then
  print -u2 "[test-tmux:zsh] tmux reported config warnings while loading $CONF_FILE:"
  print -u2 "$conf_warnings"
  exit 1
fi

print "[test-tmux:zsh] dumping global options"
run_with_check "$log_file" "show-options failed" "fail" \
  "${TMUX_CMD[@]}" show-options -g

print "[test-tmux:zsh] verifying custom key bindings can be listed"
run_with_check "$log_file" "list-keys failed" "fail" \
  "${TMUX_CMD[@]}" list-keys

# window-status フォーマットのスタイル指定が壊れていないか検査する。
# #{?cond,#[a#,b],...} のように条件分岐の中で #[...] を使うとき、#[...] 内の
# カンマを #, でエスケープし忘れると、tmux は条件分岐の区切りカンマと誤認し、
# スタイル指定が途中で割れて window 名の前に "fg=colour231]" のようなリテラルが
# 漏れる (zoom 強調色の実装で実際に踏んだ回帰。source-file 警告は出ないため
# 上のロード検査では拾えず、描画時に初めて壊れる)。
# 実 format をズーム/非ズーム両状態で展開し、整形済みの #[...] を除去した残りに
# fg=/bg=/colourN が残っていないか検査する (display-message はパイプ出力時に
# 正規タグも #[...] のまま出すので、まず正規タグを除去してから漏れを判定する)。
assert_no_style_leak() {
  local label="$1" expanded="$2" residual
  residual=$(print -r -- "$expanded" | sed -E 's/#\[[^]]*\]//g')
  if grep -qE 'fg=|bg=|colour[0-9]' <<< "$residual"; then
    print -u2 "[test-tmux:zsh] window-status format leaked a style literal ($label):"
    print -u2 "  expanded: $expanded"
    print -u2 "  residual: $residual"
    exit 1
  fi
}

print "[test-tmux:zsh] checking window-status formats expand without leaked style literals"
"${TMUX_CMD[@]}" split-window -d -t dotfiles_test "tail -f /dev/null" >"$log_file" 2>&1 \
  || handle_result "$log_file" "split-window failed" "fail"
fmt_current=$("${TMUX_CMD[@]}" show-options -gv window-status-current-format)
fmt_other=$("${TMUX_CMD[@]}" show-options -gv window-status-format)
for zoom_state in unzoomed zoomed; do
  if [[ "$zoom_state" == zoomed ]]; then
    "${TMUX_CMD[@]}" resize-pane -t dotfiles_test -Z >"$log_file" 2>&1 \
      || handle_result "$log_file" "resize-pane -Z failed" "fail"
  fi
  assert_no_style_leak "current/$zoom_state" \
    "$("${TMUX_CMD[@]}" display-message -t dotfiles_test -p "$fmt_current")"
  assert_no_style_leak "other/$zoom_state" \
    "$("${TMUX_CMD[@]}" display-message -t dotfiles_test -p "$fmt_other")"
done

# status-left も同じ「条件分岐内 #[...] のカンマ未エスケープ」回帰の対象
# (scratch セッション検出時のソフト点滅 flash 分岐で #[...] を使う)。
print "[test-tmux:zsh] checking status-left expands without leaked style literals"
fmt_sl=$("${TMUX_CMD[@]}" show-options -gv status-left)
# 通常(非 scratch)分岐
assert_no_style_leak "status-left/normal" \
  "$("${TMUX_CMD[@]}" display-message -t dotfiles_test -p "$fmt_sl")"
# scratch の flash 分岐を exercise: 検出条件 session_name==scratch を、このテストセッション名
# (dotfiles_test) に一致するよう差し替えると flash 分岐が選ばれる (条件の差し替えはスタイルの
# エスケープ検査結果に影響しない。点滅 #() は client 非 attach で空展開だが #[...] 崩れは検出可)。
fmt_sl_flash=${fmt_sl/,scratch/,dotfiles_test}
if [[ "$fmt_sl_flash" != "$fmt_sl" ]]; then
  assert_no_style_leak "status-left/scratch-flash" \
    "$("${TMUX_CMD[@]}" display-message -t dotfiles_test -p "$fmt_sl_flash")"
else
  print -u2 "[test-tmux:zsh] warning: status-left の scratch 検出条件が見つからず flash 分岐を検査できませんでした (条件式が変わった可能性)"
fi

# status-right の prefix キーガイド: 押下中 / 非押下の両分岐が同じ表示幅であること。
# 幅が変わると prefix を押すたび window list の描画幅が動く (status-left の島を 20 セル固定に
# しているのと同じ理由)。conf 側は両分岐を #{p20:} で包んでいるが、片方の p を外す・文言を
# 20 セル超へ伸ばす・片方だけ幅を変える、のどれでも壊れるので **展開後の表示幅** で pin する
# (ソース文字列の grep では「20 と書いてあるか」しか見られず、実際の幅は見ていない)。
print "[test-tmux:zsh] checking status-right key guide keeps both branches the same width"
fmt_sr=$("${TMUX_CMD[@]}" show-options -gv status-right)
if [[ "$fmt_sr" != *'client_prefix'* ]]; then
  print -u2 "[test-tmux:zsh] status-right が client_prefix で分岐していない (キーガイドの構造が変わった)"
  print -u2 "  status-right: $fmt_sr"
  exit 1
fi
# 条件を差し替えて両分岐を展開する (status-left の scratch flash 分岐と同じ手口)。
# tmux の #{?...} はリテラル 1 を真と見ないので、値を持つ user option を経由させる
sr_width() {  # $1=@sr_probe に入れる値 → #[...] を除いた表示幅
  local expanded stripped
  "${TMUX_CMD[@]}" set -g @sr_probe "$1" >/dev/null
  expanded=$("${TMUX_CMD[@]}" display-message -t dotfiles_test -p "${fmt_sr/client_prefix/@sr_probe}")
  stripped=$(print -r -- "$expanded" | sed -E 's/#\[[^]]*\]//g')
  print -r -- "${(m)#stripped}"
}
w_armed=$(sr_width 1)
w_idle=$(sr_width "")
sr_len=$("${TMUX_CMD[@]}" show-options -gv status-right-length)
"${TMUX_CMD[@]}" set -gu @sr_probe >/dev/null
if (( w_armed == 0 )); then
  print -u2 "[test-tmux:zsh] status-right の prefix 分岐が空 (キーガイドが出ない)"
  exit 1
fi
if (( w_armed != w_idle )); then
  print -u2 "[test-tmux:zsh] status-right の幅が prefix 押下で変わる (window list がガタつく): armed=${w_armed} idle=${w_idle}"
  print -u2 "  両分岐を同じ #{pN:} で包むこと (_tmux.conf の status-right ブロック)"
  exit 1
fi
if (( w_armed != sr_len )); then
  print -u2 "[test-tmux:zsh] status-right の表示幅 ${w_armed} が status-right-length ${sr_len} と一致しない (切れる/余る)"
  exit 1
fi
assert_no_style_leak "status-right/armed" \
  "$("${TMUX_CMD[@]}" set -g @sr_probe 1 >/dev/null; "${TMUX_CMD[@]}" display-message -t dotfiles_test -p "${fmt_sr/client_prefix/@sr_probe}")"
"${TMUX_CMD[@]}" set -gu @sr_probe >/dev/null
print "[test-tmux:zsh] ok: status-right は両分岐とも ${w_armed} セル (status-right-length と一致)"

# glogx popup (prefix g / C-g) の git repo ガード。repo 外では popup を出さず toast に
# 落ちることを、conf 内の実際の条件式を取り出して両方向 (repo 内 / repo 外) で実行して固定する。
# 条件式を conf から抜いて使うので、ガードの書き換え時にテスト側の複製が腐らない。
# ⚠️ if-shell の -t は入れ子コマンドの format 展開には効かない (実測 tmux 3.7b) ため、
# 判定させたいセッションを直前に作って「現在のセッション」にしてから -t なしで実行する。
print "[test-tmux:zsh] checking the glogx popup binding guards against non-git directories"
# glogx popup は prefix g と C-g の 2 本ある。片方だけ直してもう片方が素通しになる
# ドリフトを防ぐため、両方に同じガードが入っていることを確認する。
# head -1 は上流に SIGPIPE を投げて pipefail で落ちるため tail -1 で受ける
glogx_bindings=$("${TMUX_CMD[@]}" list-keys | grep -F 'glogx' || true)
guard_line=''
for key_spec in '-T root:C-g' '-T prefix:prefix g'; do
  table=${key_spec%%:*}
  key_desc=${key_spec#*:}
  line=$(print -r -- "$glogx_bindings" | grep -F -- "$table" | tail -1 || true)
  if [[ -z "$line" ]]; then
    print -u2 "[test-tmux:zsh] $key_desc の glogx バインドが見つかりません (list-keys)"
    exit 1
  fi
  for needle in 'rev-parse --git-dir' 'bin/tmux-toast'; do
    if [[ "$line" != *"$needle"* ]]; then
      print -u2 "[test-tmux:zsh] $key_desc のバインドに '$needle' がありません: $line"
      exit 1
    fi
  done
  # 条件式の取り出しは先に見た C-g 側を使う (両者同一であることは上の検査で担保)
  [[ -n "$guard_line" ]] || guard_line="$line"
done
guard_cond=${guard_line#*if-shell \"}
guard_cond=${guard_cond%%\"*}
guard_probe="$TMUX_TMPDIR/glogx_guard.log"

# 指定 cwd で条件を評価し、選ばれた枝 (popup / toast) と展開された cwd を返す。
# run-shell -b は非同期なので、probe への書き込みをポーリングで待つ。
run_guard_branch() {
  local session="$1" cwd="$2" waited=0
  : > "$guard_probe"
  "${TMUX_CMD[@]}" new-session -d -s "$session" -c "$cwd" "tail -f /dev/null" >"$log_file" 2>&1 \
    || handle_result "$log_file" "guard 用セッション ($session) の作成に失敗" "fail"
  "${TMUX_CMD[@]}" if-shell "$guard_cond" \
    "run-shell -b 'echo popup:#{pane_current_path} >> $guard_probe'" \
    "run-shell -b 'echo toast:#{pane_current_path} >> $guard_probe'" >"$log_file" 2>&1 \
    || handle_result "$log_file" "guard 条件の実行に失敗" "fail"
  while [[ ! -s "$guard_probe" && $waited -lt 50 ]]; do
    sleep 0.1
    waited=$(( waited + 1 ))  # (( waited++ )) は戻り値 0 → set -e で即死する
  done
  "${TMUX_CMD[@]}" kill-session -t "$session" >/dev/null 2>&1 || true
  REPLY=$(< "$guard_probe")
}

assert_guard_branch() {
  local label="$1" expected="$2" actual="$3"
  if [[ "$actual" != "$expected" ]]; then
    print -u2 "[test-tmux:zsh] glogx ガードの分岐が想定と違います ($label): expected '$expected', got '$actual'"
    exit 1
  fi
}

# pane_current_path は物理パスで返るため、比較側も pwd -P で揃える
# (checkout や TMPDIR が symlink 経由だと論理パスと食い違う)
nogit_dir="$TMUX_TMPDIR/nogit"
mkdir -p "$nogit_dir"
nogit_dir=$(cd "$nogit_dir" && pwd -P)
repo_dir=$(cd "$ROOT_DIR" && pwd -P)
run_guard_branch glogx_guard_nogit "$nogit_dir"
assert_guard_branch "repo 外" "toast:$nogit_dir" "$REPLY"
run_guard_branch glogx_guard_repo "$repo_dir"
assert_guard_branch "repo 内" "popup:$repo_dir" "$REPLY"

# terminal-features が conf の reload で膨張しないこと。
# ⚠️ `set -as` は追記なので、-u で既定へ戻さないと reload ごとに同じエントリが積まれる。
# 実測 (2026-08-21): 稼働サーバが 38 エントリまで膨張していた (下の 2 行 × reload 17 回 +
# conf 外の手動 set 1 件)。件数を pin するのではなく「reload しても増えない」を pin する
# (tmux の既定エントリ数は版で変わりうるため)。
print "[test-tmux:zsh] checking terminal-features does not grow on conf reload"
tf_count() { "${TMUX_CMD[@]}" show -s terminal-features 2>/dev/null | grep -c . }
tf_before=$(tf_count)
if (( tf_before < 1 )); then
  print -u2 "[test-tmux:zsh] terminal-features を読めない (前提が崩れた)"
  exit 1
fi
"${TMUX_CMD[@]}" source-file "$CONF_FILE" >/dev/null 2>&1
tf_after1=$(tf_count)
"${TMUX_CMD[@]}" source-file "$CONF_FILE" >/dev/null 2>&1
tf_after2=$(tf_count)
if (( tf_after1 != tf_before || tf_after2 != tf_before )); then
  print -u2 "[test-tmux:zsh] terminal-features が reload で増えた: ${tf_before} → ${tf_after1} → ${tf_after2}"
  print -u2 "  set -as の前に `set -u terminal-features` が必要 (_tmux.conf の該当コメント参照)"
  "${TMUX_CMD[@]}" show -s terminal-features >&2
  exit 1
fi
print "[test-tmux:zsh] ok: terminal-features は reload 2 回でも ${tf_before} 件のまま"

print "[test-tmux:zsh] done"
