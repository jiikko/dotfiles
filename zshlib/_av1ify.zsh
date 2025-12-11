# shellcheck shell=bash
# ------------------------------------------------------------------------------
# av1ify — 入力された動画ファイル、またはディレクトリ内の動画ファイルをAV1形式のMP4に一括変換します。
# ------------------------------------------------------------------------------

typeset -gi __AV1IFY_ABORT_REQUESTED=0
typeset -g  __AV1IFY_CURRENT_TMP=""
typeset -gi __AV1IFY_DRY_RUN=0

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
    diff=$(awk -v a="$a_dur_raw" -v v="$v_dur_raw" 'BEGIN{ if (a=="" || v=="") exit 1; d=a-v; if (d<0) d=-d; printf "%.6f", d }

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
' 2>/dev/null) || diff=""
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

# 内部: 単一ファイル処理
__av1ify_one() {  local in="$1"

  if (( __AV1IFY_ABORT_REQUESTED )); then
    print -r -- "✋ 中断済みのためスキップ: $in"
    return 130
  fi

  if [[ "$in" == *enc.mp4 || "$in" == *encoded.* ]]; then
    print -r -- "→ SKIP 既に出力ファイル形式です: $in"
    return 0
  fi
  [[ ! -f "$in" ]] && { print -r -- "✗ ファイルが無い: $in"; return 1; }

  # クラウド/ネットワークストレージの場合、ここで実ファイル取得が始まることがある
  print -r -- ">> ファイル取得中: $in"


  # ベース出力名（copyや無音時）
  local stem="${in%.*}"
  local out="${stem}-enc.mp4"
  local tmp="${out}.in_progress"

  local dry_run="${__AV1IFY_DRY_RUN:-0}"

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

  # 解像度を取得して CRF を自動調整（環境変数が優先）
  local crf preset
  if [[ -n "${AV1_CRF:-}" ]]; then
    crf="$AV1_CRF"
  else
    # 縦解像度を取得
    local height
    height=$(ffprobe -v error -select_streams v:0 -show_entries stream=height \
             -of default=nk=1:nw=1 -- "$in" 2>/dev/null)

    if [[ -n "$height" && "$height" =~ ^[0-9]+$ ]]; then
      # 解像度に応じて CRF を設定
      if (( height <= 480 )); then
        crf=40  # SD:
      elif (( height <= 720 )); then
        crf=40  # HD 720p
      elif (( height <= 1080 )); then
        crf=45  # Full HD 1080p
      elif (( height <= 1440 )); then
        crf=50  # 2K
      else
        crf=54  # 4K以上
      fi
      print -r -- ">> 解像度: ${height}p → CRF=$crf を自動設定"
    else
      crf=40  # デフォルト
      print -r -- "⚠️ 解像度取得失敗 → CRF=$crf（デフォルト）"
    fi
  fi
  preset="${AV1_PRESET:-5}"

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

  # ffmpeg 共通引数
  local -a args_common args_audio
  args_common=(
    -hide_banner -stats -y
    -i "$in"
    -map "0:v:0"
    -c:v "$vcodec" -crf "$crf" -preset "$preset" -pix_fmt yuv420p
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
    args_audio=(-map "0:a:0?" -c:a copy)
    print -r -- ">> 音声: copy (codec=$acodec)"
  else
    aac_bitrate_resolved="${AV1_AAC_BITRATE:-96k}"
    args_audio=(-map "0:a:0?" -c:a aac -b:a "$aac_bitrate_resolved" -ac 2 -ar 48000)
    did_aac=1
    print -r -- ">> 音声: aac へ再エンコード (元=$acodec)"
  fi

  # 予定される最終出力ファイル（copyや無音なら -enc.mp4、AACエンコードなら -aac{br}-enc.mp4）
  local final_out="$out"
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
    final_out="${stem}-aac${tag}-enc.mp4"
  fi

  # 既存チェック（過去の出力があればスキップ）
  if [[ -e "$final_out" || -e "$out" ]]; then
    local exist="$final_out"
    [[ -e "$out" ]] && exist="$out"
    print -r -- "→ SKIP 既存: $exist"
    return 0
  fi

  if (( dry_run )); then
    local audio_plan
    if [[ -z "$acodec" ]]; then
      audio_plan="無音 (-an)"
    elif (( use_copy )); then
      audio_plan="copy (codec=$acodec)"
    else
      audio_plan="aac 再エンコード (target=${aac_bitrate_resolved:-${AV1_AAC_BITRATE:-96k}})"
    fi
    print -r -- "[DRY-RUN] 変換予定: $in → $final_out"
    print -r -- "[DRY-RUN] 映像: $vcodec (crf=$crf, preset=$preset)"
    print -r -- "[DRY-RUN] 音声: $audio_plan"
    return 0
  fi

  print -r -- ">> 映像: $vcodec (crf=$crf, preset=$preset)"
  print -r -- ">> 出力(処理中マーカー): $tmp"
  __AV1IFY_CURRENT_TMP="$tmp"

  # 1回目: 設定通りに実行
  if ffmpeg "${args_common[@]}" "${args_audio[@]}" -- "$tmp"; then
    __AV1IFY_CURRENT_TMP=""
    mv -f -- "$tmp" "$final_out"
    if __av1ify_postcheck "$final_out"; then
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
      # 再計算: 最終出力名
      local br="${aac_bitrate_resolved:l}" tag
      if [[ "$br" == *k ]]; then
        tag="$br"
      elif [[ "$br" =~ ^[0-9]+$ ]]; then
        local kb; (( kb = (br + 500) / 1000 ))
        tag="${kb}k"
      else
        tag="$br"
      fi
      final_out="${stem}-aac${tag}-enc.mp4"

      __AV1IFY_CURRENT_TMP="$tmp"
      if ffmpeg "${args_common[@]}" "${args_audio[@]}" -- "$tmp"; then
        __AV1IFY_CURRENT_TMP=""
        mv -f -- "$tmp" "$final_out"
        if __av1ify_postcheck "$final_out"; then
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

  local dry_run="${__AV1IFY_DRY_RUN:-0}"
  local show_help=0
  local -a positional=()
  while (( $# > 0 )); do
    case "$1" in
      --dry-run|-n)
        dry_run=1
        ;;
      -h|--help)
        (( ! __av1ify_internal )) && show_help=1
        ;;
      *)
        positional+=("$1")
        ;;
    esac
    shift
  done
  set -- "${positional[@]}"

  if (( ! __av1ify_internal )); then
    __AV1IFY_DRY_RUN=$dry_run
  else
    dry_run="${__AV1IFY_DRY_RUN:-$dry_run}"
  fi

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

オプション:
  -h, --help: このヘルプメッセージを表示します。
  -n, --dry-run: 実行内容のみを表示し、ファイルを変更しません。
  -f <ファイル>: 改行区切りでファイルパスが記載されたリストファイルを読み込んで処理します。

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
        local status=$?
        if (( status == 130 || __AV1IFY_ABORT_REQUESTED )); then
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
        local status=$?
        if (( status == 130 || __AV1IFY_ABORT_REQUESTED )); then
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
        local status=$?
        if (( status == 130 || __AV1IFY_ABORT_REQUESTED )); then
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
