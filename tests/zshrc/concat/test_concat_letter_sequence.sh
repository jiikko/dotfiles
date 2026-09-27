#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# concat 英字連番 (lecture_03A / lecture_03B … / talk_05a / talk_05b …) のテスト

source "${0:A:h}/test_helper.sh"

printf '\n=== concat Letter Sequence Tests ===\n\n'

# Test L1: 共通サフィックス付きの英字連番を結合できる
# 誤警告の検査: マルチグループの事前判定は共通サフィックスを外さずに見るので、
# 1 グループへフォールスルーするときに「スキップしました」と出してはいけない
printf '## Test L1: Letter sequence with common suffix\n'
TEST_DIR="$TEST_TMP/letter1"
mkdir -p "$TEST_DIR"
for l in A B C; do echo "video $l" > "$TEST_DIR/lecture_03${l}-aac96k-enc.mp4"; done
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat --keep "$TEST_DIR/lecture_03C-aac96k-enc.mp4" "$TEST_DIR/lecture_03A-aac96k-enc.mp4" "$TEST_DIR/lecture_03B-aac96k-enc.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Letter sequence concat succeeds"
assert_file_exists "$TEST_DIR/lecture_03-aac96k-enc.mp4" "Output name drops the letter and keeps the common suffix"
assert_not_contains "$output" "スキップしました" "No false 'skipped' warning on single-group fallthrough"
order=$(print -r -- "$output" | grep -E '^   [0-9]+\. ' | sed -E 's/^   [0-9]+\. //' | tr '\n' ' ' || true)
if [[ "$order" == "lecture_03A-aac96k-enc.mp4 lecture_03B-aac96k-enc.mp4 lecture_03C-aac96k-enc.mp4 " ]]; then
  printf '✓ Concat order is A, B, C\n'
else
  bad '✗ Concat order should be A, B, C: got "%s"\n' "$order"
fi

# Test L14: 小文字の英字連番 (talk_05a / talk_05b) を結合できる
# (L6 / L12 が __concat_frame_hash 等を差し替える前に置く。後ろに置くとスタブの hash で順序検証が落ちる)
printf '\n## Test L14: Lowercase letter sequence with common suffix\n'
TEST_DIR="$TEST_TMP/letter14"
mkdir -p "$TEST_DIR"
for l in a b; do echo "video $l" > "$TEST_DIR/talk_05${l}-aac96k-enc.mp4"; done
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat --keep "$TEST_DIR/talk_05b-aac96k-enc.mp4" "$TEST_DIR/talk_05a-aac96k-enc.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Lowercase letter sequence concat succeeds"
assert_file_exists "$TEST_DIR/talk_05-aac96k-enc.mp4" "Output name drops the lowercase letter and keeps the common suffix"
order=$(print -r -- "$output" | grep -E '^   [0-9]+\. ' | sed -E 's/^   [0-9]+\. //' | tr '\n' ' ' || true)
if [[ "$order" == "talk_05a-aac96k-enc.mp4 talk_05b-aac96k-enc.mp4 " ]]; then
  printf '✓ Concat order is a, b\n'
else
  bad '✗ Concat order should be a, b: got "%s"\n' "$order"
fi

# Test L2: 英字が飛んでいたら欠番エラー
printf '\n## Test L2: Gap in letter sequence\n'
TEST_DIR="$TEST_TMP/letter2"
mkdir -p "$TEST_DIR"
echo a > "$TEST_DIR/lecture_03A.mp4"
echo c > "$TEST_DIR/lecture_03C.mp4"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat "$TEST_DIR/lecture_03A.mp4" "$TEST_DIR/lecture_03C.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Gap in letters is rejected"
assert_contains "$output" "欠番" "Reports the gap"

# Test L3: ディレクトリにある兄弟 (D) を渡し忘れたら拒否
printf '\n## Test L3: Sibling letter file left out\n'
TEST_DIR="$TEST_TMP/letter3"
mkdir -p "$TEST_DIR"
for l in A B C D; do echo "video $l" > "$TEST_DIR/lecture_03${l}-enc.mp4"; done
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat "$TEST_DIR/lecture_03A-enc.mp4" "$TEST_DIR/lecture_03B-enc.mp4" "$TEST_DIR/lecture_03C-enc.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Left-out sibling letter file is rejected"
assert_contains "$output" "lecture_03D-enc.mp4" "Lists the left-out sibling"

# Test L4: 英字連番と読まない形 (汎用の連番抽出にも、英字連番の判定にも当たらない)
printf '\n## Test L4: Shapes that are not letter sequences\n'
# 小文字は a から始まらなければ単位の表記 (1080p / 4k) として数字連番の判定へ回す。大文字小文字の混在も連番にしない
for pair in "video_final video_finaz" "clip_1080p clip_1080q" "clip_4k clip_4l" "lecture_03a lecture_03B"; do
  unsetopt err_exit
  letter_err=$(__concat_resolve_letter_sequence ${=pair} 2>&1)
  letter_rc=$?
  setopt err_exit
  # rc=1 (英字連番の形ではない → 数字連番の判定へ進む)。rc=2 だと「A から始まっていません」という
  # 見当違いの案内で止まる
  if (( letter_rc == 1 )) && [[ -z "$letter_err" ]]; then
    printf '✓ "%s" is not a letter sequence\n' "$pair"
  else
    bad '✗ "%s" should not be a letter sequence: rc=%s err=%s\n' "$pair" "$letter_rc" "$letter_err"
  fi
done
# 英字連番は汎用の連番抽出 (グルーピングと共用) には入れない
if __concat_extract_number "lecture_03C"; then
  bad '✗ extract_number should not read lecture_03C: got "%s"\n' "$REPLY"
else
  printf '✓ extract_number does not read lecture_03C\n'
fi

# Test L5: 本当に連番でないファイルは、グループ分けで結合するときは従来どおり警告する
printf '\n## Test L5: Skip warning still shown when grouping runs\n'
TEST_DIR="$TEST_TMP/letter5"
mkdir -p "$TEST_DIR"
for f in video_001 video_002 clip_001 clip_002 random; do echo "$f" > "$TEST_DIR/$f.mp4"; done
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat --keep "$TEST_DIR" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Directory mode succeeds"
assert_contains "$output" "スキップしました: random.mp4" "Warns about the non-sequence file"

# Test L6: フレーム順序検証が英字連番を読む (読めないと検証ごと黙って skip され rc=0 になる)
printf '\n## Test L6: Frame order verification reads letter sequence\n'
TEST_DIR="$TEST_TMP/letter6"
mkdir -p "$TEST_DIR"
touch "$TEST_DIR/lecture_03A-enc.mp4" "$TEST_DIR/lecture_03B-enc.mp4" "$TEST_DIR/out.mp4"
__concat_get_duration() { echo "10.0"; }
__concat_frame_hash() {
  case "${1:t}:$2" in
    lecture_03A-enc.mp4:3.000) echo hash_a ;;
    lecture_03B-enc.mp4:3.000) echo hash_b ;;
    out.mp4:3.000)          echo hash_b ;;  # 出力の先頭に B が来ている (順序の取り違え)
    out.mp4:13.000)         echo hash_a ;;
    *)                      echo "other_${1:t}_$2" ;;
  esac
}
unsetopt err_exit
__concat_verify_frame_order "$TEST_DIR/out.mp4" "$TEST_DIR/lecture_03B-enc.mp4" "$TEST_DIR/lecture_03A-enc.mp4"
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Swapped letter order is detected"
assert_contains "$REPLY" "lecture_03A-enc.mp4" "Mismatch names the first file (A)"

# Test L7: ディレクトリモードで英字の単独ファイルを黙って取り残さない (従来どおり警告する)
printf '\n## Test L7: Lone letter file in directory mode is warned about\n'
TEST_DIR="$TEST_TMP/letter7"
mkdir -p "$TEST_DIR"
for f in episode_01 episode_02 episode_02A; do echo "$f" > "$TEST_DIR/$f.mp4"; done
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat --dryrun "$TEST_DIR" 2>&1)
setopt err_exit
assert_contains "$output" "スキップしました: episode_02A.mp4" "Lone letter file is reported as skipped"

# Test L8: 英字と数字の連番を混ぜない (番号が重なったまま結合しない)
printf '\n## Test L8: Letter and number sequences are not mixed\n'
TEST_DIR="$TEST_TMP/letter8"
mkdir -p "$TEST_DIR"
echo a > "$TEST_DIR/lecture_03A.mp4"
echo b > "$TEST_DIR/lecture_03_1.mp4"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat --dryrun "$TEST_DIR/lecture_03A.mp4" "$TEST_DIR/lecture_03_1.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Mixed letter/number files are rejected"

# Test L9: A から始まらない英字 (版の命名) は連番にしない
printf '\n## Test L9: Letters not starting at A are rejected\n'
TEST_DIR="$TEST_TMP/letter9"
mkdir -p "$TEST_DIR"
echo p > "$TEST_DIR/movie_1080P.mp4"
echo q > "$TEST_DIR/movie_1080Q.mp4"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat --dryrun "$TEST_DIR/movie_1080P.mp4" "$TEST_DIR/movie_1080Q.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "P/Q naming is rejected"
assert_contains "$output" "A から始まっていません" "Explains why"

# Test L10: ディレクトリモードは英字の組 (カメラ違い等) を自動では結合しない
printf '\n## Test L10: Directory mode does not auto-join letter pairs\n'
TEST_DIR="$TEST_TMP/letter10"
mkdir -p "$TEST_DIR"
for f in interview_01A interview_01B; do echo "$f" > "$TEST_DIR/$f.mp4"; done
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat --dryrun "$TEST_DIR" 2>&1)
setopt err_exit
assert_not_contains "$output" "結合対象" "Letter pair is not joined in directory mode"

# Test L11: 数字で終わる prefix の数字連番は、同じ dir の英字ファイルで拒否されない
printf '\n## Test L11: Number sequence is not blocked by a letter file\n'
TEST_DIR="$TEST_TMP/letter11"
mkdir -p "$TEST_DIR"
for f in show_2024_01 show_2024_02 show_2024A; do echo "$f" > "$TEST_DIR/$f.mp4"; done
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat --dryrun "$TEST_DIR/show_2024_01.mp4" "$TEST_DIR/show_2024_02.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Number sequence succeeds despite show_2024A"

# Test L12: 共通の末尾が「数字 + 英大文字」の数字連番 (clip9A / clip10A) を、検証が英字で読んで落とさない
printf '\n## Test L12: Number sequence ending in a common letter is not failed by verification\n'
TEST_DIR="$TEST_TMP/letter12"
mkdir -p "$TEST_DIR"
touch "$TEST_DIR/clip9A.mp4" "$TEST_DIR/clip10A.mp4" "$TEST_DIR/out.mp4"
__concat_frame_hash() {
  case "${1:t}:$2" in
    clip9A.mp4:3.000)  echo hash_9 ;;
    clip10A.mp4:3.000) echo hash_10 ;;
    out.mp4:3.000)     echo hash_9 ;;   # 正しい順 (9 → 10)
    out.mp4:13.000)    echo hash_10 ;;
    *)                 echo "other_${1:t}_$2" ;;
  esac
}
unsetopt err_exit
__concat_verify_frame_order "$TEST_DIR/out.mp4" "$TEST_DIR/clip9A.mp4" "$TEST_DIR/clip10A.mp4"
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Correct 9 -> 10 output is not reported as mismatch"

# Test L13: 英字連番の渡し忘れチェックは、数字の兄弟 (ep10) を拾わない
printf '\n## Test L13: Letter mode ignores number siblings\n'
TEST_DIR="$TEST_TMP/letter13"
mkdir -p "$TEST_DIR"
for f in ep1A ep1B ep10 ep11; do echo "$f" > "$TEST_DIR/$f.mp4"; done
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat --dryrun "$TEST_DIR/ep1A.mp4" "$TEST_DIR/ep1B.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "ep1A + ep1B succeeds despite ep10 / ep11"

# Test L15: 小文字の英字連番の渡し忘れ (c) を拒否する
printf '\n## Test L15: Lowercase sibling left out\n'
TEST_DIR="$TEST_TMP/letter15"
mkdir -p "$TEST_DIR"
for l in a b c; do echo "video $l" > "$TEST_DIR/talk_05${l}-enc.mp4"; done
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat --dryrun "$TEST_DIR/talk_05a-enc.mp4" "$TEST_DIR/talk_05b-enc.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Left-out lowercase sibling is rejected"
assert_contains "$output" "talk_05c-enc.mp4" "Lists the left-out lowercase sibling"

# Test L16: フレーム順序検証が小文字の英字連番を読む
printf '\n## Test L16: Frame order verification reads lowercase letter sequence\n'
TEST_DIR="$TEST_TMP/letter16"
mkdir -p "$TEST_DIR"
touch "$TEST_DIR/talk_05a-aac96k-enc.mp4" "$TEST_DIR/talk_05b-aac96k-enc.mp4" "$TEST_DIR/out.mp4"
__concat_frame_hash() {
  case "${1:t}:$2" in
    talk_05a-aac96k-enc.mp4:3.000) echo hash_a ;;
    talk_05b-aac96k-enc.mp4:3.000) echo hash_b ;;
    out.mp4:3.000)                  echo hash_b ;;  # 出力の先頭に b が来ている (順序の取り違え)
    out.mp4:13.000)                 echo hash_a ;;
    *)                              echo "other_${1:t}_$2" ;;
  esac
}
unsetopt err_exit
__concat_verify_frame_order "$TEST_DIR/out.mp4" "$TEST_DIR/talk_05b-aac96k-enc.mp4" "$TEST_DIR/talk_05a-aac96k-enc.mp4"
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Swapped lowercase letter order is detected"
assert_contains "$REPLY" "talk_05a-aac96k-enc.mp4" "Mismatch names the first file (a)"

# Test L17: 順序検証は prefix 側の「数字 + 英字」(1080p_) を英字連番と読まない (読むと同じ番号になり黙って skip する)
printf '\n## Test L17: Frame order verification ignores letters in the prefix\n'
TEST_DIR="$TEST_TMP/letter17"
mkdir -p "$TEST_DIR"
touch "$TEST_DIR/1080p_talk_05a-enc.mp4" "$TEST_DIR/1080p_talk_05b-enc.mp4" "$TEST_DIR/out.mp4"
__concat_frame_hash() {
  case "${1:t}:$2" in
    1080p_talk_05a-enc.mp4:3.000) echo hash_a ;;
    1080p_talk_05b-enc.mp4:3.000) echo hash_b ;;
    out.mp4:3.000)                 echo hash_b ;;  # 出力の先頭に b が来ている (順序の取り違え)
    out.mp4:13.000)                echo hash_a ;;
    *)                             echo "other_${1:t}_$2" ;;
  esac
}
unsetopt err_exit
__concat_verify_frame_order "$TEST_DIR/out.mp4" "$TEST_DIR/1080p_talk_05b-enc.mp4" "$TEST_DIR/1080p_talk_05a-enc.mp4"
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Swapped order is detected despite 1080p_ prefix"

# Test L18: 大文字の英字連番の渡し忘れチェックは、小文字 1 文字の兄弟 (4k 版) を拾わない
printf '\n## Test L18: Uppercase letter mode ignores lowercase siblings\n'
TEST_DIR="$TEST_TMP/letter18"
mkdir -p "$TEST_DIR"
for f in lecture_4A lecture_4B lecture_4k; do echo "$f" > "$TEST_DIR/$f.mp4"; done
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat --dryrun "$TEST_DIR/lecture_4A.mp4" "$TEST_DIR/lecture_4B.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "lecture_4A + 4B succeeds despite lecture_4k"

# Test L19: 共通サフィックスの NFC / NFD が混ざっていても、順序検証は英字を読む (主処理は NFC に揃えて受け付ける)
printf '\n## Test L19: Frame order verification with mixed NFC / NFD suffix\n'
TEST_DIR="$TEST_TMP/letter19"
mkdir -p "$TEST_DIR"
nfc_b="$TEST_DIR/lec_1B-caf"$'\xc3\xa9'".mp4"      # é = U+00E9
nfd_a="$TEST_DIR/lec_1A-cafe"$'\xcc\x81'".mp4"     # é = e + U+0301
touch "$nfc_b" "$nfd_a" "$TEST_DIR/out.mp4"
__concat_frame_hash() {
  case "${1:t}:$2" in
    lec_1A*:3.000)  echo hash_a ;;
    lec_1B*:3.000)  echo hash_b ;;
    out.mp4:3.000)  echo hash_b ;;  # 出力の先頭に B が来ている (順序の取り違え)
    out.mp4:13.000) echo hash_a ;;
    *)              echo "other_${1:t}_$2" ;;
  esac
}
unsetopt err_exit
__concat_verify_frame_order "$TEST_DIR/out.mp4" "$nfc_b" "$nfd_a"
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Swapped order is detected despite mixed NFC / NFD suffix"

printf '\n=== Letter Sequence Tests Completed ===\n'
