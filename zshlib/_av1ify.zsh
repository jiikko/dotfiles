# -----------------------------------------------------------------------------
# av1ify — 入力ファイル/ディレクトリを AV1(mp4) 化
#   - 出力名: <stem>-enc.mp4（既存は SKIP）
#   - 処理中: <stem>-enc.mp4.in_progress に出力し、成功時に rename
#   - 音声: ffprobe で事前判定し copy（AAC/ALAC/MP3 等）or aac(192k/48kHz/2ch)
#   - ディレクトリ: 再帰で (avi|mkv|rm|wmv|mpg) を順次処理（大文字小文字無視）
#
# 使い方:
#   av1ify "/path/to/movie.avi"
#   av1ify "/path/to/dir"
#
# 調整用環境変数:
#   AV1_CRF=40        # 画質(↑で容量↓/画質↓)
#   AV1_PRESET=5      # 速度/圧縮バランス（SVT-AV1）
#   AV1_COPY_OK="aac,alac,mp3"  # MP4 で copy 許可する音声コーデック
# -----------------------------------------------------------------------------

# 内部: 単一ファイル処理
__av1ify_one() {
  local in="$1"
  [[ ! -f "$in" ]] && { print -r -- "✗ ファイルが無い: $in"; return 1; }

  # 出力パス決定: <stem>-enc.mp4
  local stem="${in%.*}"
  local out="${stem}-enc.mp4"
  local tmp="${out}.in_progress"

  # 既に最終ファイルがあれば SKIP
  if [[ -e "$out" ]]; then
    print -r -- "→ SKIP 既存: $out"
    return 0
  fi
  # 古い in_progress が残っていたら掃除（途中失敗の取り残し対策）
  if [[ -e "$tmp" ]]; then
    print -r -- "⚠️ 残骸削除: $tmp"
    rm -f -- "$tmp"
  fi

  # 映像エンコーダ選択（SVT-AV1 優先、無ければ AOM-AV1）
  local vcodec="libsvtav1"
  if ! ffmpeg -hide_banner -h encoder=libsvtav1 >/dev/null 2>&1; then
    vcodec="libaom-av1"
    print -r -- "⚠️ libsvtav1 不在 → ${vcodec} に切替"
  fi
  local crf="${AV1_CRF:-40}"
  local preset="${AV1_PRESET:-5}"

  # 音声コーデック事前判定（a:0 が無ければ空）
  local acodec
  acodec=$(ffprobe -v error -select_streams a:0 -show_entries stream=codec_name \
           -of default=nk=1:nw=1 -- "$in" 2>/dev/null)

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

  # 中断時に in_progress を掃除
  trap '[[ -n "$tmp" && -e "$tmp" ]] && rm -f -- "$tmp"' INT TERM HUP

  # ffmpeg 共通引数（※ zsh の ? 展開回避のため -map はクォート）
  local -a args
  args=(
    -hide_banner -stats -y
    -i "$in"
    -map "0:v:0"
    -c:v "$vcodec" -crf "$crf" -preset "$preset" -pix_fmt yuv420p
    -movflags +faststart -tag:v av01
  )

  # 音声指定
  if [[ -z "$acodec" ]]; then
    args+=(-an)
    print -r -- ">> 音声: なし（-an）"
  elif (( use_copy )); then
    args+=(-map "0:a:0?" -c:a copy)
    print -r -- ">> 音声: copy (codec=$acodec)"
  else
    args+=(-map "0:a:0?" -c:a aac -b:a 192k -ac 2 -ar 48000)
    print -r -- ">> 音声: aac へ再エンコード (元=$acodec)"
  fi

  print -r -- ">> 映像: $vcodec (crf=$crf, preset=$preset)"
  print -r -- ">> 出力(処理中マーカー): $tmp"
  if ffmpeg "${args[@]}" -- "$tmp"; then
    mv -f -- "$tmp" "$out"
    print -r -- "✅ 完了: $out"
  else
    [[ -e "$tmp" ]] && rm -f -- "$tmp"
    print -r -- "❌ 失敗: $in"
    return 1
  fi
}

# 公開関数: ファイル/ディレクトリを受け取って処理
av1ify() {
  set -o pipefail
  if [[ -z "$1" ]]; then
    print -r -- "Usage: av1ify <input_file|directory>"; return 2
  fi

  local target="$1"
  if [[ -d "$target" ]]; then
    # 再帰で対象拡張子のみ列挙（(#i)で大文字小文字無視、.Nで通常ファイルのみ）
    setopt LOCAL_OPTIONS extended_glob null_glob
    local -a files
    files=($target/**/*.(#i)(avi|mkv|rm|wmv|mpg)(.N))
    if (( ${#files[@]} == 0 )); then
      print -r -- "（対象ファイルなし: $target）"; return 0
    fi
    local f ok=0 ng=0
    for f in "${files[@]}"; do
      print -r -- "---- 処理: $f"
      if __av1ify_one "$f"; then ((ok++)); else ((ng++)); fi
    done
    print -r -- "== サマリ: OK=$ok / NG=$ng / ALL=$((ok+ng))"
  else
    __av1ify_one "$target"
  fi
}
