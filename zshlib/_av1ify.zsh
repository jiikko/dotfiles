# ------------------------------------------------------------------------------
# av1ify — 入力された動画ファイル、またはディレクトリ内の動画ファイルをAV1形式のMP4に一括変換します。
# ------------------------------------------------------------------------------

# 内部: 単一ファイル処理
__av1ify_one() {
  local in="$1"
  if [[ "$in" == *-enc.mp4 ]]; then
    print -r -- "→ SKIP 既に出力ファイル形式です: $in"
    return 0
  fi
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
    -f mp4
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
  if [[ "$1" == "-h" || "$1" == "--help" || -z "$1" ]]; then
    cat <<'EOF'
av1ify — 入力された動画ファイル、またはディレクトリ内の動画ファイルをAV1形式のMP4に一括変換します。

機能:
  - 指定されたファイルまたはディレクトリを対象に処理を実行します。
  - 出力ファイル名は `<元のファイル名>-enc.mp4` となります。
  - 既に変換済みのファイルが存在する場合は、処理をスキップします。
  - 処理中には `<ファイル名>.in_progress` という一時ファイルを作成し、変換成功後にリネームします。
  - ffprobeを使用して入力ファイルの音声コーデックを判別し、可能であれば音声を無劣化でコピーします。
    (デフォルトでAAC, ALAC, MP3に対応)
    対応していない形式の場合は、AAC (192kbps, 48kHz, 2ch) に再エンコードします。
  - ディレクトリを指定した場合、再帰的に動画ファイル (avi, mkv, rm, wmv, mpg) を検索して変換します。
    (ファイル名の大文字・小文字は区別しません)

使い方:
  av1ify [オプション] <ファイルパス または ディレクトリパス>

  例:
    # 単一のファイルを変換
    av1ify "/path/to/movie.avi"

    # ディレクトリ内のすべての動画ファイルを変換
    av1ify "/path/to/dir"

    # CRF値を指定して画質を調整
    AV1_CRF=35 av1ify "/path/to/movie.mp4"

オプション:
  -h, --help: このヘルプメッセージを表示します。

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
EOF
    return 0
  fi

  set -o pipefail

  local target="$1"
  if [[ -d "$target" ]]; then
    # 再帰で対象拡張子のみ列挙（(#i)で大文字小文字無視、.Nで通常ファイルのみ）
    setopt LOCAL_OPTIONS extended_glob null_glob
    local -a files
    files=($target/**/*.(#i)(avi|mkv|rm|wmv|mpg|mpeg|mov|mp4|flv|webm|3gp)(.N))
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
