# shellcheck shell=bash
# ------------------------------------------------------------------------------
# repair_mp4 — 問題のあるコンテナ（mpegts等）を正常なMP4に修復します
# ------------------------------------------------------------------------------

repair_mp4() {
  if [[ "$1" == "-h" || "$1" == "--help" || -z "$1" ]]; then
    cat <<'EOF'
repair_mp4 — 問題のあるコンテナ（mpegts等）を正常なMP4に修復します

機能:
  - mpegtsなどの問題のあるコンテナをMP4に変換
  - 異常なフレームレート（240fps超）を30fpsに正規化
  - 可能な限りストリームコピー（無劣化）で処理
  - 出力: <元ファイル名>-repaired.mp4

使い方:
  repair_mp4 [オプション] <ファイルパス> [<ファイルパス2> ...]

オプション:
  -i, --in-place    元のファイルを上書きする（-repaired.mp4を作成しない）

  例:
    repair_mp4 movie.mp4
    repair_mp4 -i movie.mp4     # 元ファイルを上書き
    repair_mp4 *.mp4

環境変数:
  REPAIR_MP4_FPS (デフォルト: 30)
    異常なフレームレート検出時に適用するfps値
EOF
    return 0
  fi

  local in_place=0
  local -a files=()

  # オプション解析
  while (( $# > 0 )); do
    case "$1" in
      -i|--in-place)
        in_place=1
        shift
        ;;
      -*)
        print -r -- "不明なオプション: $1" >&2
        return 1
        ;;
      *)
        files+=("$1")
        shift
        ;;
    esac
  done

  local file
  for file in "${files[@]}"; do
    __repair_mp4_one "$file" "$in_place"
  done
}

__repair_mp4_one() {
  local in="$1"
  local in_place="${2:-0}"
  [[ ! -f "$in" ]] && { print -r -- "✗ ファイルが無い: $in"; return 1; }

  local stem="${in%.*}"
  local ext="${in:e}"
  local out tmp

  if (( in_place )); then
    # in-placeモード: 元ファイルを上書き
    out="$in"
    tmp="${stem}.mp4.in_progress"
  else
    # 通常モード: -repaired.mp4を作成
    out="${stem}-repaired.mp4"
    tmp="${out}.in_progress"

    # 既存チェック（in-placeモードでは不要）
    if [[ -e "$out" ]]; then
      print -r -- "→ SKIP 既存: $out"
      return 0
    fi
  fi

  # 古い in_progress を掃除
  [[ -e "$tmp" ]] && { print -r -- "⚠️ 残骸削除: $tmp"; rm -f -- "$tmp"; }

  # コンテナフォーマット取得
  local fmt
  fmt=$(ffprobe -v error -show_entries format=format_name -of default=nk=1:nw=1 -- "$in" 2>/dev/null)
  print -r -- ">> 入力フォーマット: ${fmt:-unknown}"

  # フレームレート検出
  local fps_raw fps_val=0 need_reencode=0
  fps_raw=$(ffprobe -v error -select_streams v:0 -show_entries stream=r_frame_rate \
            -of default=nk=1:nw=1 -- "$in" 2>/dev/null | head -n1)

  if [[ -n "$fps_raw" && "$fps_raw" == */* ]]; then
    local num="${fps_raw%/*}" den="${fps_raw#*/}"
    if [[ "$den" -gt 0 ]]; then
      fps_val=$((num / den))
    fi
  elif [[ -n "$fps_raw" && "$fps_raw" =~ ^[0-9]+$ ]]; then
    fps_val="$fps_raw"
  fi

  local target_fps="${REPAIR_MP4_FPS:-30}"
  local -a input_opts=()
  local need_repair=0

  if (( fps_val > 240 )); then
    print -r -- "⚠️ 異常なフレームレート検出: ${fps_val}fps → ${target_fps}fpsに正規化"
    input_opts=(-r "$target_fps")
    need_repair=1
  else
    print -r -- ">> フレームレート: ${fps_val}fps (正常)"
  fi

  # コンテナがMP4以外なら修復が必要
  if [[ "$fmt" != *mp4* && "$fmt" != *mov* ]]; then
    need_repair=1
  fi

  # 修復が不要な場合はスキップ
  if (( ! need_repair )); then
    print -r -- "→ 修復不要: $in"
    return 0
  fi

  # 映像コーデック取得
  local vcodec
  vcodec=$(ffprobe -v error -select_streams v:0 -show_entries stream=codec_name \
           -of default=nk=1:nw=1 -- "$in" 2>/dev/null)

  # 音声コーデック取得
  local acodec
  acodec=$(ffprobe -v error -select_streams a:0 -show_entries stream=codec_name \
           -of default=nk=1:nw=1 -- "$in" 2>/dev/null)

  # ffmpeg引数構築
  local -a args=(
    -hide_banner -stats -y
    -fflags +genpts
    "${input_opts[@]}"
    -i "$in"
    -map 0:v:0
    -c:v copy
  )

  print -r -- ">> 映像: copy (codec=$vcodec)"

  # 音声処理
  if [[ -n "$acodec" ]]; then
    args+=(-map "0:a:0?" -c:a copy)
    print -r -- ">> 音声: copy (codec=$acodec)"
  else
    args+=(-an)
    print -r -- ">> 音声: なし"
  fi

  # 出力設定
  args+=(-movflags +faststart -f mp4)

  # 中断時に in_progress を掃除
  trap '[[ -n "$tmp" && -e "$tmp" ]] && rm -f -- "$tmp"' INT TERM HUP

  print -r -- ">> 出力: $tmp"

  if ffmpeg "${args[@]}" -- "$tmp"; then
    mv -f -- "$tmp" "$out"
    print -r -- "✅ 完了: $out"
    return 0
  else
    [[ -e "$tmp" ]] && rm -f -- "$tmp"
    print -r -- "❌ 失敗: $in"
    return 1
  fi
}
