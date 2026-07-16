# shellcheck shell=bash
# shellcheck disable=SC2154,SC2076,SC2207,SC2296
# ------------------------------------------------------------------------------
# concat — 複数の動画ファイルを無劣化で結合するzshコマンド
# ------------------------------------------------------------------------------
# 分割構成:
#   _concat_helpers.zsh — 定数, パス操作, 連番検出, グルーピング, ffprobeラッパー, 診断
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
  concat [オプション...] <ファイル1> <ファイル2> [<ファイル3> ...]
  concat [オプション...] <ディレクトリ>

  オプションは引数の先頭・途中・末尾、どこに置いても認識されます。
  名前が "--" で始まるファイルを渡したい場合は "--" を区切りに使います。

  例:
    # 連番ファイルを結合（元ファイルはデフォルトでゴミ箱へ移動される）
    concat video_001.mp4 video_002.mp4 video_003.mp4

    # 元ファイルを残す
    concat --keep video_001.mp4 video_002.mp4

    # 複数グループを自動検出して結合
    concat clip_01.mp4 clip_02.mp4 scene_1.mp4 scene_2.mp4

    # ディレクトリ内のグループを自動検出して結合（非再帰）
    concat /path/to/videos

    # 末尾にオプションを置いてもよい
    concat video1.mp4 video2.mp4 --force

    # コーデック不一致でも強制実行
    concat --force video1.mp4 video2.mp4

オプション:
  -h, --help: このヘルプメッセージを表示します。
  --force: コーデック不一致チェックと結合後のフレーム順序検証をスキップします
           （結果は保証されません）。安全側に倒すため元ファイルは削除しません。
  --verbose: 検査途中の詳細ログを表示します。
  --dryrun: 実際の結合を行わず、検証結果のみ表示します。
  --keep: 結合成功後も元ファイルを削除せず残します（デフォルトは削除）。
  --output-info <FILE>: 結合に成功した出力ファイルの絶対パスを <FILE> に
                        NUL 区切りで追記します（パイプライン連携用）。
                        失敗・既存スキップ・--dryrun では書き込みません。

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
  - デフォルト: 結合成功後、元ファイルは自動的にゴミ箱へ移動されます。
  - 既存の出力ファイルがあってスキップされた場合はゴミ箱へ移動しません。
  - --dryrun / --force 指定時は元ファイルを残します。
EOF
    return 0
  fi

  # オプションの処理（位置を問わず受け付ける。"--" 以降は全てファイルパスとして扱う）
  local force_mode=0
  local verbose_mode=0
  local dryrun_mode=0
  local keep_mode=0
  local output_info_file=""
  local -a _positional=()
  while (( $# > 0 )); do
    case "$1" in
      --force)   force_mode=1; shift ;;
      --verbose) verbose_mode=1; shift ;;
      --dryrun)  dryrun_mode=1; shift ;;
      --keep)    keep_mode=1; shift ;;
      --output-info)
        if (( $# < 2 )); then
          print -r -- "エラー: --output-info にはファイルパスが必要です" >&2
          return 1
        fi
        output_info_file="$2"; shift 2 ;;
      --) shift; _positional+=("$@"); break ;;
      --*)
        print -r -- "エラー: 不明なオプション: $1" >&2
        print -r -- "ヒント: 名前が '--' で始まるファイルを渡したい場合は '--' を区切りとして使ってください (concat ... -- --foo.mp4)" >&2
        return 1 ;;
      *) _positional+=("$1"); shift ;;
    esac
  done
  set -- "${_positional[@]}"

  # オプション再構成 (ディレクトリ/マルチグループモードがグループごとの再帰呼び出しに使う)
  local -a _group_opts=()
  (( force_mode )) && _group_opts+=(--force)
  (( verbose_mode )) && _group_opts+=(--verbose)
  (( dryrun_mode )) && _group_opts+=(--dryrun)
  (( keep_mode )) && _group_opts+=(--keep)
  [[ -n "$output_info_file" ]] && _group_opts+=(--output-info "$output_info_file")

  # ディレクトリモード: 引数が1つのディレクトリならグループを自動検出して結合
  if (( $# == 1 )) && [[ -d "$1" ]]; then
    local target_dir="${1:A}"

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

    # グルーピング + グループごとの結合 (実装は _concat_helpers.zsh)
    __concat_group_files "${video_files[@]}"
    __concat_run_groups ${#_group_opts[@]} "${_group_opts[@]}" "${video_files[@]}"
    return $?
  fi

  if (( $# < 2 )); then
    print -r -- "エラー: 最低2つのファイルが必要です" >&2
    return 1
  fi

  # マルチグループ検出: 複数ファイルが異なるグループに属するなら自動振り分け
  # (グルーピング実装は _concat_helpers.zsh の __concat_group_files / __concat_run_groups)
  if (( $# >= 3 )); then
    __concat_group_files "$@"
    if (( __CONCAT_GROUP_VIABLE >= 2 )); then
      __concat_run_groups ${#_group_opts[@]} "${_group_opts[@]}" "$@"
      return $?
    fi
    # 1グループならそのまま既存の単一グループロジックへフォールスルー
  fi

  local -a input_files=("$@")
  local -a sorted_files
  local file dir ext stem

  # 1. 入力ファイルのバリデーション
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

  # 連番パターンの検出と検証 (3 段リトライの状態機械は __concat_resolve_sequence に集約)
  if ! __concat_resolve_sequence "${stems[@]}"; then
    return 1
  fi
  local -a numbers=("${__CONCAT_R_NUMBERS[@]}")
  local common_prefix="$__CONCAT_R_COMMON_PREFIX"
  local first_suffix="$__CONCAT_R_FIRST_SUFFIX"
  local use_stripped_stems=$__CONCAT_R_USE_STRIPPED
  local detected_common_suffix="$__CONCAT_R_COMMON_SUFFIX"

  if (( ${#common_prefix} < 3 )); then
    print -r -- "エラー: ファイル名に連続性がありません: 共通プレフィックスが3文字未満です" >&2
    return 1
  fi

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
  local has_input_audio=0
  local first_video_info first_audio_info
  first_video_info=$(__concat_get_video_info "${input_files[1]}")
  first_audio_info=$(__concat_get_audio_info "${input_files[1]}")
  [[ -n "$first_audio_info" ]] && has_input_audio=1

  local video_info audio_info
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
    done

    # time_base 不一致は max timescale (= 高分解能側) に揃える方向で repair する。
    # 低→高 (例: 1/30000 → 1/90000) は PTS を整数倍するだけなので常に無損失。
    # 逆方向は PTS が divisor の倍数である必要があり、満たさない場合は丸めが起きる。
    # 旧実装は「先頭ファイル基準」で揃えていたため、順序によっては低分解能側に
    # 寄せようとして A/V 同期破綻を招く可能性があった。
    local -a tb_list=()
    local target_timescale=0
    local tb scale i
    for file in "${input_files[@]}"; do
      tb=$(__concat_get_video_time_base "$file")
      tb_list+=("$tb")
      scale="${tb#1/}"
      if [[ "$scale" =~ ^[0-9]+$ ]] && (( scale > target_timescale )); then
        target_timescale=$scale
      fi
    done

    local target_tb="1/${target_timescale}"
    local -a mismatched_files=()
    if (( target_timescale > 0 )); then
      for ((i=1; i<=${#input_files[@]}; i++)); do
        if [[ "${tb_list[$i]}" != "$target_tb" ]]; then
          mismatched_files+=("${input_files[$i]}")
        fi
      done
    fi

    if (( ${#mismatched_files[@]} > 0 )); then
      print -P -- "\n%F{red}%B❌ エラー: time_base不一致%b%f" >&2
      print -P -- "%F{red}無劣化結合すると再生が破損します%f\n" >&2
      for ((i=1; i<=${#input_files[@]}; i++)); do
        if [[ "${tb_list[$i]}" == "$target_tb" ]]; then
          print -P -- "  %F{cyan}${input_files[$i]:t}%f: %F{green}${tb_list[$i]}%f" >&2
        else
          print -P -- "  %F{cyan}${input_files[$i]:t}%f: %F{yellow}${tb_list[$i]}%f" >&2
        fi
      done
      print "" >&2
      print -P -- "%F{white}%B修復方法 (高分解能側 ${target_tb} に揃える):%b%f" >&2
      for file in "${mismatched_files[@]}"; do
        print -r -- "  repair-mp4-timebase ${target_timescale} \"${file}\"" >&2
      done
      print "" >&2
      return 1
    fi
  fi

  # 4. ファイル名の昇順でソート（スペース対応）
  sorted_files=("${(on)input_files[@]}")

  # 5. 出力ファイル名の決定
  local output_name
  # 末尾の `_#Word` / `-#Word` ("_#Ep" / "_#Sp" 等のシーン区切りマーカー) は
  # 出力名としては不要なので剥がす。`_Scene` / `_Vol` 等の `#` なし alpha
  # 接尾辞は意味のある語の可能性があるので残す (例: lecture_vol3_topic_review
  # → lecture_vol3_topic_review.mp4 を維持)。
  local clean_prefix="$common_prefix"
  if [[ "$clean_prefix" =~ '^(.*)[-_]#[a-zA-Z]+$' ]]; then
    clean_prefix="${match[1]}"
  fi
  # 末尾の _ や - を除去 (partN ケース、または上の strip 後の残り)
  clean_prefix="${clean_prefix%[-_]}"
  if (( ${#clean_prefix} >= 3 )); then
    output_name="${clean_prefix}.mp4"
  else
    output_name="output.mp4"
  fi
  local output_path="$first_dir/$output_name"

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
    # --force 指定時は検証自体をスキップ（コーデック互換と同じくユーザー判断に委ねる）
    if (( !dryrun_mode )) && (( !force_mode )); then
      (( verbose_mode )) && print -r -- ">> フレーム順序検証中..."
      start_time=$SECONDS
      if ! __concat_verify_frame_order "$output_path" "${input_files[@]}"; then
        print -r -- "❌ フレーム順序エラー: $REPLY" >&2
        rm -f -- "$output_path"
        return 1
      fi
      (( verbose_mode )) && print -r -- ">> フレーム順序検証完了 (${$(( SECONDS - start_time ))}秒)"
    elif (( force_mode )); then
      (( verbose_mode )) && print -r -- ">> フレーム順序検証スキップ (--force)"
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

  # 機械可読な連携用: 成功した出力パスを NUL 区切りで追記
  if [[ -n "$output_info_file" ]]; then
    printf '%s\0' "${output_path:A}" >> "$output_info_file"
  fi

  # 元ファイルをゴミ箱へ移動（デフォルト動作、--keep / --dryrun / --force 指定時はスキップ）
  # --force は検証をスキップして結果が保証されないため、安全側に倒して元ファイルを残す
  if (( ! keep_mode )) && (( ! dryrun_mode )) && (( ! force_mode )); then
    print -r -- ">> 元ファイルをゴミ箱へ移動中..."
    for file in "${sorted_files[@]}"; do
      if __concat_trash "$file"; then
        print -r -- "   ゴミ箱へ: ${file:t}"
      else
        print -r -- "   失敗: ${file:t}" >&2
      fi
    done
  fi

  return 0
}
