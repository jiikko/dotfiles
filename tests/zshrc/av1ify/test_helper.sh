#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# av1ify テスト共通ヘルパー
# 各テストファイルの冒頭で source して使う

setopt err_exit no_unset pipe_fail

# zshでの現在のスクリプトパス取得（呼び出し元のパスを使う）
_HELPER_DIR="${0:A:h}"
ROOT_DIR="$(cd "$_HELPER_DIR/../../.." && pwd)"
TEST_TMP="$(mktemp -d)"

# 🚨 アサーションの失敗は exit code に出す (issue 327 / 329)。
# ランナー (Makefile の run_tests) は **rc しか見ない**ので、`✗` を printf するだけの分岐は
# **失敗しても [ok] に集計される**。失敗は bad() で報告してカウントし、下の cleanup が
# 終了時に非 0 へ格上げする (落ちた assert を全部出すため、最初の 1 件で止めない)。
# 書式と引数は printf と同じ。そこで打ち切りたい「前提の崩れ」だけ bad の後に exit 1 を書く。
typeset -gi FAIL_COUNT=0
bad() { printf "$@"; FAIL_COUNT=$(( FAIL_COUNT + 1 )); }

# 🚨 後始末と「失敗を rc へ格上げする」を 1 本の EXIT trap に束ねる (trap は 1 つしか持てない)。
# zsh の EXIT trap 内の `exit 1` は終了ステータスを上書きできる (実測 2026-09-08)。
# 本体が既に非 0 で終わっているときは `return $rc` でそれを保つ (格下げしない)。
cleanup() {
  local rc=$?
  rm -rf "$TEST_TMP"
  if (( FAIL_COUNT > 0 )); then
    printf '✗ 失敗 %d 件 (assert の失敗は exit code に出す。issue 327 / 329)\n' "$FAIL_COUNT" >&2
    exit 1
  fi
  return $rc
}
trap cleanup EXIT

# モックスクリプト用のディレクトリ
MOCK_BIN_DIR="$TEST_TMP/mock_bin"
mkdir -p "$MOCK_BIN_DIR"

# ffmpegモックスクリプトを作成（シンプル版）
cat > "$MOCK_BIN_DIR/ffmpeg" <<'EOF'
#!/usr/bin/env sh
# -h フラグ（encoder チェック等）は即 exit 0
for arg in "$@"; do
  case "$arg" in -h) exit 0 ;; esac
done

# argv キャプチャ: TEST_FFMPEG_ARGS_LOG が設定されていれば実際の引数列を 1 行で記録する。
# これが無いと「オプションを決定するロジック」しか検証できず、決定を ffmpeg へ渡す
# 配線が消えてもテストが緑のままになる (実際に -colorspace の配線を丸ごと削除しても
# 全テストが通る false green が発生していた)。encoder チェックの -h は上で除外済み。
if [ -n "${TEST_FFMPEG_ARGS_LOG-}" ]; then
  printf '%s\n' "$*" >> "$TEST_FFMPEG_ARGS_LOG"
fi

# 失敗注入: MOCK_FFMPEG_FAIL=1 でエンコード失敗を再現する（失敗経路の分岐検証用）
if [ -n "${MOCK_FFMPEG_FAIL-}" ]; then
  exit 1
fi

# 最後の引数を出力ファイルとして扱う
for arg in "$@"; do
  last_arg="$arg"
done

# -で始まらない最後の引数が出力ファイル
if [ -n "$last_arg" ] && [ "${last_arg#-}" = "$last_arg" ]; then
  if [ -n "$MOCK_FFMPEG_OUTPUT_SIZE" ]; then
    dd if=/dev/zero of="$last_arg" bs=1 count="$MOCK_FFMPEG_OUTPUT_SIZE" 2>/dev/null
  else
    echo "mock" > "$last_arg"
  fi
  exit 0
fi
exit 1
EOF
chmod +x "$MOCK_BIN_DIR/ffmpeg"

# ffprobeモックスクリプトを作成（ソース vs 出力ファイルを区別）
cat > "$MOCK_BIN_DIR/ffprobe" <<'EOF'
#!/usr/bin/env sh
# どんなクエリでも成功を返す（MOCK_WIDTH/MOCK_HEIGHT で上書き可能）
# -enc を含むファイルは出力ファイル扱い（MOCK_OUTPUT_* で上書き可能）
if echo "$*" | grep -q "codec_name"; then
  if echo "$*" | grep -q "select_streams v"; then
    last_arg=""
    for arg in "$@"; do last_arg="$arg"; done
    case "$last_arg" in
      *-enc*|*check_ng*) echo "${MOCK_OUTPUT_VCODEC-av1}" ;;
      *) echo "${MOCK_VCODEC-h264}" ;;
    esac
  else
    echo "${MOCK_ACODEC-aac}"
  fi
elif echo "$*" | grep -q "stream=index"; then
  # 音声ストリーム有無の判定用。「音声なし」を再現するには MOCK_AUDIO_INDEX= (空) を設定
  last_arg=""
  for arg in "$@"; do last_arg="$arg"; done
  case "$last_arg" in
    *-enc*|*check_ng*) echo "${MOCK_OUTPUT_AUDIO_INDEX-0}" ;;
    *) echo "${MOCK_AUDIO_INDEX-0}" ;;
  esac
elif echo "$*" | grep -q "sample_rate"; then
  echo "${MOCK_SAMPLE_RATE-48000}"
elif echo "$*" | grep -q "stream=channels"; then
  echo "${MOCK_CHANNELS-2}"
elif echo "$*" | grep -q "stream=bit_rate"; then
  # 既定は再エンコード閾値 (AV1_AAC_BITRATE 96k x AV1_AUDIO_REENCODE_MARGIN 1.15 = 110400bps)
  # を下回る値にして「音声は copy = 出力名に aac タグが付かない」を既定状態にする。
  # こうしないと音声と無関係なテスト (解像度タグ / 色空間 / avsync 等) の期待ファイル名が、
  # 音声ポリシーを変えるたびに巻き添えで壊れる。再エンコード側を検証したいテストは
  # MOCK_AUDIO_BITRATE を閾値超へ明示設定すること。
  echo "${MOCK_AUDIO_BITRATE-96000}"
elif echo "$*" | grep -q "nb_frames"; then
  last_arg=""
  for arg in "$@"; do last_arg="$arg"; done
  case "$last_arg" in
    *-enc*|*check_ng*) echo "${MOCK_OUTPUT_NB_FRAMES-${MOCK_NB_FRAMES-300}}" ;;
    *) echo "${MOCK_NB_FRAMES-300}" ;;
  esac
elif echo "$*" | grep -q "format=duration"; then
  # format duration: ソース vs 出力を区別（-enc を含むファイルは出力扱い）
  last_arg=""
  for arg in "$@"; do last_arg="$arg"; done
  case "$last_arg" in
    *-enc*|*check_ng*) echo "${MOCK_OUTPUT_FORMAT_DURATION-${MOCK_FORMAT_DURATION-10.0}}" ;;
    *) echo "${MOCK_FORMAT_DURATION-10.0}" ;;
  esac
elif echo "$*" | grep -q "select_streams v:0" && echo "$*" | grep -q "stream=duration"; then
  # ソース vs 出力で別の値を返せるよう -enc を含むファイルは出力扱い
  last_arg=""
  for arg in "$@"; do last_arg="$arg"; done
  case "$last_arg" in
    *-enc*|*check_ng*) echo "${MOCK_OUTPUT_VIDEO_DURATION-${MOCK_VIDEO_DURATION-10.0}}" ;;
    *) echo "${MOCK_VIDEO_DURATION-10.0}" ;;
  esac
elif echo "$*" | grep -q "select_streams a:0" && echo "$*" | grep -q "stream=duration"; then
  last_arg=""
  for arg in "$@"; do last_arg="$arg"; done
  case "$last_arg" in
    *-enc*|*check_ng*) echo "${MOCK_OUTPUT_AUDIO_DURATION-${MOCK_AUDIO_DURATION-10.0}}" ;;
    *) echo "${MOCK_AUDIO_DURATION-10.0}" ;;
  esac
elif echo "$*" | grep -q "stream=start_time"; then
  # __av1ify_start_time 用 (issue 058)。時間シフト型の音ズレ (終端は揃うが先頭がずれる)
  # は start_time にしか出ないため、この口が無いとそのクラスの回帰テストが書けない。
  # 未設定は本物のコンテナの慣行どおり 0.0
  last_arg=""
  for arg in "$@"; do last_arg="$arg"; done
  if echo "$*" | grep -q "select_streams v"; then
    case "$last_arg" in
      *-enc*|*check_ng*) echo "${MOCK_OUTPUT_VIDEO_START-${MOCK_VIDEO_START-0.0}}" ;;
      *) echo "${MOCK_VIDEO_START-0.0}" ;;
    esac
  else
    case "$last_arg" in
      *-enc*|*check_ng*) echo "${MOCK_OUTPUT_AUDIO_START-${MOCK_AUDIO_START-0.0}}" ;;
      *) echo "${MOCK_AUDIO_START-0.0}" ;;
    esac
  fi
elif echo "$*" | grep -q "packet=pts_time"; then
  # __av1ify_packet_end 用。
  # MOCK_PACKET_LINES を設定すると "pts,duration" の生の packet 列をそのまま返す
  # (B-frame reorder のようにデコード順と表示順が食い違うケースの検証用)。
  if [ -n "${MOCK_PACKET_LINES-}" ]; then
    printf '%b\n' "$MOCK_PACKET_LINES"
    exit 0
  fi
  # 既定は 1 行だけ返す簡易モック。本物の ffprobe が返す packet 列と違い
  # 「表示終端そのもの」を返す = MOCK_*_DURATION と同値になる。実装側は
  # max(pts+duration) を取るので、単一行モックでも表示終端の意味は一致する。
  last_arg=""
  for arg in "$@"; do last_arg="$arg"; done
  if echo "$*" | grep -q "select_streams v"; then
    case "$last_arg" in
      *-enc*|*check_ng*) echo "${MOCK_OUTPUT_VIDEO_LAST_PTS-${MOCK_OUTPUT_VIDEO_DURATION-${MOCK_VIDEO_LAST_PTS-${MOCK_VIDEO_DURATION-10.0}}}}" ;;
      *) echo "${MOCK_VIDEO_LAST_PTS-${MOCK_VIDEO_DURATION-10.0}}" ;;
    esac
  elif echo "$*" | grep -q "select_streams a"; then
    case "$last_arg" in
      *-enc*|*check_ng*) echo "${MOCK_OUTPUT_AUDIO_LAST_PTS-${MOCK_OUTPUT_AUDIO_DURATION-${MOCK_AUDIO_LAST_PTS-${MOCK_AUDIO_DURATION-10.0}}}}" ;;
      *) echo "${MOCK_AUDIO_LAST_PTS-${MOCK_AUDIO_DURATION-10.0}}" ;;
    esac
  fi
elif echo "$*" | grep -q "packet=dts"; then
  # DTS単調性チェック用 (__video_health_check チェック3): MOCK_DTS_BACKWARD=1 で逆行を注入
  if [ -n "${MOCK_DTS_BACKWARD-}" ]; then
    printf '0\n3750\n2000\n7500\n'   # 3750→2000 で逆行=破損
  else
    printf '0\n3750\n7500\n11250\n'  # 単調増加=健全
  fi
elif echo "$*" | grep -q "duration"; then
  echo "10.0"
elif echo "$*" | grep -q "avg_frame_rate"; then
  echo "${MOCK_AVG_FPS-${MOCK_FPS-30000/1001}}"
elif echo "$*" | grep -q "r_frame_rate"; then
  echo "${MOCK_FPS-30000/1001}"
elif echo "$*" | grep -q "stream_side_data=rotation"; then
  echo "${MOCK_ROTATION-}"
elif echo "$*" | grep -q "color_space"; then
  echo "${MOCK_COLOR_SPACE-}"
elif echo "$*" | grep -q "width"; then
  last_arg=""
  for arg in "$@"; do last_arg="$arg"; done
  case "$last_arg" in
    *-enc*|*check_ng*) echo "${MOCK_OUTPUT_WIDTH-${MOCK_WIDTH-1920}}" ;;
    *) echo "${MOCK_WIDTH-1920}" ;;
  esac
elif echo "$*" | grep -q "height"; then
  last_arg=""
  for arg in "$@"; do last_arg="$arg"; done
  case "$last_arg" in
    *-enc*|*check_ng*) echo "${MOCK_OUTPUT_HEIGHT-${MOCK_HEIGHT-1080}}" ;;
    *) echo "${MOCK_HEIGHT-1080}" ;;
  esac
elif echo "$*" | grep -q "format_name"; then
  echo "mp4"
fi
exit 0
EOF
chmod +x "$MOCK_BIN_DIR/ffprobe"

# trash モック: 受け取った引数を TEST_TRASH_LOG に追記し、対象ファイルを削除する
# (本物の /usr/bin/trash と違い、Finder の Trash には触らない)
cat > "$MOCK_BIN_DIR/trash" <<'EOF'
#!/usr/bin/env sh
log_file="${TEST_TRASH_LOG:-/dev/null}"
for arg in "$@"; do
  printf '%s\n' "$arg" >> "$log_file"
  [ -e "$arg" ] && rm -f -- "$arg"
done
exit 0
EOF
chmod +x "$MOCK_BIN_DIR/trash"

# mount モック: MOCK_MOUNT_OUTPUT があればそれを出力、無ければ実際の mount に委譲
# `mount` 出力の典型行: "device on /mountpoint (fstype, opts...)"
cat > "$MOCK_BIN_DIR/mount" <<'EOF'
#!/usr/bin/env sh
if [ -n "${MOCK_MOUNT_OUTPUT-}" ]; then
  printf '%s\n' "$MOCK_MOUNT_OUTPUT"
  exit 0
fi
exec /sbin/mount "$@"
EOF
chmod +x "$MOCK_BIN_DIR/mount"

# osascript モック: クリップボードのファイル参照 (file URL) 読み取りを決定論にする。
# 🚨 モックしないと __av1ify_clipboard_file_paths が実マシンのクリップボードを読み、
#    テスト結果がその時のコピー内容で変わる (Finder でファイルをコピーした直後だけ落ちる)。
# MOCK_PASTEBOARD_FILES に改行区切りのパスを入れるとそれを返す。未設定なら空 = 取れなかった
# 扱いになり、テキスト (pbpaste モック) 経路へフォールバックする。
cat > "$MOCK_BIN_DIR/osascript" <<'EOF'
#!/usr/bin/env sh
if [ -n "${OSASCRIPT_LOG-}" ]; then echo called >> "$OSASCRIPT_LOG"; fi
printf '%s\n' "${MOCK_PASTEBOARD_FILES-}"
# 失敗しても途中まで出力するのが実物の挙動 (JXA の例外は途中で投げられる)。
# 「rc を見ずに stdout だけ使う」実装を検出できるよう、出力してから落ちる。
if [ -n "${MOCK_OSASCRIPT_FAIL-}" ]; then exit 1; fi
exit 0
EOF
chmod +x "$MOCK_BIN_DIR/osascript"

# PATH設定
export PATH="$MOCK_BIN_DIR:$PATH"

# av1ifyをロード
source "$ROOT_DIR/zshlib/_av1ify.zsh"

# validate-mp4 (issue 005 Phase B) の mock。
#
# 🚨 **source の後で上書きする**こと。__validate_mp4_check は外部バイナリでは
# なく sourced な zsh 関数なので、MOCK_BIN_DIR の PATH mock では差し替わらない。
# _av1ify.zsh が _validate_mp4.zsh を source した後にここで再定義する。
#
# MOCK_VALIDATE_NG に理由文字列を入れると NG を注入する (例: decode-error)。
# 未設定なら常に OK。
#
# 🚨 呼ばれた回数は**ファイル**に記録する。テストは `output=$(av1ify ...)` の
# 形で呼ぶので av1ify はサブシェルで走り、シェル変数への加算は親に返らない
# (実測 2026-09-08: 変数で数えたら常に 0 になり「呼ばれていない」と誤判定した)。
# 「skip されたこと」は回数 0 でしか確かめられない — 出力ファイルの有無では
# skip と OK を区別できないため。
MOCK_VALIDATE_LOG="$TEST_TMP/validate_calls.log"
: > "$MOCK_VALIDATE_LOG"
# 🚨 **関数として install する形にしてある**。`_av1ify.zsh` を再 source する
# テスト (test_av1ify_clipboard.sh の Test 10 が本番のゲートを復元するために
# やっている) は `_validate_mp4.zsh` も一緒に読み直すので、その時点で
# **この mock が本物に戻る**。再 source した側は install を呼び直すこと。
# (実測 2026-09-08: 呼び直しを忘れて Test 22 が「元ファイルが消えない」で落ちた)
mock_validate_install() {
  __validate_mp4_check() {
    print -r -- "$1" >> "$MOCK_VALIDATE_LOG"
    if [[ -n "${MOCK_VALIDATE_NG-}" ]]; then
      REPLY="$MOCK_VALIDATE_NG"
      return 1
    fi
    REPLY=""
    return 0
  }
}
mock_validate_install

# validate の呼び出し回数を数える (ファイル経由なのでサブシェルを跨げる)
mock_validate_calls() {
  # 🚨 `grep -c` は 0 件のとき **exit 1** を返すので `|| print 0` を付けると
  # "0\n0" の 2 行になり、(( )) が「bad math expression」で落ちる
  # (実測 2026-09-08)。件数は wc で数える。
  [[ -f "$MOCK_VALIDATE_LOG" ]] || { print -r -- 0; return; }
  local n
  n=$(wc -l < "$MOCK_VALIDATE_LOG")
  print -r -- "${n// /}"
}

mock_validate_reset() {
  : > "$MOCK_VALIDATE_LOG"
}

assert_file_exists() {
  local file="$1"
  local message="$2"
  if [[ -f "$file" ]]; then
    printf '✓ %s\n' "$message"
    return 0
  else
    printf '✗ %s (file not found: %s)\n' "$message" "$file"
    return 1
  fi
}

assert_file_not_exists() {
  local file="$1"
  local message="$2"
  if [[ ! -f "$file" ]]; then
    printf '✓ %s\n' "$message"
    return 0
  else
    printf '✗ %s (file exists: %s)\n' "$message" "$file"
    return 1
  fi
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  local message="$3"
  if [[ "$haystack" == *"$needle"* ]]; then
    printf '✓ %s\n' "$message"
    return 0
  else
    printf '✗ %s (expected to contain: %s)\n' "$message" "$needle"
    return 1
  fi
}

assert_not_contains() {
  local haystack="$1"
  local needle="$2"
  local message="$3"
  if [[ "$haystack" != *"$needle"* ]]; then
    printf '✓ %s\n' "$message"
    return 0
  else
    printf '✗ %s (expected NOT to contain: %s)\n' "$message" "$needle"
    return 1
  fi
}
