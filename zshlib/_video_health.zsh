# shellcheck shell=bash
# shellcheck disable=SC2154,SC2296
# ------------------------------------------------------------------------------
# video_health — 動画ファイルの健全性チェック（time_base破損検出など）
# ------------------------------------------------------------------------------

# 単一ファイルの健全性チェック
# $1: ファイルパス
# 戻り値: 0=正常, 1=破損検出, 2=チェック不可（映像なし等）
# REPLY: エラー時の詳細メッセージ
__video_health_check() {
  local file="$1"
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

  # 比率を計算: video_duration / format_duration
  local ratio
  ratio=$(awk -v v="$video_duration" -v f="$format_duration" \
    'BEGIN{ if(f<=0){print "0"; exit} printf "%.4f", v/f }')

  # 正常: 0.90〜1.10 の範囲内
  # 破損: 映像が極端に短い（time_base不一致で結合された場合、約0.5になる）
  local is_bad
  is_bad=$(awk -v r="$ratio" 'BEGIN{ print (r < 0.90) ? 1 : 0 }')

  if (( is_bad )); then
    local video_hms format_hms pct
    video_hms=$(awk -v s="$video_duration" 'BEGIN{ h=int(s/3600); m=int((s%3600)/60); sec=s%60; printf "%d:%02d:%05.2f", h, m, sec }')
    format_hms=$(awk -v s="$format_duration" 'BEGIN{ h=int(s/3600); m=int((s%3600)/60); sec=s%60; printf "%d:%02d:%05.2f", h, m, sec }')
    pct=$(awk -v r="$ratio" 'BEGIN{ printf "%.0f", r*100 }')
    REPLY="映像duration(${video_hms})がフォーマットduration(${format_hms})の${pct}%しかありません（time_base破損の疑い）"
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

結合時のtime_base不一致による再生破損を検出します。
映像ストリームのdurationとコンテナのdurationが大幅に乖離しているファイルを報告します。

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
      (( ${#files[@]} == 1 )) && print -P -- "✅ %F{green}正常: ${f:t}%f"
      ((ok++))
    elif (( rc == 1 )); then
      print -P -- "❌ %F{red}%B${f:t}%b%f" >&2
      print -P -- "   %F{red}$REPLY%f" >&2
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
