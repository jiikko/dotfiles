# shellcheck shell=bash

# ffprobe 単一フィールド取得は共通ヘルパーを使う (テストが本ファイルを単体 source するため自己 source)
# shellcheck disable=SC1091,SC2296,SC2298  # zsh 固有の自ファイルパス展開 (shellcheck は解析不可)
source "${${(%):-%x}:A:h}/_ffprobe_helpers.zsh"

# 内部補助: 変換後の検査で NG の場合にファイル名へ注記を付加
# リネーム先が既に存在する場合 (再実行で同名 check_ng が再生成されるケース) は、
# 前回の成果物を mv -f で無言上書きせず、注記に連番を付けて衝突を回避する
# (例: foo-check_ng-enc.mp4 が既存なら foo-check_ng2-enc.mp4)。
__av1ify_mark_issue() {
  local fpath="$1" note="$2"
  local dir="${fpath:h}"
  local base="${fpath:t}"
  local stem ext new_name dest
  local -i try=1
  local unique_note="$note"

  while :; do
    if [[ "$base" == *.* && "$base" != .* ]]; then
      stem="${base%.*}"
      ext="${base##*.}"
      # -enc の前にアノテーションを挿入 (例: foo-enc.mp4 → foo-check_ng-enc.mp4)
      if [[ "$stem" == *-enc ]]; then
        new_name="${stem%-enc}-${unique_note}-enc.${ext}"
      else
        new_name="${stem}-${unique_note}.${ext}"
      fi
    else
      new_name="${base}-${unique_note}"
    fi

    if [[ "$dir" == "." ]]; then
      dest="$new_name"
    else
      dest="$dir/$new_name"
    fi

    [[ ! -e "$dest" ]] && break
    (( try++ ))
    unique_note="${note}${try}"
  done

  if mv -f -- "$fpath" "$dest"; then
    REPLY="$dest"
    return 0
  fi

  REPLY="$fpath"
  return 1
}

# 数値判定ヘルパー
# 注意: zsh の `[[ =~ ]]` は右辺を直接書くと `^` が glob 否定として解釈され、
# `^...$` アンカーが効かず誤マッチする (例: "-1" や "1e10" まで通る)。
# パターンを変数に入れて `=~ $re` で渡すと zsh では glob 解釈を回避できる
# (shellcheck SC2076 にも触れない)。
# 注意: BSD ERE では `\+` のエスケープが「repetition-operator operand invalid」になるため
# `[+]?` を使って先頭の任意の `+` 記号を表す。
# duration 用: 符号付き小数（負値も許容、ffprobe 出力想定）
__av1ify_is_num() {
  local re='^-?([0-9]+(\.[0-9]*)?|\.[0-9]+)$'
  [[ "$1" =~ $re ]]
}
# threshold 用: 非負小数（負の閾値で全件警告化を防ぐ）
__av1ify_is_nonneg_num() {
  local re='^[+]?([0-9]+(\.[0-9]*)?|\.[0-9]+)$'
  [[ "$1" =~ $re ]]
}

# 内部補助: ストリームの実質的な末尾時刻 (= duration) を取得
# まず安価な `stream=duration` を試し、N/A なら packet PTS 走査にフォールバックする。
# MKV / 一部 MP4 は `stream=duration` を出さないため、ソース由来の A/V mismatch を
# encode 由来と誤検出しないために真の duration が必要 (issue: 元動画の音声末尾が
# 短いケース)。
#
# 引数: $1 = ファイルパス, $2 = stream specifier (例: v:0, a:0)
# 出力: REPLY = 取得した duration [秒] (取得不能なら空)
# 戻り値: 0=取得成功, 1=取得不能
#
# パフォーマンス: 安価パスは ffprobe 1 回。フォールバックは「format duration 取得」
# + 「末尾 60s 区間の packet 走査」 (= 5GB クラス MKV で数秒オーダー)。最後の手段
# として全走査もある (区間スキャンで packet が拾えない超変則ケース用)。
__av1ify_get_stream_end() {
  local file="$1" spec="$2" val
  # 安価パス: stream=duration
  val=$(__ff_stream_field "$file" "$spec" stream=duration)
  if __av1ify_is_num "$val"; then
    REPLY="$val"
    return 0
  fi
  # 宣言 duration が無い (MKV 等) → packet 実測にフォールバック
  __av1ify_packet_end "$file" "$spec"
}

# 内部補助: packet PTS 走査でストリームの「実測」末尾時刻を取得
# 宣言 duration (mp4 の mdhd/tkhd 等) を一切信じず、実際の packet 列から測る。
# __av1ify_get_stream_end のフォールバックと、avsync 判定の再検証で共用する。
#
# 引数: $1 = ファイルパス, $2 = stream specifier (例: v:0, a:0)
# 出力: REPLY = 表示終端 max(pts_time + duration_time) [秒] (取得不能なら空)
# 戻り値: 0=取得成功, 1=取得不能
__av1ify_packet_end() {
  local file="$1" spec="$2" val fmt_dur start
  # ⚠️ 「packet 列の最後の行の pts_time」を終端に使わないこと。
  # packet はデコード順で出るため、B-frame の表示順入れ替えで最終行が最大 PTS に
  # ならない (実測: 1fps / x264 bframes=16 の 20s ソースで最終行 18.0s に対し実際の
  # 表示終端は 20.0s)。過小評価は drift を縮める方向に働き、本物の音ズレを見逃す。
  # 表示終端 = max(pts_time + duration_time) で測る (duration_time が N/A の行は pts のみ)。
  # shellcheck disable=SC2016  # $1/$2 は awk のフィールド (シェル変数ではない)
  local prog='/^[0-9]/ {
    e = $1 + 0
    if (NF > 1 && $2 ~ /^[0-9]/) e += $2
    if (!seen || e > max) { max = e; seen = 1 }
  }
  END { if (seen) printf "%.6f", max }'
  # 末尾 60s 区間だけ走査する (5GB クラスでも数秒オーダー)。
  # ffprobe -read_intervals "START%" で START 秒から末尾までを読む。
  fmt_dur=$(__ff_format_field "$file" format=duration)
  if __av1ify_is_num "$fmt_dur"; then
    start=$(LC_ALL=C awk -v d="$fmt_dur" 'BEGIN { s = d - 60; if (s < 0) s = 0; printf "%.0f", s }')
    val=$(ffprobe -v error -read_intervals "${start}%" -select_streams "$spec" \
          -show_entries packet=pts_time,duration_time -of csv=p=0 -- "$file" 2>/dev/null \
          | LC_ALL=C awk -F, "$prog")
    if __av1ify_is_num "$val"; then
      REPLY="$val"
      return 0
    fi
  fi
  # 最後の手段: 全 packet 走査 (区間スキャンで packet が拾えない超変則ケース用)
  val=$(ffprobe -v error -select_streams "$spec" -show_entries packet=pts_time,duration_time \
        -of csv=p=0 -- "$file" 2>/dev/null \
        | LC_ALL=C awk -F, "$prog")
  if __av1ify_is_num "$val"; then
    REPLY="$val"
    return 0
  fi
  REPLY=""
  return 1
}

# 内部補助: ストリームの宣言 start_time [秒] を取得 (取れなければ 0 とみなす)
# 引数: $1 = ファイルパス, $2 = stream specifier (例: v:0, a:0)
# 出力: REPLY = start_time (欠落/N/A は "0"。コンテナの慣行どおり先頭 0 と解釈する。
#        mp4 出力の時間シフトは edit list として必ずここに書かれるため、出力側で
#        欠落 = 0 とみなしてもシフトを取りこぼさない)
__av1ify_start_time() {
  local val
  val=$(__ff_stream_field "$1" "$2" stream=start_time)
  if __av1ify_is_num "$val"; then
    REPLY="$val"
  else
    REPLY="0"
  fi
}

# 内部補助: A/V drift の計算
# 引数: src_v src_a out_v out_a threshold
# 出力: REPLY = "<src_delta> <out_delta> <drift> <bad>" (bad: 1=閾値超過)
# 戻り値: 0=計算成功, 1=失敗
__av1ify_calc_drift() {
  REPLY=$(LC_ALL=C awk -v sv="$1" -v sa="$2" -v ov="$3" -v oa="$4" -v t="$5" 'BEGIN{
    sd = sa - sv
    od = oa - ov
    drift = od - sd; if (drift < 0) drift = -drift
    printf "%.6f %.6f %.6f %d", sd, od, drift, (drift > t) ? 1 : 0
  }') || REPLY=""
  [[ -n "$REPLY" ]]
}

# 内部補助: 出力ファイルの簡易チェック（音声有無と音ズレ）
__av1ify_postcheck() {
  local filepath="$1"
  local src_path="${2:-}"
  local fps_changed="${3:-0}"
  local expected_height="${4:-}"
  local -a issues suffixes

  local audio_stream
  audio_stream=$(ffprobe -v error -select_streams a -show_entries stream=index -of csv=p=0 -- "$filepath" 2>/dev/null | head -n1)
  if [[ -z "$audio_stream" ]]; then
    # ソースに音声が無い場合、出力に音声が無いのは -an エンコードの正常な結果
    # (__av1ify_one が「音声: なし（-an）」で意図的に作る)。NG にすると音声なし素材が
    # 毎回 check_ng-noaudio へリネームされ、再実行のたびフルエンコードが再走する。
    # ソースが参照できない / ffprobe 自体が失敗した場合は判定不能なので
    # 従来どおり NG side に倒す (probe 失敗を「音声なし」と誤解釈しない)。
    local src_probe_out="" src_silent=0
    if [[ -n "$src_path" && -f "$src_path" ]]; then
      if src_probe_out=$(ffprobe -v error -select_streams a -show_entries stream=index -of csv=p=0 -- "$src_path" 2>/dev/null); then
        [[ -z "${src_probe_out%%$'\n'*}" ]] && src_silent=1
      fi
    fi
    if (( src_silent )); then
      print -r -- ">> 音声なしソースのため noaudio 判定をスキップ"
    else
      issues+=("音声ストリーム検出できず")
      suffixes+=("noaudio")
    fi
  fi

  # A/V duration 判定: ソースとの符号付き相対比較で「encode が新たに作った drift」だけを見る。
  # ソースが元から持っている A/V mismatch (例: 末尾無音映像が残る MKV、雑なリッピング素材)
  # を encode 由来と誤検出しないために、絶対値ではなく enc 前後の差分のみを評価する。
  local out_v out_a src_v="" src_a=""
  out_v=$(__ff_stream_field "$filepath" v:0 stream=duration)
  out_a=$(__ff_stream_field "$filepath" a:0 stream=duration)

  # 閾値デフォルトは 2.0s。encode (ffmpeg) は通常 1〜数十ms の精度で A/V 同期を保つので、
  # 2 秒を超える「新たに作った drift」は実害級と判定する。
  local threshold="${AV1IFY_SYNC_TOLERANCE:-2.0}"
  __av1ify_is_nonneg_num "$threshold" || threshold=2.0

  if [[ -z "$audio_stream" ]]; then
    : # 音声なしは noaudio で扱われるので avsync 判定はスキップ
  elif ! __av1ify_is_num "$out_v" || ! __av1ify_is_num "$out_a"; then
    # 出力 mp4 で stream duration が取れないのは異常 (av1ify は mp4 出力固定)。他のチェックに委ねる。
    :
  elif [[ -z "$src_path" || ! -f "$src_path" ]]; then
    # ソースが無い場合は判定スキップ (relative 比較ができないため)
    :
  else
    # ソースの真の duration を取得 (stream=duration → packet PTS スキャンの順)
    if __av1ify_get_stream_end "$src_path" "v:0"; then
      src_v="$REPLY"
    fi
    if __av1ify_get_stream_end "$src_path" "a:0"; then
      src_a="$REPLY"
    fi

    if __av1ify_is_num "$src_v" && __av1ify_is_num "$src_a"; then
      # 符号付きで関係差を見る (方向反転も検出)
      if __av1ify_calc_drift "$src_v" "$src_a" "$out_v" "$out_a" "$threshold"; then
        local sd_v od_v drift_v drift_bad
        read -r sd_v od_v drift_v drift_bad <<< "$REPLY"

        if [[ "$drift_bad" == "1" ]]; then
          # 宣言 duration (mp4 の mdhd/tkhd) が自分のサンプルテーブルと食い違う壊れた
          # ソースが実在する (実例: 宣言 8270.84s / 実測 8287.48s = 500 フレーム
          # 分の嘘)。この手のソースでは encode が正しい値を書き直すため、その
          # 「メタデータの修復」がそのまま drift として計上され誤検知になる
          # (実測 Δ=0.013s に対し宣言ベースだと Δ=16.68s)。
          #
          # 閾値を超えたときだけ、4 値すべてを packet 実測で測り直して再判定する。
          # ・通常時の ffprobe コストは増えない (超過時のみ走る)
          # ・src/out を同じ測り方 (packet 実測の表示終端) に揃えるので量が混ざらない
          # ・false negative は増えない: 本物の encode 起因ズレは出力の packet 列に必ず出る
          # ⚠️ packet 実測が測るのは「表示終端」で、ストリームの「長さ」ではない。
          # 音声の先頭が落ちて edit-list 遅延が書かれ、終端は映像と揃っている
          # 時間シフト型のズレは終端 Δ に出ない (実測 issue 058: 宣言 Δ=4.98s の
          # 本物のシフトを終端 Δ=0.0007s で正常と誤判定していた)。シフトは開始
          # オフセットに必ず出るので、FLAG→ok の降格は「終端が揃っている」かつ
          # 「音声開始の関係差も閾値内」の 2 条件で行う。
          # なお ok→FLAG 方向の再判定 (双方向化) は入れない: 全件で packet 走査
          # 4 本のコストが常時掛かる上、宣言ベースの初回判定を素通りするズレは
          # 変更前 (997d078) も検知しておらず、ここで守るのは降格の正しさだけ。
          local m_sv m_sa m_ov m_oa
          if __av1ify_packet_end "$src_path" "v:0" && m_sv="$REPLY" \
            && __av1ify_packet_end "$src_path" "a:0" && m_sa="$REPLY" \
            && __av1ify_packet_end "$filepath" "v:0" && m_ov="$REPLY" \
            && __av1ify_packet_end "$filepath" "a:0" && m_oa="$REPLY" \
            && __av1ify_calc_drift "$m_sv" "$m_sa" "$m_ov" "$m_oa" "$threshold"; then
            local m_sd m_od m_drift m_bad
            read -r m_sd m_od m_drift m_bad <<< "$REPLY"
            if [[ "$m_bad" != "1" ]]; then
              # 開始オフセットの関係差 (calc_drift と同じ式を start に適用)。
              # 計算に失敗したら降格しない (検査できなかったときに ok へ倒さない)
              local s_sv s_sa s_ov s_oa s_drift="" s_bad=1
              __av1ify_start_time "$src_path" "v:0"; s_sv="$REPLY"
              __av1ify_start_time "$src_path" "a:0"; s_sa="$REPLY"
              __av1ify_start_time "$filepath" "v:0"; s_ov="$REPLY"
              __av1ify_start_time "$filepath" "a:0"; s_oa="$REPLY"
              if __av1ify_calc_drift "$s_sv" "$s_sa" "$s_ov" "$s_oa" "$threshold"; then
                read -r _ _ s_drift s_bad <<< "$REPLY"
              fi
              if [[ "$s_bad" == "1" ]]; then
                # 終端は揃っているが開始がずれている = 時間シフト型の本物。降格しない
                print -r -- ">> 音ズレ判定: packet 実測の終端は Δ=${m_drift}s だが音声開始のシフト Δ=${s_drift:-?}s が閾値超過のため音ズレと判定 (時間シフト型)"
              else
                print -r -- ">> 音ズレ判定: 宣言 duration ベースでは Δ=${drift_v}s だが packet 実測では Δ=${m_drift}s のため正常と判定 (ソースの宣言 duration が不正確)"
                sd_v="$m_sd"; od_v="$m_od"; drift_v="$m_drift"; drift_bad="$m_bad"
              fi
            else
              sd_v="$m_sd"; od_v="$m_od"; drift_v="$m_drift"; drift_bad="$m_bad"
            fi
          fi
        fi

        if [[ "$drift_bad" == "1" ]]; then
          issues+=("音ズレ疑い (src_delta=${sd_v}s out_delta=${od_v}s Δ=${drift_v}s threshold=${threshold}s)")
          suffixes+=("avsync")
        fi
      else
        # awk 失敗時は無言スキップ (他のチェックに委ねる)
        print -ru2 -- "⚠️ A/V drift計算スキップ (awk失敗)"
      fi
    fi
    # ソース duration が両方とも取得不能 (= 安価パスも packet 走査も失敗) の超レアケースは
    # 判定スキップ。絶対値 fallback を入れるとソース音ズレ素材で誤検出が再発するため敢えて入れない。
  fi

  # ソースとの再生時間比較
  if [[ -n "$src_path" ]]; then
    local src_fmt_dur out_fmt_dur dur_diff
    src_fmt_dur=$(__ff_format_field "$src_path" format=duration)
    out_fmt_dur=$(__ff_format_field "$filepath" format=duration)
    if [[ -n "$src_fmt_dur" && -n "$out_fmt_dur" ]]; then
      dur_diff=$(awk -v s="$src_fmt_dur" -v o="$out_fmt_dur" 'BEGIN{ if (s=="" || o=="") exit 1; d=s-o; if (d<0) d=-d; printf "%.3f", d }' 2>/dev/null) || dur_diff=""
      if [[ -n "$dur_diff" ]]; then
        local dur_threshold="${AV1IFY_DURATION_TOLERANCE:-2.0}"
        local -F dur_diff_f dur_threshold_f
        dur_diff_f=$dur_diff
        dur_threshold_f=$dur_threshold
        if (( dur_diff_f > dur_threshold_f )); then
          issues+=("再生時間ズレ (src=${src_fmt_dur}s, out=${out_fmt_dur}s, Δ=${dur_diff}s)")
          suffixes+=("duration")
        fi
      fi
    fi
  fi

  # フレーム数比較（fps変更なしの場合のみ）
  if [[ -n "$src_path" ]] && (( ! fps_changed )); then
    local src_frames out_frames
    src_frames=$(__ff_stream_field "$src_path" v:0 stream=nb_frames)
    out_frames=$(__ff_stream_field "$filepath" v:0 stream=nb_frames)
    if [[ -n "$src_frames" && "$src_frames" =~ ^[0-9]+$ && -n "$out_frames" && "$out_frames" =~ ^[0-9]+$ ]]; then
      local frame_diff=$(( src_frames > out_frames ? src_frames - out_frames : out_frames - src_frames ))
      # 許容値 = max(絶対フロア, ソースの相対%)。固定 24 だけだと長尺に厳しすぎる
      # (Δ=88 でも 25分@30fps なら 0.1% 台。nb_frames はコンテナメタデータでソース側が
      # 不正確なことも多い)。「途中で切れた」級の実害は再生時間ズレ (AV1IFY_DURATION_TOLERANCE)
      # が独立に守るため、ここは相対で緩めても事故は素通りしない。
      local frame_tolerance="${AV1IFY_FRAME_TOLERANCE:-24}"
      local frame_tol_pct="${AV1IFY_FRAME_TOLERANCE_PCT:-0.5}"
      # shellcheck disable=SC2079  # zsh の (( )) は小数を扱える (bash 前提の誤検知)
      local -i rel_tolerance=$(( src_frames * frame_tol_pct / 100.0 )) # -i 代入で切り捨て
      (( rel_tolerance > frame_tolerance )) && frame_tolerance=$rel_tolerance
      if (( frame_diff > frame_tolerance )); then
        issues+=("フレーム数不一致 (src=${src_frames}, out=${out_frames}, Δ=${frame_diff}, 許容=${frame_tolerance})")
        suffixes+=("frames")
      fi
    fi
  fi

  # 出力解像度の検証
  if [[ -n "$expected_height" ]]; then
    local out_w out_h out_short
    out_w=$(__ff_stream_field "$filepath" v:0 stream=width)
    out_h=$(__ff_stream_field "$filepath" v:0 stream=height)
    if [[ -n "$out_w" && "$out_w" =~ ^[0-9]+$ && -n "$out_h" && "$out_h" =~ ^[0-9]+$ ]]; then
      if (( out_h > out_w )); then
        out_short=$out_w
      else
        out_short=$out_h
      fi
      if (( out_short != expected_height )); then
        issues+=("解像度不一致 (期待=${expected_height}p, 実際=${out_short}p, ${out_w}x${out_h})")
        suffixes+=("resolution")
      fi
    fi
  fi

  # ファイルサイズの妥当性チェック
  if [[ -n "$src_path" && -f "$src_path" && -f "$filepath" ]]; then
    local src_size out_size
    # __av1ify_file_size は _av1ify_encode.zsh 定義 (読み込み順は postcheck → encode
    # だが、呼び出しは av1ify 実行時 = 全ファイル source 済みなので解決できる)
    src_size=$(__av1ify_file_size "$src_path") || src_size=""
    out_size=$(__av1ify_file_size "$filepath") || out_size=""
    if [[ -n "$src_size" && "$src_size" =~ ^[0-9]+$ && -n "$out_size" && "$out_size" =~ ^[0-9]+$ ]] && (( src_size > 0 )); then
      local size_ratio
      size_ratio=$(awk -v o="$out_size" -v s="$src_size" 'BEGIN{ printf "%.4f", o / s }')
      local min_ratio="${AV1IFY_MIN_SIZE_RATIO:-0.001}"
      local too_small
      too_small=$(awk -v r="$size_ratio" -v m="$min_ratio" 'BEGIN{ print (r < m) ? 1 : 0 }')
      if (( too_small )); then
        issues+=("ファイルサイズ異常 (src=${src_size}B, out=${out_size}B, ratio=${size_ratio})")
        suffixes+=("tinyfile")
      elif (( out_size > src_size )); then
        local pct_increase
        pct_increase=$(awk -v o="$out_size" -v s="$src_size" 'BEGIN{ printf "%.0f", (o - s) / s * 100 }')
        issues+=("サイズ増加 (src=${src_size}B, out=${out_size}B, +${pct_increase}%)")
        suffixes+=("bigger")
      fi
    fi
  fi

  # 出力映像コーデックの検証
  local out_vcodec
  out_vcodec=$(__ff_stream_field "$filepath" v:0 stream=codec_name)
  if [[ -n "$out_vcodec" && "${out_vcodec:l}" != "av1" ]]; then
    issues+=("映像コーデック不一致 (期待=av1, 実際=${out_vcodec})")
    suffixes+=("codec")
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
    __AV1IFY_LAST_NG_REASON="変換後チェック NG: $issues_joined"
    REPLY="$new_path"
    return 1
  fi

  return 0
}
