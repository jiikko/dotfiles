#!/usr/bin/env zsh
# shellcheck shell=bash
# concat エッジケース + NFC正規化テスト (Test 17-23)

source "${0:A:h}/test_helper.sh"

printf '\n=== concat Edge Case Tests (17-23) ===\n\n'

# Test 17: 出力ファイル既存時のスキップ
printf '## Test 17: Skip when output file exists\n'
TEST_DIR="$TEST_TMP/test17"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.mp4"
echo "video 2" > "$TEST_DIR/video_002.mp4"
echo "existing output" > "$TEST_DIR/video.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "SKIP" "Skips when output file already exists"

# Test 18: 出力ファイル名が入力と衝突するエラー
printf '\n## Test 18: Error when output filename collides with input\n'
TEST_DIR="$TEST_TMP/test18"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_1.mp4"
echo "video 2" > "$TEST_DIR/video.mp4"  # これが入力かつ出力名と衝突
cd "$TEST_DIR"
# この場合、video.mp4 が入力ファイルで、出力も video.mp4 になる
# 実際には video_1 と video で共通プレフィックスが "video" になる
unsetopt err_exit
output=$(concat "$TEST_DIR/video_1.mp4" "$TEST_DIR/video.mp4" 2>&1 || true)
# 連番パターンがないのでそちらでエラーになる
setopt err_exit
# このテストは連番パターンエラーになるので、別のケースでテスト

# Test 19: 0から始まる連番
printf '\n## Test 19: Sequence starting from 0\n'
TEST_DIR="$TEST_TMP/test19"
mkdir -p "$TEST_DIR"
echo "video 0" > "$TEST_DIR/video_000.mp4"
echo "video 1" > "$TEST_DIR/video_001.mp4"
echo "video 2" > "$TEST_DIR/video_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_000.mp4" "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_exists "$TEST_DIR/video.mp4" "Output file is created for sequence starting from 0"
assert_contains "$output" "完了" "Reports success for 0-starting sequence"

# Test 20: 大文字小文字を区別しない拡張子
printf '\n## Test 20: Case-insensitive extension\n'
TEST_DIR="$TEST_TMP/test20"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.MP4"
echo "video 2" > "$TEST_DIR/video_002.Mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.MP4" "$TEST_DIR/video_002.Mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_exists "$TEST_DIR/video.mp4" "Output file is created with mixed-case extensions"

# Test 21: 様々な許可拡張子
printf '\n## Test 21: Various allowed extensions\n'
for ext in avi mov mkv webm flv wmv m4v mpg mpeg 3gp ts m2ts; do
  TEST_DIR="$TEST_TMP/test21_$ext"
  mkdir -p "$TEST_DIR"
  echo "video 1" > "$TEST_DIR/video_001.$ext"
  echo "video 2" > "$TEST_DIR/video_002.$ext"
  cd "$TEST_DIR"
  unsetopt err_exit
  concat "$TEST_DIR/video_001.$ext" "$TEST_DIR/video_002.$ext" > /dev/null 2>&1
  exit_code=$?
  setopt err_exit
  if [[ -f "$TEST_DIR/video.mp4" ]]; then
    printf '✓ Extension .%s is accepted\n' "$ext"
  else
    printf '✗ Extension .%s failed\n' "$ext"
  fi
done

# Test 22: スペースを含むパス
printf '\n## Test 22: Paths with spaces\n'
TEST_DIR="$TEST_TMP/test22 with spaces/sub dir"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video file_001.mp4"
echo "video 2" > "$TEST_DIR/video file_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video file_001.mp4" "$TEST_DIR/video file_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_exists "$TEST_DIR/video file.mp4" "Output file is created with spaces in path"

# Test 23a: __concat_get_stem のNFC正規化ユニットテスト
printf '\n## Test 23a: __concat_get_stem normalizes NFD to NFC\n'
nfd_pu=$'\xe3\x83\x95\xe3\x82\x9a'  # フ + combining mark (NFD)
nfc_pu=$'\xe3\x83\x97'               # プ (NFC)
result_nfd=$(__concat_get_stem "/path/to/clip_${nfd_pu}.mp4")
result_nfc=$(__concat_get_stem "/path/to/clip_${nfc_pu}.mp4")
if [[ "$result_nfd" == "$result_nfc" ]]; then
  printf '✓ NFD and NFC inputs produce identical stems\n'
else
  printf '✗ NFD and NFC stems differ (nfd=%s, nfc=%s)\n' "$result_nfd" "$result_nfc"
  return 1
fi
assert_contains "$result_nfd" "clip_" "Stem contains expected prefix"

# Test 23b: __concat_get_stem の基本動作
printf '\n## Test 23b: __concat_get_stem basic behavior\n'
result=$(__concat_get_stem "/some/dir/video_001.mp4")
if [[ "$result" == "video_001" ]]; then
  printf '✓ Extracts stem without extension\n'
else
  printf '✗ Expected video_001, got %s\n' "$result"
  return 1
fi
result=$(__concat_get_stem "noext")
if [[ "$result" == "noext" ]]; then
  printf '✓ Handles file without extension\n'
else
  printf '✗ Expected noext, got %s\n' "$result"
  return 1
fi

# Test 23: NFD/NFC混在ファイル名
printf '\n## Test 23: Mixed NFD/NFC filenames\n'
TEST_DIR="$TEST_TMP/test23"
mkdir -p "$TEST_DIR"
# NFD: プ = フ(U+30D5) + combining semi-voiced mark(U+309A)
# NFC: プ = U+30D7
nfd_pu=$'\xe3\x83\x95\xe3\x82\x9a'  # フ + combining mark
nfc_pu=$'\xe3\x83\x97'               # プ
echo "video 1" > "$TEST_DIR/clip_${nfd_pu}_1.mp4"
echo "video 2" > "$TEST_DIR/clip_${nfc_pu}_2.mp4"
echo "video 3" > "$TEST_DIR/clip_${nfc_pu}_3.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/clip_${nfd_pu}_1.mp4" "$TEST_DIR/clip_${nfc_pu}_2.mp4" "$TEST_DIR/clip_${nfc_pu}_3.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Mixed NFD/NFC succeeds after normalization"
assert_contains "$output" "完了" "Reports success for mixed NFD/NFC"

# Test 24a: __concat_extract_number unit test for _#WordN pattern
printf '\n## Test 24a: __concat_extract_number handles _#WordN\n'
__concat_extract_number "movie_X_#Ep1"
if [[ "$REPLY" == "1::movie_X_#Ep" ]]; then
  printf '✓ _#Ep1 → num=1, prefix=movie_X_#Ep\n'
else
  printf '✗ Expected "1::movie_X_#Ep", got "%s"\n' "$REPLY"
  return 1
fi
__concat_extract_number "movie_X_#Sp2"
if [[ "$REPLY" == "2::movie_X_#Sp" ]]; then
  printf '✓ _#Sp2 → num=2, prefix=movie_X_#Sp (異ワードは別グループ)\n'
else
  printf '✗ Expected "2::movie_X_#Sp", got "%s"\n' "$REPLY"
  return 1
fi
__concat_extract_number "movie_X_#Ep10"
if [[ "$REPLY" == "10::movie_X_#Ep" ]]; then
  printf '✓ _#Ep10 → num=10 (multi-digit OK)\n'
else
  printf '✗ Expected "10::movie_X_#Ep", got "%s"\n' "$REPLY"
  return 1
fi
# 互換性チェック: 既存パターンが従来通り動くこと
# partN は専用 branch で "part" を prefix から落とすので prefix=video_
__concat_extract_number "video_part1"
if [[ "$REPLY" == "1::video_" ]]; then
  printf '✓ 互換: _partN は専用 branch で従来通り解釈される (prefix=video_)\n'
else
  printf '✗ Backward compat broken for _partN: got "%s"\n' "$REPLY"
  return 1
fi
# SceneN は alpha+num branch で prefix にワードごと取り込む
__concat_extract_number "video_Scene3"
if [[ "$REPLY" == "3::video_Scene" ]]; then
  printf '✓ 互換: _SceneN は alpha+num branch で従来通り解釈される (prefix=video_Scene)\n'
else
  printf '✗ Backward compat broken for _SceneN: got "%s"\n' "$REPLY"
  return 1
fi

# Test 24: _#WordN 連番パターンの結合 (integration)
# 出力ファイル名は末尾の _#Ep が剥がれて movie_X.mp4 になる。
printf '\n## Test 24: _#EpN sequence concat\n'
TEST_DIR="$TEST_TMP/test24"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/movie_X_#Ep1.mp4"
echo "video 2" > "$TEST_DIR/movie_X_#Ep2.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/movie_X_#Ep1.mp4" "$TEST_DIR/movie_X_#Ep2.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_exists "$TEST_DIR/movie_X.mp4" "Output file created for _#EpN sequence (suffix _#Ep stripped)"
assert_contains "$output" "完了" "Reports success for _#EpN sequence"

printf '\n=== Edge Case Tests Completed ===\n'
