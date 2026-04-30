# shellcheck shell=bash
# shellcheck disable=SC2154,SC2296
# ------------------------------------------------------------------------------
# video_health — 動画ファイルの健全性チェック（time_base破損検出など）
# ------------------------------------------------------------------------------

# 秒数をH:MM:SS.ss形式に変換
__video_health_hms() {
  awk -v s="$1" 'BEGIN{ h=int(s/3600); m=int((s%3600)/60); sec=s%60; printf "%d:%02d:%05.2f", h, m, sec }'
}

# 単一ファイルの健全性チェック
# $1: ファイルパス
# 戻り値: 0=正常, 1=破損検出, 2=チェック不可（映像なし等）
# REPLY: エラー時の詳細メッセージ（複数項目は改行区切り）
__video_health_check() {
  local file="$1"
  local -a issues=()
  local video_duration format_duration

  # 映像ストリームの duration を取得
  video_duration=$(ffprobe -v error -select_streams v:0 \
    -show_entries stream=duration -of default=nk=1:nw=1 -- "$file" 2>/dev/null | head -n1)

  if [[ -z "$video_duration" ]] || [[ "$video_duration" == "N/A" ]]; then
    REPLY="映像ストリームのdurationを取得できません"
    return 2
  fi

  # フォーマット全体の duration を取得
  format_duration=$(ffprobe -v error \
    -show_entries format=duration -of default=nk=1:nw=1 -- "$file" 2>/dev/null | head -n1)

  if [[ -z "$format_duration" ]] || [[ "$format_duration" == "N/A" ]]; then
    REPLY="フォーマットのdurationを取得できません"
    return 2
  fi

  # --- チェック1: 映像duration vs フォーマットduration ---
  local ratio
  ratio=$(awk -v v="$video_duration" -v f="$format_duration" \
    'BEGIN{ if(f<=0){print "0"; exit} printf "%.4f", v/f }')

  local is_bad
  is_bad=$(awk -v r="$ratio" 'BEGIN{ print (r < 0.90 || r > 1.10) ? 1 : 0 }')

  if (( is_bad )); then
    local video_hms format_hms pct
    video_hms=$(__video_health_hms "$video_duration")
    format_hms=$(__video_health_hms "$format_duration")
    pct=$(awk -v r="$ratio" 'BEGIN{ printf "%.0f", r*100 }')
    if (( pct < 100 )); then
      issues+=("映像duration(${video_hms})がフォーマットduration(${format_hms})の${pct}%しかありません（time_base破損の疑い）")
    else
      issues+=("映像duration(${video_hms})がフォーマットduration(${format_hms})の${pct}%あります（time_base破損の疑い）")
    fi
  fi

  # --- チェック2: 音声ストリームの存在確認 ---
  local audio_stream
  audio_stream=$(ffprobe -v error -select_streams a -show_entries stream=index \
    -of csv=p=0 -- "$file" 2>/dev/null | head -n1)

  if [[ -z "$audio_stream" ]]; then
    issues+=("音声ストリームなし")
  fi

  # 注: A/V duration 差のチェックはここでは行わない。
  # 録画末尾差は録画器材／編集ソフト由来の正常な現象として頻出し、本編のリップシンクとは無関係。
  # 出力後の postcheck がソース相対比較（|out_delta - src_delta|）で
  # 「AV1 エンコ起因の真のズレ」だけを検出する。

  # --- チェック3: avg_frame_rate vs r_frame_rate 乖離（スロー/早送り検出） ---
  local r_fps avg_fps
  r_fps=$(ffprobe -v error -select_streams v:0 \
    -show_entries stream=r_frame_rate -of default=nk=1:nw=1 -- "$file" 2>/dev/null | head -n1)
  avg_fps=$(ffprobe -v error -select_streams v:0 \
    -show_entries stream=avg_frame_rate -of default=nk=1:nw=1 -- "$file" 2>/dev/null | head -n1)

  if [[ -n "$r_fps" && -n "$avg_fps" && "$r_fps" != "0/0" && "$avg_fps" != "0/0" ]]; then
    local fps_ratio
    fps_ratio=$(awk -v r="$r_fps" -v a="$avg_fps" 'BEGIN{
      split(r, rp, "/"); split(a, ap, "/")
      rv = (rp[2]+0 > 0) ? rp[1]/rp[2] : 0
      av = (ap[2]+0 > 0) ? ap[1]/ap[2] : 0
      if(rv <= 0){ print "0"; exit }
      printf "%.4f", av/rv
    }')
    local fps_bad
    fps_bad=$(awk -v r="$fps_ratio" 'BEGIN{ print (r < 0.80 || r > 1.20) ? 1 : 0 }')
    if (( fps_bad )); then
      local r_val avg_val
      r_val=$(awk -v f="$r_fps" 'BEGIN{ split(f,p,"/"); printf "%.2f", (p[2]+0>0)?p[1]/p[2]:0 }')
      avg_val=$(awk -v f="$avg_fps" 'BEGIN{ split(f,p,"/"); printf "%.2f", (p[2]+0>0)?p[1]/p[2]:0 }')
      issues+=("フレームレート異常: r_frame_rate=${r_val}fps avg_frame_rate=${avg_val}fps (タイムスタンプ破損の疑い)")
    fi
  fi

  # --- 結果 ---
  if (( ${#issues[@]} )); then
    local IFS=$'\n'
    REPLY="${issues[*]}"
    return 1
  fi

  REPLY=""
  return 0
}

# ユーザー向けコマンド: ファイルまたはディレクトリの健全性チェック
video_health() {
  if [[ "$1" == "-h" || "$1" == "--help" || $# -eq 0 ]]; then
    cat <<'EOF'
video_health — 動画ファイルの健全性チェック

動画ファイルの健全性を複数の観点でチェックします。
  1. 映像duration vs コンテナduration の乖離（time_base破損）
  2. 音声ストリームの有無
  3. avg_frame_rate vs r_frame_rate の乖離（タイムスタンプ破損→スロー再生）

注: A/V duration 末尾差はチェックしません（録画末尾差は正常な現象。
本物のズレは av1ify postcheck がソース相対比較で検出します）。

使い方:
  video_health <ファイル> [<ファイル2> ...]
  video_health <ディレクトリ>

  例:
    video_health movie.mp4
    video_health file1.mp4 file2.mp4
    video_health /path/to/videos/
EOF
    return 0
  fi

  local -a files=()
  local target
  for target in "$@"; do
    if [[ -f "$target" ]]; then
      files+=("$target")
    elif [[ -d "$target" ]]; then
      while IFS= read -r -d '' f; do
        files+=("$f")
      done < <(find "$target" -type f \( \
          -iname '*.mp4' -o -iname '*.mkv' -o -iname '*.avi' -o -iname '*.mov' -o \
          -iname '*.webm' -o -iname '*.flv' -o -iname '*.wmv' -o -iname '*.m4v' -o \
          -iname '*.ts' -o -iname '*.m2ts' \
        \) -print0 | sort -z)
    else
      print -r -- "エラー: ファイルまたはディレクトリが見つかりません: $target" >&2
      return 1
    fi
  done

  if (( ${#files[@]} == 0 )); then
    print -r -- "対象ファイルなし"
    return 0
  fi
  (( ${#files[@]} > 1 )) && print -r -- ">> ${#files[@]}ファイルをチェック中..."

  local f ok=0 ng=0 skip=0
  for f in "${files[@]}"; do
    __video_health_check "$f"
    local rc=$?
    if (( rc == 0 )); then
      print -P -- "✅ %F{green}正常: ${f:t}%f"
      ((ok++))
    elif (( rc == 1 )); then
      print -P -- "❌ %F{red}%B${f:t}%b%f" >&2
      local line
      while IFS= read -r line; do
        print -P -- "   %F{red}${line//\%/%%}%f" >&2
      done <<< "$REPLY"
      ((ng++))
    else
      ((skip++))
    fi
  done

  if (( ${#files[@]} > 1 )); then
    print -r -- ""
    print -r -- "== 結果: 正常=${ok} / 破損=${ng} / スキップ=${skip} / 合計=${#files[@]}"
  fi

  return $(( ng > 0 ))
}
