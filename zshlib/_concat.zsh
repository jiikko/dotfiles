# shellcheck shell=bash
# shellcheck disable=SC2154,SC2076,SC2207,SC2296
# concat v1.0.0
# ------------------------------------------------------------------------------
# concat — 複数の動画ファイルを無劣化で結合するzshコマンド
# ------------------------------------------------------------------------------
# 分割構成:
#   _concat_helpers.zsh — 定数, パス操作, 連番検出, ffprobeラッパー, 診断
#   _concat.zsh (本ファイル) — concat() エントリポイント
# shellcheck disable=SC1091
source "${0:A:h}/_concat_helpers.zsh"

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
  concat <ディレクトリ>
  concat --force <ファイル1> <ファイル2> ...
  concat --verbose <ファイル1> <ファイル2> ...
  concat --keep <ファイル1> <ファイル2> ...

  例:
    # 連番ファイルを結合（元ファイルはデフォルトで削除される）
    concat video_001.mp4 video_002.mp4 video_003.mp4

    # 元ファイルを残す
    concat --keep video_001.mp4 video_002.mp4

    # 複数グループを自動検出して結合
    concat clip_01.mp4 clip_02.mp4 scene_1.mp4 scene_2.mp4

    # ディレクトリ内のグループを自動検出して結合（非再帰）
    concat /path/to/videos

    # コーデック不一致でも強制実行
    concat --force video1.mp4 video2.mp4

オプション:
  -h, --help: このヘルプメッセージを表示します。
  --force: コーデック不一致でも強制的に結合を実行します（結果は保証されません）。
  --verbose: 検査途中の詳細ログを表示します。
  --dryrun: 実際の結合を行わず、検証結果のみ表示します。
  --keep: 結合成功後も元ファイルを削除せず残します（デフォルトは削除）。

入力:
  - 2つ以上の動画ファイルパス
  - 全ファイルが同一ディレクトリに存在すること

許可される拡張子:
  .mp4 .avi .mov .mkv .webm .flv .wmv .m4v .mpg .mpeg .3gp .ts .m2ts

出力:
  - 形式: .mp4（固定）
  - ファイル名: 入力ファイル名の共通プレフィックスから自動生成
    例: video_001.mp4, video_002.mp4 → video.mp4

注意:
  - デフォルト: 結合成功後、元ファイルは自動的に削除されます。
  - 既存の出力ファイルがあってスキップされた場合は削除しません。
  - --dryrun 指定時は削除されません。
EOF
    return 0
  fi

  # オプションの処理
  local force_mode=0
  local verbose_mode=0
  local dryrun_mode=0
  local keep_mode=0
  while [[ "$1" == --* ]]; do
    case "$1" in
      --force)   force_mode=1; shift ;;
      --verbose) verbose_mode=1; shift ;;
      --dryrun)  dryrun_mode=1; shift ;;
      --keep)    keep_mode=1; shift ;;
      *) break ;;
    esac
  done

  # ディレクトリモード: 引数が1つのディレクトリならグループを自動検出して結合
  if (( $# == 1 )) && [[ -d "$1" ]]; then
    local target_dir="${1:A}"
    local -a _opts=()
    (( force_mode )) && _opts+=(--force)
    (( verbose_mode )) && _opts+=(--verbose)
    (( dryrun_mode )) && _opts+=(--dryrun)
    (( keep_mode )) && _opts+=(--keep)

    # ディレクトリ内の動画ファイルを収集（非再帰）
    local -a video_files=()
    local _f _ext
    for _f in "$target_dir"/*(.N); do
      _ext=$(__concat_get_ext "$_f")
      [[ -n "$_ext" ]] && __concat_is_allowed_ext "$_ext" && video_files+=("$_f")
    done

    if (( ${#video_files[@]} == 0 )); then
      print -r -- "エラー: ディレクトリ内に動画ファイルが見つかりません" >&2
      return 1
    fi

    # ファイルをグループ化（番号部分を除いた共通部分でグルーピング）
    local -A file_to_key=()
    local -a all_keys=() _skipped_files=()
    local _stem _num _rest _suffix _prefix _key
    for _f in "${video_files[@]}"; do
      _stem=$(__concat_get_stem "$_f")
      if __concat_extract_number "$_stem"; then
        _num="${REPLY%%:*}"
        _rest="${REPLY#*:}"
        _suffix="${_rest%%:*}"
        _prefix="${_rest#*:}"
        _key="${_prefix}::${_suffix}"
        file_to_key[${_f:A}]="$_key"
        all_keys+=("$_key")
      else
        _skipped_files+=("${_f:t}")
      fi
    done
    if (( ${#_skipped_files[@]} > 0 )); then
      print -r -- "⚠️  連番パターンに一致しないファイルをスキップしました: ${(j:, :)_skipped_files}" >&2
    fi

    local -a unique_keys=("${(u)all_keys[@]}")
    local _total=0 _ok=0 _fail=0

    local -a group_files sorted_group
    for _key in "${unique_keys[@]}"; do
      group_files=()
      for _f in "${video_files[@]}"; do
        [[ "${file_to_key[${_f:A}]-}" == "$_key" ]] && group_files+=("$_f")
      done
      (( ${#group_files[@]} < 2 )) && continue

      _total=$((_total + 1))
      sorted_group=("${(on)group_files[@]}")

      print -r -- ""
      print -r -- "=========================================="
      print -r -- "グループ ${_total}: ${sorted_group[1]:t} 他${#sorted_group[@]}ファイル"
      print -r -- "=========================================="

      # 単一グループ側で削除処理を行うため、ここでは削除しない
      if concat "${_opts[@]}" "${sorted_group[@]}"; then
        _ok=$((_ok + 1))
      else
        _fail=$((_fail + 1))
      fi
    done

    if (( _total == 0 )); then
      print -r -- "結合可能なグループが見つかりませんでした"
      return 0
    fi
    print -r -- ""
    print -r -- "=========================================="
    print -r -- "完了: ${_ok}/${_total} グループ成功${_fail:+, ${_fail}失敗}"
    print -r -- "=========================================="
    return $(( _fail > 0 ))
  fi

  # 引数チェック: 最低2ファイル必要
  if (( $# < 2 )); then
    print -r -- "エラー: 最低2つのファイルが必要です" >&2
    return 1
  fi

  # マルチグループ検出: 複数ファイルが異なるグループに属するなら自動振り分け
  if (( $# >= 3 )); then
    local -A _mg_file_to_key=()
    local -a _mg_all_keys=() _mg_skipped_files=()
    local _mgf _mg_stem _mg_num _mg_rest _mg_suffix _mg_prefix _mg_key
    for _mgf in "$@"; do
      _mg_stem=$(__concat_get_stem "$_mgf")
      if __concat_extract_number "$_mg_stem"; then
        _mg_num="${REPLY%%:*}"
        _mg_rest="${REPLY#*:}"
        _mg_suffix="${_mg_rest%%:*}"
        _mg_prefix="${_mg_rest#*:}"
        _mg_key="${_mg_prefix}::${_mg_suffix}"
        _mg_file_to_key[${_mgf:A}]="$_mg_key"
        _mg_all_keys+=("$_mg_key")
      else
        _mg_skipped_files+=("${_mgf:t}")
      fi
    done
    if (( ${#_mg_skipped_files[@]} > 0 )); then
      print -r -- "⚠️  連番パターンに一致しないファイルをスキップしました: ${(j:, :)_mg_skipped_files}" >&2
    fi

    local -a _mg_unique_keys=("${(u)_mg_all_keys[@]}")

    # 結合可能（2ファイル以上）なグループが2つ以上あるか判定
    local _mg_viable=0 _mg_count
    for _mg_key in "${_mg_unique_keys[@]}"; do
      _mg_count=0
      for _mgf in "$@"; do
        [[ "${_mg_file_to_key[${_mgf:A}]}" == "$_mg_key" ]] && _mg_count=$((_mg_count + 1))
      done
      (( _mg_count >= 2 )) && _mg_viable=$((_mg_viable + 1))
    done

    if (( _mg_viable >= 2 )); then
      local -a _mg_opts=()
      (( force_mode )) && _mg_opts+=(--force)
      (( verbose_mode )) && _mg_opts+=(--verbose)
      (( dryrun_mode )) && _mg_opts+=(--dryrun)
      (( keep_mode )) && _mg_opts+=(--keep)

      local _mg_total=0 _mg_ok=0 _mg_fail=0
      local -a _mg_group_files _mg_sorted_group
      for _mg_key in "${_mg_unique_keys[@]}"; do
        _mg_group_files=()
        for _mgf in "$@"; do
          [[ "${_mg_file_to_key[${_mgf:A}]}" == "$_mg_key" ]] && _mg_group_files+=("$_mgf")
        done
        (( ${#_mg_group_files[@]} < 2 )) && continue

        _mg_total=$((_mg_total + 1))
        _mg_sorted_group=("${(on)_mg_group_files[@]}")

        print -r -- ""
        print -r -- "=========================================="
        print -r -- "グループ ${_mg_total}: ${_mg_sorted_group[1]:t} 他${#_mg_sorted_group[@]}ファイル"
        print -r -- "=========================================="

        if concat "${_mg_opts[@]}" "${_mg_sorted_group[@]}"; then
          _mg_ok=$((_mg_ok + 1))
        else
          _mg_fail=$((_mg_fail + 1))
        fi
      done

      if (( _mg_total == 0 )); then
        print -r -- "結合可能なグループが見つかりませんでした"
        return 0
      fi
      print -r -- ""
      print -r -- "=========================================="
      print -r -- "完了: ${_mg_ok}/${_mg_total} グループ成功${_mg_fail:+, ${_mg_fail}失敗}"
      print -r -- "=========================================="
      return $(( _mg_fail > 0 ))
    fi
    # 1グループならそのまま既存の単一グループロジックへフォールスルー
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
  (( verbose_mode )) && print -r -- ">> ファイルをプリフェッチ中..."
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
  (( verbose_mode )) && print -r -- ">> プリフェッチ完了"

  (( verbose_mode )) && print -r -- ">> ファイル検証中..."
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

  (( verbose_mode )) && print -r -- ">> 連続性チェック中..."
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
  local num_part rest suffix_part prefix_part
  for stem in "${stems[@]}"; do
    if __concat_extract_number "$stem"; then
      num_part="${REPLY%%:*}"
      rest="${REPLY#*:}"
      suffix_part="${rest%%:*}"
      prefix_part="${rest#*:}"
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
        num_part="${REPLY%%:*}"
        rest="${REPLY#*:}"
        suffix_part="${rest%%:*}"
        prefix_part="${rest#*:}"

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
  (( verbose_mode )) && print -r -- ">> ディレクトリ内の関連ファイルチェック中..."
  setopt LOCAL_OPTIONS EXTENDED_GLOB
  local check_ext
  check_ext="${$(__concat_get_ext "${input_files[1]}"):l}"  # 小文字に正規化

  local -a input_abs=()
  for file in "${input_files[@]}"; do
    input_abs+=("${file:A}")
  done

  local -a missing_files=()
  local f_abs is_input f_stem f_check_stem remaining
  for f in "$first_dir"/(#i)*."$check_ext"(N); do
    f_abs="${f:A}"
    is_input=0
    for inp in "${input_abs[@]}"; do
      if [[ "$f_abs" == "$inp" ]]; then
        is_input=1
        break
      fi
    done
    (( is_input )) && continue

    f_stem=$(__concat_get_stem "$f")

    f_check_stem="$f_stem"
    if (( use_stripped_stems )) && [[ -n "$detected_common_suffix" ]]; then
      [[ "$f_stem" != *"$detected_common_suffix" ]] && continue
      f_check_stem="${f_stem%$detected_common_suffix}"
    fi

    # common_prefixで始まるか確認（文字列比較）
    if [[ "${f_check_stem:0:${#common_prefix}}" != "$common_prefix" ]]; then
      continue
    fi

    remaining="${f_check_stem:${#common_prefix}}"

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

  (( verbose_mode )) && print -r -- ">> コーデック確認中..."
  # 3. 再エンコード回避チェック（--forceでスキップ）
  # 入力に音声があるかどうかを記録
  local has_input_audio=0
  local first_video_info first_audio_info
  first_video_info=$(__concat_get_video_info "${input_files[1]}")
  first_audio_info=$(__concat_get_audio_info "${input_files[1]}")
  [[ -n "$first_audio_info" ]] && has_input_audio=1

  local video_info audio_info
  local first_time_base video_time_base
  first_time_base=$(__concat_get_video_time_base "${input_files[1]}")
  if (( ! force_mode )); then
    for file in "${input_files[@]:1}"; do
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

      video_time_base=$(__concat_get_video_time_base "$file")
      if [[ "$video_time_base" != "$first_time_base" ]]; then
        print -P -- "\n%F{red}%B❌ エラー: time_base不一致%b%f" >&2
        print -P -- "%F{red}無劣化結合すると再生が破損します%f\n" >&2
        print -P -- "  %F{cyan}${input_files[1]:t}%f: %F{green}$first_time_base%f" >&2
        print -P -- "  %F{cyan}${file:t}%f: %F{yellow}$video_time_base%f\n" >&2
        local _target_timescale="${first_time_base#1/}"
        print -P -- "%F{white}%B修復方法:%b%f" >&2
        print -r -- "  repair-mp4-timebase ${_target_timescale} \"${file}\"" >&2
        print "" >&2
        return 1
      fi
    done
  fi

  # 4. ファイル名の昇順でソート（スペース対応）
  sorted_files=("${(on)input_files[@]}")

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
  uuid=$(uuidgen 2>/dev/null || cat /proc/sys/kernel/random/uuid 2>/dev/null || echo "${$}_${RANDOM}")
  local list_file="$first_dir/.concat_${uuid}.txt"
  local tmp_output="$first_dir/.concat_${uuid}.mp4"

  # 7. duration・サイズの事前計算
  local total_duration=0 abs_path dur
  for file in "${sorted_files[@]}"; do
    dur=$(__concat_get_duration "$file")
    if [[ -n "$dur" ]]; then
      total_duration=$(awk -v t="$total_duration" -v d="$dur" 'BEGIN{ printf "%.3f", t+d }')
    fi
  done

  local duration_hms
  duration_hms=$(awk -v s="$total_duration" 'BEGIN{
    h=int(s/3600); m=int((s%3600)/60); sec=s%60
    printf "%d:%02d:%05.2f", h, m, sec
  }')

  local total_size=0 fsize
  for file in "${sorted_files[@]}"; do
    fsize=$(stat -f%z -- "$file" 2>/dev/null || stat -c%s -- "$file" 2>/dev/null)
    [[ -n "$fsize" ]] && total_size=$((total_size + fsize))
  done
  local total_size_mb=$((total_size / 1024 / 1024))

  print -r -- ">> 結合対象: ${#sorted_files[@]}ファイル (合計 ${total_size_mb}MB)"
  print -r -- ">> 入力ファイル合計duration: ${duration_hms}"
  print -r -- ">> 出力: $output_path"

  # dryrun: 結合順序を表示して終了（一時ファイルを作成しない）
  if (( dryrun_mode )); then
    print -r -- ">> 結合順序:"
    local idx=1
    for file in "${sorted_files[@]}"; do
      print -r -- "   ${idx}. ${file:t}"
      (( idx++ ))
    done
    print -r -- ">> dryrun: 結合をスキップしました"
    return 0
  fi

  # 8. 結合実行（alwaysブロックで一時ファイルを確実にクリーンアップ）
  # trap ではなく always を使うことで、再帰呼び出し時に外側の trap を破壊しない
  {
    # 9. concatリストファイルの作成
    for file in "${sorted_files[@]}"; do
      abs_path="${file:A}"
      if ! __concat_escape_path "$abs_path" >> "$list_file"; then
        print -r -- "エラー: パスのエスケープに失敗しました: $abs_path" >&2
        return 1
      fi
    done

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
    if ! mv -f -- "$tmp_output" "$output_path"; then
      print -r -- "❌ エラー: 出力ファイルのリネームに失敗しました" >&2
      return 1
    fi
    print -r -- ">> 結合完了 (${$(( SECONDS - start_time ))}秒)"

    (( verbose_mode )) && print -r -- ">> 診断中..."
    start_time=$SECONDS
    # 11. 出力ファイルの診断
    __concat_diagnose_output "$output_path" "$total_duration" "$has_input_audio" "$total_size" "${sorted_files[@]}"
    local _diag_rc=$?
    if (( _diag_rc == 2 )); then
      # 警告: 入力に未参照データあるが出力は正常
      print -r -- "⚠️  診断警告:" >&2
      print -r -- "$REPLY" >&2
    elif (( _diag_rc != 0 )); then
      print -r -- "❌ 診断エラー: $REPLY" >&2
      rm -f -- "$output_path"
      return 1
    fi
    (( verbose_mode )) && print -r -- ">> 診断完了 (${$(( SECONDS - start_time ))}秒)"

    # 12. フレーム順序の検証（入力ファイルを独自ソートして検証）
    if (( !dryrun_mode )); then
      (( verbose_mode )) && print -r -- ">> フレーム順序検証中..."
      start_time=$SECONDS
      if ! __concat_verify_frame_order "$output_path" "${input_files[@]}"; then
        print -r -- "❌ フレーム順序エラー: $REPLY" >&2
        rm -f -- "$output_path"
        return 1
      fi
      (( verbose_mode )) && print -r -- ">> フレーム順序検証完了 (${$(( SECONDS - start_time ))}秒)"
    fi
  } always {
    # 一時ファイルのクリーンアップ（成功・失敗・シグナル問わず実行）
    rm -f -- "$list_file"
    [[ -e "$tmp_output" ]] && rm -f -- "$tmp_output"
  }

  print -r -- ">> 結合順序:"
  local idx=1
  for file in "${sorted_files[@]}"; do
    print -r -- "   ${idx}. ${file:t}"
    (( idx++ ))
  done
  print -r -- "✅ 完了: $output_path"

  # 元ファイル削除（デフォルト動作、--keep / --dryrun 指定時はスキップ）
  if (( ! keep_mode )) && (( ! dryrun_mode )); then
    print -r -- ">> 元ファイルを削除中..."
    for file in "${sorted_files[@]}"; do
      if rm -f -- "$file"; then
        print -r -- "   削除: ${file:t}"
      fi
    done
  fi

  return 0
}
