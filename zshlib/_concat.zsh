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
  # まず通常のstemで試し、プレフィックスが一致しない場合は共通サフィックスを除去して再試行
  local -a numbers=()
  local first_suffix="" first_prefix="" common_prefix=""
  local use_stripped_stems=0
  local detected_common_suffix=""

  # 最初のパス: 通常のstemで連番を検出
  local -a temp_numbers=()
  local -a temp_prefixes=()
  local -a temp_suffixes=()
  local all_matched=1
  for stem in "${stems[@]}"; do
    if __concat_extract_number "$stem"; then
      local num_part="${REPLY%%:*}"
      local rest="${REPLY#*:}"
      local suffix_part="${rest%%:*}"
      local prefix_part="${rest#*:}"
      temp_numbers+=("$((10#$num_part))")
      temp_prefixes+=("$prefix_part")
      temp_suffixes+=("$suffix_part")
    else
      all_matched=0
      break
    fi
  done

  # プレフィックスとサフィックスが一致するかチェック
  if (( all_matched )); then
    local prefixes_match=1
    local suffixes_match=1
    for p in "${temp_prefixes[@]:1}"; do
      if [[ "$p" != "${temp_prefixes[1]}" ]]; then
        prefixes_match=0
        break
      fi
    done
    for s in "${temp_suffixes[@]:1}"; do
      if [[ "$s" != "${temp_suffixes[1]}" ]]; then
        suffixes_match=0
        break
      fi
    done

    if (( prefixes_match && suffixes_match )); then
      # 通常のstemで成功
      numbers=("${temp_numbers[@]}")
      first_prefix="${temp_prefixes[1]}"
      first_suffix="${temp_suffixes[1]}"
    elif (( ! suffixes_match )); then
      # サフィックスが異なる → 末尾数字パターンで再試行
      local -a retry_numbers=()
      local -a retry_prefixes=()
      local retry_all_matched=1
      for stem in "${stems[@]}"; do
        if [[ "$stem" =~ '^(.*[^0-9])([0-9]+)$' ]]; then
          retry_prefixes+=("${match[1]}")
          retry_numbers+=("$((10#${match[2]}))")
        else
          retry_all_matched=0
          break
        fi
      done

      if (( retry_all_matched )); then
        local retry_prefixes_match=1
        for p in "${retry_prefixes[@]:1}"; do
          if [[ "$p" != "${retry_prefixes[1]}" ]]; then
            retry_prefixes_match=0
            break
          fi
        done
        if (( retry_prefixes_match )); then
          numbers=("${retry_numbers[@]}")
          first_prefix="${retry_prefixes[1]}"
          first_suffix=""
        else
          print -r -- "エラー: サフィックスが異なります: '${temp_suffixes[1]}' と 異なるサフィックスがあります" >&2
          return 1
        fi
      else
        print -r -- "エラー: サフィックスが異なります: '${temp_suffixes[1]}' と 異なるサフィックスがあります" >&2
        return 1
      fi
    else
      # プレフィックスが一致しない: 共通サフィックスを除去して再試行
      use_stripped_stems=1
    fi
  else
    # 連番パターンが見つからない: 共通サフィックスを除去して再試行
    use_stripped_stems=1
  fi

  # 共通サフィックス除去が必要な場合
  if (( use_stripped_stems )); then
    __concat_find_common_suffix "${stems[@]}"
    detected_common_suffix="$REPLY"

    if [[ -z "$detected_common_suffix" ]]; then
      # 共通サフィックスがない場合は元のエラーを出力
      for stem in "${stems[@]}"; do
        if ! __concat_extract_number "$stem"; then
          print -r -- "エラー: ファイル名に連番パターンがありません: $stem" >&2
          return 1
        fi
      done
      print -r -- "エラー: ファイル名に連続性がありません: 共通プレフィックスがありません" >&2
      return 1
    fi

    # 共通サフィックスを除去したstemsを作成
    local -a stripped_stems=()
    for stem in "${stems[@]}"; do
      stripped_stems+=("${stem%$detected_common_suffix}")
    done

    # 再試行
    numbers=()
    for stem in "${stripped_stems[@]}"; do
      if __concat_extract_number "$stem"; then
        local num_part="${REPLY%%:*}"
        local rest="${REPLY#*:}"
        local suffix_part="${rest%%:*}"
        local prefix_part="${rest#*:}"

        numbers+=("$((10#$num_part))")

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
  fi

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

  # ディレクトリ内の同一パターンファイル欠落チェック
  print -r -- ">> ディレクトリ内の関連ファイルチェック中..."
  setopt LOCAL_OPTIONS EXTENDED_GLOB
  local check_ext
  check_ext="${$(__concat_get_ext "${input_files[1]}"):l}"  # 小文字に正規化

  local -a input_abs=()
  for file in "${input_files[@]}"; do
    input_abs+=("${file:A}")
  done

  local -a missing_files=()
  for f in "$first_dir"/(#i)*."$check_ext"(N); do
    local f_abs="${f:A}"
    local is_input=0
    for inp in "${input_abs[@]}"; do
      if [[ "$f_abs" == "$inp" ]]; then
        is_input=1
        break
      fi
    done
    (( is_input )) && continue

    local f_stem
    f_stem=$(__concat_get_stem "$f")

    local f_check_stem="$f_stem"
    if (( use_stripped_stems )) && [[ -n "$detected_common_suffix" ]]; then
      [[ "$f_stem" != *"$detected_common_suffix" ]] && continue
      f_check_stem="${f_stem%$detected_common_suffix}"
    fi

    # common_prefixで始まるか確認（文字列比較）
    if [[ "${f_check_stem:0:${#common_prefix}}" != "$common_prefix" ]]; then
      continue
    fi

    local remaining="${f_check_stem:${#common_prefix}}"

    # first_suffixがあればそれで終わるか確認して除去
    if [[ -n "$first_suffix" ]]; then
      [[ "$remaining" != *"$first_suffix" ]] && continue
      remaining="${remaining%$first_suffix}"
    fi

    # 残りが [separator?][number] のパターンに一致するか
    if [[ "$remaining" =~ '^[-_]?[0-9]+$' ]] || \
       [[ "$remaining" =~ '^part[0-9]+$' ]] || \
       [[ "$remaining" =~ '^\([0-9]+\)$' ]]; then
      missing_files+=("${f:t}")
    fi
  done

  if (( ${#missing_files[@]} > 0 )); then
    print -r -- "エラー: 同じパターンのファイルが指定されていません:" >&2
    for mf in "${missing_files[@]}"; do
      print -r -- "  - $mf" >&2
    done
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

  print -r -- ">> 結合順序:"
  local idx=1
  for file in "${sorted_files[@]}"; do
    print -r -- "   ${idx}. ${file:t}"
    (( idx++ ))
  done
  print -r -- "✅ 完了: $output_path"
  return 0
}
