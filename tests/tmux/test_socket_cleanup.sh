#!/usr/bin/env bash
# 隔離 tmux サーバの後始末が **socket ファイルまで** 消すことを固定する (issue 305 ①)。
#
# 🚨 なぜ: **`tmux kill-server` は socket ファイルを消さない** (実測 2026-09-10。SIGKILL でも同じ)。
# つまり中断時だけでなく**正常終了のたびに 1 個ずつ漏れる**。同日の実測で
# `/private/tmp/tmux-501/` に 536 ファイルあり、生きているのは `default` (本番) の 1 個だけだった
# (最古 2026-07-05)。大半は `ctrlv-test-*` / `pane-state-bell-*` = `-L <name>-$$` を使うテスト。
#
# 🚨 **脅威モデル**: 止めるのは「テストが自分の socket を置き去りにする」形だけ。
# 掃除機構 (母集合を走査して消す) は作らないので、他人の socket を消す経路は原理的に無い。
# 🚨 **検出しないと決めた形**:
#   - **中断 (SIGKILL) で trap が走らない場合**。次の run が同じ名前を作らない限り残る
#     (名前に `$$` が入るため)。回収は手動 (手順は issue 305 に実測つきで書いてある)
#   - `TMUX_TMPDIR` を差し替えるテスト。そちらは dir ごと消えるのでこの検査の対象外
#
# ⑥ だけは socket ではなく **その dir 自体**を見る。「dir ごと消える」は上の除外の前提なので、
# その前提が成り立っていること自体を固定する。
set -uo pipefail
unset CDPATH
unset TMUX TMUX_PANE   # 🚨 $TMUX は TMUX_TMPDIR より優先される。残すと本番を向く

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=tests/tmux/lib/kill_socket.sh
. "$ROOT_DIR/tests/tmux/lib/kill_socket.sh"

fails=0
checks=0
ok()  { checks=$((checks + 1)); printf '✓ %s\n' "$1"; }
bad() { checks=$((checks + 1)); printf '✗ %s\n' "$1" >&2; fails=$((fails + 1)); }


# --- ⑥ 一時 dir が「作成と同時に登録される」形を保っていること -----------------------------
#
# 🚨 **tmux を起こす ① より前に置く**。①〜④ は tmux が無いと `exit 77` (skip) で終わるが、
# ⑥ はテキスト照合なので tmux に依存しない (敵対レビュー 1 周目 P2-1)。
#
# 🚨 **軸を構文から外した経緯** (adversarial-review-own-safeguards.md §8)。
# 最初は「`mktemp -d` で作った変数が cleanup の `rm -rf` に書かれているか」を見ていたが、
# 2 周の敵対レビューが **6 通りの迂回**を実測で通した:
#   クォート形 / インデント / `typeset` `export` 前置き / 1 行に 2 つ / `mktemp -q -d` /
#   行内コメントや `print "…$VAR"` が「載っている」と読まれる。
# 正規表現を 6 回目に広げるのではなく、**発生源側を「作成と登録が 1 つの関数」に変えた**
# (`reap_mktemp_d`)。こうすると検査は「素の `mktemp` がヘルパーの外に無いか」の
# **1 イディオム**を見るだけでよく、綴りの揺れに影響されない。
#
# 🚨 **検出しないと決めた形**: 変数経由の削除 / heredoc で書き出す子スクリプト内の `mktemp` /
# `reap_mktemp_d` を通さずに `install -d` 等で dir を作る形。review の責務。
# **判別力の本体は実行前後の `/tmp/reap*` の個数差 (A-B)** で、ここはその事前フィルタ。

REAP="$ROOT_DIR/tests/tmux/test_reap_orphan_servers.sh"

# ヘルパーの行範囲 (この中の mktemp だけが正当)
helper_range() { # helper_range <file> -> "開始行 終了行"
  awk '/^reap_mktemp_d\(\) \{/ { s = NR } s && !e && /^\}$/ { e = NR } END { if (s && e) print s, e }' "$1"
}
# 非コメント行の mktemp の行番号 (綴りに依存しない: mktemp という語だけを見る)。
# 🚨 **語境界で照合する**。素の `grep mktemp` は `reap_mktemp_d …` という**呼び出し側の
# 識別子**にも当たり、全呼び出しを違反として報告する (最初そう書いて 6 件の偽陽性が出た)。
mktemp_lines() { # mktemp_lines <file>
  grep -nE '(^|[^A-Za-z0-9_])mktemp([^A-Za-z0-9_]|$)' "$1" |
    grep -v '^[0-9]*:[[:space:]]*#' | cut -d: -f1
}
# ヘルパーの外に居る mktemp を列挙する (本走査と canary が通る唯一の判定)
stray_mktemp() { # stray_mktemp <file>
  local range s e n
  range=$(helper_range "$1") || return 1
  [ -n "$range" ] || { printf 'NO_HELPER\n'; return 0; }
  s=${range%% *}; e=${range##* }
  while IFS= read -r n; do
    [ -n "$n" ] || continue
    if [ "$n" -lt "$s" ] || [ "$n" -gt "$e" ]; then printf '%s\n' "$n"; fi
  done < <(mktemp_lines "$1")
}

# canary — 本走査と**同じ関数**に既知の入力を通す。綴りの揺れを 1 つの fixture で固定する
canary_src=$(mktemp "${TMPDIR:-/tmp}/socket_cleanup_canary.XXXXXX")
# 🚨 この検査は「一時ファイルの後始末」を守る検査なので、自分が漏らすと自己矛盾になる
# (敵対レビュー 2 周目 P3-F: trap が 1 つも無く、SIGTERM 60 回で 7 件残った)
trap 'rm -f "$canary_src"' EXIT INT TERM HUP
cat > "$canary_src" <<'CANARY'
reap_mktemp_d() {
  local d
  d=$(mktemp -d "$2") || return 1
  REAP_TMPDIRS+=("$d")
  typeset -g "$1"="$d"
}
reap_mktemp_d A_DIR /tmp/a.XXXXXX
  typeset B_DIR="$(mktemp -q -d /tmp/b.XXXXXX)"
export C_DIR=$(mktemp -d /tmp/c.XXXXXX); D_DIR=$(mktemp -d /tmp/d.XXXXXX)
# コメントの中の mktemp -d は拾わない
CANARY
canary_stray=$(stray_mktemp "$canary_src" | tr '\n' ' ')
if [ "$canary_stray" = "8 9 " ]; then
  ok "canary: ヘルパー外の mktemp を綴りに依存せず拾う (typeset / export / -q -d / 1 行 2 つ / コメント除外)"
else
  bad "canary: 抽出が壊れている (ヘルパー外の行 = [$canary_stray]、期待は [8 9 ])。この状態の ⑥ は何も守らない"
fi
rm -f "$canary_src"; trap - EXIT INT TERM HUP

# 本走査
stray=$(stray_mktemp "$REAP" | tr '\n' ' ')
case "$stray" in
  '')        ok "test_reap_orphan_servers.sh: 素の mktemp はヘルパーの外に無い" ;;
  NO_HELPER*) bad "test_reap_orphan_servers.sh に reap_mktemp_d が無い (作成と登録を分ける形へ戻っている)" ;;
  *)         bad "test_reap_orphan_servers.sh: ヘルパーを通らない mktemp が行 $stray に在る (登録されないので cleanup が消せない)" ;;
esac
if grep -q 'REAP_TMPDIRS+=(' "$REAP"; then
  ok "test_reap_orphan_servers.sh: ヘルパーが作った dir を登録している"
else
  bad "test_reap_orphan_servers.sh: ヘルパーが REAP_TMPDIRS へ登録していない (cleanup の対象が空になる)"
fi
if grep -q 'rm -rf "\${REAP_TMPDIRS\[@\]}"' "$REAP"; then
  ok "test_reap_orphan_servers.sh: cleanup が登録された dir を全部消す"
else
  bad "test_reap_orphan_servers.sh: cleanup が REAP_TMPDIRS を消していない (個別の dir 名を書く形へ戻った?)"
fi
# 🚨 ヘルパーがパスを stdout で返す形へ戻っていないか。`X=$(reap_mktemp_d …)` は
# コマンド置換 = サブシェルなので、登録が親へ届かず **cleanup が 1 件も消さないまま緑**になる
# 🚨 `awk … | grep -q` にしない。pipefail 下では grep -q が先に抜けて awk が SIGPIPE で
# 死に、**一致しているのに rc≠0** になって判定が反転する (issue 096 / check_pipefail_grep_q.sh)
helper_body=$(awk '/^reap_mktemp_d\(\) \{/ { i = 1 } i && /^\}$/ { i = 0 } i' "$REAP")
if grep -qE '^[[:space:]]*(printf|echo)[^#]*\$d' <<< "$helper_body"; then
  bad "reap_mktemp_d がパスを stdout で返している (呼び出し側が \$( ) で受けると登録がサブシェルに閉じる)"
else
  ok "reap_mktemp_d は stdout で返さず typeset -g で呼び出し元へ入れる"
fi


# --- ① 前提: kill-server だけでは socket が残る (この検査が守っている事実そのもの) ------------
#
# 🚨 これを canary として先に確かめる。もし tmux 側が将来 socket を消すようになったら、
# この検査は「何も守っていない」状態になるので、そのときに気づけるようにしておく。
raw="tt-cleanup-raw-$$"
tmux -L "$raw" -f /dev/null new-session -d 'sleep 30' >/dev/null 2>&1 || {
  # 🚨 ⑥ で既に違反を見つけているなら skip に畳まない (skip は緑に見える)
  [ "$fails" -eq 0 ] || { printf '✗ tmux は起動できないが、⑥ が %d 件の違反を出している\n' "$fails" >&2; exit 1; }
  echo "SKIP: tmux を起動できない"; exit 77
}
raw_path=$(tmux -L "$raw" display -p '#{socket_path}' 2>/dev/null)
tmux -L "$raw" kill-server 2>/dev/null || :
if [ -S "$raw_path" ]; then
  ok "前提: kill-server は socket ファイルを残す (この検査が要る理由)"
  rm -f -- "$raw_path"
else
  bad "前提が崩れた: kill-server が socket を消すようになった (この検査はもう何も守っていない。tt_tmux_kill_socket ごと見直すこと)"
fi

# --- ② ヘルパーは socket ファイルまで消す ------------------------------------------------------
s1="tt-cleanup-a-$$"
tmux -L "$s1" -f /dev/null new-session -d 'sleep 30' >/dev/null 2>&1
p1=$(tmux -L "$s1" display -p '#{socket_path}' 2>/dev/null)
[ -S "$p1" ] || bad "前提: socket が作られていない ($p1)"
tt_tmux_kill_socket "$s1"
if [ -e "$p1" ]; then bad "tt_tmux_kill_socket が socket ファイルを残した: $p1"; rm -f -- "$p1"
else ok "tt_tmux_kill_socket は socket ファイルまで消す"; fi

# --- ③ 既に死んでいるサーバの socket も回収する ------------------------------------------------
s2="tt-cleanup-b-$$"
tmux -L "$s2" -f /dev/null new-session -d 'sleep 30' >/dev/null 2>&1
p2=$(tmux -L "$s2" display -p '#{socket_path}' 2>/dev/null)
tmux -L "$s2" kill-server 2>/dev/null || :      # 先に殺す = display -p が失敗する状態を作る
[ -S "$p2" ] || bad "前提: 先に kill しても socket は残るはず"
tt_tmux_kill_socket "$s2"
if [ -e "$p2" ]; then bad "死んでいるサーバの socket を回収できていない: $p2"; rm -f -- "$p2"
else ok "サーバが既に死んでいても socket を回収する"; fi

# --- ④ 🚨 default (本番) は絶対に消さない ------------------------------------------------------
#
# 名前を組み立てる経路 (③) が誤っても本番へ届かないことを固定する。
guard_dir=$(mktemp -d); mkdir -p "$guard_dir/tmux-$(id -u)"
guard="$guard_dir/tmux-$(id -u)/default"
python3 -c 'import socket,sys; s=socket.socket(socket.AF_UNIX); s.bind(sys.argv[1])' "$guard" 2>/dev/null || : > "$guard"
TMUX_TMPDIR="$guard_dir" tt_tmux_kill_socket default
if [ -e "$guard" ]; then ok "default という名前の socket は消さない"
else bad "🚨 default を消した (本番の socket を消しうる)"; fi
rm -rf "$guard_dir"

# --- ⑤ 実テストが漏らさないこと (配線の確認) ---------------------------------------------------
#
# 🚨 ヘルパー単体の検査だけでは「呼び出し側が使っている」を 1 mm も守らない
# (mutation-verify-new-tests.md の「計算が正しい ≠ 配線されている」)。
for t in tests/tmux/test_ctrl_v_paste.sh tests/claude/test_tmux_pane_state_bell.sh; do
  if grep -q 'tt_tmux_kill_socket' "$ROOT_DIR/$t"; then
    ok "$(basename "$t"): tt_tmux_kill_socket を使っている"
  else
    bad "$(basename "$t") が socket を置き去りにする形へ戻っている (kill-server だけでは消えない)"
  fi
done

# 🚨 件数を assert する (「1 件も走らないまま 0 件 fail=0 で緑」を塞ぐ)。
# 🚨 **dir の個数に結合させない**。旧版は mktemp の件数ぶん checks が増える設計で、
# 「dir を減らす正しいリファクタ」が閾値割れで red になっていた (敵対レビュー 2 周目 P3-D)。
# いまの ⑥ は dir の数に関係なく 5 件固定なので、合計も固定できる。
want_checks=11
[ "$checks" -ge "$want_checks" ] || bad "検査が $checks 件しか走っていない (${want_checks} 件以上のはず)"
printf '\n検査 %d 件: fail=%d\n' "$checks" "$fails"
[ "$fails" -eq 0 ] || exit 1
echo "OK tmux socket cleanup"
