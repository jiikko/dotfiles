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

# 数値判定ヘルパー
# 注意: zsh の `[[ =~ ]]` は右辺を直接書くと `^` が glob 否定として解釈され、
# `^...$` アンカーが効かず誤マッチする (例: "-1" や "1e10" まで通る)。
# パターンを変数に入れて `=~ $re` で渡すと zsh では glob 解釈を回避できる
# (shellcheck SC2076 にも触れない)。
# 注意: BSD ERE では `\+` のエスケープが「repetition-operator operand invalid」になるため
# `[+]?` を使って先頭の任意の `+` 記号を表す。
# duration 用: 符号付き小数（負値も許容、ffprobe 出力想定）
__av1ify_is_num() {
  local re='^-?([0-9]+(\.[0-9]*)?|\.[0-9]+)$'
  [[ "$1" =~ $re ]]
}
# threshold 用: 非負小数（負の閾値で全件警告化を防ぐ）
__av1ify_is_nonneg_num() {
  local re='^[+]?([0-9]+(\.[0-9]*)?|\.[0-9]+)$'
  [[ "$1" =~ $re ]]
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

  # A/V duration 判定: ソースとの符号付き相対比較で「AV1 が新たに広げた分」を見る
  # （ソースが元から持っている末尾差を誤検出しないため）
  local out_v out_a src_v src_a
  out_v=$(ffprobe -v error -select_streams v:0 -show_entries stream=duration -of default=nk=1:nw=1 -- "$filepath" 2>/dev/null | head -n1)
  out_a=$(ffprobe -v error -select_streams a:0 -show_entries stream=duration -of default=nk=1:nw=1 -- "$filepath" 2>/dev/null | head -n1)

  local threshold="${AV1IFY_SYNC_TOLERANCE:-0.5}"
  __av1ify_is_nonneg_num "$threshold" || threshold=0.5

  if [[ -z "$audio_stream" ]]; then
    : # 音声なしは noaudio で扱われるので avsync 判定はスキップ
  elif ! __av1ify_is_num "$out_v" || ! __av1ify_is_num "$out_a"; then
    # MKV や一部 MP4 では stream duration が N/A になるが正常なケース。
    # format duration や frame count など他のチェックに委ねて avsync 判定は無言スキップ。
    :
  else
    local use_relative=0
    if [[ -n "$src_path" && -f "$src_path" ]]; then
      src_v=$(ffprobe -v error -select_streams v:0 -show_entries stream=duration -of default=nk=1:nw=1 -- "$src_path" 2>/dev/null | head -n1)
      src_a=$(ffprobe -v error -select_streams a:0 -show_entries stream=duration -of default=nk=1:nw=1 -- "$src_path" 2>/dev/null | head -n1)
      if __av1ify_is_num "$src_v" && __av1ify_is_num "$src_a"; then
        use_relative=1
      fi
    fi

    if (( use_relative )); then
      # 符号付きで関係差を見る（方向反転も検出）
      local result
      result=$(LC_ALL=C awk -v sv="$src_v" -v sa="$src_a" -v ov="$out_v" -v oa="$out_a" -v t="$threshold" 'BEGIN{
        sd = sa - sv
        od = oa - ov
        drift = od - sd; if (drift < 0) drift = -drift
        printf "%.6f %.6f %.6f %d", sd, od, drift, (drift > t) ? 1 : 0
      }') || result=""
      if [[ -n "$result" ]]; then
        local sd_v="${result%% *}"; result="${result#* }"
        local od_v="${result%% *}"; result="${result#* }"
        local drift_v="${result%% *}"; result="${result#* }"
        local drift_bad="$result"
        if [[ "$drift_bad" == "1" ]]; then
          issues+=("音ズレ疑い (src_delta=${sd_v}s out_delta=${od_v}s Δ=${drift_v}s)")
          suffixes+=("avsync")
        fi
      else
        # awk 失敗時は無言スキップ（他のチェックに委ねる）
        print -ru2 -- "⚠️ A/V drift計算スキップ (awk失敗)"
      fi
    else
      # fallback: ソース duration 取れない時は従来の絶対値判定
      local diff
      diff=$(LC_ALL=C awk -v a="$out_a" -v v="$out_v" -v t="$threshold" 'BEGIN{
        d = a - v; if (d < 0) d = -d
        printf "%.6f %d", d, (d > t) ? 1 : 0
      }') || diff=""
      if [[ -n "$diff" ]]; then
        local diff_v="${diff%% *}"
        local diff_bad="${diff#* }"
        if [[ "$diff_bad" == "1" ]]; then
          issues+=("音ズレ疑い (Δ=${diff_v}s)")
          suffixes+=("avsync")
        fi
      else
        # awk 失敗時は無言スキップ（他のチェックに委ねる）
        print -ru2 -- "⚠️ A/V diff計算スキップ (awk失敗)"
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
      local frame_diff=$(( src_frames > out_frames ? src_frames - out_frames : out_frames - src_frames ))
      local frame_tolerance="${AV1IFY_FRAME_TOLERANCE:-24}"
      if (( frame_diff > frame_tolerance )); then
        issues+=("フレーム数不一致 (src=${src_frames}, out=${out_frames}, Δ=${frame_diff})")
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
    # macOS: stat -f%z, Linux: stat -c%s
    src_size=$(stat -f%z -- "$src_path" 2>/dev/null || stat -c%s -- "$src_path" 2>/dev/null) || src_size=""
    out_size=$(stat -f%z -- "$filepath" 2>/dev/null || stat -c%s -- "$filepath" 2>/dev/null) || out_size=""
    if [[ -n "$src_size" && "$src_size" =~ ^[0-9]+$ && -n "$out_size" && "$out_size" =~ ^[0-9]+$ ]] && (( src_size > 0 )); then
      local size_ratio
      size_ratio=$(awk -v o="$out_size" -v s="$src_size" 'BEGIN{ printf "%.4f", o / s }')
      local min_ratio="${AV1IFY_MIN_SIZE_RATIO:-0.001}"
      local too_small
      too_small=$(awk -v r="$size_ratio" -v m="$min_ratio" 'BEGIN{ print (r < m) ? 1 : 0 }')
      if (( too_small )); then
        issues+=("ファイルサイズ異常 (src=${src_size}B, out=${out_size}B, ratio=${size_ratio})")
        suffixes+=("tinyfile")
      elif (( out_size > src_size )); then
        local pct_increase
        pct_increase=$(awk -v o="$out_size" -v s="$src_size" 'BEGIN{ printf "%.0f", (o - s) / s * 100 }')
        issues+=("サイズ増加 (src=${src_size}B, out=${out_size}B, +${pct_increase}%)")
        suffixes+=("bigger")
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
