# shellcheck shell=bash

# 内部補助: 変換後の検査で NG の場合にファイル名へ注記を付加
__av1ify_mark_issue() {
  local fpath="$1" note="$2"
  local dir="${fpath:h}"
  local base="${fpath:t}"
  local stem ext new_name dest

  if [[ "$base" == *.* && "$base" != .* ]]; then
    stem="${base%.*}"
    ext="${base##*.}"
    # -enc の前にアノテーションを挿入 (例: foo-enc.mp4 → foo-check_ng-enc.mp4)
    if [[ "$stem" == *-enc ]]; then
      new_name="${stem%-enc}-${note}-enc.${ext}"
    else
      new_name="${stem}-${note}.${ext}"
    fi
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

  # 出力映像コーデックの検証
  local out_vcodec
  out_vcodec=$(ffprobe -v error -select_streams v:0 -show_entries stream=codec_name -of default=nk=1:nw=1 -- "$filepath" 2>/dev/null | head -n1)
  if [[ -n "$out_vcodec" && "${out_vcodec:l}" != "av1" ]]; then
    issues+=("映像コーデック不一致 (期待=av1, 実際=${out_vcodec})")
    suffixes+=("codec")
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
