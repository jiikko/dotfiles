# shellcheck shell=bash

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
  # 解像度は av1ify() で CLI/環境変数を統合・検証済み
  local target_resolution="$__AV1IFY_RESOLUTION"
  local target_fps="${__AV1IFY_FPS:-${AV1_FPS:-}}"

  # fpsのバリデーション（dry-run時も実行）
  local validated_resolution="$target_resolution" validated_fps=""
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
