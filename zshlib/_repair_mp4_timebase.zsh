# shellcheck shell=bash
# shellcheck disable=SC2154,SC2296
# ------------------------------------------------------------------------------
# repair-mp4-timebase — 動画ファイルのtime_baseを指定値に修正する
# ------------------------------------------------------------------------------

repair-mp4-timebase() {
  if [[ "$1" == "-h" || "$1" == "--help" || $# -lt 2 ]]; then
    cat <<'EOF'
repair-mp4-timebase — 動画ファイルのtime_baseを修正します。

使い方:
  repair-mp4-timebase <timescale> <ファイル> [<ファイル2> ...]

  例:
    repair-mp4-timebase 90000 video.mp4
    repair-mp4-timebase 90000 part1.mp4 part2.mp4

  timescale: 目標のtime_base分母（例: 90000 → time_base=1/90000）

動作:
  1. 元ファイルを <ファイル名>_origin.<ext> にリネーム
  2. ffmpeg -c copy -video_track_timescale でtime_baseを修正
  3. 修正後のファイルを元のファイル名で出力
EOF
    return 0
  fi

  local timescale="$1"
  shift

  if ! [[ "$timescale" =~ ^[0-9]+$ ]] || (( timescale < 1 )); then
    print -r -- "エラー: timescaleは正の整数で指定してください: $timescale" >&2
    return 1
  fi

  local file
  for file in "$@"; do
    if [[ ! -f "$file" ]]; then
      print -r -- "エラー: ファイルが見つかりません: $file" >&2
      return 1
    fi

    # 現在のtime_baseを確認
    local current_tb
    current_tb=$(ffprobe -v error -select_streams v:0 \
      -show_entries stream=time_base -of csv=p=0 -- "$file" 2>/dev/null | head -n1)

    if [[ "$current_tb" == "1/${timescale}" ]]; then
      print -P -- "→ %F{green}スキップ: ${file:t} (既に 1/${timescale})%f"
      continue
    fi

    local dir="${file:A:h}"
    local stem="${file:t:r}"
    local ext="${file:t:e}"
    local origin="${dir}/${stem}_origin.${ext}"
    local tmp="${dir}/.repair_${stem}_$$.${ext}"

    if [[ -e "$origin" ]]; then
      print -r -- "エラー: originファイルが既に存在します: ${origin:t}" >&2
      return 1
    fi

    print -P -- ">> %F{cyan}${file:t}%f: ${current_tb} → 1/${timescale}"

    # 修正実行（一時ファイルに出力）
    if ! ffmpeg -hide_banner -nostdin -loglevel error \
      -i "$file" -c copy -video_track_timescale "$timescale" \
      -y "$tmp"; then
      rm -f -- "$tmp"
      print -r -- "❌ ffmpegエラー: ${file:t}" >&2
      return 1
    fi

    # 検証: time_baseが変わったか
    local new_tb
    new_tb=$(ffprobe -v error -select_streams v:0 \
      -show_entries stream=time_base -of csv=p=0 -- "$tmp" 2>/dev/null | head -n1)

    if [[ "$new_tb" != "1/${timescale}" ]]; then
      rm -f -- "$tmp"
      print -r -- "❌ 修復失敗: time_baseが変更されませんでした (${new_tb})" >&2
      return 1
    fi

    # 元ファイル → origin、修復ファイル → 元ファイル名
    mv -f -- "$file" "$origin" || { rm -f -- "$tmp"; return 1; }
    mv -f -- "$tmp" "$file" || { mv -f -- "$origin" "$file"; return 1; }

    print -P -- "✅ %F{green}${file:t}%f (元ファイル → ${origin:t})"
  done
}
