#!/usr/bin/env zsh
#
# /fork-scratch + prefix+b fork popup + scratch popup (bind t) の回帰テスト。
# インタラクティブな popup の開閉体感 (display-popup -E) は tty 必須で自動検証できないため、
# その手前までの「壊れると気づきにくい不変条件」を検査する:
#   A) bind b が claude-fork popup (tmux_fork_popup.sh) を参照して登録されている
#      (fork popup が無効化中は skip。bind b 本体の new-session -A 検査も含む)
#   B) scratch (bind t。2026-06-28 復活済み) は fork の無効状態と独立して常時検査する:
#      bind t / C-t が tmux_scratch_popup.sh を参照し、bind 本体にもスクリプト実体の
#      コード行にも new-session -A が無い (= popup を閉じるのに 2 回押す回帰のガード)
#   C) tmux_fork_popup.sh が new-session を含まない (= 空セッションを作らず偽 fork を生まない不変条件)
#      かつ has-session ガードを持つ
#   D) popup 専用セッション (scratch/claude-fork/launcher) の fzf 候補除外が、共有 lib
#      (lib/tmux_popup_sessions.sh) と jump / pane_move の両利用側で配線されている
#   E) [挙動] claude-fork 不在時、popup script は案内を出すだけでセッションを作らない
#   F) [挙動] /fork-scratch の起動コマンドは、env 未設定なら guard で中断し、設定済みなら
#      stub claude で claude-fork を作り `--resume <id> --fork-session` を正しく渡す
#
# 隔離方針: conf ロード/bind 検査は named socket (-L)。スクリプトの挙動検査は、スクリプトが
# 素の `tmux` (= default socket) を叩くため、TMUX_TMPDIR を temp に倒した default socket で行う。
# ⚠️ TMUX_TMPDIR 隔離だけでは不十分: tmux クライアントの socket 解決は
#   -S > -L > $TMUX(継承) > TMUX_TMPDIR/default
# の優先順で、tmux ペイン内から実行すると継承 $TMUX が TMUX_TMPDIR を上書きし、本テストの
# bare `tmux kill-server` が実運用サーバを直撃する（2026-07-07 に実発生: ペイン内の make test が
# default サーバを kill し全セッション消滅 = "[server exited]"）。対策は二段:
#   (1) 下の Darwin ガード = 開発機 macOS では挙動テストを実行しない
#   (2) unset TMUX = 実行される環境 (CI/Linux) でも継承 socket 経路を遮断する

set -euo pipefail
unset CDPATH

# macOS (開発機) では実行しない。本テストは実 tmux サーバと同居する環境で走らせない設計
# (bare `tmux` を叩く挙動テストを含むため)。CI の Linux runner でのみ実行する。
# Linux でも tmux ペイン内なら同じ事故が起きるため、無条件の unset TMUX も必須 (下記)。
if [[ "$(uname -s)" == "Darwin" ]]; then
  print "[test-fork-scratch:zsh] skipped: macOS (開発機) では実行しない。実 tmux サーバ誤 kill 防止 (2026-07-07 の再発防止)"
  exit 0
fi

# 継承 $TMUX を遮断: これが残っていると bare `tmux` が TMUX_TMPDIR でなく実サーバの socket に
# 接続する (上記 ⚠️ 参照)。TMUX_PANE も対で消す。
unset TMUX TMUX_PANE 2>/dev/null || true

TMUX_BIN_PATH=${TMUX_BIN:-tmux}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
CONF_FILE="$ROOT_DIR/_tmux.conf"
POPUP_SCRIPT="$ROOT_DIR/scripts/tmux_fork_popup.sh"
CMD_FILE="$ROOT_DIR/_claude/commands/fork-scratch.md"
JUMP_SCRIPT="$ROOT_DIR/scripts/tmux_fzf_jump.sh"

SCRATCH_SCRIPT="$ROOT_DIR/scripts/tmux_scratch_popup.sh"

TMUX_TMPDIR=$(mktemp -d)
export TMUX_TMPDIR
# このテストが作った temp dir を控える。cleanup の bare kill-server はこの値と一致するときだけ叩き、
# 万一 TMUX_TMPDIR が外れても実本番サーバ (/tmp/tmux-$UID/default) を巻き込まないための安定キー。
EXPECTED_TMPDIR="$TMUX_TMPDIR"
export HOME="$TMUX_TMPDIR/home"
export DOTFILES_DIR="$ROOT_DIR"
mkdir -p "$HOME"
# socket 名は短く保つ: TMUX_TMPDIR が /var/folders 配下の長い mktemp dir になる環境では、
# 長い socket 名と合わさって unix socket のパス長上限 (macOS sun_path 104 byte) を超えて
# "File name too long" で接続失敗する（実測: dotfiles-fork-test-<pid> で超過）。
SOCKET_NAME="dffk-$$"

fail() { print -u2 "[test-fork-scratch:zsh] FAIL: $1"; exit 1; }
ok()   { print "[test-fork-scratch:zsh] ok: $1"; }

cleanup() {
  "$TMUX_BIN_PATH" -L "$SOCKET_NAME" kill-server >/dev/null 2>&1 || true
  # 挙動テストで使う default socket は隔離 TMUX_TMPDIR 内にある。bare kill-server が実本番
  # サーバを巻き込まない不変条件として、TMUX_TMPDIR が「このテストが mktemp で作った temp dir
  # そのもの」のときだけ叩く。空/別値なら実サーバ保護を優先してスキップする。
  if [[ -n "${TMUX_TMPDIR:-}" && "$TMUX_TMPDIR" == "$EXPECTED_TMPDIR" ]]; then
    "$TMUX_BIN_PATH" kill-server >/dev/null 2>&1 || true
  fi
  rm -rf "$TMUX_TMPDIR"
}
# EXIT に加え INT/TERM でも cleanup を確実に走らせる。シグナルは `exit 130` 経由で EXIT trap に
# 集約する（二重実行を避けつつ、テスト中断時に temp socket 上のサーバを孤児化させない。中断で
# cleanup が走らず孤児が残る経路が、今回の continuum Gate2 破り＝復元不発の発生源だった）。
trap cleanup EXIT
trap 'exit 130' INT TERM

command -v "$TMUX_BIN_PATH" >/dev/null 2>&1 || { print -u2 "tmux not found (set \$TMUX_BIN)"; exit 1; }
[[ -f "$CONF_FILE" ]]    || fail "conf not found: $CONF_FILE"
[[ -f "$POPUP_SCRIPT" ]] || fail "popup script not found: $POPUP_SCRIPT"
[[ -f "$CMD_FILE" ]]     || fail "command file not found: $CMD_FILE"
[[ -f "$JUMP_SCRIPT" ]]  || fail "jump script not found: $JUMP_SCRIPT"
[[ -f "$SCRATCH_SCRIPT" ]] || fail "scratch script not found: $SCRATCH_SCRIPT"

# ---- conf を named socket でロードして bind を検査 (A, B) ----
log="$TMUX_TMPDIR/tmux.log"
if ! "$TMUX_BIN_PATH" -L "$SOCKET_NAME" -f "$CONF_FILE" new-session -d -s fork_test "tail -f /dev/null" >"$log" 2>&1; then
  if grep -qiE "operation not permitted|permission denied" "$log"; then
    print -u2 "[test-fork-scratch:zsh] skipped: tmux cannot create sockets in this environment"
    cat "$log" >&2
    exit 0
  fi
  cat "$log" >&2
  fail "failed to create test session"
fi

keys=$("$TMUX_BIN_PATH" -L "$SOCKET_NAME" list-keys -T prefix)

# fork popup (bind b) は 2026-06-28 に A/B 観測のため _tmux.conf で一時無効化されている
# （コメントアウト）。無効中は bind b が登録されないため、bind b 依存の検査 (A) だけ skip する。
# scratch (bind t) は同日にユーザ判断で先行復活済みのため、B は fork の無効状態と独立して
# 常時検査する（かつては A/B とも bind b の有効判定に相乗りしており、scratch 復活後も
# その回帰ガードが一度も走らない false-skip になっていた）。C〜F はスクリプト/コマンド
# ファイル自体を直接検査するので有効/無効に関係なく走らせる。
# NOTE: 「有効」の判定に bind の有無を使ってはいけない。tmux の既定で prefix+t は clock-mode に
#   bind 済みのため、popup 無効でも bind t は list-keys に現れる（誤検知の元）。有効か否かは
#   「bind が対応スクリプトを参照しているか」で判定する。
bind_b=$(print -r -- "$keys" | grep -E '^bind-key +-T prefix +b ' || true)
if ! print -r -- "$bind_b" | grep -q 'tmux_fork_popup.sh'; then
  print "[test-fork-scratch:zsh] skip A: fork popup (bind b) は現在無効 (A/B 観測期間)。B〜F を検査する。"
else
  # A) bind b が claude-fork と tmux_fork_popup.sh を参照し、new-session -A を使わない
  print -r -- "$bind_b" | grep -q 'claude-fork'         || fail "bind b が claude-fork を参照していない"
  print -r -- "$bind_b" | grep -q 'tmux_fork_popup.sh'  || fail "bind b が tmux_fork_popup.sh を参照していない"
  if print -r -- "$bind_b" | grep -qE 'new-session[^|]*-A'; then
    fail "bind b が new-session -A を使用 (popup を閉じるのに 2 回押す回帰の恐れ)"
  fi
  ok "A: bind b が claude-fork popup (tmux_fork_popup.sh) を参照 + new-session -A 不使用"
fi

# B) scratch popup (bind t / C-t。復活済み) の 2 回押し回帰ガード。fork の無効状態と独立して常時検査。
for k in t C-t; do
  body=$(print -r -- "$keys" | grep -E "^bind-key +-T prefix +$k " || true)
  [[ -n "$body" ]] || fail "bind $k が見つからない (scratch 復活状態の前提が変わった可能性)"
  print -r -- "$body" | grep -q 'tmux_scratch_popup.sh' \
    || fail "bind $k が tmux_scratch_popup.sh を参照していない (開閉判定の集約が壊れた可能性)"
  if print -r -- "$body" | grep -qE 'new-session[^|]*-A'; then
    fail "bind $k が new-session -A を使用 (popup を閉じるのに 2 回押す回帰の恐れ)"
  fi
done
# bind 本体は run-shell でスクリプトを呼ぶだけなので、実体の new-session が -A を
# 使っていないこともコード行 (コメント除外) で検査する (check C と同型)。
if grep -vE '^[[:space:]]*#' "$SCRATCH_SCRIPT" | grep -qE 'new-session[^|]*-A'; then
  fail "tmux_scratch_popup.sh のコード行が new-session -A を使用 (2 回押し回帰の恐れ)"
fi
ok "B: bind t / C-t が scratch script を参照 + bind/実体とも new-session -A 不使用"

# C) popup script は new-session を含まない (空セッション非生成) + has-session ガードを持つ。
# 検査対象は「実コード」であって説明コメントではない。tmux_fork_popup.sh は「なぜ new-session を
# 使わないか」をコメントで明記しているため、コメント行を除外してから検査する（さもないと説明文の
# "new-session" に grep が誤反応して false positive になる）。
if grep -vE '^[[:space:]]*#' "$POPUP_SCRIPT" | grep -q 'new-session'; then
  fail "tmux_fork_popup.sh のコード行が new-session を含む (空セッション非生成の不変条件に違反)"
fi
grep -q 'has-session' "$POPUP_SCRIPT" || fail "tmux_fork_popup.sh に has-session ガードがない"
ok "C: popup script は new-session 不使用 + has-session ガードあり"

# D) popup 専用セッション (scratch / claude-fork / launcher) が fzf 候補から除外されること。
# 除外パターンは lib/tmux_popup_sessions.sh に一本化された (2026-07-08。かつて本チェックは
# jump スクリプト本文への直書き '(scratch|claude-fork):' を pin しており、一本化で恒久
# false-fail になった)。よって (1) lib のパターンが 3 セッションを含む / (2) jump と
# pane_move の両方が lib を source しパターン変数でフィルタしている、の 2 段で検査する。
POPUP_SESSIONS_LIB="$ROOT_DIR/scripts/lib/tmux_popup_sessions.sh"
PANE_MOVE_SCRIPT="$ROOT_DIR/scripts/tmux_fzf_pane_move.sh"
[[ -f "$POPUP_SESSIONS_LIB" ]] || fail "popup sessions lib not found: $POPUP_SESSIONS_LIB"
[[ -f "$PANE_MOVE_SCRIPT" ]]   || fail "pane_move script not found: $PANE_MOVE_SCRIPT"
for _name in scratch claude-fork launcher; do
  grep -E '^TT_POPUP_SESSION_RE=' "$POPUP_SESSIONS_LIB" | grep -q "$_name" \
    || fail "除外 lib のパターンに $_name が含まれていない"
done
for _s in "$JUMP_SCRIPT" "$PANE_MOVE_SCRIPT"; do
  grep -q 'lib/tmux_popup_sessions.sh' "$_s" \
    || fail "$(basename "$_s") が共有除外 lib を source していない"
  grep -q 'TT_POPUP_SESSION_RE' "$_s" \
    || fail "$(basename "$_s") が除外パターン変数 (TT_POPUP_SESSION_RE) でフィルタしていない"
done
ok "D: 除外 lib が scratch/claude-fork/launcher を含み、jump / pane_move とも lib 経由で除外"

# ---- 挙動テスト (default socket は隔離 TMUX_TMPDIR 内) ----

# E) claude-fork 不在時: popup script は案内を出すだけでセッションを作らない
"$TMUX_BIN_PATH" kill-server >/dev/null 2>&1 || true
e_out=$( (unset TMUX; sh "$POPUP_SCRIPT" </dev/null) 2>&1 || true )
print -r -- "$e_out" | grep -q 'フォーク未作成' \
  || fail "popup script else 分岐の案内が出ない (出力: $e_out)"
if "$TMUX_BIN_PATH" has-session -t claude-fork 2>/dev/null; then
  fail "else 分岐で claude-fork セッションが作られてしまった"
fi
ok "E: 未作成時は案内表示のみ・セッション非生成"

# /fork-scratch.md の bash ブロックを抽出 (出荷物そのものをテストする)
block_file="$TMUX_TMPDIR/fork_cmd.sh"
awk '/^```bash$/{f=1;next} /^```/{if(f){f=0}} f' "$CMD_FILE" > "$block_file"
[[ -s "$block_file" ]] || fail "fork-scratch.md から bash ブロックを抽出できなかった"

# /fork-scratch コマンドも 2026-06-28 に A/B 観測のため早期 exit ガードで無効化されている
# (block 冒頭に `echo "...一時無効化中です"; exit 0`)。無効スタブのときは本物の fork ロジックを
# 検査できないので F-1/F-2 を skip して正常終了する。bind/コマンドを復活させたら自動的に再検査される。
if grep -q '一時無効化' "$block_file"; then
  print "[test-fork-scratch:zsh] skip F: /fork-scratch コマンドは現在無効 (A/B 観測期間)。C〜E のみ検査した。"
  print "[test-fork-scratch:zsh] done"
  exit 0
fi

# F-1) env 未設定: guard で中断し claude-fork を作らない
"$TMUX_BIN_PATH" kill-server >/dev/null 2>&1 || true
f1_out=$(env -u CLAUDE_CODE_SESSION_ID PWD="$ROOT_DIR" sh "$block_file" 2>&1 || true)
print -r -- "$f1_out" | grep -q '未設定' \
  || fail "空 env で未設定ガードのメッセージが出ない (出力: $f1_out)"
if "$TMUX_BIN_PATH" has-session -t claude-fork 2>/dev/null; then
  fail "空 env で claude-fork が作られてしまった (guard すり抜け)"
fi
ok "F-1: env 未設定で guard が効く (セッション非生成)"

# F-2) env 設定 + stub claude: claude-fork を作り --resume <id> --fork-session を正しく渡す
stub_bin="$TMUX_TMPDIR/bin"
mkdir -p "$stub_bin"
args_file="$TMUX_TMPDIR/claude_args"
cat > "$stub_bin/claude" <<STUB
#!/bin/sh
# 受け取った引数を記録し、セッションを生かしたまま待機する (本物の claude の代役)
printf '%s\n' "\$*" > "$args_file"
exec tail -f /dev/null
STUB
chmod +x "$stub_bin/claude"

fake_id="testsess-0000-1111-2222"
"$TMUX_BIN_PATH" kill-server >/dev/null 2>&1 || true
f2_out=$(env CLAUDE_CODE_SESSION_ID="$fake_id" PWD="$ROOT_DIR" PATH="$stub_bin:$PATH" sh "$block_file" 2>&1 || true)
print -r -- "$f2_out" | grep -q 'fork OK' || fail "stub env で 'fork OK' が出ない (出力: $f2_out)"
"$TMUX_BIN_PATH" has-session -t claude-fork 2>/dev/null || fail "claude-fork セッションが作られていない"

# stub が引数を書き出すのを待つ (detached 起動直後は未書き込みのことがある)
i=0
while [[ ! -s "$args_file" && $i -lt 60 ]]; do sleep 0.05; i=$((i+1)); done
[[ -s "$args_file" ]] || fail "stub claude が引数を記録しなかった (起動失敗の可能性)"
recorded=$(cat "$args_file")
print -r -- "$recorded" | grep -q -- '--resume'       || fail "claude 引数に --resume がない (実際: $recorded)"
print -r -- "$recorded" | grep -q -- "$fake_id"        || fail "claude 引数に session id がない (実際: $recorded)"
print -r -- "$recorded" | grep -q -- '--fork-session'  || fail "claude 引数に --fork-session がない (実際: $recorded)"
ok "F-2: stub で claude-fork 作成 + 引数 (--resume <id> --fork-session) を検証"
"$TMUX_BIN_PATH" kill-server >/dev/null 2>&1 || true

print "[test-fork-scratch:zsh] done"
