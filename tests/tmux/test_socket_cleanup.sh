#!/usr/bin/env bash
# 隔離 tmux サーバの後始末が **socket ファイルまで** 消すことを固定する (issue 305 ①)。
#
# 🚨 なぜ: **`tmux kill-server` は socket ファイルを消さない** (実測 2026-09-10。SIGKILL でも同じ)。
# つまり中断時だけでなく**正常終了のたびに 1 個ずつ漏れる**。同日の実測で
# `/private/tmp/tmux-501/` に 536 ファイルあり、生きているのは `default` (本番) の 1 個だけだった
# (最古 2026-07-05)。大半は `ctrlv-test-*` / `pane-state-bell-*` = `-L <name>-$$` を使うテスト。
#
# 🚨 **脅威モデル**: 止めるのは「テストが自分の socket / 一時 dir を置き去りにする」形だけ。
# 掃除機構 (母集合を走査して消す) は作らないので、他人のものを消す経路は原理的に無い。
#
# 🚨 **検出しないと決めた形** (敵対レビュー 3 周目で射程を実装と突き合わせた):
#   - **中断 (SIGKILL) で trap が走らない場合**。名前に `$$` が入るので次の run では回収されない。
#     `/private/tmp` は**起動時に一掃される**ので放置しても溜まり続けはしない (実測 2026-09-10:
#     uptime 67 日 / 起動より古いエントリ 0 件 / `/etc/periodic` の掃除は無し)
#   - **行内コメントの中の `mktemp`** は**偽陽性**になる (`X=$(id -u)  # 以前は mktemp -d だった`)。
#     落としているのは**行頭コメントだけ**。正規表現をこれ以上広げない (§8: 迂回を潰し続けない)
#   - `install -d` / `mkdir -p` で dir を作る形、heredoc で書き出す子スクリプトの中の `mktemp`
#   - 🚨 **「変数経由」は除外していない**。`MK=mktemp` は `=` が語境界なので**定義行が検出される**
#     (実測)。除外されているのは変数経由の *削除* であって、変数経由の mktemp ではない
#
# ⑥ は socket ではなく **一時 dir の作り方**を見る。`reap_mktemp_d` が lib に在るので、
# 検査は「テスト本体に素の `mktemp` が 1 つも無いか」で済み、**行範囲を切り出すアンカーが要らない**
# (3 周目 P1-2: `}` に行末コメントを足すだけで信頼窓が 6 行 → 51 行へ無警告で広がっていた)。
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

# --- 後始末: **ファイル全体を 1 本の trap で覆う** -------------------------------------------
#
# 🚨 旧版は canary ファイルだけを消す trap を張り、その直後に**解除**していた。以降に作る
# `guard_dir` と ①②③ の socket 3 個は誰も消さず、**変異検証 (わざと落とす実行) のたびに
# 3 個ずつ漏れた** (実測 2026-09-10: `/private/tmp/tmux-501/` に `tt-cleanup-*` が 97 件、
# すべて当日のこのテスト由来)。**305 の failure mode を、その修正を検証するテストが
# 再生産していた**形。しかも commit の A-B は `/tmp/reap*` の dir しか数えておらず、
# この漏れを**構造的に測れなかった** (§6「その計測が構造的に見落とすもの」)。
#
# 🚨 **後始末に `tt_tmux_kill_socket` (検査対象そのもの) を使わない**。使うと、それを壊す
# 変異を当てたときに後始末も一緒に壊れて漏れる。ここは独立に kill + rm する。
TT_SOCKETS=()
tt_new_socket() {  # tt_new_socket <変数名> <prefix> — 名前を決めた瞬間に登録する
  local name="$2-$$"
  TT_SOCKETS+=("$name")
  printf -v "$1" '%s' "$name"
}
canary_src=""
guard_dir=""
# 🚨 shellcheck は `printf -v "$1"` の間接代入を追えない (SC2154) ので、ここで宣言しておく。
# 消すと lint_test_scripts.sh が「referenced but not assigned」で落ちる
raw=""; s1=""; s2=""
tt_cleanup_all() {
  local s p uid; uid=$(id -u)
  [ -z "$canary_src" ] || rm -f -- "$canary_src"
  [ -z "$guard_dir" ] || rm -rf -- "$guard_dir"
  for s in ${TT_SOCKETS+"${TT_SOCKETS[@]}"}; do
    [ "$s" = default ] && continue          # 🚨 本番は絶対に触らない
    tmux -L "$s" kill-server 2>/dev/null || :
    p="${TMUX_TMPDIR:-/tmp}/tmux-$uid/$s"
    [ -e "$p" ] && rm -f -- "$p"
  done
  return 0
}
trap tt_cleanup_all EXIT INT TERM HUP


# --- ⑥ 一時 dir が「作成と同時に登録される」形を保っていること -----------------------------
#
# 🚨 tmux を起こす ① より前に置く (①〜④ は tmux が無いと exit 77 = skip で終わるため)。
# 🚨 軸を構文から外した経緯と「検出しないと決めた形」はファイル冒頭に書いてある。
REAP="$ROOT_DIR/tests/tmux/test_reap_orphan_servers.sh"
LIB="$ROOT_DIR/tests/tmux/lib/reap_mktemp.sh"

# 非コメント行の mktemp の行番号。
# 🚨 **語境界で照合する**。素の `grep mktemp` は `reap_mktemp_d …` という**呼び出し側の識別子**
# にも当たり、全呼び出しを違反として報告する (最初そう書いて 6 件の偽陽性が出た)。
mktemp_lines() { # mktemp_lines <file>
  grep -nE '(^|[^A-Za-z0-9_])mktemp([^A-Za-z0-9_]|$)' "$1" |
    grep -v '^[0-9]*:[[:space:]]*#' | cut -d: -f1
}

# 🚨 **入力の不在を緑にしない** (3 周目 P3)。ファイルが読めないと抽出が空になり、
# 「違反 0 件」で通る。canary は**壊れた抽出器**は守るが**入力の不在**は守らない。
for f in "$REAP" "$LIB"; do
  [ -r "$f" ] || bad "検査対象が読めない: $f (rename/削除された? この状態の ⑥ は何も守らない)"
done

# canary — 本走査と**同じ関数**に既知の入力を通す。
# 🚨 期待値を**行番号で持たない** (3 周目 P3)。fixture に 1 行足すだけで壊れ、しかも
# 「抽出が壊れている」というメッセージが本物の退行と区別できなくなる。行に置いた sentinel で見る。
canary_src=$(mktemp "${TMPDIR:-/tmp}/socket_cleanup_canary.XXXXXX")
cat > "$canary_src" <<'CANARY'
reap_mktemp_d A_DIR /tmp/a.XXXXXX
  typeset B_DIR="$(mktemp -q -d /tmp/b.XXXXXX)"   # SENTINEL_B
export C_DIR=$(mktemp -d /tmp/c.XXXXXX); D_DIR=$(mktemp -d /tmp/d.XXXXXX)   # SENTINEL_C
# コメント行の mktemp -d は拾わない (SENTINEL_NEVER)
MK=mktemp   # SENTINEL_VAR — 変数経由も定義行で捕まる (除外していない)
CANARY
canary_got=$(while IFS= read -r n; do
    [ -n "$n" ] || continue
    sed -n "${n}p" "$canary_src" | grep -oE 'SENTINEL_[A-Z]+' || printf 'NO_SENTINEL_AT_%s\n' "$n"
  done < <(mktemp_lines "$canary_src") | sort | tr '\n' ' ')
if [ "$canary_got" = "SENTINEL_B SENTINEL_C SENTINEL_VAR " ]; then
  ok "canary: 綴りに依存せず拾う (typeset / export / -q -d / 1 行 2 つ / 変数経由) & コメント行は拾わない"
else
  bad "canary: 抽出が壊れている (拾った sentinel = [$canary_got])。この状態の ⑥ は何も守らない"
fi
rm -f -- "$canary_src"; canary_src=""

# 本走査 — ヘルパーが lib に在るので、テスト本体に素の mktemp は **1 つも無い**のが正
stray=$(mktemp_lines "$REAP" | tr '\n' ' ')
if [ -z "$stray" ]; then
  ok "test_reap_orphan_servers.sh: 素の mktemp が 1 つも無い (すべて reap_mktemp_d 経由)"
else
  bad "test_reap_orphan_servers.sh: ヘルパーを通らない mktemp が行 $stray に在る (登録されないので cleanup が消せない)"
fi
# 🚨 **呼び出し側も pin する** (3 周目 P2)。`X=$(reap_mktemp_d …)` はコマンド置換 = サブシェル
# なので、値は親へ届くのに**登録は届かず**、正常終了のたびに 1 個ずつ漏れる (最小再現で実証済み)
if grep -qE '\$\(.*reap_mktemp_d' "$REAP"; then
  bad "test_reap_orphan_servers.sh: reap_mktemp_d を \$( ) で受けている (登録がサブシェルに閉じる)"
else
  ok "test_reap_orphan_servers.sh: reap_mktemp_d を \$( ) で受けていない"
fi
# 🚨 **実際の source 文を見る**。素の `grep 'reap_mktemp.sh'` は直前の
# `# shellcheck source=tests/tmux/lib/reap_mktemp.sh` という**コメント**に当たり、
# source を消す変異が緑で通った (自分で変異を当てて見つけた)
if grep -qE '^[[:space:]]*\.[[:space:]].*reap_mktemp\.sh' "$REAP" && grep -q 'reap_cleanup_tmpdirs' "$REAP"; then
  ok "test_reap_orphan_servers.sh: lib を source し、cleanup で登録ぶんを消している"
else
  bad "test_reap_orphan_servers.sh が lib を使っていない (source 文 / reap_cleanup_tmpdirs が無い)"
fi
# 🚨 **この検査自身の後始末も pin する**。3 周目 P1-1 は「trap を途中で解除して以降が無防備」
# だった。自分が満たしていない基準を相手に課さない (一時ファイルの後始末を守る検査が自分で漏らす)
SELF="${BASH_SOURCE[0]}"
# 🚨 **行頭アンカーで見る**。素の部分一致だと**この検査行そのもの**が pin を満たしてしまい、
# 本物の trap を消す変異が緑で通った (自分で変異を当てて見つけた。pin の自己参照)
if grep -qE '^trap tt_cleanup_all EXIT INT TERM HUP$' "$SELF" && ! grep -qE '^[[:space:]]*trap[[:space:]]+-[[:space:]]' "$SELF"; then
  ok "この検査自身: 後始末の trap がファイル全体を覆い、途中で解除していない"
else
  bad "この検査自身の trap が弱い (tt_cleanup_all がファイル全体を覆っていない / trap - で解除している)"
fi
if grep -q 'REAP_TMPDIRS+=(' "$LIB" && grep -q 'rm -rf "\${REAP_TMPDIRS\[@\]}"' "$LIB"; then
  ok "lib: 作った dir を登録し、まとめて消す関数を持つ"
else
  bad "lib が登録 / 一括削除を持っていない (reap_mktemp.sh)"
fi
# 🚨 ヘルパーがパスを stdout で返す形へ戻っていないか (戻すと呼び出し側が \$( ) を使いたくなる)
if grep -qE '^[[:space:]]*(printf|echo)[^#]*\$d' "$LIB"; then
  bad "reap_mktemp_d がパスを stdout で返している (呼び出し側が \$( ) で受けると登録がサブシェルに閉じる)"
else
  ok "reap_mktemp_d は stdout で返さず typeset -g で呼び出し元へ入れる"
fi


# --- ① 前提: kill-server だけでは socket が残る (この検査が守っている事実そのもの) ------------
#
# 🚨 これを canary として先に確かめる。もし tmux 側が将来 socket を消すようになったら、
# この検査は「何も守っていない」状態になるので、そのときに気づけるようにしておく。
tt_new_socket raw tt-cleanup-raw
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
tt_new_socket s1 tt-cleanup-a
tmux -L "$s1" -f /dev/null new-session -d 'sleep 30' >/dev/null 2>&1
p1=$(tmux -L "$s1" display -p '#{socket_path}' 2>/dev/null)
[ -S "$p1" ] || bad "前提: socket が作られていない ($p1)"
tt_tmux_kill_socket "$s1"
if [ -e "$p1" ]; then bad "tt_tmux_kill_socket が socket ファイルを残した: $p1"; rm -f -- "$p1"
else ok "tt_tmux_kill_socket は socket ファイルまで消す"; fi

# --- ③ 既に死んでいるサーバの socket も回収する ------------------------------------------------
tt_new_socket s2 tt-cleanup-b
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
# 🚨 **テンプレートを明示する**。macOS の `mktemp -d` は引数なしだと `$TMPDIR` を無視して
# Darwin のユーザ一時領域へ出るので、呼び出し側から隔離できない (3 周目 P3 の実測)
guard_dir=$(mktemp -d "${TMPDIR:-/tmp}/tt-guard.XXXXXX"); mkdir -p "$guard_dir/tmux-$(id -u)"
guard="$guard_dir/tmux-$(id -u)/default"
python3 -c 'import socket,sys; s=socket.socket(socket.AF_UNIX); s.bind(sys.argv[1])' "$guard" 2>/dev/null || : > "$guard"
TMUX_TMPDIR="$guard_dir" tt_tmux_kill_socket default
if [ -e "$guard" ]; then ok "default という名前の socket は消さない"
else bad "🚨 default を消した (本番の socket を消しうる)"; fi
rm -rf -- "$guard_dir"; guard_dir=""

# --- ⑤ 実テストが漏らさないこと (配線の確認) ---------------------------------------------------
#
# 🚨 ヘルパー単体の検査だけでは「呼び出し側が使っている」を 1 mm も守らない
# (mutation-verify-new-tests.md の「計算が正しい ≠ 配線されている」)。
WIRED=(tests/tmux/test_ctrl_v_paste.sh tests/claude/test_tmux_pane_state_bell.sh)
for t in "${WIRED[@]}"; do
  if grep -q 'tt_tmux_kill_socket' "$ROOT_DIR/$t"; then
    ok "$(basename "$t"): tt_tmux_kill_socket を使っている"
  else
    bad "$(basename "$t") が socket を置き去りにする形へ戻っている (kill-server だけでは消えない)"
  fi
done

# 🚨 件数を assert する (「1 件も走らないまま 0 件 fail=0 で緑」を塞ぐ)。
# 🚨 **固定値を書かない**。旧版は dir の個数 (2 周目 P3-D) / ⑤ のファイル列の長さ (3 周目 P3) に
# 結合しており、「対象を 1 つ減らす正しい変更」が閾値割れで red になった。可変部から導出する。
want_checks=$(( 9 + ${#WIRED[@]} + 2 ))   # ⑥ が 8 (対象 2 件の可読性 + canary + 本走査 5) / ⑤ / ①〜④ のうち固定 4 のうち 2 は tmux 依存
[ "$checks" -ge "$want_checks" ] || bad "検査が $checks 件しか走っていない (${want_checks} 件以上のはず)"
printf '\n検査 %d 件: fail=%d\n' "$checks" "$fails"
[ "$fails" -eq 0 ] || exit 1
echo "OK tmux socket cleanup"
