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
#       $3 = force_full (省略時 0)。1 を渡すと末尾区間の seek 最適化を使わず、
#       最初から全 packet 走査する (下記 🚨 参照)。
# 出力: REPLY = 表示終端 max(pts_time + duration_time) [秒] (取得不能なら空)
# 戻り値: 0=取得成功, 1=取得不能
#
# 🚨 seek 最適化 (末尾 60s だけ読む) は、宣言 duration 自体が水増しされた壊れた
# コンテナ (idx1 が実データを超える AVI 等) では**実データの終端を素通りして
# 存在しない位置へ着地し、宣言値に近い誤った値を返す**ことがある (実測 issue 412:
# 実終端 9312.67s のソースで seek 版が 9364.72s = 宣言値とほぼ同値を返した)。
# この場合 __av1ify_is_num は通ってしまう (値は構文的に正常) ため、呼び出し側は
# 「値が取れたか」では検出できない。既に閾値超過で再測定に入っている経路
# (vidloss/avsync の降格判定) でさらに疑わしいときは、force_full=1 で
# 全走査に強制すること。全走査は demux のみ (デコードなし) なので
# 1.5GB クラスでも 1 秒未満で終わる (実測)。
__av1ify_packet_end() {
  local file="$1" spec="$2" force_full="${3:-0}" val fmt_dur start
  # 🚨 「packet 列の最後の行の pts_time」を終端に使わないこと。
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
  # force_full=1 のときはこの最適化を丸ごとスキップして下の全走査へ進む。
  if (( ! force_full )); then
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

# 内部補助: 実パケット数を取得 (seek を使わない全走査)
#
# frames チェック (下記) の再測定専用。コンテナの宣言 nb_frames が壊れている
# (idx1 が実データを超えて水増しされた AVI 等) と、実際のフレーム数と乖離しうる
# (実測 issue 412: 宣言 280,661 に対し実パケット数 278,888)。demux のみで
# デコードを伴わないため、1.5GB クラスでも 1 秒未満で終わる (実測)。
#
# 引数: $1 = ファイルパス, $2 = stream specifier (例: v:0)
# 出力: REPLY = 実パケット数 (取得不能なら空)
# 戻り値: 0=取得成功, 1=取得不能
#
# 🚨 既知の限界 (issue 412 の敵対的レビューで指摘): 測っているのは「パケット数」で
# あって「フレーム数」そのものではない。映像ストリームでは 1 packet = 1 frame が
# 一般的な前提 (`nb_frames` 自体も多くの実装でこれを前提に算出される) だが、
# コーデック/コンテナによっては 1 packet に複数フレームが入る、あるいは 1 フレームが
# 複数 packet に分割される形がありうる。その場合、実フレームが欠落していても
# パケット数の差が許容内に収まり降格してしまう可能性がある。全走査でのデコードは
# 「壊れた index の影響を受けない」利点と引き換えに遅く、かつ本 issue の実ファイルの
# ように**ビットストリーム自体に別の破損 (corrupt decoded frame 等) がある場合は
# デコード結果自体が信用できない**ため、意図的にデコードを避けて packet 数で妥協している。
__av1ify_count_packets() {
  local file="$1" spec="$2" val
  val=$(ffprobe -v error -select_streams "$spec" -count_packets \
        -show_entries stream=nb_read_packets -of default=nk=1:nw=1 -- "$file" 2>/dev/null | head -n1)
  if [[ "$val" =~ ^[0-9]+$ ]]; then
    REPLY="$val"
    return 0
  fi
  REPLY=""
  return 1
}

# 内部補助: 映像ストリームの「尺」[秒] を返す (終端時刻ではない)
#
# 🚨 __av1ify_get_stream_end と混同しないこと。あちらは**終端時刻**を返し、
# 経路によって意味が変わる: stream=duration が在れば「長さ」、無ければ
# __av1ify_packet_end の max(pts + duration) = **start_time を含む絶対時刻**。
# 後者を尺とみなして fps を掛けると、start_time ぶん期待値が過大になる
# (実測 2026-09-19: -output_ts_offset 600 の mkv で vend=620.0 → 期待値 21266 フレーム、
#  実デコードは 686)。影響するのは stream duration を持たない MKV / FLV / WebM。
#
# 引数: $1 = ファイルパス
# 出力: REPLY = 尺 [秒] (取得不能なら空)
# 戻り値: 0=取得成功, 1=取得不能
__av1ify_video_span() {
  local file="$1" val
  REPLY=""
  # 宣言 duration は既に「長さ」なのでそのまま使う (start_time を引かない)
  val=$(__ff_stream_field "$file" v:0 stream=duration)
  if __av1ify_is_num "$val"; then
    REPLY="$val"
    return 0
  fi
  # packet 実測は絶対時刻なので start_time を引いて長さにする
  if __av1ify_packet_end "$file" "v:0"; then
    local vend="$REPLY" vstart
    __av1ify_start_time "$file" "v:0"; vstart="$REPLY"
    REPLY=$(LC_ALL=C awk -v e="$vend" -v s="$vstart" 'BEGIN { d = e - s; if (d < 0) d = 0; printf "%.6f", d }')
    __av1ify_is_num "$REPLY" && return 0
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
  # 第5引数: fps を変更したとき、実際に ffmpeg へ渡した fps (有理数でも 10 進でも可)。
  # これが在ると、フレーム数チェックを捨てる代わりに密度検査 (下記) を行う (issue 397)。
  local applied_fps="${5:-}"
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

    # 開始オフセットの関係差 (calc_drift と同じ式を start_time に適用)。
    # 宣言/終端の drift とは**独立した第 2 の avsync 信号**で、duration が測れたかに
    # 関わらず常時走らせる: 音声を後ろへシフトしつつ長さを映像に近づけると宣言 drift が
    # 閾値未満に収まり初回ゲートを潜るが、開始オフセットには必ず出る
    # (issue 066: 3.8s シフト + 音声長≒映像 で宣言 drift 1.88s < 2.0)。
    # __av1ify_start_time は N/A を 0 に正規化するので calc_drift 入力は常に数値。
    # 計算失敗 (awk 破損等) は s_bad=0 = 「開始のズレは判定不能、宣言/終端に委ねる」とする
    # (常時チェックなので、失敗を FLAG に倒すと全ファイルが誤検知になる)。
    # ただし「測れなかった (s_measured=0)」と「測って閾値内だった (s_bad=0)」は別物なので
    # 分けて持つ。降格ゲート (下の再判定) は後者でしか降格してはならない。1 変数で兼ねると
    # 常時判定の fail-open が降格ゲートの fail-closed を黙って壊す (058 の不変条件が消える)。
    # 🚨 関係差 (a_start − v_start の src/out 差分) で見ること。絶対 start では TS の
    # PCR 由来ベースオフセット (実測: video 1.423 / audio 1.400 が mp4 出力で 0 付近へ
    # 正規化される) や AAC priming をまるごと音ズレと誤検知する。関係差なら実測 0.0098s。
    local s_sv s_sa s_ov s_oa s_drift="" s_bad=0 s_measured=0
    __av1ify_start_time "$src_path" "v:0"; s_sv="$REPLY"
    __av1ify_start_time "$src_path" "a:0"; s_sa="$REPLY"
    __av1ify_start_time "$filepath" "v:0"; s_ov="$REPLY"
    __av1ify_start_time "$filepath" "a:0"; s_oa="$REPLY"
    if __av1ify_calc_drift "$s_sv" "$s_sa" "$s_ov" "$s_oa" "$threshold"; then
      read -r _ _ s_drift s_bad <<< "$REPLY"
      s_measured=1
    fi

    local sd_v="" od_v="" drift_v="" drift_bad=0
    if __av1ify_is_num "$src_v" && __av1ify_is_num "$src_a"; then
      # 符号付きで関係差を見る (方向反転も検出)
      if __av1ify_calc_drift "$src_v" "$src_a" "$out_v" "$out_a" "$threshold"; then
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
          # 🚨 packet 実測が測るのは「表示終端」で、ストリームの「長さ」ではない。
          # 音声の先頭が落ちて edit-list 遅延が書かれ終端は映像と揃っている時間シフト型は
          # 終端 Δ に出ない (実測 issue 058)。そのため降格 (FLAG→ok) は「終端が揃っている」
          # かつ「開始の関係差を実際に測れて (s_measured) 閾値内だった (s_bad != 1)」の
          # 2 条件で行う。測れなかったときは降格しない (検査できなかったときに ok へ倒さない)。
          # 開始のズレ自体は上の常時チェックが最終判定でも拾うが、ここで見ないと
          # 「正常と判定」と報告した直後に avsync で NG にする矛盾した出力になる。
          local m_sv m_sa m_ov m_oa
          if __av1ify_packet_end "$src_path" "v:0" && m_sv="$REPLY" \
            && __av1ify_packet_end "$src_path" "a:0" && m_sa="$REPLY" \
            && __av1ify_packet_end "$filepath" "v:0" && m_ov="$REPLY" \
            && __av1ify_packet_end "$filepath" "a:0" && m_oa="$REPLY" \
            && __av1ify_calc_drift "$m_sv" "$m_sa" "$m_ov" "$m_oa" "$threshold"; then
            local m_sd m_od m_drift m_bad
            read -r m_sd m_od m_drift m_bad <<< "$REPLY"
            if [[ "$m_bad" != "1" && "$s_measured" == "1" && "$s_bad" != "1" ]]; then
              # 終端も開始も揃っている = 宣言 duration が不正確だっただけ。降格する
              print -r -- ">> 音ズレ判定: 宣言 duration ベースでは Δ=${drift_v}s だが packet 実測では Δ=${m_drift}s のため正常と判定 (ソースの宣言 duration が不正確)"
              sd_v="$m_sd"; od_v="$m_od"; drift_v="$m_drift"; drift_bad="$m_bad"
            elif [[ "$m_bad" == "1" ]]; then
              # packet 実測でもズレが残る = 本物。実測値へ更新して FLAG 維持
              sd_v="$m_sd"; od_v="$m_od"; drift_v="$m_drift"; drift_bad="$m_bad"
            fi
            # 残り (m_bad!=1 かつ 開始がズレている / 測れなかった) = 時間シフト型もしくは
            # 判定不能: 降格せず drift_bad=1 のまま (下で FLAG)
          fi
        fi
      else
        # awk 失敗時は無言スキップ (他のチェックに委ねる)
        print -ru2 -- "🚨 A/V drift計算スキップ (awk失敗)"
      fi
    fi
    # ソース duration が両方とも取得不能 (= 安価パスも packet 走査も失敗) の超レアケースは
    # duration 側の判定をスキップ (drift_bad=0 のまま)。絶対値 fallback を入れるとソース
    # 音ズレ素材で誤検出が再発するため敢えて入れない。開始シフトの判定は duration を必要と
    # しないので、このケースでも上の常時チェックが生きている。

    # 最終判定: 宣言/終端の drift か、開始オフセットのシフトのどちらかで音ズレ
    # (issue 066: drift_bad==0 でも s_bad==1 ならここで拾う)
    if [[ "$drift_bad" == "1" || "$s_bad" == "1" ]]; then
      local reason
      if [[ "$drift_bad" == "1" && "$s_bad" == "1" ]]; then
        reason="src_delta=${sd_v}s out_delta=${od_v}s Δ=${drift_v}s + 開始シフト Δ=${s_drift}s"
      elif [[ "$s_bad" == "1" ]]; then
        reason="開始シフト Δ=${s_drift}s (音声が ${s_drift}s ずれて始まる)"
      else
        reason="src_delta=${sd_v}s out_delta=${od_v}s Δ=${drift_v}s"
      fi
      issues+=("音ズレ疑い (${reason} threshold=${threshold}s)")
      suffixes+=("avsync")
    fi
  fi

  # 映像ストリーム尺の直接比較 (音声の状態に一切依存しない)
  #
  # 上の avsync はソースとの「相対差」(音声-映像ギャップの変化) を見るため、
  # 音声も映像と同程度に短くなった場合は drift が小さいまま見逃しうる。
  # 「ソースとの再生時間比較」(下のブロック、format duration) はコンテナ全体の
  # 長さを見るため、音声だけ満尺のまま残ると映像の欠落が隠れる (実例: 別マシンで
  # のエンコードで映像が 1:14:14 で打ち切られたが音声は満尺のまま残り、format
  # duration は音声側に引きずられてソースとほぼ一致していた)。
  # この検査は音声を一切参照せず、映像ストリームの尺だけをソースと直接突き合わせる。
  if [[ -n "$src_path" && -f "$src_path" ]] && __av1ify_is_num "$out_v"; then
    local src_v_direct
    if __av1ify_get_stream_end "$src_path" "v:0"; then
      src_v_direct="$REPLY"
      local vidloss_threshold="${AV1IFY_VIDEO_LOSS_TOLERANCE:-2.0}"
      __av1ify_is_nonneg_num "$vidloss_threshold" || vidloss_threshold=2.0
      local vidloss_diff
      vidloss_diff=$(awk -v s="$src_v_direct" -v o="$out_v" 'BEGIN{ d=s-o; if (d<0) d=-d; printf "%.3f", d }' 2>/dev/null) || vidloss_diff=""
      if [[ -n "$vidloss_diff" ]]; then
        local -F vidloss_diff_f vidloss_threshold_f
        vidloss_diff_f=$vidloss_diff
        vidloss_threshold_f=$vidloss_threshold
        if (( vidloss_diff_f > vidloss_threshold_f )); then
          # 宣言 duration (mp4 の mdhd/tkhd 等) がサンプルテーブルと食い違う壊れた
          # ソース/出力が実在する (avsync の再判定と同じ罠)。これを encode 由来の
          # 映像欠落と誤検出しないよう、閾値超過時だけ packet 実測で再判定する。
          local vidloss_bad=1 m_src_v m_out_v
          if __av1ify_packet_end "$src_path" "v:0" && m_src_v="$REPLY" \
            && __av1ify_packet_end "$filepath" "v:0" && m_out_v="$REPLY"; then
            local m_diff
            m_diff=$(awk -v s="$m_src_v" -v o="$m_out_v" 'BEGIN{ d=s-o; if (d<0) d=-d; printf "%.3f", d }' 2>/dev/null) || m_diff=""
            if [[ -n "$m_diff" ]]; then
              local -F m_diff_f
              m_diff_f=$m_diff
              if (( m_diff_f <= vidloss_threshold_f )); then
                # 終端 (packet 実測) は揃っている = 宣言 duration が不正確だっただけ。降格する
                print -r -- ">> 映像尺判定: 宣言 duration ベースでは Δ=${vidloss_diff}s だが packet 実測では Δ=${m_diff}s のため正常と判定 (宣言 duration が不正確)"
                vidloss_bad=0
              else
                # 末尾区間の packet 実測 (seek 最適化) でも欠落が残る。ただし
                # この seek 最適化自体が、宣言 duration が水増しされた壊れた
                # コンテナ (idx1 が実データを超える AVI 等) では実データの終端を
                # 素通りして宣言値に近い誤った値を再現することがある
                # (実測 issue 412: 区間 seek 版が宣言値とほぼ同値の 9364.72s を
                # 返し、全走査だと実際の終端 9312.67s が出た)。この経路に来るのは
                # 既に2段階の閾値超過を経た後なので、全走査に強制して最終確認する。
                local f_src_v f_out_v
                if __av1ify_packet_end "$src_path" "v:0" 1 && f_src_v="$REPLY" \
                  && __av1ify_packet_end "$filepath" "v:0" 1 && f_out_v="$REPLY"; then
                  local f_diff
                  f_diff=$(awk -v s="$f_src_v" -v o="$f_out_v" 'BEGIN{ d=s-o; if (d<0) d=-d; printf "%.3f", d }' 2>/dev/null) || f_diff=""
                  if [[ -n "$f_diff" ]]; then
                    local -F f_diff_f
                    f_diff_f=$f_diff
                    if (( f_diff_f <= vidloss_threshold_f )); then
                      print -r -- ">> 映像尺判定: 末尾区間の packet 実測でも Δ=${m_diff}s だったが、全走査では Δ=${f_diff}s のため正常と判定 (区間 seek が壊れた index に着地していた)"
                      vidloss_bad=0
                    fi
                    vidloss_diff="$f_diff"
                  else
                    vidloss_diff="$m_diff"
                  fi
                else
                  # 全走査自体が失敗した場合は判定不能。区間 seek の実測値を維持する
                  # (降格には実測の裏付けが要る)。
                  vidloss_diff="$m_diff"
                fi
              fi
            fi
            # m_diff が算出不能 (awk 失敗) なケースは vidloss_bad=1 のまま (宣言ベースを維持)
          fi
          # packet 実測自体が失敗した場合も vidloss_bad=1 のまま (判定不能を降格の
          # 根拠にしない。既に FLAG 側にいる状態からは、降格には実測の裏付けが要る)
          if (( vidloss_bad )); then
            issues+=("映像ストリーム尺不一致 (src_v=${src_v_direct}s, out_v=${out_v}s, Δ=${vidloss_diff}s, threshold=${vidloss_threshold}s)")
            suffixes+=("vidloss")
          fi
        fi
      fi
    fi
    # src_v_direct が取得不能 (packet 走査も失敗) なケースは判定スキップ。
    # 他の A/V チェックと同じ fail-open 方針 (誤検知よりスキップを優先)。
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
          # 宣言 format=duration 自体が水増しされた壊れたコンテナ (idx1 が実データを
          # 超える AVI 等) だと、この不一致は encode 由来ではなく宣言値の誤りに
          # すぎないことがある (実測 issue 412)。閾値超過時だけ、映像・音声それぞれの
          # 実終端 (全走査) から「真の再生時間」を測り直して再確認する。
          # 🚨 音声も見ること: vidloss は映像だけを見るため、映像は完全一致していて
          # 音声だけ欠落/延長しているケースを検出できない (このブロックの上のコメント
          # 参照)。ここで映像だけ再測定すると同じ穴が復活するので、音声ストリームが
          # 在るなら必ず一緒に測る。出力 (av1ify が直後に生成した mp4) の宣言値は
          # 信頼できるとみなし、ソース側だけ実測し直す。
          local dur_bad=1 true_src_v
          if __av1ify_packet_end "$src_path" "v:0" 1 && true_src_v="$REPLY"; then
            local true_src_dur="$true_src_v" true_measured=1
            if [[ -n "$audio_stream" ]]; then
              local true_src_a
              if __av1ify_packet_end "$src_path" "a:0" 1 && true_src_a="$REPLY"; then
                local a_bigger
                a_bigger=$(awk -v a="$true_src_a" -v v="$true_src_dur" 'BEGIN{ print (a>v)?1:0 }' 2>/dev/null) || a_bigger=0
                (( a_bigger )) && true_src_dur="$true_src_a"
              else
                # 音声ストリームが在るのに実測できない = 判定不能。降格しない。
                true_measured=0
              fi
            fi
            if (( true_measured )); then
              local true_dur_diff
              true_dur_diff=$(awk -v s="$true_src_dur" -v o="$out_fmt_dur" 'BEGIN{ d=s-o; if (d<0) d=-d; printf "%.3f", d }' 2>/dev/null) || true_dur_diff=""
              if [[ -n "$true_dur_diff" ]]; then
                local -F true_dur_diff_f
                true_dur_diff_f=$true_dur_diff
                if (( true_dur_diff_f <= dur_threshold_f )); then
                  print -r -- ">> 再生時間判定: 宣言 format=duration では Δ=${dur_diff}s だが実測 (映像/音声終端の全走査) では Δ=${true_dur_diff}s のため正常と判定 (宣言 duration が水増し)"
                  dur_bad=0
                fi
              fi
            fi
          fi
          # 実測自体が失敗した場合も dur_bad=1 のまま (判定不能を降格の根拠にしない。
          # 既存の vidloss 降格と同じ fail-safe 方針)。
          if (( dur_bad )); then
            issues+=("再生時間ズレ (src=${src_fmt_dur}s, out=${out_fmt_dur}s, Δ=${dur_diff}s)")
            suffixes+=("duration")
          fi
        fi
      fi
    fi
  fi

  # フレーム密度比較 (fps を変更した場合)。
  #
  # 🚨 fps を変えたからといって検査を丸ごと捨てないこと (issue 397)。フレーム数の比較は
  # このパイプライン唯一の「密度」検査で、他 (duration / vidloss / avsync) は全て
  # エンドポイント検査なので代替にならない。実測: 30fps/10s/300 フレームを -r 1 で
  # 作り直すと 12 フレーム (96% 欠落) になるが、duration の差はちょうど 2.000s で
  # 閾値を超えず、check_ng に転ばないまま**元ファイルが削除される**。
  # CFR retiming の duration 変位はターゲット fps の 1〜2 フレーム間隔が上限で、
  # 尺に比例しない (120 秒素材でも同じ 2.000s) ため、duration ではこの帯を原理的に拾えない。
  #
  # 🚨 期待値を「ソース尺 × 適用fps」で立てないこと。適用fps は **我々が -r に渡した値と
  # 同一の変数**なので、その値が壊れると ffmpeg の出力と期待値が一緒に壊れ、必ず一致する
  # (= 守りたい故障クラスを原理的に検出できない。2026-09-19 の敵対レビュー 2 周目が
  # 決定点への変異で実証: 514 フレームのソースが 202 フレームになっても ✅ 完了 だった)。
  # 🚨 もう一方の死角 (逆向き): nb_frames を出さないコンテナ (MKV / FLV / WebM) では尺ベースしか
  # 使えないが、ffprobe が返す avg_frame_rate が**公称値**のことがある (実測 2026-09-19:
  # 真の平均 34.3 のソースで avg=40/1)。その場合期待値が構造的に過大になり、正しいエンコードが
  # check_ng-density に転びうる。現状これらのコンテナは r==avg で VFR 判定に掛からないため
  # production からは到達しないが、検出側の射程が広がったら**ここを先に直すこと** (issue 397)。
  # 🚨 射程: この検査が拾えるのは**粗い fps 誤り**だけ。許容が max(フロア 24, 期待値 × 5%) なので、
  # 実測で 300 フレーム (10s/30fps) なら 8% 未満、686 フレーム (20s) なら 5% 未満の fps 誤りは
  # 無警告で通る。issue 397 が名指しした実害 (-r 1 で 96% 欠落) は確実に拾えるが、
  # 「誤った -r の検出」一般を満たすわけではない (微小な誤差は duration 検査の担当)。
  # 期待値は**加害変数を経由しない量**から立てる: ソースのフレーム数そのもの。
  # avg_frame_rate の定義が nb_frames / duration なので、ソースをその avg で CFR 化した
  # 出力のフレーム数は元と (丸めを除いて) 一致する。誤った fps を渡せばここがズレる。
  if [[ -n "$src_path" ]] && (( fps_changed )) && [[ -n "$applied_fps" ]]; then
    local src_frames_d out_frames_d
    src_frames_d=$(__ff_stream_field "$src_path" v:0 stream=nb_frames)
    out_frames_d=$(__ff_stream_field "$filepath" v:0 stream=nb_frames)

    # 期待値の第 2 候補: 映像の実測終端 × ソース側の avg_frame_rate。
    # 🚨 format=duration を使わないこと: コンテナの duration は**全ストリームの最大**なので、
    # 音声が映像より長い素材で期待値が過大になり、1 フレームも失っていない出力が
    # check_ng-density に転ぶ (2026-09-19 実測: 映像 60s / 音声 64s の VFR mp4 で発生)。
    # 映像の尺は __av1ify_video_span が返す (終端時刻ではなく長さ。start_time の扱いは同関数の注記)。
    # avg はソースから読み直す (applied_fps を使うと、決定点が壊れたとき期待値も一緒に
    # 壊れて必ず一致する = 守りたい故障を検出できない)。
    local want_by_duration=""
    local src_vend=""
    if __av1ify_video_span "$src_path"; then src_vend="$REPLY"; fi
    if __av1ify_is_num "$src_vend"; then
      local src_avg_raw
      src_avg_raw=$(__ff_stream_field "$src_path" v:0 stream=avg_frame_rate)
      want_by_duration=$(LC_ALL=C awk -v dur="$src_vend" -v fps="$src_avg_raw" 'BEGIN {
        n = split(fps, a, "/")
        f = (n == 2) ? ((a[2]+0 > 0) ? a[1] / a[2] : 0) : a[1]+0
        if (f <= 0 || dur <= 0) exit 1
        printf "%d", int(dur * f + 0.5)
      }') || want_by_duration=""
    fi

    # 期待値の第 1 候補はソースの nb_frames。ただし **無条件には信じない**。
    # 🚨 mp4 の nb_frames は stsz のサンプル数であって「デコードされるフレーム数」ではない。
    # edit list (elst) があると ffmpeg は提示区間だけを出すので両者が乖離する。
    # `ffmpeg -ss N -i x.mp4 -c copy` は elst を書くので、「スマホ動画を切ってから
    # av1ify に食わせる」という普通の運用でこれが起きる (2026-09-19 実測: 20s/800 frames を
    # -ss 5 で切ると duration=15.0 / nb_frames=800 / 実デコード 620。素朴に信じると
    # 正しいエンコードが check_ng-density に転び、再実行のたびフルエンコードが走り続ける)。
    # 乖離は duration × avg と突き合わせれば分かる (上の実測で 800 vs 598 と明確に割れる)
    # ので、食い違ったら nb_frames を捨てて duration ベースへ倒す。
    # 🚨 許容の env は **両方の窓が使う前に 1 回だけ**検証する。片方 (密度) だけ検証して
    # もう片方 (trust) が生値を読むと、__av1ify_is_nonneg_num が通さない表記 (1e9 等) で
    # 二つの窓が別々の値を使い、「trust は密度の半分」という不変条件が反転する
    # (実測 2026-09-19: AV1IFY_DENSITY_TOLERANCE_PCT=1e9 で trust は無条件採用・密度は
    #  既定 5% に落ち、正しいエンコードが check_ng-density に転んだ)。
    local density_pct="${AV1IFY_DENSITY_TOLERANCE_PCT:-5}"
    __av1ify_is_nonneg_num "$density_pct" || density_pct=5
    local density_floor="${AV1IFY_DENSITY_FLOOR:-24}"
    __av1ify_is_nonneg_num "$density_floor" || density_floor=24

    local want_frames=""
    if [[ "$src_frames_d" =~ ^[0-9]+$ ]] && (( src_frames_d > 0 )); then
      if [[ "$want_by_duration" =~ ^[0-9]+$ ]] && (( want_by_duration > 0 )); then
        local nb_trusted
        # 🚨 ここの許容を密度検査の許容と同値にしないこと。採用した nb_frames は最大で
        # この許容ぶんズレているので、同値だと「nb を信じた直後に、正しいエンコードの
        # 丸め 1〜2 フレームで密度検査を突き抜ける」崖ができる (実測 2026-09-19:
        # ズレ 25 なら却下されて助かり、24 なら採用されて check_ng に転ぶ)。
        # 密度側の半分にして、採用した nb の残差ぶんの余裕を必ず残す。
        nb_trusted=$(LC_ALL=C awk -v nb="$src_frames_d" -v dur_based="$want_by_duration" \
                                  -v pct="$density_pct" -v floor="$density_floor" 'BEGIN {
          d = nb - dur_based; if (d < 0) d = -d
          tol = dur_based * pct / 200; if (tol < floor / 2) tol = floor / 2
          print (d > tol) ? 0 : 1
        }')
        if (( nb_trusted )); then
          want_frames="$src_frames_d"
        else
          want_frames="$want_by_duration"
          print -r -- ">> nb_frames (${src_frames_d}) が尺と矛盾するため密度の期待値に使いません (edit list 等。尺ベース=${want_by_duration})"
        fi
      else
        want_frames="$src_frames_d"
      fi
    else
      # nb_frames を出さないコンテナ (MKV / TS / FLV 等) はこちらだけが頼り
      want_frames="$want_by_duration"
    fi

    if [[ "$out_frames_d" =~ ^[0-9]+$ && "$want_frames" =~ ^[0-9]+$ ]] && (( want_frames > 0 )); then
      # フロアが密度検査**専用**の変数である理由 (issue 398 と同型): ユーザー向けに
      # 「変換前後のフレーム数差の許容」として文書化済みの AV1IFY_FRAME_TOLERANCE を
      # 兼ねさせると、それを緩めた瞬間にこの破壊的判定が黙って無効化される。
      local density_out
      density_out=$(LC_ALL=C awk -v want="$want_frames" -v out="$out_frames_d" \
                                 -v pct="$density_pct" -v floor="$density_floor" 'BEGIN {
        if (want <= 0) exit 1
        d = want - out; if (d < 0) d = -d
        tol = want * pct / 100
        if (tol < floor) tol = floor
        printf "%d %d", int(d + 0.5), (d > tol) ? 1 : 0
      }') || density_out=""
      if [[ -n "$density_out" ]]; then
        local d_diff d_bad
        read -r d_diff d_bad <<< "$density_out"
        if (( d_bad )); then
          issues+=("フレーム密度不一致 (期待≈${want_frames}, out=${out_frames_d}, Δ=${d_diff}, 適用fps=${applied_fps})")
          suffixes+=("density")
        fi
      else
        print -r -- ">> フレーム密度は判定不能 (期待=${want_frames}, out=${out_frames_d})" >&2
      fi
    else
      # 🚨 ガードが偽のときも黙って消さない。「判定不能」は合格ではない (issue 397)。
      print -r -- ">> フレーム密度は判定不能 (ソースのフレーム数を測れない: src=${src_frames_d:-N/A}, out=${out_frames_d:-N/A})" >&2
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
        # 宣言 nb_frames 自体が水増しされた壊れたコンテナ (idx1 が実データを超える
        # AVI 等) だと、この不一致は encode 由来の欠落ではなく宣言値の誤りにすぎない
        # (実測 issue 412)。閾値超過時だけ実パケット数 (seek なし全走査) で再確認し、
        # そちらが許容内なら降格する。
        local true_src_frames true_out_frames frames_confirmed=1
        if __av1ify_count_packets "$src_path" v:0 && true_src_frames="$REPLY" \
          && __av1ify_count_packets "$filepath" v:0 && true_out_frames="$REPLY"; then
          local true_frame_diff=$(( true_src_frames > true_out_frames ? true_src_frames - true_out_frames : true_out_frames - true_src_frames ))
          if (( true_frame_diff <= frame_tolerance )); then
            frames_confirmed=0
            print -r -- ">> フレーム数判定: 宣言 nb_frames では Δ=${frame_diff} だが実パケット数 (全走査) では Δ=${true_frame_diff} のため正常と判定 (宣言 nb_frames が水増し。src実測=${true_src_frames}, out実測=${true_out_frames})"
          fi
        fi
        # 実パケット数の取得自体が失敗した場合は判定不能。frames_confirmed=1 のまま
        # (降格には実測の裏付けが要る。既存の vidloss 降格と同じ fail-safe 方針)。
        if (( frames_confirmed )); then
          issues+=("フレーム数不一致 (src=${src_frames}, out=${out_frames}, Δ=${frame_diff}, 許容=${frame_tolerance})")
          suffixes+=("frames")
        fi
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
    print -r -- "🚨 チェック警告: $issues_joined"
    __AV1IFY_LAST_NG_REASON="変換後チェック NG: $issues_joined"
    REPLY="$new_path"
    return 1
  fi

  return 0
}
