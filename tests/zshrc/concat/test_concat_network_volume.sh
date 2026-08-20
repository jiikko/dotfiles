#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# ネットワークボリューム (smbfs 等) 上の元ファイルは、ゴミ箱ではなく物理削除される
#
# 仕様:
#   - smbfs/afpfs/nfs/webdav/cifs マウント上のファイルは trash を経由せず rm される
#     (ネットワークボリュームはゴミ箱を持たず trash が必ず失敗するため)
#   - ローカル FS (apfs 等) は従来どおり trash 経由

source "${0:A:h}/test_helper.sh"

printf '\n=== concat Network Volume Cleanup Tests ===\n\n'

# Test 1: smbfs 上のファイルは物理削除される (trash に入らない)
printf '## Test 1: smbfs source files are physically removed (not trashed)\n'
TEST_DIR="$TEST_TMP/netvol_1"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/nclip_001.mp4"
echo "video 2" > "$TEST_DIR/nclip_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_MOUNT_OUTPUT="//user@host/share on ${TEST_DIR:A} (smbfs, nodev, nosuid)" \
  concat "$TEST_DIR/nclip_001.mp4" "$TEST_DIR/nclip_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "concat on smbfs path succeeds"
assert_file_not_exists "$TEST_DIR/nclip_001.mp4" "Source file 1 removed"
assert_file_not_exists "$TEST_DIR/nclip_002.mp4" "Source file 2 removed"
assert_contains "$output" "network volume [smbfs]" "Output explains physical delete on network volume"
if [[ -f "$TEST_TMP/_mock_trash/nclip_001.mp4" ]]; then
  printf '✗ smbfs file must not go through trash\n'
  return 1
fi
printf '✓ smbfs file did not go through trash\n'

# Test 2: ローカル FS (apfs) は従来どおり trash 経由
printf '\n## Test 2: local apfs source files still go to trash\n'
TEST_DIR="$TEST_TMP/netvol_2"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/lclip_001.mp4"
echo "video 2" > "$TEST_DIR/lclip_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_MOUNT_OUTPUT="/dev/disk1 on / (apfs, local)" \
  concat "$TEST_DIR/lclip_001.mp4" "$TEST_DIR/lclip_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "concat on local path succeeds"
assert_in_mock_trash "lclip_001.mp4" "Local file 1 landed in trash"
assert_in_mock_trash "lclip_002.mp4" "Local file 2 landed in trash"

printf '\n=== Network Volume Cleanup Tests Completed ===\n'
