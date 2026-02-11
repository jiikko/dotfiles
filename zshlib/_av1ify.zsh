# shellcheck shell=bash
# ------------------------------------------------------------------------------
# av1ify — 入力された動画ファイル、またはディレクトリ内の動画ファイルをAV1形式のMP4に一括変換します。
# ------------------------------------------------------------------------------

__AV1IFY_VERSION="1.6.0"
__AV1IFY_SPEC_VERSION="1.6.0"

# 内部補助: バナー出力
__av1ify_banner() {
  print -ru2 -- "av1ify v${__AV1IFY_VERSION} (spec: v${__AV1IFY_SPEC_VERSION})"
}

typeset -gi __AV1IFY_ABORT_REQUESTED=0
typeset -g  __AV1IFY_CURRENT_TMP=""
typeset -gi __AV1IFY_DRY_RUN=0
typeset -g  __AV1IFY_RESOLUTION=""
typeset -g  __AV1IFY_FPS=""
typeset -g  __AV1IFY_DENOISE=""
typeset -gi __AV1IFY_COMPACT=0

__av1ify_on_interrupt() {
  if (( __AV1IFY_ABORT_REQUESTED )); then
    return
  fi
  __AV1IFY_ABORT_REQUESTED=1
  local tmp="${__AV1IFY_CURRENT_TMP:-}"
  if [[ -n "$tmp" && -e "$tmp" ]]; then
    rm -f -- "$tmp"
    print -r -- "✋ 中断要求: 進行中の一時ファイルを削除しました ($tmp)"
  else
    print -r -- "✋ 中断要求: 残りの処理を停止します"
  fi
}

# 内部補助: 変換後の検査で NG の場合にファイル名へ注記を付加
__av1ify_mark_issue() {
  local fpath="$1" note="$2"
  local dir="${fpath:h}"
  local base="${fpath:t}"
  local stem ext new_name dest

  if [[ "$base" == *.* && "$base" != .* ]]; then
    stem="${base%.*}"
    ext="${base##*.}"
    new_name="${stem}-${note}.${ext}"
  else
    new_name="${base}-${note}"
  fi

  if [[ "$dir" == "." ]]; then
    dest="$new_name"
  else
    dest="$dir/$new_name"
  fi

  if mv -f -- "$fpath" "$dest"; then
    REPLY="$dest"
    return 0
  fi

  REPLY="$fpath"
  return 1
}

# 内部補助: 出力ファイルの簡易チェック（音声有無と音ズレ）
__av1ify_postcheck() {
  local filepath="$1"
  local src_path="${2:-}"
  local fps_changed="${3:-0}"
  local expected_height="${4:-}"
  local -a issues suffixes

  local audio_stream
  audio_stream=$(ffprobe -v error -select_streams a -show_entries stream=index -of csv=p=0 -- "$filepath" 2>/dev/null | head -n1)
  if [[ -z "$audio_stream" ]]; then
    issues+=("音声ストリーム検出できず")
    suffixes+=("noaudio")
  fi

  local v_dur_raw a_dur_raw diff
  v_dur_raw=$(ffprobe -v error -select_streams v:0 -show_entries stream=duration -of default=nk=1:nw=1 -- "$filepath" 2>/dev/null | head -n1)
  a_dur_raw=$(ffprobe -v error -select_streams a:0 -show_entries stream=duration -of default=nk=1:nw=1 -- "$filepath" 2>/dev/null | head -n1)
  if [[ -n "$v_dur_raw" && -n "$a_dur_raw" ]]; then
    diff=$(awk -v a="$a_dur_raw" -v v="$v_dur_raw" 'BEGIN{ if (a=="" || v=="") exit 1; d=a-v; if (d<0) d=-d; printf "%.6f", d }' 2>/dev/null) || diff=""
    if [[ -n "$diff" ]]; then
      local threshold="${AV1IFY_SYNC_TOLERANCE:-0.5}"
      local -F diff_f threshold_f
      diff_f=$diff
      threshold_f=$threshold
      if (( diff_f > threshold_f )); then
        issues+=("音ズレ疑い (Δ=${diff}s)")
        suffixes+=("avsync")
      fi
    fi
  fi

  # ソースとの再生時間比較
  if [[ -n "$src_path" ]]; then
    local src_fmt_dur out_fmt_dur dur_diff
    src_fmt_dur=$(ffprobe -v error -show_entries format=duration -of default=nk=1:nw=1 -- "$src_path" 2>/dev/null | head -n1)
    out_fmt_dur=$(ffprobe -v error -show_entries format=duration -of default=nk=1:nw=1 -- "$filepath" 2>/dev/null | head -n1)
    if [[ -n "$src_fmt_dur" && -n "$out_fmt_dur" ]]; then
      dur_diff=$(awk -v s="$src_fmt_dur" -v o="$out_fmt_dur" 'BEGIN{ if (s=="" || o=="") exit 1; d=s-o; if (d<0) d=-d; printf "%.3f", d }' 2>/dev/null) || dur_diff=""
      if [[ -n "$dur_diff" ]]; then
        local dur_threshold="${AV1IFY_DURATION_TOLERANCE:-2.0}"
        local -F dur_diff_f dur_threshold_f
        dur_diff_f=$dur_diff
        dur_threshold_f=$dur_threshold
        if (( dur_diff_f > dur_threshold_f )); then
          issues+=("再生時間ズレ (src=${src_fmt_dur}s, out=${out_fmt_dur}s, Δ=${dur_diff}s)")
          suffixes+=("duration")
        fi
      fi
    fi
  fi

  # フレーム数比較（fps変更なしの場合のみ）
  if [[ -n "$src_path" ]] && (( ! fps_changed )); then
    local src_frames out_frames
    src_frames=$(ffprobe -v error -select_streams v:0 -show_entries stream=nb_frames -of default=nk=1:nw=1 -- "$src_path" 2>/dev/null | head -n1)
    out_frames=$(ffprobe -v error -select_streams v:0 -show_entries stream=nb_frames -of default=nk=1:nw=1 -- "$filepath" 2>/dev/null | head -n1)
    if [[ -n "$src_frames" && "$src_frames" =~ ^[0-9]+$ && -n "$out_frames" && "$out_frames" =~ ^[0-9]+$ ]]; then
      if (( src_frames != out_frames )); then
        issues+=("フレーム数不一致 (src=${src_frames}, out=${out_frames})")
        suffixes+=("frames")
      fi
    fi
  fi

  # 出力解像度の検証
  if [[ -n "$expected_height" ]]; then
    local out_w out_h out_short
    out_w=$(ffprobe -v error -select_streams v:0 -show_entries stream=width -of default=nk=1:nw=1 -- "$filepath" 2>/dev/null | head -n1)
    out_h=$(ffprobe -v error -select_streams v:0 -show_entries stream=height -of default=nk=1:nw=1 -- "$filepath" 2>/dev/null | head -n1)
    if [[ -n "$out_w" && "$out_w" =~ ^[0-9]+$ && -n "$out_h" && "$out_h" =~ ^[0-9]+$ ]]; then
      if (( out_h > out_w )); then
        out_short=$out_w
      else
        out_short=$out_h
      fi
      if (( out_short != expected_height )); then
        issues+=("解像度不一致 (期待=${expected_height}p, 実際=${out_short}p, ${out_w}x${out_h})")
        suffixes+=("resolution")
      fi
    fi
  fi

  # ファイルサイズの妥当性チェック
  if [[ -n "$src_path" && -f "$src_path" && -f "$filepath" ]]; then
    local src_size out_size
    src_size=$(stat -f%z -- "$src_path" 2>/dev/null) || src_size=""
    out_size=$(stat -f%z -- "$filepath" 2>/dev/null) || out_size=""
    if [[ -n "$src_size" && "$src_size" =~ ^[0-9]+$ && -n "$out_size" && "$out_size" =~ ^[0-9]+$ ]] && (( src_size > 0 )); then
      local size_ratio
      size_ratio=$(awk -v o="$out_size" -v s="$src_size" 'BEGIN{ printf "%.4f", o / s }')
      local min_ratio="${AV1IFY_MIN_SIZE_RATIO:-0.001}"
      local too_small
      too_small=$(awk -v r="$size_ratio" -v m="$min_ratio" 'BEGIN{ print (r < m) ? 1 : 0 }')
      if (( too_small )); then
        issues+=("ファイルサイズ異常 (src=${src_size}B, out=${out_size}B, ratio=${size_ratio})")
        suffixes+=("tinyfile")
      fi
    fi
  fi

  REPLY="$filepath"
    if (( ${#issues[@]} )); then
      local note="check_ng"
      if (( ${#suffixes[@]} )); then
        local suffix_joined
        local IFS='-'
        suffix_joined="${suffixes[*]}"
        note+="-$suffix_joined"
      fi
      local new_path="$filepath"
      if __av1ify_mark_issue "$filepath" "$note"; then
        new_path="$REPLY"
      fi
      local issues_joined
      issues_joined=$(printf '%s, ' "${issues[@]}")
      issues_joined="${issues_joined%, }"
      print -r -- "⚠️ チェック警告: $issues_joined"
      REPLY="$new_path"
      return 1
    fi

  return 0
}

# 内部補助: 事前リペア（コンテナ/インデックス修復のためのストリームコピー）
# 入力: $1=元ファイルパス
# 出力: REPLY=リペア後パス（成功時は <stem>-repaired.<ext>、失敗/スキップ時は元パス）
__av1ify_pre_repair() {
  local src="$1"
  local stem ext repaired tmp
  if [[ "$src" == *.* && "$src" != .* ]]; then
    stem="${src%.*}"; ext="${src##*.}"
  else
    stem="$src"; ext=""
  fi
  repaired="${stem}-repaired${ext:+.${ext}}"
  tmp="${repaired}.in_progress"

  # 既存の repaired があれば再利用
  if [[ -e "$repaired" ]]; then
    print -r -- "→ 事前リペア済みを使用: $repaired"
    REPLY="$repaired"; return 0
  fi
  [[ -e "$tmp" ]] && { print -r -- "⚠️ 残骸削除: $tmp"; rm -f -- "$tmp"; }

  # 判定: packed B-frames 展開が必要な mpeg4（Xvid/DivX）かどうか
  local vcodec fmt
  vcodec=$(ffprobe -v error -select_streams v:0 -show_entries stream=codec_name -of default=nk=1:nw=1 -- "$src" 2>/dev/null)
  fmt=$(ffprobe -v error -show_entries format=format_name -of default=nk=1:nw=1 -- "$src" 2>/dev/null)

  local -a args=( -hide_banner -loglevel warning -y -fflags +genpts -i "$src" -map 0 -c copy )
  if [[ "${vcodec:l}" == "mpeg4" ]]; then
    args+=( -bsf:v mpeg4_unpack_bframes )
  fi

  print -r -- ">> 事前リペア: stream copy (${fmt:-unknown}/${vcodec:-?}) → $repaired"
  if ffmpeg "${args[@]}" -- "$tmp"; then
    mv -f -- "$tmp" "$repaired"
    REPLY="$repaired"; return 0
  else
    [[ -e "$tmp" ]] && rm -f -- "$tmp"
    print -r -- "⚠️ 事前リペア失敗: $src（元ファイルで続行）"
    REPLY="$src"; return 0
  fi
}

# 内部: 単一ファイル処理
__av1ify_one() {
  local in="$1"

  if (( __AV1IFY_ABORT_REQUESTED )); then
    print -r -- "✋ 中断済みのためスキップ: $in"
    return 130
  fi

  if [[ "$in" == *enc.mp4 || "$in" == *encoded.* ]]; then
    print -r -- "→ SKIP 既に出力ファイル形式です: $in"
    return 0
  fi

  # ベース出力名（copyや無音時）
  local stem="${in%.*}"
  local out="${stem}-enc.mp4"
  local tmp="${out}.in_progress"

  local dry_run="${__AV1IFY_DRY_RUN:-0}"

  # 解像度・fps オプションを取得
  local target_resolution="${__AV1IFY_RESOLUTION:-${AV1_RESOLUTION:-}}"
  local target_fps="${__AV1IFY_FPS:-${AV1_FPS:-}}"

  # 解像度・fpsのバリデーション（dry-run時も実行）
  local validated_resolution="" validated_fps=""
  if [[ -n "$target_resolution" ]]; then
    case "${target_resolution:l}" in
      480p|720p|1080p|1440p|4k)
        validated_resolution="$target_resolution"
        ;;
      *)
        if [[ "$target_resolution" =~ ^[0-9]+$ ]]; then
          if (( target_resolution >= 16 && target_resolution <= 8640 )); then
            validated_resolution="$target_resolution"
          else
            print -r -- "⚠️ 無効な解像度指定: $target_resolution（16-8640の範囲で指定してください）"
          fi
        else
          print -r -- "⚠️ 無効な解像度指定: $target_resolution（無視します）"
        fi
        ;;
    esac
  fi
  if [[ -n "$target_fps" ]]; then
    if [[ "$target_fps" =~ ^[0-9]+(\.[0-9]+)?$ ]]; then
      local fps_valid
      fps_valid=$(awk -v fps="$target_fps" 'BEGIN { print (fps > 0 && fps <= 240) ? 1 : 0 }')
      if (( fps_valid )); then
        validated_fps="$target_fps"
      else
        print -r -- "⚠️ 無効なfps指定: $target_fps（0より大きく240以下で指定してください）"
      fi
    else
      print -r -- "⚠️ 無効なfps指定: $target_fps（無視します）"
    fi
  fi

  # ノイズ除去オプションのバリデーション
  local target_denoise="${__AV1IFY_DENOISE:-${AV1_DENOISE:-}}"
  local validated_denoise=""
  if [[ -n "$target_denoise" ]]; then
    case "${target_denoise:l}" in
      light|medium|strong)
        validated_denoise="${target_denoise:l}"
        ;;
      *)
        print -r -- "⚠️ 無効なdenoise指定: $target_denoise（light/medium/strong から選択してください）"
        ;;
    esac
  fi

  # ドライラン: ファイル名ベースで計画だけ表示（ファイルへ一切アクセスしない）
  if (( dry_run )); then
    local crf_plan="${AV1_CRF:-auto}"
    local preset_plan="${AV1_PRESET:-5}"
    local res_plan="${validated_resolution:-auto}"
    local fps_plan="${validated_fps:-auto}"
    local denoise_plan="${validated_denoise:-off}"
    print -r -- "[DRY-RUN] 変換予定: $in"
    print -r -- "[DRY-RUN] 出力候補: $out (音声/解像度は実行時判定: ファイル未参照)"
    print -r -- "[DRY-RUN] 映像: libsvtav1 (crf=${crf_plan}, preset=${preset_plan}, resolution=${res_plan}, fps=${fps_plan}, denoise=${denoise_plan})"
    if (( __AV1IFY_COMPACT )); then
      print -r -- "[DRY-RUN] 音声: compact (96kbps超はaac 96kへ再エンコード)"
    else
      print -r -- "[DRY-RUN] 音声: 実行時に判定"
    fi
    return 0
  fi

  [[ ! -f "$in" ]] && { print -r -- "✗ ファイルが無い: $in"; return 1; }

  # 古い in_progress が残っていたら掃除（ドライラン時は触らない）
  if [[ -e "$tmp" ]]; then
    if (( dry_run )); then
      print -r -- "[DRY-RUN] 残骸検出: $tmp（変更なし）"
    else
      print -r -- "⚠️ 残骸削除: $tmp"
      rm -f -- "$tmp"
    fi
  fi

  # 映像エンコーダ（SVT-AV1 必須）
  local vcodec="libsvtav1"
  if ! ffmpeg -hide_banner -h encoder=libsvtav1 >/dev/null 2>&1; then
    print -r -- "❌ libsvtav1 が利用できません。ffmpeg を libsvtav1 付きでビルドしてください。"
    return 1
  fi

  # クラウド/ネットワークストレージの場合、ここで実ファイル取得が始まることがある
  # ffprobe でメタデータ取得することでファイルのダウンロードがトリガーされる
  print -r -- ">> ファイル取得中: $in"

  # ソース映像の寸法を取得（アップスケール防止・CRF自動調整・縦横判定に使用）
  local source_width="" source_height="" source_short_side="" source_is_portrait=0
  source_width=$(ffprobe -v error -select_streams v:0 -show_entries stream=width \
           -of default=nk=1:nw=1 -- "$in" 2>/dev/null | head -n1)
  source_height=$(ffprobe -v error -select_streams v:0 -show_entries stream=height \
           -of default=nk=1:nw=1 -- "$in" 2>/dev/null | head -n1)
  if [[ -n "$source_width" && "$source_width" =~ ^[0-9]+$ && -n "$source_height" && "$source_height" =~ ^[0-9]+$ ]]; then
    if (( source_height > source_width )); then
      source_is_portrait=1
      source_short_side=$source_width
    else
      source_short_side=$source_height
    fi
  fi

  # 解像度オプションの解析（縦解像度を数値に変換）
  # バリデーション済みの値を使用
  local target_height="" resolution_tag=""
  if [[ -n "$validated_resolution" ]]; then
    case "${validated_resolution:l}" in
      480p)  target_height=480;  resolution_tag="480p" ;;
      720p)  target_height=720;  resolution_tag="720p" ;;
      1080p) target_height=1080; resolution_tag="1080p" ;;
      1440p) target_height=1440; resolution_tag="1440p" ;;
      4k)    target_height=2160; resolution_tag="4k" ;;
      *)
        target_height="$validated_resolution"
        resolution_tag="${validated_resolution}p"
        ;;
    esac
    print -r -- ">> 出力解像度: ${resolution_tag} (height=${target_height})"
  fi

  # アップスケール防止: 解像度オプション指定時にソース解像度が必須
  if [[ -n "$target_height" ]]; then
    if [[ -z "$source_short_side" ]]; then
      print -r -- "❌ 解像度変更が指定されていますが、ソース映像の解像度を取得できませんでした: $in"
      return 1
    fi
    if (( source_short_side <= target_height )); then
      print -r -- ">> 元の短辺 (${source_short_side}px) が指定解像度 (${resolution_tag}) 以下のため、解像度変更をスキップします"
      target_height=""
      resolution_tag=""
    fi
  fi

  # fps オプションの解析（バリデーション済みの値を使用）
  # ソースfpsがtarget以下なら変更しない（キャップ動作）
  local fps_tag=""
  if [[ -n "$validated_fps" ]]; then
    local source_fps_raw source_fps_val=""
    source_fps_raw=$(ffprobe -v error -select_streams v:0 -show_entries stream=r_frame_rate \
             -of default=nk=1:nw=1 -- "$in" 2>/dev/null | head -n1)
    if [[ -n "$source_fps_raw" ]]; then
      # r_frame_rate は "30000/1001" のような分数形式
      source_fps_val=$(awk -v fps="$source_fps_raw" 'BEGIN {
        n = split(fps, a, "/")
        if (n == 2 && a[2]+0 > 0) printf "%.3f", a[1] / a[2]
        else printf "%.3f", a[1]+0
      }')
    fi
    if [[ -n "$source_fps_val" ]]; then
      local fps_skip
      fps_skip=$(awk -v src="$source_fps_val" -v tgt="$validated_fps" 'BEGIN { print (src <= tgt) ? 1 : 0 }')
      if (( fps_skip )); then
        print -r -- ">> ソースfps (${source_fps_val}) が ${validated_fps}fps 以下のため、fps変更をスキップ"
        target_fps=""
      else
        target_fps="$validated_fps"
        fps_tag="${validated_fps}fps"
        print -r -- ">> 出力フレームレート: ${source_fps_val}fps → ${validated_fps}fps"
      fi
    else
      target_fps="$validated_fps"
      fps_tag="${validated_fps}fps"
      print -r -- ">> 出力フレームレート: ${validated_fps}fps (ソースfps取得失敗)"
    fi
  else
    target_fps=""
  fi

  # 解像度を取得して CRF を自動調整（環境変数が優先）
  # 出力解像度が指定されている場合はそれを基準にする
  local crf preset
  if [[ -n "${AV1_CRF:-}" ]]; then
    crf="$AV1_CRF"
  else
    # CRF判定に使う解像度（出力解像度優先、なければソース短辺、最終手段でffprobe height）
    local height_for_crf
    if [[ -n "$target_height" ]]; then
      height_for_crf="$target_height"
    elif [[ -n "$source_short_side" ]]; then
      height_for_crf="$source_short_side"
    else
      height_for_crf=$(ffprobe -v error -select_streams v:0 -show_entries stream=height \
               -of default=nk=1:nw=1 -- "$in" 2>/dev/null)
    fi

    if [[ -n "$height_for_crf" && "$height_for_crf" =~ ^[0-9]+$ ]]; then
      # 解像度に応じて CRF を設定
      if (( height_for_crf <= 480 )); then
        crf=40  # SD:
      elif (( height_for_crf <= 720 )); then
        crf=40  # HD 720p
      elif (( height_for_crf <= 1080 )); then
        crf=45  # Full HD 1080p
      elif (( height_for_crf <= 1440 )); then
        crf=50  # 2K
      else
        crf=54  # 4K以上
      fi
      print -r -- ">> 解像度: ${height_for_crf}p → CRF=$crf を自動設定"
    else
      crf=40  # デフォルト
      print -r -- "⚠️ 解像度取得失敗 → CRF=$crf（デフォルト）"
    fi
  fi
  preset="${AV1_PRESET:-5}"

  # 音声コーデック事前判定（a:0 が無ければ空）
  local acodec
  acodec=$(ffprobe -v error -select_streams a:0 -show_entries stream=codec_name \
           -of default=nk=1:nw=1 -- "$in" 2>/dev/null | head -n1)

  # copy 許可コーデック（MP4 と相性の良いもの）
  local allow="${AV1_COPY_OK:-aac,alac,mp3}"
  local -a allow_list; IFS=',' read -rA allow_list <<< "$allow"

  # acodec が許可リストに含まれるか（大文字小文字無視）
  local use_copy=0
  if [[ -n "$acodec" ]]; then
    local c
    for c in "${allow_list[@]}"; do
      [[ "${acodec:l}" == "${c:l}" ]] && { use_copy=1; break; }
    done
  fi

  # ビデオフィルタの構築（縦長動画は短辺=width にスケーリング）
  local -a vf_parts=()
  local denoise_tag=""
  # ノイズ除去フィルタ（hqdn3d）を最初に適用
  if [[ -n "$validated_denoise" ]]; then
    case "$validated_denoise" in
      light)
        vf_parts+=("hqdn3d=2:2:3:3")
        denoise_tag="dn1"
        print -r -- ">> ノイズ除去: light (hqdn3d=2:2:3:3)"
        ;;
      medium)
        vf_parts+=("hqdn3d=4:4:6:6")
        denoise_tag="dn2"
        print -r -- ">> ノイズ除去: medium (hqdn3d=4:4:6:6)"
        ;;
      strong)
        vf_parts+=("hqdn3d=6:6:9:9")
        denoise_tag="dn3"
        print -r -- ">> ノイズ除去: strong (hqdn3d=6:6:9:9)"
        ;;
    esac
  fi
  if [[ -n "$target_height" ]]; then
    if (( source_is_portrait )); then
      vf_parts+=("scale=${target_height}:-2")
    else
      vf_parts+=("scale=-2:${target_height}")
    fi
  fi
  local vf_option=""
  if (( ${#vf_parts[@]} > 0 )); then
    vf_option=$(IFS=','; echo "${vf_parts[*]}")
  fi

  # ffmpeg 共通引数
  local -a args_common args_audio
  args_common=(
    -hide_banner -nostdin -stats -y
    -i "$in"
    -map "0:v:0"
    -c:v "$vcodec" -crf "$crf" -preset "$preset" -pix_fmt yuv420p
  )
  # ビデオフィルタ追加
  if [[ -n "$vf_option" ]]; then
    args_common+=(-vf "$vf_option")
  fi
  # fps 追加
  if [[ -n "$target_fps" ]]; then
    args_common+=(-r "$target_fps")
  fi
  args_common+=(
    -movflags +faststart -tag:v av01
    -f mp4
  )

  # 音声指定（命名用フラグ・ビットレート保持）
  local did_aac=0
  local aac_bitrate_resolved=""

  if [[ -z "$acodec" ]]; then
    args_audio=(-an)
    print -r -- ">> 音声: なし（-an）"
  elif (( use_copy )); then
    # compact モード: 音声ビットレートが96kbps超ならAAC 96kに再エンコード
    if (( __AV1IFY_COMPACT )); then
      local src_abitrate
      src_abitrate=$(ffprobe -v error -select_streams a:0 -show_entries stream=bit_rate \
                     -of default=nk=1:nw=1 -- "$in" 2>/dev/null | head -n1)
      if [[ -n "$src_abitrate" && "$src_abitrate" =~ ^[0-9]+$ ]] && (( src_abitrate > 96000 )); then
        aac_bitrate_resolved="96k"
        args_audio=(-map "0:a:0?" -c:a aac -b:a "$aac_bitrate_resolved" -ac 2 -ar 48000)
        did_aac=1
        print -r -- ">> 音声: aac 96k へ再エンコード (compact, 元=$acodec ${src_abitrate}bps)"
      else
        args_audio=(-map "0:a:0?" -c:a copy)
        print -r -- ">> 音声: copy (codec=$acodec, compact だが96kbps以下)"
      fi
    else
      args_audio=(-map "0:a:0?" -c:a copy)
      print -r -- ">> 音声: copy (codec=$acodec)"
    fi
  else
    aac_bitrate_resolved="${AV1_AAC_BITRATE:-96k}"
    args_audio=(-map "0:a:0?" -c:a aac -b:a "$aac_bitrate_resolved" -ac 2 -ar 48000)
    did_aac=1
    print -r -- ">> 音声: aac へ再エンコード (元=$acodec)"
  fi

  # 予定される最終出力ファイル
  # 命名規則: <stem>[-解像度][-fps][-denoise][-aac{br}]-enc.mp4
  local final_out="$out"
  local name_suffix=""
  if [[ -n "$resolution_tag" ]]; then
    name_suffix+="-${resolution_tag}"
  fi
  if [[ -n "$fps_tag" ]]; then
    name_suffix+="-${fps_tag}"
  fi
  if [[ -n "$denoise_tag" ]]; then
    name_suffix+="-${denoise_tag}"
  fi
  if (( did_aac )); then
    local br="${aac_bitrate_resolved:l}" tag
    if [[ "$br" == *k ]]; then
      tag="$br"
    elif [[ "$br" =~ ^[0-9]+$ ]]; then
      local kb; (( kb = (br + 500) / 1000 ))
      tag="${kb}k"
    else
      tag="$br"
    fi
    name_suffix+="-aac${tag}"
  fi
  if [[ -n "$name_suffix" ]]; then
    final_out="${stem}${name_suffix}-enc.mp4"
  fi

  # 既存チェック（過去の出力があればスキップ）
  if [[ -e "$final_out" || -e "$out" ]]; then
    local exist="$final_out"
    [[ -e "$out" ]] && exist="$out"
    print -r -- "→ SKIP 既存: $exist"
    return 0
  fi

  print -r -- ">> 映像: $vcodec (crf=$crf, preset=$preset)"
  print -r -- ">> 出力(処理中マーカー): $tmp"
  __AV1IFY_CURRENT_TMP="$tmp"

  # 1回目: 設定通りに実行
  if ffmpeg "${args_common[@]}" "${args_audio[@]}" -- "$tmp"; then
    __AV1IFY_CURRENT_TMP=""
    mv -f -- "$tmp" "$final_out"
    if __av1ify_postcheck "$final_out" "$in" "$( [[ -n "$target_fps" ]] && echo 1 || echo 0 )" "$target_height"; then
      final_out="$REPLY"; print -r -- "✅ 完了: $final_out"; return 0
    else
      final_out="$REPLY"; print -r -- "⚠️ 完了 (要確認): $final_out"; return 1
    fi
  else
    local ffmpeg_status=$?
    [[ -e "$tmp" ]] && rm -f -- "$tmp"
    if (( __AV1IFY_ABORT_REQUESTED || ffmpeg_status == 130 )); then
      __AV1IFY_CURRENT_TMP=""
      print -r -- "✋ 中断: $in"
      return 130
    fi

    # 失敗時: copy 選択だった場合は AAC で再試行（命名もAACタグへ）
    if (( use_copy )); then
      print -r -- "⚠️ 音声copy失敗 → AAC再エンコードで再試行"
      aac_bitrate_resolved="${AV1_AAC_BITRATE:-96k}"
      args_audio=(-map "0:a:0?" -c:a aac -b:a "$aac_bitrate_resolved" -ac 2 -ar 48000)
      did_aac=1
      # 再計算: 最終出力名（解像度/fpsタグを維持）
      local br="${aac_bitrate_resolved:l}" tag
      if [[ "$br" == *k ]]; then
        tag="$br"
      elif [[ "$br" =~ ^[0-9]+$ ]]; then
        local kb; (( kb = (br + 500) / 1000 ))
        tag="${kb}k"
      else
        tag="$br"
      fi
      name_suffix=""
      if [[ -n "$resolution_tag" ]]; then
        name_suffix+="-${resolution_tag}"
      fi
      if [[ -n "$fps_tag" ]]; then
        name_suffix+="-${fps_tag}"
      fi
      if [[ -n "$denoise_tag" ]]; then
        name_suffix+="-${denoise_tag}"
      fi
      name_suffix+="-aac${tag}"
      final_out="${stem}${name_suffix}-enc.mp4"

      __AV1IFY_CURRENT_TMP="$tmp"
      if ffmpeg "${args_common[@]}" "${args_audio[@]}" -- "$tmp"; then
        __AV1IFY_CURRENT_TMP=""
        mv -f -- "$tmp" "$final_out"
        if __av1ify_postcheck "$final_out" "$in" "$( [[ -n "$target_fps" ]] && echo 1 || echo 0 )" "$target_height"; then
          final_out="$REPLY"; print -r -- "✅ 完了: $final_out"; return 0
        else
          final_out="$REPLY"; print -r -- "⚠️ 完了 (要確認): $final_out"; return 1
        fi
      else
        local retry_status=$?
        [[ -e "$tmp" ]] && rm -f -- "$tmp"
        if (( __AV1IFY_ABORT_REQUESTED || retry_status == 130 )); then
          __AV1IFY_CURRENT_TMP=""
          print -r -- "✋ 中断: $in"
          return 130
        fi
      fi
    fi
  fi

  __AV1IFY_CURRENT_TMP=""
  print -r -- "❌ 失敗: $in"
  return 1
}
av1ify() {
  local __av1ify_internal=0
  if [[ -n ${__AV1IFY_INTERNAL_CALL:-} ]]; then
    __av1ify_internal=1
    unset __AV1IFY_INTERNAL_CALL
  fi

  setopt LOCAL_OPTIONS localtraps

  # ルート呼び出しでは毎回デフォルト（内部呼び出しのみ伝搬）
  local dry_run=0
  local show_help=0
  local opt_resolution=""
  local opt_fps=""
  local opt_denoise=""
  local opt_compact=0
  local -a positional=()
  while (( $# > 0 )); do
    case "$1" in
      --dry-run|-n)
        dry_run=1
        ;;
      -h|--help)
        (( ! __av1ify_internal )) && show_help=1
        ;;
      -c|--compact)
        opt_compact=1
        ;;
      -r|--resolution)
        shift
        if (( $# == 0 )); then
          print -r -- "エラー: --resolution には値が必要です" >&2
          return 1
        fi
        opt_resolution="$1"
        ;;
      --fps)
        shift
        if (( $# == 0 )); then
          print -r -- "エラー: --fps には値が必要です" >&2
          return 1
        fi
        opt_fps="$1"
        ;;
      --denoise)
        shift
        if (( $# == 0 )); then
          print -r -- "エラー: --denoise には値が必要です" >&2
          return 1
        fi
        opt_denoise="$1"
        ;;
      -f)
        positional+=("$1")
        ;;
      -*)
        print -r -- "エラー: 不明なオプション: $1" >&2
        return 1
        ;;
      *)
        positional+=("$1")
        ;;
    esac
    shift
  done
  set -- "${positional[@]}"

  # --compact: 720p + 30fps プリセット（明示的な -r/--fps が優先）
  if (( opt_compact )); then
    [[ -z "$opt_resolution" ]] && opt_resolution="720p"
    [[ -z "$opt_fps" ]] && opt_fps="30"
  fi

  if (( ! __av1ify_internal )); then
    __AV1IFY_DRY_RUN=$dry_run
    __AV1IFY_RESOLUTION="$opt_resolution"
    __AV1IFY_FPS="$opt_fps"
    __AV1IFY_DENOISE="$opt_denoise"
    __AV1IFY_COMPACT=$opt_compact
  else
    dry_run="${__AV1IFY_DRY_RUN:-$dry_run}"
  fi

  # バナー出力（内部呼び出し・ヘルプ時は除く）
  if (( ! __av1ify_internal )) && (( ! show_help )) && (( $# > 0 )); then
    __av1ify_banner
    if (( opt_compact )); then
      print -r -- ">> compact モード: -r ${opt_resolution} --fps ${opt_fps}"
    fi
  fi

  (( ! __av1ify_internal && dry_run )) && print -r -- "[DRY-RUN] ファイルは変更しません"

  if (( ! __av1ify_internal )) && { (( show_help )) || (( $# == 0 )); }; then
    cat <<'EOF'
av1ify — 入力された動画ファイル、またはディレクトリ内の動画ファイルをAV1形式のMP4に一括変換します。

機能:
  - 指定されたファイルまたはディレクトリを対象に処理を実行します。
  - 出力ファイル名は `<元のファイル名>-enc.mp4` となります。
  - 既に変換済みのファイルが存在する場合は、処理をスキップします。
  - 処理中には `<ファイル名>.in_progress` という一時ファイルを作成し、変換成功後にリネームします。
  - 変換後に音声ストリームと音ズレを簡易チェックし、問題が見つかればファイル名末尾に注意書きを付けます。
  - ffprobeを使用して入力ファイルの音声コーデックを判別し、可能であれば音声を無劣化でコピーします。
    (デフォルトでAAC, ALAC, MP3に対応)
    対応していない形式の場合は、AAC (96kbps, 48kHz, 2ch) に再エンコードします。
  - ディレクトリを指定した場合、再帰的に動画ファイル (avi, mkv, rm, wmv, mpg) を検索して変換します。
    (ファイル名の大文字・小文字は区別しません)

使い方:
  av1ify [オプション] <ファイルパス または ディレクトリパス> [<ファイルパス2> ...]
  av1ify -f <ファイルリスト>

  例:
    # 単一のファイルを変換
    av1ify "/path/to/movie.avi"

    # 複数のファイルを順番に変換
    av1ify xxx.mp4 yyy.mp4 zzz.mp4

    # ファイルリストから変換（改行区切り）
    av1ify -f list.txt

    # ディレクトリ内のすべての動画ファイルを変換
    av1ify "/path/to/dir"

    # CRF値を指定して画質を調整
    AV1_CRF=35 av1ify "/path/to/movie.mp4"

    # 720pに解像度を変更して変換（アスペクト比は維持）
    av1ify -r 720p "/path/to/movie.mp4"

    # 24fpsに変更して変換
    av1ify --fps 24 "/path/to/movie.mp4"

    # 解像度とfpsを両方指定
    av1ify -r 1080p --fps 30 "/path/to/movie.mp4"

    # ノイズ除去で圧縮率を上げる（ノイジーな素材に効果的）
    av1ify --denoise medium "/path/to/movie.mp4"

    # 720p + ノイズ除去の組み合わせ
    av1ify -r 720p --denoise light "/path/to/movie.mp4"

    # 保存用プリセット（720p + 30fps）
    av1ify --compact "/path/to/movie.mp4"

    # --compact + 解像度だけ上書き（480p + 30fps）
    av1ify --compact -r 480p "/path/to/movie.mp4"

オプション:
  -h, --help: このヘルプメッセージを表示します。
  -n, --dry-run: 実行内容のみを表示し、ファイルを変更しません。
  -f <ファイル>: 改行区切りでファイルパスが記載されたリストファイルを読み込んで処理します。
  -r, --resolution <値>: 出力解像度（縦）を指定します。アスペクト比は維持されます。
      480p / 720p / 1080p / 1440p / 4k または数値（例: 540）
  --fps <値>: 出力フレームレートを指定します（例: 24, 30, 60）。
  -c, --compact: 保存用プリセット（720p + 30fps）。-r や --fps で個別に上書き可能。
  --denoise <レベル>: ノイズ除去を適用します。圧縮率が向上しますが、ディテールが失われます。
      light: 軽度（hqdn3d=2:2:3:3）
      medium: 中程度（hqdn3d=4:4:6:6）
      strong: 強め（hqdn3d=6:6:9:9）

依存関係:
  - ffmpeg: 動画のエンコードとデコードに使用します。
  - ffprobe: (ffmpegに含まれます) メディアファイルの情報を取得するために使用します。

環境変数による設定:
  以下の環境変数を設定することで、エンコードの挙動を調整できます。

  AV1_CRF (デフォルト: 40)
    品質を制御します (Constant Rate Factor)。値が低いほど高画質・高ビットレートになります。
    SVT-AV1の推奨範囲は 20 (高画質) から 50 (低画質) です。

  AV1_PRESET (デフォルト: 5)
    エンコード速度と圧縮率のバランスを調整します。値が小さいほど高品質（高圧縮）になりますが、
    エンコードに時間がかかります。SVT-AV1では 0 (最高品質) から 12 (最速) の範囲で設定します。

  AV1_COPY_OK (デフォルト: "aac,alac,mp3")
    MP4コンテナで音声を無劣化コピーすることを許可する音声コーデックをカンマ区切りで指定します。

  AV1_RESOLUTION (デフォルト: なし)
    出力解像度を指定します。--resolution オプションと同等です。
    CLIオプションが優先されます。

  AV1_FPS (デフォルト: なし)
    出力フレームレートを指定します。--fps オプションと同等です。
    CLIオプションが優先されます。

  AV1_DENOISE (デフォルト: なし)
    ノイズ除去レベルを指定します。--denoise オプションと同等です。
    light / medium / strong から選択。CLIオプションが優先されます。
EOF
    return 0
  fi

  local __av1ify_is_root=0
  if (( ! __av1ify_internal )); then
    __av1ify_is_root=1
    __AV1IFY_ABORT_REQUESTED=0
    __AV1IFY_CURRENT_TMP=""
    trap '__av1ify_on_interrupt' INT TERM HUP
  fi

  set -o pipefail

  # -f オプションでファイルリストを読み込む
  if [[ "$1" == "-f" ]]; then
    if (( $# < 2 )); then
      print -r -- "エラー: -f オプションにはファイルパスが必要です" >&2
      return 1
    fi
    local listfile="$2"
    if [[ -z "$listfile" ]]; then
      print -r -- "エラー: -f オプションにはファイルパスが必要です" >&2
      return 1
    fi
    if [[ ! -f "$listfile" ]]; then
      print -r -- "エラー: ファイルが見つかりません: $listfile" >&2
      return 1
    fi

    local -a files=()
    while IFS= read -r line; do
      # 空行とコメント行（#で始まる）をスキップ
      [[ -z "$line" || "$line" == \#* ]] && continue
      files+=("$line")
    done < "$listfile"

    if (( ${#files[@]} == 0 )); then
      print -r -- "（対象ファイルなし: $listfile）"
      return 0
    fi

    local target ok=0 ng=0
    for target in "${files[@]}"; do
      print -r -- "---- 処理: $target"
      if __AV1IFY_INTERNAL_CALL=1 av1ify "$target"; then
        ((ok++))
      else
        local exit_status=$?
        if (( exit_status == 130 || __AV1IFY_ABORT_REQUESTED )); then
          print -r -- "✋ 中断: 残りのファイルをスキップします"
          return 130
        fi
        ((ng++))
      fi
    done
    print -r -- "== サマリ: OK=$ok / NG=$ng / ALL=$((ok+ng))"
    return 0
  fi

  # 複数の引数がある場合は、それぞれを順番に処理
  if (( $# > 1 )); then
    local target ok=0 ng=0
    for target in "$@"; do
      print -r -- "---- 処理: $target"
      if __AV1IFY_INTERNAL_CALL=1 av1ify "$target"; then
        ((ok++))
      else
        local exit_status=$?
        if (( exit_status == 130 || __AV1IFY_ABORT_REQUESTED )); then
          print -r -- "✋ 中断: 残りのファイルをスキップします"
          return 130
        fi
        ((ng++))
      fi
    done
    print -r -- "== サマリ: OK=$ok / NG=$ng / ALL=$((ok+ng))"
    return 0
  fi

  local target="$1"
  if [[ -d "$target" ]]; then
    # 再帰で対象拡張子のみ列挙（(#i)で大文字小文字無視、.Nで通常ファイルのみ）
    setopt LOCAL_OPTIONS extended_glob null_glob
    unsetopt LOCAL_OPTIONS SH_WORD_SPLIT
    local -a files=()
    while IFS= read -r -d '' f; do
      files+=("$f")
    done < <(find "$target" -type f \( \
        -iname '*.avi' -o -iname '*.mkv' -o -iname '*.rm' -o -iname '*.wmv' -o \
        -iname '*.mpg' -o -iname '*.mpeg' -o -iname '*.mov' -o -iname '*.mp4' -o \
        -iname '*.flv' -o -iname '*.webm' -o -iname '*.3gp' \
      \) -print0)
    if (( ${#files[@]} == 0 )); then
      print -r -- "（対象ファイルなし: $target）"; return 0
    fi
    local f ok=0 ng=0
    # 各ファイルは av1ify() を通して単体処理ルートを再利用（直列実行）
    for f in "${files[@]}"; do
      print -r -- "---- 処理: $f"
      if __AV1IFY_INTERNAL_CALL=1 av1ify "$f"; then
        ((ok++))
      else
        local exit_status=$?
        if (( exit_status == 130 || __AV1IFY_ABORT_REQUESTED )); then
          print -r -- "✋ 中断: 残りのファイルをスキップします"
          return 130
        fi
        ((ng++))
      fi
    done
    print -r -- "== サマリ: OK=$ok / NG=$ng / ALL=$((ok+ng))"
  else
    __av1ify_one "$target"
  fi
}
