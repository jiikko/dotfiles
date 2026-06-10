# shellcheck shell=bash

# __av1ify_decide_* / __av1ify_auto_crf / __av1ify_build_final_out から
# 結果を返却するためのグローバル。__av1ify_one が各反復で初期化する。
# REPLY を 1 つだけ使う関数 (__av1ify_auto_crf, __av1ify_build_final_out,
# __av1ify_finalize) は REPLY を使い、複数値を返す関数だけ専用グローバルを使う。
typeset -g __AV1IFY_R_HEIGHT=""
typeset -g __AV1IFY_R_RES_TAG=""
typeset -g __AV1IFY_R_FPS=""
typeset -g __AV1IFY_R_FPS_TAG=""
typeset -g __AV1IFY_R_DENOISE_VF=""
typeset -g __AV1IFY_R_DENOISE_TAG=""
typeset -g __AV1IFY_R_AAC_BITRATE=""
typeset -g __AV1IFY_R_AAC_SRC_BPS=""
typeset -gi __AV1IFY_R_AAC_CAPPED=0

# 内部補助: 与えられたパスが属するマウントポイントの filesystem type を返す
# 例: /Volumes/koji (smbfs マウント) -> "smbfs"
# 取得できない場合は空文字列
# macOS の `stat -f "%T"` は file type (ls -F 形式) を返すため使えない。
# `mount` 出力をパースしてパスにマッチする最長 mount point を選ぶ。
__av1ify_fs_type_for() {
  # 注意: zsh では `path` (小文字) は `PATH` (大文字) の配列形 tied parameter なので
  # `local path=...` で書くと関数内 PATH を引数値で上書きしてしまい、
  # `command -v` や process substitution での `mount` lookup が壊れる。
  # 必ず別名 (target_path) を使う。
  local target_path="$1"
  [[ -z "$target_path" ]] && return 0
  local mount_bin="mount"
  command -v "$mount_bin" >/dev/null 2>&1 || mount_bin="/sbin/mount"
  local line best="" best_len=0 mp
  while IFS= read -r line; do
    # フォーマット: "device on /mount/point (fstype, opts...)"
    mp="${line#* on }"
    mp="${mp%% \(*}"
    [[ -z "$mp" ]] && continue
    if [[ "$target_path" == "$mp" || "$target_path" == "$mp/"* || "$mp" == "/" ]]; then
      if (( ${#mp} > best_len )); then
        best_len=${#mp}
        best="$line"
      fi
    fi
  done < <("$mount_bin" 2>/dev/null)
  local fs="${best##*\(}"
  fs="${fs%%,*}"
  fs="${fs%%\)*}"
  print -r -- "$fs"
}

# 内部補助: ファイルサイズ(バイト)を返す。取得失敗時は空文字。
# macOS: stat -f%z, Linux: stat -c%s
__av1ify_file_size() {
  stat -f%z -- "$1" 2>/dev/null || stat -c%s -- "$1" 2>/dev/null
}

# 内部補助: バイト数を人間可読 (B/KB/MB/GB/TB) にフォーマット
__av1ify_format_size() {
  awk -v b="$1" 'BEGIN{
    if (b < 1024) { printf "%d B", b; exit }
    u="KMGT"; i=0; s=b
    do { s /= 1024; i++ } while (s >= 1024 && i < length(u))
    printf "%.1f %sB", s, substr(u, i, 1)
  }'
}

# 内部補助: エンコード成功後の後処理（postcheck + 元ファイル削除）
# 引数: $1=tmp, $2=final_out, $3=in, $4=target_fps, $5=target_height
# 戻り値: 0=成功, 1=要確認(NG)
# 副作用: REPLY に最終パスを設定
__av1ify_finalize() {
  local tmp="$1" final_out="$2" in="$3" target_fps="$4" target_height="$5"
  __AV1IFY_CURRENT_TMP=""
  mv -f -- "$tmp" "$final_out"
  if __av1ify_postcheck "$final_out" "$in" "$( [[ -n "$target_fps" ]] && echo 1 || echo 0 )" "$target_height"; then
    final_out="$REPLY"; print -P -- "%F{green}✅ 完了: $final_out%f"
    # サイズ削減サマリ (元→出力)。元ファイル ($in) は削除前なのでサイズ取得可能。
    local _src_size _out_size
    _src_size=$(__av1ify_file_size "$in")
    _out_size=$(__av1ify_file_size "$final_out")
    if [[ "$_src_size" =~ ^[0-9]+$ && "$_out_size" =~ ^[0-9]+$ ]] && (( _src_size > 0 )); then
      local _src_h _out_h _pct _icon
      _src_h=$(__av1ify_format_size "$_src_size")
      _out_h=$(__av1ify_format_size "$_out_size")
      # %+.0f で符号付き (削減=負, 増加=正)。削減率 = (out - src) / src * 100
      _pct=$(awk -v s="$_src_size" -v o="$_out_size" 'BEGIN{ printf "%+.0f", (o - s) / s * 100 }')
      _icon="📉"; (( _out_size > _src_size )) && _icon="📈"
      print -P -- "%F{green}   ${_icon} ${_src_h} → ${_out_h} (${_pct}%%)%f"
    fi
    if (( __AV1IFY_DELETE_ORIGIN )) && [[ -f "$in" ]]; then
      # /usr/bin/trash は -- を end-of-options として扱わないため絶対パスで渡す
      local in_abs="${in:A}"
      # ネットワーク FS（smbfs/afpfs/nfs/webdav 等）はゴミ箱を持たないため rm を使う
      local fs_type
      fs_type=$(__av1ify_fs_type_for "$in_abs")
      # trash/rm は Spotlight・Time Machine・iCloud 同期と競合すると
      # 大きなファイルで数十秒〜分単位ブロックすることがあり「最後でスタック」に見える。
      # 予告 1 行を出して原因切り分けを容易にする。
      case "$fs_type" in
        smbfs|afpfs|nfs|webdav|cifs)
          print -r -- ">> 元ファイル削除中 (rm, ${fs_type}): $in"
          rm -f -- "$in_abs" && print -P -- "%F{green}🗑️ 元ファイル削除 (network volume [$fs_type] のため rm): $in%f"
          ;;
        *)
          if command -v trash >/dev/null 2>&1; then
            print -r -- ">> 元ファイルをゴミ箱へ移動中: $in"
            trash "$in_abs" && print -P -- "%F{green}🗑️ 元ファイルをゴミ箱へ移動: $in%f"
          else
            print -r -- ">> 元ファイル削除中 (rm, trash 未導入): $in"
            rm -f -- "$in_abs" && print -P -- "%F{green}🗑️ 元ファイル削除 (trash 未導入のため rm): $in%f"
          fi
          ;;
      esac
    fi
    REPLY="$final_out"; return 0
  else
    final_out="$REPLY"; print -r -- "⚠️ 完了 (要確認): $final_out"
    REPLY="$final_out"; return 1
  fi
}

# 内部補助: 解像度オプションを目標 height + 命名タグに解決する
# 引数: $1 = validated_resolution (空可), $2 = source_short_side (空可), $3 = in (エラー表示用)
# 出力: __AV1IFY_R_HEIGHT, __AV1IFY_R_RES_TAG (アップスケール防止スキップ時は空)
# 戻り値: 0=成功, 1=ソース解像度が取得できず変換中止すべき
__av1ify_decide_resolution() {
  local validated="$1" short_side="$2" in="$3"
  __AV1IFY_R_HEIGHT=""
  __AV1IFY_R_RES_TAG=""
  [[ -z "$validated" ]] && return 0

  case "${validated:l}" in
    480p)  __AV1IFY_R_HEIGHT=480;  __AV1IFY_R_RES_TAG="480p" ;;
    720p)  __AV1IFY_R_HEIGHT=720;  __AV1IFY_R_RES_TAG="720p" ;;
    1080p) __AV1IFY_R_HEIGHT=1080; __AV1IFY_R_RES_TAG="1080p" ;;
    1440p) __AV1IFY_R_HEIGHT=1440; __AV1IFY_R_RES_TAG="1440p" ;;
    4k)    __AV1IFY_R_HEIGHT=2160; __AV1IFY_R_RES_TAG="4k" ;;
    *)
      __AV1IFY_R_HEIGHT="$validated"
      __AV1IFY_R_RES_TAG="${validated}p"
      ;;
  esac
  print -P -- "%F{cyan}>> 出力解像度: ${__AV1IFY_R_RES_TAG} (height=${__AV1IFY_R_HEIGHT})%f"

  # アップスケール防止: ソース解像度が必須
  if [[ -z "$short_side" ]]; then
    print -r -- "❌ 解像度変更が指定されていますが、ソース映像の解像度を取得できませんでした: $in"
    return 1
  fi
  if (( short_side <= __AV1IFY_R_HEIGHT )); then
    print -P -- "%F{yellow}>> 元の短辺 (${short_side}px) が指定解像度 (${__AV1IFY_R_RES_TAG}) 以下のため、解像度変更をスキップします%f"
    __AV1IFY_R_HEIGHT=""
    __AV1IFY_R_RES_TAG=""
  fi
  return 0
}

# 内部補助: fps オプションを target_fps + 命名タグに解決する (キャップ動作)
# 引数: $1 = validated_fps (空可), $2 = in
# 出力: __AV1IFY_R_FPS, __AV1IFY_R_FPS_TAG (キャップ時は両方空)
__av1ify_decide_fps() {
  local validated="$1" in="$2"
  __AV1IFY_R_FPS=""
  __AV1IFY_R_FPS_TAG=""
  [[ -z "$validated" ]] && return 0

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
    fps_skip=$(awk -v src="$source_fps_val" -v tgt="$validated" 'BEGIN { print (src <= tgt) ? 1 : 0 }')
    if (( fps_skip )); then
      print -P -- "%F{yellow}>> ソースfps (${source_fps_val}) が ${validated}fps 以下のため、fps変更をスキップ%f"
      return 0
    fi
    __AV1IFY_R_FPS="$validated"
    __AV1IFY_R_FPS_TAG="${validated}fps"
    print -P -- "%F{cyan}>> 出力フレームレート: ${source_fps_val}fps → ${validated}fps%f"
  else
    __AV1IFY_R_FPS="$validated"
    __AV1IFY_R_FPS_TAG="${validated}fps"
    print -P -- "%F{cyan}>> 出力フレームレート: ${validated}fps (ソースfps取得失敗)%f"
  fi
  return 0
}

# 内部補助: ノイズ除去レベルから vf 部品と命名タグを返す
# 引数: $1 = validated_denoise (light/medium/strong/空)
# 出力: __AV1IFY_R_DENOISE_VF, __AV1IFY_R_DENOISE_TAG (無効値/空入力なら両方空)
__av1ify_decide_denoise() {
  __AV1IFY_R_DENOISE_VF=""
  __AV1IFY_R_DENOISE_TAG=""
  case "$1" in
    light)
      __AV1IFY_R_DENOISE_VF="hqdn3d=2:2:3:3"
      __AV1IFY_R_DENOISE_TAG="dn1"
      print -P -- "%F{cyan}>> ノイズ除去: light (hqdn3d=2:2:3:3)%f"
      ;;
    medium)
      __AV1IFY_R_DENOISE_VF="hqdn3d=4:4:6:6"
      __AV1IFY_R_DENOISE_TAG="dn2"
      print -P -- "%F{cyan}>> ノイズ除去: medium (hqdn3d=4:4:6:6)%f"
      ;;
    strong)
      __AV1IFY_R_DENOISE_VF="hqdn3d=6:6:9:9"
      __AV1IFY_R_DENOISE_TAG="dn3"
      print -P -- "%F{cyan}>> ノイズ除去: strong (hqdn3d=6:6:9:9)%f"
      ;;
  esac
}

# 内部補助: 出力解像度に応じた CRF を自動選択 (AV1_CRF 環境変数があれば優先)
# 引数: $1 = target_height (空可), $2 = source_short_side (空可), $3 = in (ffprobe フォールバック)
# 出力: REPLY = crf 値
__av1ify_auto_crf() {
  local target_height="$1" source_short_side="$2" in="$3"
  if [[ -n "${AV1_CRF:-}" ]]; then
    REPLY="$AV1_CRF"
    return 0
  fi
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
    local crf
    if (( height_for_crf <= 480 )); then
      crf=40   # SD
    elif (( height_for_crf <= 720 )); then
      crf=40   # HD 720p
    elif (( height_for_crf <= 1080 )); then
      crf=45   # Full HD 1080p
    elif (( height_for_crf <= 1440 )); then
      crf=50   # 2K
    else
      crf=54   # 4K以上
    fi
    print -P -- "%F{cyan}>> 解像度: ${height_for_crf}p → CRF=$crf を自動設定%f"
    REPLY="$crf"
  else
    REPLY=40   # デフォルト
    print -r -- "⚠️ 解像度取得失敗 → CRF=40（デフォルト）"
  fi
}

# 内部補助: AAC ターゲットビットレートをソースビットレートでキャップする
# (ソース < target なら最低 32k 〜 ソース値、ソース ≥ target もしくは取得不能なら desired のまま)
# 引数: $1=in (ffprobe 用), $2=desired (例: "96k")
# 出力: __AV1IFY_R_AAC_BITRATE = キャップ後の値
#       __AV1IFY_R_AAC_SRC_BPS = 取得できたソースビットレート (取得失敗なら空)
#       __AV1IFY_R_AAC_CAPPED  = 1 ならキャップ発生 (呼び出し側のメッセージ分岐用)
__av1ify_cap_aac_bitrate() {
  local in="$1" desired="$2"
  __AV1IFY_R_AAC_BITRATE="$desired"
  __AV1IFY_R_AAC_SRC_BPS=""
  __AV1IFY_R_AAC_CAPPED=0

  local src_abitrate
  src_abitrate=$(ffprobe -v error -select_streams a:0 -show_entries stream=bit_rate \
                 -of default=nk=1:nw=1 -- "$in" 2>/dev/null | head -n1)
  [[ -z "$src_abitrate" || ! "$src_abitrate" =~ ^[0-9]+$ ]] && return 0
  __AV1IFY_R_AAC_SRC_BPS="$src_abitrate"

  local target_bps
  case "$desired" in
    *[kK]) target_bps=$(( ${desired%[kK]} * 1000 )) ;;
    *) target_bps="$desired" ;;
  esac
  if (( src_abitrate < target_bps )); then
    local capped_kbps=$(( src_abitrate / 1000 ))
    (( capped_kbps < 32 )) && capped_kbps=32
    __AV1IFY_R_AAC_BITRATE="${capped_kbps}k"
    __AV1IFY_R_AAC_CAPPED=1
  fi
}

# 内部補助: 命名規則 <stem>[-解像度][-fps][-denoise][-aac{br}][-auderr]-enc.mp4 から
# 最終出力ファイル名を組み立てる
# 引数: $1=stem, $2=resolution_tag, $3=fps_tag, $4=denoise_tag,
#        $5=did_aac (0/1), $6=aac_bitrate_resolved, $7=audio_param_error (0/1)
# 出力: REPLY = 最終出力ファイル名
__av1ify_build_final_out() {
  local stem="$1"
  local resolution_tag="$2" fps_tag="$3" denoise_tag="$4"
  local did_aac="$5" aac_bitrate_resolved="$6" audio_param_error="$7"

  local name_suffix=""
  [[ -n "$resolution_tag" ]] && name_suffix+="-${resolution_tag}"
  [[ -n "$fps_tag" ]]        && name_suffix+="-${fps_tag}"
  [[ -n "$denoise_tag" ]]    && name_suffix+="-${denoise_tag}"
  if (( did_aac )); then
    local br="${aac_bitrate_resolved:l}" tag
    if [[ "$br" == *k ]]; then
      tag="$br"
    elif [[ "$br" =~ ^[0-9]+$ ]]; then
      local kb=$(( (br + 500) / 1000 ))
      tag="${kb}k"
    else
      tag="$br"
    fi
    name_suffix+="-aac${tag}"
  fi
  (( audio_param_error )) && name_suffix+="-auderr"

  if [[ -n "$name_suffix" ]]; then
    REPLY="${stem}${name_suffix}-enc.mp4"
  else
    REPLY="${stem}-enc.mp4"
  fi
}

# 内部補助: av1ify バリアントセグメントとして有効なタグかを判定する。
# av1ify 出力命名 <stem>[-<tag>...]-enc.mp4 における各 <tag> の Single Source of Truth。
#
# ⚠️ 重要: __av1ify_build_final_out が新タグを追加する場合、ここにも同じ規則を追加すること。
# 同期漏れは「変換済みなのに再変換される」誤動作を引き起こす。test_av1ify_prefetch.sh の
# round-trip テストが builder → validator の一致を検証するので、新タグ追加時にそこも更新する。
#
# 引数: $1 = 1 セグメント (例: "720p", "30fps", "aac96k")
# 戻り値: 0 = 有効, 1 = 無効
__av1ify_is_valid_variant_tag() {
  local seg="$1"
  [[ "$seg" =~ ^[0-9]+p$ ]] && return 0              # NNNp (resolution)
  [[ "$seg" == "4k" ]] && return 0                   # 4k (resolution)
  [[ "$seg" =~ ^[0-9]+(\.[0-9]+)?fps$ ]] && return 0 # NN[.N]fps (frame rate)
  [[ "$seg" =~ ^aac[0-9]+k$ ]] && return 0           # aacNk (audio bitrate)
  [[ "$seg" =~ ^dn[0-9]+$ ]] && return 0             # dnN (denoise level)
  [[ "$seg" == "auderr" ]] && return 0               # auderr (audio param error)
  return 1
}

# 内部補助: 入力ファイル名が av1ify 出力形式 (-enc.mp4 / -encoded.*) を満たしているか。
# = 「これ自体が既に av1ify 出力なので再エンコードしない」判定。ファイル内容には触れない。
# 引数: $1 = 入力パス
# 戻り値: 0 = 出力形式 (SKIP 対象), 1 = そうでない
__av1ify_input_is_encoded_form() {
  local in="$1"
  [[ "$in" == *enc.mp4 || "$in" == *encoded.* ]]
}

# 内部補助: 既定出力 <stem>-enc.mp4 が存在するかを判定する。
# 入力ファイル本体には触れないため、クラウド materialize を起こさない。
# 引数: $1 = 入力パス
# 出力: REPLY = 既存ファイルパス (見つからなければ "")
# 戻り値: 0 = 既存, 1 = 無し
__av1ify_default_output_exists() {
  local in="$1"
  REPLY="${in%.*}-enc.mp4"
  [[ -e "$REPLY" ]] && return 0
  REPLY=""
  return 1
}

# 内部補助: 入力に対応する既存のバリアント出力 <stem>-<tag>...-enc.mp4 を探す。
# タグの妥当性判定は __av1ify_is_valid_variant_tag に委譲 (命名規則の Single Source of Truth)。
# 入力ファイル本体には触れず、ローカル glob のみで判定するため、クラウド materialize を起こさない。
# 引数: $1 = 入力パス
# 出力: REPLY = 一致した既存ファイルパス (見つからなければ "")
# 戻り値: 0 = 一致あり, 1 = 無し
__av1ify_match_existing_variant() {
  local in="$1"
  REPLY=""
  [[ -z "$in" ]] && return 1
  local stem="${in%.*}"
  setopt LOCAL_OPTIONS extended_glob null_glob
  # shellcheck disable=SC1036,SC2206
  local -a variants=( "${stem}"-*-enc.mp4(N) )
  (( ${#variants[@]} == 0 )) && return 1
  local v t seg valid
  for v in "${variants[@]}"; do
    # shellcheck disable=SC2295
    t="${v#${stem}-}"
    t="${t%-enc.mp4}"
    valid=1
    [[ -z "$t" ]] && valid=0
    while [[ -n "$t" ]]; do
      if [[ "$t" == *-* ]]; then
        seg="${t%%-*}"
        t="${t#*-}"
      else
        seg="$t"
        t=""
      fi
      if ! __av1ify_is_valid_variant_tag "$seg"; then
        valid=0
        break
      fi
    done
    if (( valid )); then
      REPLY="$v"
      return 0
    fi
  done
  return 1
}

# 内部補助: 「この入力はファイル名/ローカル glob だけで SKIP 判定できるか」を返す述語。
# 内容は一切読まないため、クラウドの placeholder ファイルでも materialize を起こさない。
# 主用途: __av1ify_run_batch における prefetch 前ゲート
#   (ディレクトリ指定でも既変換ファイルは prefetch しない)。
# __av1ify_one 側の SKIP 判定 (suffix / 既定出力 / バリアント) と同じ 3 条件を網羅する。
# 引数: $1 = 入力パス
# 戻り値: 0 = SKIP 確定 (prefetch 不要), 1 = 処理候補 (prefetch する価値あり)
__av1ify_skip_by_name() {
  local in="$1"
  [[ -z "$in" ]] && return 0
  __av1ify_input_is_encoded_form "$in" && return 0
  __av1ify_default_output_exists "$in" && return 0
  __av1ify_match_existing_variant "$in" && return 0
  return 1
}

# 内部: 単一ファイル処理
__av1ify_one() {
  local in="$1"

  if (( __AV1IFY_ABORT_REQUESTED )); then
    print -r -- "✋ 中断済みのためスキップ: $in"
    return 130
  fi

  if __av1ify_input_is_encoded_form "$in"; then
    print -r -- "→ SKIP 既に出力ファイル形式です: $in"
    return 0
  fi

  # ベース出力名（copyや無音時）
  local stem="${in%.*}"
  local out="${stem}-enc.mp4"
  local tmp="${out}.in_progress"

  local dry_run="${__AV1IFY_DRY_RUN:-0}"

  # 解像度・fps・denoise は av1ify() root で CLI/環境変数の統合と fail-fast 検証済み
  # (ここに届く値は常に有効。無効値は root が return 1 でバッチごと止める)
  local validated_resolution="$__AV1IFY_RESOLUTION"
  local validated_fps="$__AV1IFY_FPS"
  local validated_denoise="$__AV1IFY_DENOISE"
  local target_fps=""

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
      print -r -- "[DRY-RUN] 音声: compact (130kbps超はaac 96kへ再エンコード)"
    else
      print -r -- "[DRY-RUN] 音声: 実行時に判定"
    fi
    return 0
  fi

  # 早期スキップ: ffprobe前に既存出力をチェック（クラウドファイルの不要なダウンロードを防止）
  if __av1ify_default_output_exists "$in"; then
    print -r -- "→ SKIP 既存: $REPLY"
    return 0
  fi
  if __av1ify_match_existing_variant "$in"; then
    print -r -- "→ SKIP 既存(別バリアント): $REPLY"
    return 0
  fi

  [[ ! -f "$in" ]] && {
    print -r -- "✗ ファイルが無い: $in"
    __AV1IFY_LAST_NG_REASON="ファイルが見つからない"
    return 1
  }

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
  # チェック結果は成功時のみキャッシュ (バッチで毎ファイル ffmpeg を起動しない)。
  # 失敗はキャッシュしない: シェル常駐の関数なので、ffmpeg を入れ直した後に
  # 同じシェルから再実行したとき stale な「利用不可」が残らないようにする。
  local vcodec="libsvtav1"
  if [[ "${__AV1IFY_SVTAV1_OK:-}" != 1 ]]; then
    if ffmpeg -hide_banner -h encoder=libsvtav1 >/dev/null 2>&1; then
      typeset -g __AV1IFY_SVTAV1_OK=1
    else
      print -r -- "❌ libsvtav1 が利用できません。ffmpeg を libsvtav1 付きでビルドしてください。"
      __AV1IFY_LAST_NG_REASON="libsvtav1 が利用不可 (ffmpeg を再ビルドしてください)"
      return 1
    fi
  fi

  # クラウド/ネットワークストレージの場合、ここで実ファイル取得が始まることがある
  # ffprobe でメタデータ取得することでファイルのダウンロードがトリガーされる
  # サイズは stat の論理サイズ。クラウド未 materialize のプレースホルダでも取得できる。
  local _in_size; _in_size=$(__av1ify_file_size "$in")
  if [[ "$_in_size" =~ ^[0-9]+$ ]]; then
    print -r -- ">> ファイル取得中: $in ($(__av1ify_format_size "$_in_size"))"
  else
    print -r -- ">> ファイル取得中: $in"
  fi

  # ソース映像の寸法を取得（アップスケール防止・CRF自動調整・縦横判定に使用）
  #
  # 注: 本関数は ffprobe を項目別に複数回呼ぶ (width / height / rotation / fps /
  # 音声 codec / sample_rate / channels / bit_rate)。1 回の -show_entries に統合する
  # 案は、tests/zshrc/av1ify/test_helper.sh の mock ffprobe が「クエリ文字列の部分
  # 一致で項目別に応答する」設計に依存しているため見送り (統合するなら mock の
  # 再設計とセットで行うこと)。クラウドファイルの materialize は直前の
  # 「ファイル取得中」表示時点の初回アクセスで完了しており、以降の ffprobe は
  # ローカル read (~数十ms/回) なので実害は小さい。
  local source_width="" source_height="" source_display_width="" source_display_height="" source_short_side="" source_is_portrait=0 source_rotation=""
  source_width=$(ffprobe -v error -select_streams v:0 -show_entries stream=width \
           -of default=nk=1:nw=1 -- "$in" 2>/dev/null | head -n1)
  source_height=$(ffprobe -v error -select_streams v:0 -show_entries stream=height \
           -of default=nk=1:nw=1 -- "$in" 2>/dev/null | head -n1)
  source_rotation=$(ffprobe -v error -select_streams v:0 -show_entries stream_side_data=rotation \
             -of default=nk=1:nw=1 -- "$in" 2>/dev/null | head -n1)
  if [[ -n "$source_width" && "$source_width" =~ ^[0-9]+$ && -n "$source_height" && "$source_height" =~ ^[0-9]+$ ]]; then
    source_display_width=$source_width
    source_display_height=$source_height
    if [[ -n "$source_rotation" && "$source_rotation" =~ ^-?[0-9]+$ ]]; then
      local normalized_rotation=$(( (source_rotation % 360 + 360) % 360 ))
      if (( normalized_rotation == 90 || normalized_rotation == 270 )); then
        source_display_width=$source_height
        source_display_height=$source_width
      fi
    fi

    if (( source_display_height > source_display_width )); then
      source_is_portrait=1
    fi

    if (( source_display_width < source_display_height )); then
      source_short_side=$source_display_width
    else
      source_short_side=$source_display_height
    fi
  fi

  # 健全性チェック: time_base破損等の検出
  __video_health_check "$in"
  local _health_rc=$?
  if (( _health_rc == 1 )); then
    if (( __AV1IFY_FORCE )); then
      print -P -- "⚠️ %F{yellow}%B健全性チェック警告（--force で続行）%b%f: ${in:t}" >&2
      print -P -- "   %F{yellow}$REPLY%f" >&2
    else
      print -P -- "❌ %F{red}%B入力ファイルが破損しています%b%f: ${in:t}" >&2
      print -P -- "   %F{red}$REPLY%f" >&2
      print -r -- "   → エンコードをスキップします（--force で強制続行可能）" >&2
      __AV1IFY_LAST_NG_REASON="入力ファイル破損: $REPLY"
      return 1
    fi
  fi

  # 解像度オプション解析 + アップスケール防止
  if ! __av1ify_decide_resolution "$validated_resolution" "$source_short_side" "$in"; then
    __AV1IFY_LAST_NG_REASON="ソース解像度を取得できない (アップスケール防止のため中止)"
    return 1
  fi
  local target_height="$__AV1IFY_R_HEIGHT" resolution_tag="$__AV1IFY_R_RES_TAG"

  # fps オプション解析 (キャップ動作: ソース ≤ target なら変更しない)
  __av1ify_decide_fps "$validated_fps" "$in"
  target_fps="$__AV1IFY_R_FPS"
  local fps_tag="$__AV1IFY_R_FPS_TAG"

  # CRF 自動選択 (AV1_CRF が指定されていればそちらが優先)
  __av1ify_auto_crf "$target_height" "$source_short_side" "$in"
  local crf="$REPLY"
  local preset="${AV1_PRESET:-5}"

  # 音声コーデック事前判定（a:0 が無ければ空）
  local acodec
  acodec=$(ffprobe -v error -select_streams a:0 -show_entries stream=codec_name \
           -of default=nk=1:nw=1 -- "$in" 2>/dev/null | head -n1)

  # ソース音声のサンプルレートとチャンネル数を取得（アップスケール防止）
  local src_sample_rate="" src_channels=""
  if [[ -n "$acodec" ]]; then
    src_sample_rate=$(ffprobe -v error -select_streams a:0 -show_entries stream=sample_rate \
                      -of default=nk=1:nw=1 -- "$in" 2>/dev/null | head -n1)
    src_channels=$(ffprobe -v error -select_streams a:0 -show_entries stream=channels \
                   -of default=nk=1:nw=1 -- "$in" 2>/dev/null | head -n1)
  fi
  # AAC再エンコード時の上限（ソースがこれより低ければソースに合わせる）
  local aac_max_ar=48000 aac_max_ac=2
  local aac_ar="${aac_max_ar}" aac_ac="${aac_max_ac}"
  local aac_params_available=1
  if [[ -n "$acodec" ]]; then
    if [[ -z "$src_sample_rate" || ! "$src_sample_rate" =~ ^[0-9]+$ ]]; then
      aac_params_available=0
    elif (( src_sample_rate < aac_max_ar )); then
      aac_ar="$src_sample_rate"
      print -P -- "%F{yellow}>> 音声: ソースが ${src_sample_rate}Hz のため ${aac_max_ar}Hz へのアップスケールをスキップ%f"
    fi
    if [[ -z "$src_channels" || ! "$src_channels" =~ ^[0-9]+$ ]]; then
      aac_params_available=0
    elif (( src_channels < aac_max_ac )); then
      aac_ac="$src_channels"
      print -P -- "%F{yellow}>> 音声: ソースが mono のためステレオへのアップスケールをスキップ%f"
    fi
  fi

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
  # ノイズ除去フィルタ（hqdn3d）を最初に適用
  __av1ify_decide_denoise "$validated_denoise"
  local denoise_tag="$__AV1IFY_R_DENOISE_TAG"
  [[ -n "$__AV1IFY_R_DENOISE_VF" ]] && vf_parts+=("$__AV1IFY_R_DENOISE_VF")
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
  local audio_param_error=0
  local aac_bitrate_resolved=""

  if [[ -z "$acodec" ]]; then
    args_audio=(-an)
    print -P -- "%F{cyan}>> 音声: なし（-an）%f"
  elif (( use_copy )); then
    # compact モード: 音声ビットレートが96kbps超ならAAC 96kに再エンコード
    if (( __AV1IFY_COMPACT )); then
      local src_abitrate
      src_abitrate=$(ffprobe -v error -select_streams a:0 -show_entries stream=bit_rate \
                     -of default=nk=1:nw=1 -- "$in" 2>/dev/null | head -n1)
      if [[ -n "$src_abitrate" && "$src_abitrate" =~ ^[0-9]+$ ]] && (( src_abitrate > 130000 )); then
        if (( ! aac_params_available )); then
          args_audio=(-map "0:a:0?" -c:a copy)
          audio_param_error=1
          print -r -- "⚠️ 音声パラメータ取得失敗のため copy にフォールバック (codec=$acodec)"
        else
          aac_bitrate_resolved="96k"
          args_audio=(-map "0:a:0?" -c:a aac -b:a "$aac_bitrate_resolved" -ac "$aac_ac" -ar "$aac_ar")
          did_aac=1
          print -P -- "%F{cyan}>> 音声: aac 96k へ再エンコード (compact, 元=$acodec ${src_abitrate}bps)%f"
        fi
      else
        args_audio=(-map "0:a:0?" -c:a copy)
        print -P -- "%F{cyan}>> 音声: copy (codec=$acodec, compact だが130kbps以下)%f"
      fi
    else
      args_audio=(-map "0:a:0?" -c:a copy)
      print -P -- "%F{cyan}>> 音声: copy (codec=$acodec)%f"
    fi
  else
    if (( ! aac_params_available )); then
      args_audio=(-map "0:a:0?" -c:a copy)
      audio_param_error=1
      use_copy=1  # retry パスを有効化
      print -r -- "⚠️ 音声パラメータ取得失敗のため copy にフォールバック (codec=$acodec)"
    fi
    if (( ! audio_param_error )); then
      __av1ify_cap_aac_bitrate "$in" "${AV1_AAC_BITRATE:-96k}"
      aac_bitrate_resolved="$__AV1IFY_R_AAC_BITRATE"
      local src_abitrate_raw="$__AV1IFY_R_AAC_SRC_BPS"
      if [[ -n "$src_abitrate_raw" ]]; then
        if (( __AV1IFY_R_AAC_CAPPED )); then
          print -P -- "%F{cyan}>> 音声: aac ${aac_bitrate_resolved} へ再エンコード (元=$acodec ${src_abitrate_raw}bps, アップスケール防止)%f"
        else
          print -P -- "%F{cyan}>> 音声: aac ${aac_bitrate_resolved} へ再エンコード (元=$acodec ${src_abitrate_raw}bps)%f"
        fi
      else
        print -P -- "%F{cyan}>> 音声: aac ${aac_bitrate_resolved} へ再エンコード (元=$acodec, ビットレート不明)%f"
      fi
      args_audio=(-map "0:a:0?" -c:a aac -b:a "$aac_bitrate_resolved" -ac "$aac_ac" -ar "$aac_ar")
      did_aac=1
    fi
  fi

  # 予定される最終出力ファイル
  # 既存チェックは関数冒頭の __av1ify_default_output_exists ($out) と
  # __av1ify_match_existing_variant (final_out を含む全有効タグ名) で実施済み。
  # ここでの再チェックは冗長なため削除した (並行実行の競合はエンコード自体が
  # 分単位で走る以上ここで再確認しても防げない)。
  __av1ify_build_final_out "$stem" "$resolution_tag" "$fps_tag" "$denoise_tag" \
    "$did_aac" "$aac_bitrate_resolved" "$audio_param_error"
  local final_out="$REPLY"

  print -P -- "%F{cyan}>> 映像: $vcodec (crf=$crf, preset=$preset)%f"
  print -r -- ">> 出力(処理中マーカー): $tmp"
  __AV1IFY_CURRENT_TMP="$tmp"

  # 1回目: 設定通りに実行
  if ffmpeg "${args_common[@]}" "${args_audio[@]}" -- "$tmp"; then
    if __av1ify_finalize "$tmp" "$final_out" "$in" "$target_fps" "$target_height"; then
      return 0
    else
      return 1
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
      if (( audio_param_error )); then
        print -r -- "❌ 音声copy失敗 & パラメータ不明のため再試行不可: $in"
        __AV1IFY_LAST_NG_REASON="音声copy失敗 (音声パラメータ取得不能で AAC 再試行も不可)"
        return 1
      fi
      print -r -- "⚠️ 音声copy失敗 → AAC再エンコードで再試行"
      __av1ify_cap_aac_bitrate "$in" "${AV1_AAC_BITRATE:-96k}"
      aac_bitrate_resolved="$__AV1IFY_R_AAC_BITRATE"
      args_audio=(-map "0:a:0?" -c:a aac -b:a "$aac_bitrate_resolved" -ac "$aac_ac" -ar "$aac_ar")
      did_aac=1
      # 再計算: 最終出力名（解像度/fpsタグを維持しつつ aac ビットレート反映）
      __av1ify_build_final_out "$stem" "$resolution_tag" "$fps_tag" "$denoise_tag" \
        "$did_aac" "$aac_bitrate_resolved" "$audio_param_error"
      final_out="$REPLY"

      __AV1IFY_CURRENT_TMP="$tmp"
      if ffmpeg "${args_common[@]}" "${args_audio[@]}" -- "$tmp"; then
        if __av1ify_finalize "$tmp" "$final_out" "$in" "$target_fps" "$target_height"; then
          return 0
        else
          return 1
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
  # ffmpeg 失敗ルートでは postcheck が走っていないので reason は基本的に空。
  # 下流が既に値をセットしているケースに備えて空のときだけデフォルトを書く。
  if [[ -z "$__AV1IFY_LAST_NG_REASON" ]]; then
    __AV1IFY_LAST_NG_REASON="ffmpeg エンコード失敗 (詳細はログを参照)"
  fi
  return 1
}
