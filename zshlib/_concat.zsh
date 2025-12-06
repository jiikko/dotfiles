# shellcheck shell=bash
# shellcheck disable=SC2154,SC2076,SC2207,SC2296
# concat v1.0.0
# ------------------------------------------------------------------------------
# concat — 複数の動画ファイルを無劣化で結合するzshコマンド
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

# 内部補助: ファイルからベースネーム（拡張子なし）を取得
__concat_get_stem() {
  local file="$1"
  local base="${file:t}"
  if [[ "$base" == *.* ]]; then
    echo "${base%.*}"
  else
    echo "$base"
  fi
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
  # パターン: -N-<suffix> または _N_<suffix>
  elif [[ "$stem" =~ '^(.*)[-_]([0-9]+)([-_][^0-9]+)$' ]]; then
    prefix="${match[1]}"
    num="${match[2]}"
    suffix="${match[3]}"
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

  # 0または1から始まるか確認
  if (( min != 0 && min != 1 )); then
    REPLY="連番が0または1から始まっていません: 最小値=$min"
    return 1
  fi

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
  # バックスラッシュとシングルクォートをエスケープ
  path="${path//\\/\\\\}"
  path="${path//\'/\'\\\'\'}"
  echo "file '$path'"
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

concat() {
  if [[ "$1" == "-h" || "$1" == "--help" ]]; then
    cat <<'EOF'
concat — 複数の動画ファイルを無劣化で結合します。

機能:
  - FFmpegのconcat demuxerを使用して無劣化結合します。
  - 同一コーデック・フォーマットの動画を高速に連結できます。
  - ファイル名の連続性（連番パターン）をチェックします。
  - コーデック・解像度などの不一致を検出し、再エンコードが必要な場合はエラーにします。

使い方:
  concat <ファイル1> <ファイル2> [<ファイル3> ...]
  concat --force <ファイル1> <ファイル2> ...

  例:
    # 連番ファイルを結合
    concat video_001.mp4 video_002.mp4 video_003.mp4

    # コーデック不一致でも強制実行
    concat --force video1.mp4 video2.mp4

オプション:
  -h, --help: このヘルプメッセージを表示します。
  --force: コーデック不一致でも強制的に結合を実行します（結果は保証されません）。

入力:
  - 2つ以上の動画ファイルパス
  - 全ファイルが同一ディレクトリに存在すること

許可される拡張子:
  .mp4 .avi .mov .mkv .webm .flv .wmv .m4v .mpg .mpeg .3gp .ts .m2ts

出力:
  - 形式: .mp4（固定）
  - ファイル名: 入力ファイル名の共通プレフィックスから自動生成
    例: video_001.mp4, video_002.mp4 → video.mp4
EOF
    return 0
  fi

  # --force オプションの処理
  local force_mode=0
  if [[ "$1" == "--force" ]]; then
    force_mode=1
    shift
  fi

  # 引数チェック: 最低2ファイル必要
  if (( $# < 2 )); then
    print -r -- "エラー: 最低2つのファイルが必要です" >&2
    return 1
  fi

  local -a input_files=("$@")
  local -a sorted_files
  local file dir ext stem

  # 1. 入力ファイルのバリデーション
  # ファイル存在確認
  for file in "${input_files[@]}"; do
    if [[ ! -f "$file" ]]; then
      print -r -- "エラー: ファイルが見つかりません: $file" >&2
      return 1
    fi
  done

  # 1.5. クラウドストレージ対応: 並列プリフェッチ
  # ファイルアクセスでダウンロードをトリガー
  print -r -- ">> ファイルをプリフェッチ中..."
  local -a prefetch_pids=()
  # バックグラウンドジョブの通知を抑制
  setopt LOCAL_OPTIONS NO_NOTIFY NO_MONITOR
  for file in "${input_files[@]}"; do
    head -c 1 -- "$file" > /dev/null 2>&1 &
    prefetch_pids+=($!)
  done
  # 全プリフェッチ完了を待機
  for pid in "${prefetch_pids[@]}"; do
    wait "$pid" 2>/dev/null
  done
  print -r -- ">> プリフェッチ完了"

  print -r -- ">> ファイル検証中..."
  # 同一ディレクトリ確認（:A で絶対パスに変換、スペース対応）
  local first_dir="${input_files[1]:A:h}"

  for file in "${input_files[@]}"; do
    dir="${file:A:h}"
    if [[ "$dir" != "$first_dir" ]]; then
      print -r -- "エラー: 全ファイルが同一ディレクトリに存在する必要があります" >&2
      print -r -- "  $first_dir != $dir" >&2
      return 1
    fi
  done

  # 拡張子チェック
  for file in "${input_files[@]}"; do
    ext=$(__concat_get_ext "$file")
    if [[ -z "$ext" ]] || ! __concat_is_allowed_ext "$ext"; then
      print -r -- "エラー: 未対応の拡張子です: .$ext (file: ${file:t})" >&2
      return 1
    fi
  done

  print -r -- ">> 連続性チェック中..."
  # 2. ファイル名の連続性チェック
  local -a stems=()
  for file in "${input_files[@]}"; do
    stems+=("$(__concat_get_stem "$file")")
  done

  # 連番パターンの検出と検証
  local -a numbers=()
  local first_suffix="" first_prefix="" common_prefix=""
  for stem in "${stems[@]}"; do
    if __concat_extract_number "$stem"; then
      # REPLY は "番号:サフィックス:プレフィックス" 形式
      local num_part="${REPLY%%:*}"
      local rest="${REPLY#*:}"
      local suffix_part="${rest%%:*}"
      local prefix_part="${rest#*:}"

      # 先頭の0を除去して数値として扱う
      numbers+=("$((10#$num_part))")

      # サフィックスとプレフィックスの一致チェック
      if [[ -z "$first_prefix" ]]; then
        first_suffix="$suffix_part"
        first_prefix="$prefix_part"
      else
        if [[ "$suffix_part" != "$first_suffix" ]]; then
          print -r -- "エラー: サフィックスが異なります: '$first_suffix' と '$suffix_part'" >&2
          return 1
        fi
        if [[ "$prefix_part" != "$first_prefix" ]]; then
          print -r -- "エラー: ファイル名に連続性がありません: 共通プレフィックスがありません ('$first_prefix' と '$prefix_part')" >&2
          return 1
        fi
      fi
    else
      print -r -- "エラー: ファイル名に連番パターンがありません: $stem" >&2
      return 1
    fi
  done

  # 共通プレフィックスを設定
  common_prefix="$first_prefix"

  # 共通プレフィックスの長さチェック
  if (( ${#common_prefix} < 3 )); then
    print -r -- "エラー: ファイル名に連続性がありません: 共通プレフィックスが3文字未満です" >&2
    return 1
  fi

  # 連番の連続性検証
  if ! __concat_validate_sequence "${numbers[@]}"; then
    print -r -- "エラー: $REPLY" >&2
    return 1
  fi

  print -r -- ">> コーデック確認中..."
  # 3. 再エンコード回避チェック（--forceでスキップ）
  # 入力に音声があるかどうかを記録
  local has_input_audio=0
  local first_video_info first_audio_info
  first_video_info=$(__concat_get_video_info "${input_files[1]}")
  first_audio_info=$(__concat_get_audio_info "${input_files[1]}")
  [[ -n "$first_audio_info" ]] && has_input_audio=1

  if (( ! force_mode )); then
    for file in "${input_files[@]:1}"; do
      local video_info audio_info
      video_info=$(__concat_get_video_info "$file")
      audio_info=$(__concat_get_audio_info "$file")

      if [[ "$video_info" != "$first_video_info" ]]; then
        print -r -- "エラー: 再エンコードが必要です - 映像情報不一致:" >&2
        print -r -- "  ${input_files[1]:t}: $first_video_info" >&2
        print -r -- "  ${file:t}: $video_info" >&2
        return 1
      fi

      if [[ "$audio_info" != "$first_audio_info" ]]; then
        print -r -- "エラー: 再エンコードが必要です - 音声情報不一致:" >&2
        print -r -- "  ${input_files[1]:t}: $first_audio_info" >&2
        print -r -- "  ${file:t}: $audio_info" >&2
        return 1
      fi
    done
  fi

  # 4. ファイル名の昇順でソート（スペース対応）
  sorted_files=("${(o)input_files[@]}")

  # 5. 出力ファイル名の決定
  local output_name
  # 末尾の _ や - を除去
  local clean_prefix="${common_prefix%[-_]}"
  if (( ${#clean_prefix} >= 3 )); then
    output_name="${clean_prefix}.mp4"
  else
    output_name="output.mp4"
  fi
  local output_path="$first_dir/$output_name"

  # 入力ファイルと同名になる場合のチェック
  for file in "${sorted_files[@]}"; do
    if [[ "${file:A}" == "$output_path" ]]; then
      print -r -- "エラー: 出力ファイル名が入力ファイルと衝突します: $output_name" >&2
      return 1
    fi
  done

  # 6. 出力ファイルの存在確認
  if [[ -e "$output_path" ]]; then
    print -r -- "→ SKIP 既存: $output_path"
    return 0
  fi

  # 一時ファイル名の生成（UUID）
  local uuid
  uuid=$(uuidgen 2>/dev/null || cat /proc/sys/kernel/random/uuid 2>/dev/null || date +%s%N)
  local list_file="./${uuid}.txt"
  local tmp_output="./${uuid}.mp4"

  # 7. シグナルハンドラ設定
  trap '[[ -n "${list_file:-}" && -e "$list_file" ]] && rm -f -- "$list_file"; [[ -n "${tmp_output:-}" && -e "$tmp_output" ]] && rm -f -- "$tmp_output"' INT TERM HUP EXIT

  # 8. concatリストファイルの作成
  local total_duration=0 abs_path dur
  for file in "${sorted_files[@]}"; do
    abs_path="${file:A}"
    __concat_escape_path "$abs_path" >> "$list_file"

    # duration合計を計算
    dur=$(__concat_get_duration "$file")
    if [[ -n "$dur" ]]; then
      total_duration=$(awk -v t="$total_duration" -v d="$dur" 'BEGIN{ printf "%.3f", t+d }')
    fi
  done

  # durationを時:分:秒に変換
  local duration_hms
  duration_hms=$(awk -v s="$total_duration" 'BEGIN{
    h=int(s/3600); m=int((s%3600)/60); sec=s%60
    printf "%d:%02d:%05.2f", h, m, sec
  }')

  print -r -- ">> 結合対象: ${#sorted_files[@]}ファイル"
  print -r -- ">> 入力ファイル合計duration: ${duration_hms}"
  print -r -- ">> 出力: $output_path"
  print -r -- ">> 結合中..."
  local start_time=$SECONDS

  # 9. FFmpegで結合実行
  if ! ffmpeg -hide_banner -nostdin -loglevel error \
    -f concat -safe 0 -i "$list_file" \
    -fflags +genpts -avoid_negative_ts make_zero \
    -c copy -movflags +faststart \
    -y "$tmp_output"; then
    print -r -- "❌ FFmpegエラー: 結合に失敗しました" >&2
    return 1
  fi

  # 10. 一時ファイルを最終ファイル名にリネーム
  mv -f -- "$tmp_output" "$output_path"
  print -r -- ">> 結合完了 (${$(( SECONDS - start_time ))}秒)"

  print -r -- ">> 診断中..."
  start_time=$SECONDS
  # 11. 出力ファイルの診断
  if ! __concat_diagnose_output "$output_path" "$total_duration" "$has_input_audio"; then
    print -r -- "❌ 診断エラー: $REPLY" >&2
    rm -f -- "$output_path"
    return 1
  fi
  print -r -- ">> 診断完了 (${$(( SECONDS - start_time ))}秒)"

  # 12. クリーンアップ（trapでも実行されるが念のため）
  rm -f -- "$list_file"

  # trapを解除（正常終了）
  trap - INT TERM HUP EXIT

  print -r -- "✅ 完了: $output_path"
  return 0
}
