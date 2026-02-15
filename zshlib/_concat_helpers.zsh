# shellcheck shell=bash
# shellcheck disable=SC2154,SC2076,SC2207,SC2296
# ------------------------------------------------------------------------------
# concat helpers — concat コマンドの内部補助関数群
# ------------------------------------------------------------------------------

# 許可される拡張子のリスト
__concat_allowed_extensions=(mp4 avi mov mkv webm flv wmv m4v mpg mpeg 3gp ts m2ts)

# 内部補助: 拡張子が許可されているか確認
__concat_is_allowed_ext() {
  local ext="${1:l}"  # 小文字に変換
  local allowed
  for allowed in "${__concat_allowed_extensions[@]}"; do
    [[ "$ext" == "$allowed" ]] && return 0
  done
  return 1
}

# 内部補助: ファイルから拡張子を取得
__concat_get_ext() {
  local file="$1"
  local base="${file:t}"
  if [[ "$base" == *.* ]]; then
    echo "${base##*.}"
  else
    echo ""
  fi
}

# 内部補助: ファイルからベースネーム（拡張子なし）を取得（NFC正規化済み）
__concat_get_stem() {
  local file="$1"
  local base="${file:t}"
  local stem
  if [[ "$base" == *.* ]]; then
    stem="${base%.*}"
  else
    stem="$base"
  fi
  # macOSのファイルシステムはNFDを使う場合があるためNFCに正規化
  printf '%s' "$stem" | iconv -f UTF-8-MAC -t UTF-8 2>/dev/null || printf '%s' "$stem"
}

# 内部補助: 複数の文字列から共通サフィックスを見つける
# $1...: 文字列の配列
# 戻り値: REPLY に共通サフィックスを設定
__concat_find_common_suffix() {
  local -a strings=("$@")
  (( ${#strings[@]} == 0 )) && { REPLY=""; return 0; }

  local first="${strings[1]}"
  local suffix=""
  local i len char all_match

  # 末尾から1文字ずつ比較
  len=${#first}
  for (( i = 0; i < len; i++ )); do
    char="${first:(-1-i):1}"
    all_match=1
    for s in "${strings[@]:1}"; do
      if (( ${#s} <= i )) || [[ "${s:(-1-i):1}" != "$char" ]]; then
        all_match=0
        break
      fi
    done
    if (( all_match )); then
      suffix="${char}${suffix}"
    else
      break
    fi
  done

  REPLY="$suffix"
}

# 内部補助: 連番パターンを検出して番号、サフィックス、プレフィックスを抽出
# 戻り値: REPLY に "番号:サフィックス:プレフィックス" を設定
__concat_extract_number() {
  local stem="$1"
  local num="" suffix="" prefix=""

  # パターン: _NNN または -NNN (末尾)
  if [[ "$stem" =~ ^(.*)_([0-9]+)$ ]]; then
    prefix="${match[1]}"
    num="${match[2]}"
    suffix=""
  elif [[ "$stem" =~ ^(.*)-([0-9]+)$ ]]; then
    prefix="${match[1]}"
    num="${match[2]}"
    suffix=""
  # パターン: (N)
  elif [[ "$stem" =~ '^(.*)\(([0-9]+)\)$' ]]; then
    prefix="${match[1]}"
    num="${match[2]}"
    suffix=""
  # パターン: partN
  elif [[ "$stem" =~ ^(.*)part([0-9]+)$ ]]; then
    prefix="${match[1]}"
    num="${match[2]}"
    suffix=""
  # パターン: -N-<suffix> または _N_<suffix>（サフィックスに数字を含む場合も対応）
  elif [[ "$stem" =~ '^(.*)[-_]([0-9]+)([-_].+)$' ]]; then
    prefix="${match[1]}"
    num="${match[2]}"
    suffix="${match[3]}"
  # パターン: 末尾が数字（区切り文字なし、例: clip28）
  # 共通サフィックス除去後のフォールバックとして使用
  elif [[ "$stem" =~ '^(.*[^0-9])([0-9]+)$' ]]; then
    prefix="${match[1]}"
    num="${match[2]}"
    suffix=""
  fi

  REPLY="${num}:${suffix}:${prefix}"
  [[ -n "$num" ]]
}

# 内部補助: 連番の連続性を検証
__concat_validate_sequence() {
  local -a numbers=("$@")
  local -a sorted_nums
  local min max

  # 数値としてソート
  sorted_nums=($(printf '%s\n' "${numbers[@]}" | sort -n))

  min="${sorted_nums[1]}"
  max="${sorted_nums[-1]}"

  # 欠番チェック
  local -a missing=()
  for (( i = min; i <= max; i++ )); do
    local found=0
    for n in "${sorted_nums[@]}"; do
      if (( n == i )); then
        found=1
        break
      fi
    done
    if (( ! found )); then
      missing+=("$(printf '%03d' $i)")
    fi
  done

  if (( ${#missing[@]} > 0 )); then
    local missing_str="${(j:, :)missing}"
    REPLY="連番に欠番があります: $missing_str が見つかりません"
    return 1
  fi

  REPLY=""
  return 0
}

# 内部補助: ffprobeで映像情報を取得
__concat_get_video_info() {
  local file="$1"
  ffprobe -v error -select_streams v:0 \
    -show_entries stream=codec_name,width,height,r_frame_rate,pix_fmt \
    -of csv=p=0 -- "$file" 2>/dev/null | head -n1
}

# 内部補助: ffprobeで音声情報を取得
__concat_get_audio_info() {
  local file="$1"
  ffprobe -v error -select_streams a:0 \
    -show_entries stream=codec_name,sample_rate,channels \
    -of csv=p=0 -- "$file" 2>/dev/null | head -n1
}

# 内部補助: ffprobeでdurationを取得
__concat_get_duration() {
  local file="$1"
  ffprobe -v error -show_entries format=duration \
    -of default=nk=1:nw=1 -- "$file" 2>/dev/null | head -n1
}

# 内部補助: パスをエスケープしてconcat用に整形
__concat_escape_path() {
  local path="$1"
  # FFmpeg concat demuxerのエスケープ: シングルクォートで囲み、' は '\'' でエスケープ
  path="${path//\'/\'\\\'\'}"
  echo "file '${path}'"
}

# 内部補助: 出力ファイルの診断
# $1: 出力ファイルパス
# $2: 期待されるduration
# $3: 入力に音声があったかどうか (1=あり, 0=なし)
__concat_diagnose_output() {
  local outfile="$1"
  local expected_duration="$2"
  local has_input_audio="${3:-1}"

  # 1. メタデータ取得
  local info
  info=$(ffprobe -v error -show_entries format=duration,bit_rate:stream=codec_type,codec_name \
    -of json -- "$outfile" 2>/dev/null)

  if [[ -z "$info" ]]; then
    REPLY="メタデータの取得に失敗しました"
    return 1
  fi

  # 映像ストリームの存在確認（スペースの有無に対応）
  if ! echo "$info" | grep -q '"codec_type"[[:space:]]*:[[:space:]]*"video"'; then
    REPLY="映像ストリームが存在しません"
    return 1
  fi

  # 音声ストリームの存在確認（入力にあれば）
  if (( has_input_audio )); then
    if ! echo "$info" | grep -q '"codec_type"[[:space:]]*:[[:space:]]*"audio"'; then
      REPLY="音声ストリームが存在しません（入力には音声がありました）"
      return 1
    fi
  fi

  # duration > 0 のチェック
  local actual_duration
  actual_duration=$(__concat_get_duration "$outfile")
  if [[ -z "$actual_duration" ]] || (( $(echo "$actual_duration <= 0" | bc -l) )); then
    REPLY="durationが0以下または取得できません"
    return 1
  fi

  REPLY=""
  return 0
}
