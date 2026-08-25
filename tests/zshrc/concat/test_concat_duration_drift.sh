#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# concat の duration 乖離検査の回帰テスト (issue 076)
#
# なぜ必要か: production の乖離検査 (__concat_diagnose_output の「duration乖離チェック」) は
# 生きているが、**その検査が壊れても concat テストは緑のまま通る**状態だった。原因は 2 つ:
#   ① test_helper.sh が `CONCAT_DURATION_TOLERANCE=100` を export している (モック環境で実時間の
#      duration を作れないための **意図的な test seam**。drift ではないので触らない)
#   ② ffprobe モックが出力 duration を 20.0 に固定していて「ずれた出力」を作れない
# ここでは ② を関数 seam で回避する: `__concat_get_duration` を上書きして出力側の duration だけを
# 制御し、tolerance もこのファイル内でだけ production 既定 (5) に戻して 2 方向を pin する。
#
# ⚠️ `CONCAT_DURATION_TOLERANCE=100` を 5 へ「直す」修正は誤り。モックが duration を 20.0 に
#   固定している他の 14 ファイルで正常なテストが乖離扱いになり false failure を作る (issue 076 の
#   「崩れた側」)。tolerance の上書きはこのテストの中だけに閉じること。

source "${0:A:h}/test_helper.sh"

TEST_DIR="$TEST_TMP/drift"
mkdir -p "$TEST_DIR"

printf '\n=== concat Duration Drift Tests ===\n\n'

# 出力ファイルの duration だけを差し替える seam。入力側は呼ばれない (expected は引数で渡す)。
typeset -g MOCK_OUT_DURATION=""
__concat_get_duration() {
  printf '%s\n' "$MOCK_OUT_DURATION"
}

# 乖離検査だけを見たいので、サイズ検査に入らない引数で呼ぶ (expected_size=0 は 1MB 未満で skip)。
#   drift_rc <出力duration> <入力合計duration> [tolerance]
# 返り値は __concat_diagnose_output の rc、理由は REPLY。
drift_rc() {
  MOCK_OUT_DURATION="$1"
  local expected="$2" tol="${3:-5}"
  REPLY=""
  local rc=0
  if [[ "$tol" == "default" ]]; then
    # production 既定 (`:-5`) を見る。test_helper.sh が 100 を export しているので明示的に外す
    local saved="$CONCAT_DURATION_TOLERANCE"
    unset CONCAT_DURATION_TOLERANCE
    __concat_diagnose_output "$TEST_DIR/out.mp4" "$expected" 0 0 || rc=$?
    export CONCAT_DURATION_TOLERANCE="$saved"
  else
    CONCAT_DURATION_TOLERANCE="$tol" __concat_diagnose_output "$TEST_DIR/out.mp4" "$expected" 0 0 || rc=$?
  fi
  return $rc
}

: > "$TEST_DIR/out.mp4"

# --- 1. 乖離なし: 通る -------------------------------------------------------
unsetopt err_exit
drift_rc 20.0 20
rc=$?
setopt err_exit
assert_exit_code 0 "$rc" "乖離なし (20.0 / 20) は通る"

# --- 2. 短い側: 検査が発火する ----------------------------------------------
unsetopt err_exit
drift_rc 15.0 20
rc=$?
setopt err_exit
assert_exit_code 1 "$rc" "出力が 25% 短いと検査が発火する"
assert_contains "$REPLY" "短い" "短い側の理由が REPLY に入る"
assert_contains "$REPLY" "25.0%" "短くなった割合を数値で報告する"

# --- 3. 長い側: 検査が発火する ----------------------------------------------
unsetopt err_exit
drift_rc 26.0 20
rc=$?
setopt err_exit
assert_exit_code 1 "$rc" "出力が 30% 長いと検査が発火する"
assert_contains "$REPLY" "長い" "長い側の理由が REPLY に入る"

# --- 4. 帯の境界 (tolerance 5% = ±0.05) --------------------------------------
# 19.0/20 = 0.95 はちょうど下限。lt 判定なので通る側に入る
unsetopt err_exit
drift_rc 19.0 20
rc=$?
setopt err_exit
assert_exit_code 0 "$rc" "ratio がちょうど下限 (0.95) なら通る"
unsetopt err_exit
drift_rc 18.9 20
rc=$?
setopt err_exit
assert_exit_code 1 "$rc" "下限を 1 つ割る (0.945) と発火する"

# --- 5. 10 秒以下はスキップする契約 ------------------------------------------
# 入力合計が 10 秒以下だと乖離検査そのものに入らない (短尺は誤検知が多いため)
unsetopt err_exit
drift_rc 5.0 10
rc=$?
setopt err_exit
assert_exit_code 0 "$rc" "入力合計が 10 秒以下なら乖離検査をスキップする"

# --- 6. production の既定 tolerance (5%) が生きている ------------------------
# ⚠️ ここを pin しないと `${CONCAT_DURATION_TOLERANCE:-5}` を `:-100` に変える変異が
#   **緑のまま通る** (実測 2026-08-25)。test_helper.sh が 100 を export しているので、既定値を
#   見るテストは自分で unset する必要がある = 「seam が本番の既定を隠している」形。
unsetopt err_exit
drift_rc 18.8 20 default
rc=$?
setopt err_exit
assert_exit_code 1 "$rc" "既定 tolerance (5%) では 6% の乖離が発火する"
unsetopt err_exit
drift_rc 19.5 20 default
rc=$?
setopt err_exit
assert_exit_code 0 "$rc" "既定 tolerance (5%) では 2.5% の乖離は通る"

printf '\n=== Duration Drift Tests Completed ===\n'
