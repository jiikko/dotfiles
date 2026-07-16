# shellcheck shell=bash
# shellcheck disable=SC2154,SC2296

# ffprobe 単一フィールド取得は共通ヘルパーを使う (テストが本ファイルを単体 source するため自己 source)
# shellcheck disable=SC1091,SC2296,SC2298  # zsh 固有の自ファイルパス展開 (shellcheck は解析不可)
source "${${(%):-%x}:A:h}/_ffprobe_helpers.zsh"
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

  video_duration=$(__ff_stream_field "$file" v:0 stream=duration)

  if [[ -z "$video_duration" ]] || [[ "$video_duration" == "N/A" ]]; then
    REPLY="映像ストリームのdurationを取得できません"
    return 2
  fi

  format_duration=$(__ff_format_field "$file" format=duration)

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

  # --- チェック3: DTS 単調性（DTS 逆行 = 本物のタイムスタンプ破損） ---
  #
  # 旧: r_frame_rate vs avg_frame_rate によるスロー/早送り破損検出は誤検知製造機だったため撤去済み
  # (r_frame_rate は推測値でCFR/VFRとも誤判定する。再追加しないこと)。
  #
  # 代わりに、本物のタイムスタンプ破損である DTS (デコード順タイムスタンプ) の逆行を
  # 検出する。DTS はストリーム順 = デコード順で単調非減少であるべきで、逆行は破損。
  # (PTS は B-frame で表示順が前後しうるため判定に使わない。DTS のみで判定する)  #
  # 前提が変わったら再評価: ffprobe が CFR で正確な r_frame_rate を返すようになれば、
  # スロー再生検出を r ベースで復活させる余地がある (現状の ffprobe 仕様では不可)。
  #
  # concat 安全性: このツールの出自は「動画結合 (bin/concat_movies) 後の破損検出」だが、
  # 結合物でも誤検知しない。concat_movies の最終段は concat demuxer + -c copy で、
  # ffmpeg の concat demuxer は各セグメントの DTS をオフセットして連続化するため、
  # 正常な結合物の DTS は単調を保つ (B-frame 入りクリップを -c copy 結合して逆行 0 件を実証)。
  # concat_movies の結合方式が変わった (例: protocol concat や手動 mux) 場合は再検証が必要。
  #
  # 見逃し側の前提: DTS が全フレーム N/A のコンテナでは逆行を検出できず健全扱いになる。
  # 誤検知 (正常を破損扱い) より見逃しに倒す方針 (--force 必須化で判定が形骸化するのを避ける)。
  # MPEG-TS (.ts/.m2ts) は DTS 単調性チェックの対象外にする。
  # 放送録画/splice/PCR discontinuity/33-bit PCR wrap で DTS が「正当に」逆行しうるため、
  # bad>0 で破損判定すると今回直した fps 比較と同じ failure mode (正常を破損扱い→--force 必須)
  # を再導入してしまう。TS の正当な不連続を ffprobe 出力だけで安定分類するのは困難なため、
  # 構造的に「TS コンテナでは DTS 逆行を破損としない」方針とする。
  # (mp4/mov/mkv/avi 等は単一の連続タイムスタンプ epoch を持つため DTS 逆行=破損)
  # 判定は拡張子偽装に強い format_name で行う (av1c の入力に .ts が含まれる)。
  local container_format
  container_format=$(__ff_format_field "$file" format=format_name)

  if [[ "$container_format" != *mpegts* ]]; then
    local dts_backward
    dts_backward=$(ffprobe -v error -select_streams v:0 \
      -show_entries packet=dts -of csv=p=0 -- "$file" 2>/dev/null | awk '
        {
          if ($1 == "N/A" || $1 == "") next
          cur = $1 + 0
          if (seen && cur < prev) bad++
          prev = cur; seen = 1
        }
        END { print bad + 0 }')

    if [[ -n "$dts_backward" ]] && (( dts_backward > 0 )); then
      issues+=("タイムスタンプ破損: DTS非単調(逆行)を${dts_backward}箇所検出")
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
  3. DTS単調性（DTS逆行＝本物のタイムスタンプ破損）

注1: A/V duration 末尾差はチェックしません（録画末尾差は正常な現象。
本物のズレは av1ify postcheck がソース相対比較で検出します）。
注2: avg_frame_rate vs r_frame_rate 比較は廃止しました（r_frame_rate は
ffprobe の推測値で CFR/VFR とも誤検知するため。詳細は __video_health_check の
チェック3 のコメントを参照）。

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
