#!/usr/bin/env zsh
# vendor/tmux-plugins/tmux-smooth-scroll の補強パッチ部分のユニットテスト。
# tmux サーバ不要 (socket が作れない sandbox 環境でも走る。E2E の test_smooth_scroll.sh は
# その環境では skip されるため、コアロジックの実行率をここで担保する)。
#
# 対象と根拠:
#   - arbiter.pl: held 判定の境界・壊れた状態ファイルの自己回復・flock による押下直列化
#     (直列化は bash 実装時代に実際に踏んだレース = 並行インスタンスが同値 gen になり
#      打ち切り不発、の再発防止 property test)
#   - animator.pl: PATH に偽 tmux スタブを置いて送出プランを検証 (chunk 化の行数保存・
#     世代打ち切り・send-keys 失敗での打ち切り・終了時の anim_until クリア)
# scroll.sh / init.sh はスタブだと「モックの検証」になるため対象外 (実物との境界は
# E2E test_smooth_scroll.sh が担保する)。

set -euo pipefail
unset CDPATH

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
SRC="$ROOT_DIR/vendor/tmux-plugins/tmux-smooth-scroll/src"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
trap 'exit 130' INT TERM

fail() {
  print -u2 "[test-smooth-scroll-unit] FAIL: $1"
  exit 1
}

now_ms() {
  perl -MTime::HiRes=time -e 'printf "%d", time()*1000'
}

# arbiter を実行して "GEN HELD" を返す
arbiter() {
  perl "$SRC/arbiter.pl" "$@"
}

##### arbiter.pl #####

# A1: 初回 (状態ファイル不在) → アニメ開始 (held=0, gen=1)、anim_until が立つ
st="$WORK/a1"
out=$(arbiter "$st" 150 1500)
[[ "$out" == "1 0" ]] || fail "A1: expected '1 0', got '$out'"
read -r gen last until < "$st"
[[ "$until" -gt 0 ]] || fail "A1: anim_until not set (until=$until)"

# A2: repeat_ms 未満の再押下 → 素通し (held=1, gen 増加)、anim_until はクリア
st="$WORK/a2"
print -r -- "1 $(now_ms) 0" > "$st"
out=$(arbiter "$st" 150 1500)
[[ "$out" == "2 1" ]] || fail "A2: expected '2 1', got '$out'"
read -r gen last until < "$st"
[[ "$until" -eq 0 ]] || fail "A2: anim_until should be 0 on held (until=$until)"

# A3: repeat_ms を過ぎた押下 (アニメ非進行) → アニメ開始
st="$WORK/a3"
print -r -- "5 $(( $(now_ms) - 10000 )) 0" > "$st"
out=$(arbiter "$st" 150 1500)
[[ "$out" == "6 0" ]] || fail "A3: expected '6 0', got '$out'"

# A4: 間隔は空いているがアニメ進行中 (anim_until 未来) → 素通し
st="$WORK/a4"
print -r -- "5 $(( $(now_ms) - 10000 )) $(( $(now_ms) + 10000 ))" > "$st"
out=$(arbiter "$st" 150 1500)
[[ "$out" == "6 1" ]] || fail "A4: expected '6 1', got '$out'"

# A5: 壊れた状態ファイル → 自己回復して初回相当 (gen=1, held=0)
st="$WORK/a5"
print -r -- "garbage not-a-number %0" > "$st"
out=$(arbiter "$st" 150 1500)
[[ "$out" == "1 0" ]] || fail "A5: expected '1 0' on corrupt state, got '$out'"

# A6 (property): 並行起動しても flock で押下が直列化され、gen が重複なく 1..N になる。
# bash 実装時代の実レース (全員が同じ旧状態を読み同値 gen → 世代打ち切り不発) の再発防止
st="$WORK/a6"
N=20
for i in {1..$N}; do
  arbiter "$st" 150 1500 > "$WORK/a6.out.$i" &
done
wait
gens=$(cut -d' ' -f1 "$WORK"/a6.out.* | sort -n)
expected=$(seq 1 $N)
[[ "$gens" == "$expected" ]] || fail "A6: concurrent gens not serialized 1..$N: $(print -r -- $gens | tr '\n' ' ')"
read -r gen last until < "$st"
[[ "$gen" -eq $N ]] || fail "A6: final gen=$gen, expected $N"

##### animator.pl (偽 tmux スタブ) #####

# スタブ: 呼び出し引数を 1 行ずつログし、環境変数で失敗/世代書き換えを注入できる
#   STUB_FAIL_AFTER=n     : n 回を超えた呼び出しは exit 1 (send-keys 失敗の再現)
#   STUB_BUMP_GEN_AFTER=n : ちょうど n 回目の呼び出し後に状態ファイルの gen を 999 へ
#                           (アニメ中に別の押下が gen を進めた状況の再現)
mkdir -p "$WORK/bin"
cat > "$WORK/bin/tmux" <<'EOF'
#!/bin/sh
echo "$@" >> "$STUB_LOG"
n=$(wc -l < "$STUB_LOG")
if [ -n "${STUB_FAIL_AFTER:-}" ] && [ "$n" -gt "$STUB_FAIL_AFTER" ]; then exit 1; fi
if [ -n "${STUB_BUMP_GEN_AFTER:-}" ] && [ "$n" -eq "$STUB_BUMP_GEN_AFTER" ]; then
  echo "999 0 0" > "$STUB_STATE"
fi
exit 0
EOF
chmod +x "$WORK/bin/tmux"

# animator を偽 tmux + base_delay=1µs で実行する (アニメ待ちを実質ゼロにする)
animator() {
  local log="$1" state="$2"; shift 2
  : > "$log"
  STUB_LOG="$log" STUB_STATE="$state" PATH="$WORK/bin:$PATH" \
    perl "$SRC/animator.pl" "$@"
}

# 送出ログから -N の合計行数を出す
sum_n() {
  awk '{for(i=1;i<NF;i++) if($i=="-N"){s+=$(i+1)}} END{print s+0}' "$1"
}

# B1: 行ごとモード (max_steps=0): 14 回送出 × 各 -N 1 = 計 14 行
log="$WORK/b1.log"
animator "$log" "" 1 14 up sine "" "" "" 0
[[ $(wc -l < "$log") -eq 14 ]] || fail "B1: expected 14 sends, got $(wc -l < "$log")"
[[ $(sum_n "$log") -eq 14 ]] || fail "B1: total lines $(sum_n "$log"), expected 14"
grep -q -- "-X scroll-up" "$log" || fail "B1: direction not propagated"

# B2: chunk モード (max_steps=10 < lines=14): 送出は 10 回に抑えつつ合計行数は保存
log="$WORK/b2.log"
animator "$log" "" 1 14 up sine "" "" "" 10
[[ $(wc -l < "$log") -eq 10 ]] || fail "B2: expected 10 sends, got $(wc -l < "$log")"
[[ $(sum_n "$log") -eq 14 ]] || fail "B2: total lines $(sum_n "$log"), expected 14 (行数保存)"

# B3: lines <= max_steps なら行ごとのまま
log="$WORK/b3.log"
animator "$log" "" 1 5 down quad "" "" "" 10
[[ $(wc -l < "$log") -eq 5 ]] || fail "B3: expected 5 sends, got $(wc -l < "$log")"
grep -q -- "-X scroll-down" "$log" || fail "B3: direction not propagated"

# B4: 世代打ち切り: 3 回目の送出後に gen が書き換わる → 4 回目以降は送出されない
log="$WORK/b4.log"; st="$WORK/b4.state"
print -r -- "7 1234 9999999999999" > "$st"
STUB_BUMP_GEN_AFTER=3 animator "$log" "$st" 1 14 up sine "" "$st" 7 0
[[ $(wc -l < "$log") -eq 3 ]] || fail "B4: expected stop after 3 sends, got $(wc -l < "$log")"
read -r gen last until < "$st"
[[ "$gen" -eq 999 ]] || fail "B4: 追い越し側の状態を上書きしてはいけない (gen=$gen)"

# B5: send-keys 失敗 (copy-mode 離脱相当): 失敗した呼び出しで打ち切り
log="$WORK/b5.log"
STUB_FAIL_AFTER=2 animator "$log" "" 1 14 up sine "" "" "" 0
[[ $(wc -l < "$log") -eq 3 ]] || fail "B5: expected stop at 3rd (failed) send, got $(wc -l < "$log")"

# B6: 正常完走: anim_until がクリアされ、gen/last は保存される (flock 下の終了処理)
log="$WORK/b6.log"; st="$WORK/b6.state"
print -r -- "5 1234 9999999999999" > "$st"
animator "$log" "$st" 1 6 up sine "" "$st" 5 0
read -r gen last until < "$st"
[[ "$gen" -eq 5 && "$last" -eq 1234 && "$until" -eq 0 ]] \
  || fail "B6: finish-clear broken (state: gen=$gen last=$last until=$until, expected 5 1234 0)"

print "[test-smooth-scroll-unit] OK (arbiter A1-A6, animator B1-B6)"
