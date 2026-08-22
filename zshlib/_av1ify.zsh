# shellcheck shell=bash
# ------------------------------------------------------------------------------
# av1ify — 入力された動画ファイル、またはディレクトリ内の動画ファイルをAV1形式のMP4に一括変換します。
# ------------------------------------------------------------------------------

__AV1IFY_VERSION="1.9.0"
__AV1IFY_SPEC_VERSION="1.9.0"

__av1ify_banner() {
  print -ru2 -- "av1ify v${__AV1IFY_VERSION} (spec: v${__AV1IFY_SPEC_VERSION})"
}

typeset -gi __AV1IFY_ABORT_REQUESTED=0
typeset -g  __AV1IFY_CURRENT_TMP=""
typeset -gi __AV1IFY_DRY_RUN=0
typeset -g  __AV1IFY_RESOLUTION=""
typeset -g  __AV1IFY_FPS=""
typeset -g  __AV1IFY_DENOISE=""
typeset -g  __AV1IFY_COLOR_TAGS=""
typeset -gi __AV1IFY_COMPACT=0
typeset -gi __AV1IFY_FORCE=0
typeset -gi __AV1IFY_DELETE_ORIGIN=0
# NG 発生時の理由文字列。__av1ify_one / __av1ify_postcheck が return 1 直前に設定し、
# バッチループ (__av1ify_run_batch) が末尾の NG 一覧で使用する。
typeset -g  __AV1IFY_LAST_NG_REASON=""
# 走行中の prefetch (バックグラウンド先読み) の PID。中断時に __av1ify_kill_prefetches でまとめて掃除する。
typeset -ga __AV1IFY_PREFETCH_PIDS=()
# 引数なし呼び出しでクリップボードから読み取った処理対象 (__av1ify_targets_from_clipboard)
typeset -ga __AV1IFY_CLIP_TARGETS=()
# __av1ify_resolve_pasted_path が置く「前後の空白だけ落とした原文」(一覧の併記用)
typeset -g __AV1IFY_PASTED_TRIMMED=""

# 内部補助: 次に処理予定のファイルを background で先読みし、
# Dropbox / iCloud の File Provider materialize を現エンコード中に進めておく。
#
# 仕組み: head -c 1 で open() させると File Provider が fetchContents を発火し、
# replicated extension モデルでは「全体ダウンロードが終わるまで read() がブロック」
# する。1 byte 読めば materialize は完了済みなので、プロセス終了後もファイルは
# ローカルに残る。cat /dev/null と違い、materialize 後にローカル SSD 全バイトを
# 再読みする無駄が無い。
#
# range-based fetch の File Provider (一部の iCloud 使い方など) では先頭 1 byte
# だけしか落ちない可能性があるが、その場合でも prefetch しない場合と比べて損は
# しないため、最悪ケースでも安全 (= "効かない" だけ)。
#
# 引数: $1 = 先読みしたいファイルパス
# 副作用: __AV1IFY_PREFETCH_PIDS に PID を追加 (中断時掃除用)
__av1ify_prefetch() {
  local target="$1"
  [[ -z "$target" || ! -f "$target" ]] && return
  (( __AV1IFY_DRY_RUN )) && return
  # 終了済み prefetch の PID をリストから間引く。長いバッチで PID を溜め込むと、
  # 中断時の __av1ify_kill_prefetches が「とっくに終了して OS に再利用された PID」へ
  # kill を打つ誤爆リスクが広がるため、生存中のものだけ持ち越す (best-effort)。
  local -a alive=()
  local pid
  for pid in "${__AV1IFY_PREFETCH_PIDS[@]}"; do
    kill -0 "$pid" 2>/dev/null && alive+=("$pid")
  done
  __AV1IFY_PREFETCH_PIDS=("${alive[@]}")
  ( head -c 1 < "$target" > /dev/null 2>&1 ) &
  __AV1IFY_PREFETCH_PIDS+=("$!")
}

# 内部補助: 走行中の prefetch を全て kill (中断時の掃除用)
# 既に終了している PID への kill は exit 1 を返すが、err_exit 環境 (テスト harness 等)
# でも安全に呼べるよう `|| true` で吸収する。
__av1ify_kill_prefetches() {
  local pid
  for pid in "${__AV1IFY_PREFETCH_PIDS[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
  __AV1IFY_PREFETCH_PIDS=()
}

__av1ify_on_interrupt() {
  if (( __AV1IFY_ABORT_REQUESTED )); then
    return
  fi
  __AV1IFY_ABORT_REQUESTED=1
  __av1ify_kill_prefetches
  local tmp="${__AV1IFY_CURRENT_TMP:-}"
  if [[ -n "$tmp" && -e "$tmp" ]]; then
    rm -f -- "$tmp"
    print -r -- "✋ 中断要求: 進行中の一時ファイルを削除しました ($tmp)"
  else
    print -r -- "✋ 中断要求: 残りの処理を停止します"
  fi
}

# 分割構成:
#   _av1ify_postcheck.zsh — __av1ify_mark_issue() + __av1ify_postcheck() (変換後チェック)
#   _av1ify_encode.zsh    — __av1ify_one() + エンコード補助ヘルパー群
#   _av1ify.zsh (本ファイル) — 状態変数, バナー, 割り込み処理, av1ify() エントリポイント
# 読み込み順: postcheck → encode（__av1ify_one が __av1ify_postcheck を呼ぶため）
# shellcheck disable=SC1091
source "${0:A:h}/_video_health.zsh"
# shellcheck disable=SC1091
source "${0:A:h}/_ansi_colors.zsh"
# shellcheck disable=SC1091
source "${0:A:h}/_av1ify_postcheck.zsh"
# shellcheck disable=SC1091
source "${0:A:h}/_av1ify_encode.zsh"

# 解像度値の検証と部分一致解決
# 入力: $1 = 解像度文字列
# 出力: REPLY = 解決後の解像度値
# 戻り値: 0=成功, 1=エラー
__av1ify_resolve_resolution() {
  local input="$1"
  local input_lower="${input:l}"

  # 完全一致（プリセット名）
  case "$input_lower" in
    480p|720p|1080p|1440p|4k)
      REPLY="$input_lower"
      return 0
      ;;
  esac

  # 純粋な数値で有効範囲内
  if [[ "$input" =~ ^[0-9]+$ ]] && (( input >= 16 && input <= 8640 )); then
    REPLY="$input"
    return 0
  fi

  # 部分一致（プリセット名の前方一致）
  local -a presets=(480p 720p 1080p 1440p 4k)
  local -a matches=()
  local p
  for p in "${presets[@]}"; do
    if [[ "$p" == "$input_lower"* ]]; then
      matches+=("$p")
    fi
  done

  if (( ${#matches[@]} == 1 )); then
    print -r -- "${_C_CYAN}>> 解像度 '${input}' → ${matches[1]} に解決しました${_C_OFF}"
    REPLY="${matches[1]}"
    return 0
  fi

  if (( ${#matches[@]} > 1 )); then
    # shellcheck disable=SC2296
    print -r -- "${_C_CYAN}>> 解像度 '${input}' → ${matches[1]} に解決しました (候補: ${(j:, :)matches})${_C_OFF}"
    REPLY="${matches[1]}"
    return 0
  fi

  # 一致なし → エラー
  print -r -- "エラー: 無効な解像度: ${input}" >&2
  print -r -- "  有効な値: 480p, 720p, 1080p, 1440p, 4k, または 16-8640 の数値" >&2
  return 1
}

# fps 値の検証 (0 < fps <= 240 の数値)
# 引数: $1 = fps 文字列
# 戻り値: 0=有効, 1=無効
__av1ify_validate_fps() {
  local fps="$1"
  # 小数点は \. でなく [.] と書く: 未クォートの =~ パターンでは zsh 自身が
  # バックスラッシュを剥がすため \. が「任意の 1 文字」に化け、"1,15" や "1x5" を
  # 通してしまう ([.] は剥がされる対象が無いので未クォートでも安全)。
  [[ "$fps" =~ ^[0-9]+([.][0-9]+)?$ ]] || return 1
  local ok
  ok=$(awk -v fps="$fps" 'BEGIN { print (fps > 0 && fps <= 240) ? 1 : 0 }')
  (( ok ))
}

# 内部補助: クリップボード読取りを発火させてよい文脈か
#
# 2 条件とも必要:
#   -o interactive … 人が対話シェルで打ったときだけ。**-t 0 だけでは足りない**:
#     端末から起動したスクリプトの stdin は端末のままなので -t 0 は真になり、
#     `bin/av1c foo/*.mkv(N)` のようにマッチ 0 件で引数が消えた呼び出しが
#     「クリップボードの中身を処理するか?」の確認に化ける (実測 2026-08-22)
#     ⚠️ 対話シェルの `av1c` は _zshrc のラッパー経由で関数として動くので、このゲートは
#     通る (ユーザーの明示要求)。防波堤は av1c() のコメントに書いた 4 点だけになる。
#   -t 0 … リダイレクト・パイプ・CI では読まない (確認を取る相手がいない)
#
# ⚠️ interactive 側は自動テストから真にできない (setopt interactive は実行時に変更不能で、
#    pty も必要)。tests/zshrc/av1ify/test_av1ify_clipboard.sh は「この関数が文字列として
#    -o interactive を要求すること」を静的に pin している。挙動は pty 駆動の手動検証で見る。
__av1ify_clipboard_mode_available() {
  [[ -o interactive ]] || return 1
  [[ -t 0 ]] || return 1
  command -v pbpaste >/dev/null 2>&1
}

# 内部補助: 端末に溜まっている先行入力を捨てる
# 破壊的な確認の前に呼ぶ。複数行貼り付けの残りや y の連打が「一覧を見る前の承認」に
# なるのを防ぐ (この機能の利用者は「複数行を貼り付ける人」そのもの)。
# read -t 0 は入力が無ければ非 0 を返すので、溜まっている分だけを捨てて止まる。
__av1ify_drain_typeahead() {
  local junk
  # junk は捨てるための受け皿なので参照しないのが正しい
  # shellcheck disable=SC2034
  while read -r -t 0 -k 1 junk 2>/dev/null; do : ; done
}

# 内部補助: ディレクトリ配下の動画ファイルを列挙する
# 引数: $1 = ディレクトリ
# 出力: reply 配列 (見つかった順にソート済み)
# 呼び出し元: av1ify() のディレクトリ分岐と、クリップボード確認の件数表示
#   (確認画面が「1 行 = 1 ファイル」を装わないよう、同じ列挙を使って実件数を出す)
__av1ify_find_videos() {
  local dir="$1"
  reply=()
  local f
  while IFS= read -r -d '' f; do
    reply+=("$f")
  done < <(find "$dir" -type f \( \
      -iname '*.avi' -o -iname '*.mkv' -o -iname '*.rm' -o -iname '*.wmv' -o \
      -iname '*.mpg' -o -iname '*.mpeg' -o -iname '*.mov' -o -iname '*.mp4' -o \
      -iname '*.flv' -o -iname '*.webm' -o -iname '*.3gp' -o -iname '*.ts' \
    \) -print0 | sort -z)
}

# 内部補助: 貼り付けられた 1 行を実パスへ解決する
# 引数: $1 = クリップボードの 1 行
# 出力: REPLY = 解決したパス (:A で絶対化 + symlink 解決済み)
#       __AV1IFY_PASTED_TRIMMED = 前後の空白だけ落とした原文 (一覧で併記する用)
# 戻り値: 0=存在する, 1=存在しない/空行
__av1ify_resolve_pasted_path() {
  setopt LOCAL_OPTIONS extended_glob
  local line="$1"
  # 前後の空白 (CRLF の \r・タブを含む) を除去
  line="${line##[[:space:]]##}"
  line="${line%%[[:space:]]##}"
  __AV1IFY_PASTED_TRIMMED="$line"
  REPLY="$line"
  [[ -z "$line" ]] && return 1

  # 候補を順に試す: 原文 → クォート/エスケープを剥がしたもの → 先頭 ~ を展開したもの。
  # (Q) は zsh のクォート 1 段を外すフラグで '...' / "..." / \x の全形に効き、
  # 対応の取れないクォート (it's.mp4 等) は原文のまま返す (実測: rc=0)。~ は展開しない。
  local -a candidates=("$line")
  # shellcheck disable=SC2296
  local unquoted="${(Q)line}"
  [[ -n "$unquoted" && "$unquoted" != "$line" ]] && candidates+=("$unquoted")
  local c
  for c in "${candidates[@]}"; do
    [[ "$c" == '~'* ]] && candidates+=("${c/#\~/$HOME}")
  done

  for c in "${candidates[@]}"; do
    if [[ -e "$c" ]]; then
      # 表示と処理を「実際に読み書き・削除されるパス」へ揃える。
      # :A = 絶対化 + symlink 解決。_av1ify_encode.zsh の finalize が ${in:A} を
      # trash へ渡すため、ここで揃えないと「一覧に出たファイルは残り、出ていない
      # ファイルが消える」ことになる (symlink を貼った場合。実測 2026-08-22)。
      # 絶対化は「先頭が - のファイル名がオプションとして再パースされる」事故も潰す。
      REPLY="${c:A}"
      return 0
    fi
  done
  return 1
}

# 内部補助: 一覧表示用にパスを切り詰める (極端に長い 1 行で一覧が流れるのを防ぐ)
# 引数: $1 = 表示したい文字列 / 出力: REPLY
__av1ify_shorten_for_display() {
  local s="$1"
  if (( ${#s} > 300 )); then
    REPLY="${s[1,300]}…(全 ${#s} 文字)"
  else
    REPLY="$s"
  fi
}

# 内部補助: クリップボードから処理対象を読み取り、内容を見せて確認を取る
# 出力: __AV1IFY_CLIP_TARGETS に存在するパスを格納
# 戻り値: 0=続行, 1=読み取り失敗/有効なパス 0 件, 130=ユーザーが中止
__av1ify_targets_from_clipboard() {
  __AV1IFY_CLIP_TARGETS=()
  local clip
  if ! clip="$(pbpaste 2>/dev/null)"; then
    print -r -- "エラー: クリップボードの読み取りに失敗しました (pbpaste)" >&2
    return 1
  fi

  # 1 行 1 パスとして扱う (-f リストと同じ規則)。ただし `#` 始まりはコメント扱いにしない:
  # 貼り付け内容は「パスの列」であって注釈付きリストではなく、黙って消えるより
  # 下の一覧に ✗ で出て気づける方が安全 (文字抜けの検出がこの機能の目的)。
  local -a lines=() missing=()
  local line
  while IFS= read -r line; do
    [[ -z "${line//[[:space:]]/}" ]] && continue
    lines+=("$line")
  done <<< "$clip"

  if (( ${#lines[@]} == 0 )); then
    print -r -- "エラー: クリップボードが空です (使い方は av1ify --help)" >&2
    return 1
  fi

  # ⚠️ 一覧と確認プロンプトは stderr へ出す。stdout だと `av1c > log` で
  # 「端末には何も出ないまま入力待ち」になる (バナーも stderr で統一されている)。
  print -ru2 -- "${_C_CYAN}>> 引数がないためクリップボードから読み取りました (${#lines[@]} 行)${_C_OFF}"
  # ⚠️ パスの表示に print -P を使わないこと。prompt 展開はファイル名に含まれる $(...) を
  # 実行する (issue 089)。クリップボードは貼り付けミス由来の信頼できない文字列なので、
  # ここは常に print -r -- で出す。
  local file_count=0 dir_count=0 expanded=0 shown
  for line in "${lines[@]}"; do
    if __av1ify_resolve_pasted_path "$line"; then
      __AV1IFY_CLIP_TARGETS+=("$REPLY")
      local resolved="$REPLY" trimmed="$__AV1IFY_PASTED_TRIMMED"
      __av1ify_shorten_for_display "$resolved"; shown="$REPLY"
      if [[ -d "$resolved" ]]; then
        # ディレクトリは配下を再帰処理する = 1 行が N 件になる。件数を出さないと
        # 「対象 1件」への y が配下全部の承認 (av1c ではゴミ箱行き) になる。
        __av1ify_find_videos "$resolved"
        (( dir_count++ )); (( expanded += ${#reply[@]} ))
        print -ru2 -- "  ✓ [ディレクトリ] $shown  (配下の動画 ${#reply[@]} 件)"
      else
        (( file_count++ ))
        print -ru2 -- "  ✓ $shown"
      fi
      if [[ "$resolved" != "$trimmed" ]]; then
        # 貼った文字列と実体が違う場合 (相対パス / symlink / 引用符付き) は原文も出す。
        # symlink では「表示されたファイルは残り、実体が消える」ため、どちらも見せる。
        __av1ify_shorten_for_display "$trimmed"
        print -ru2 -- "      (貼付: $REPLY)"
      fi
    else
      missing+=("$__AV1IFY_PASTED_TRIMMED")
      __av1ify_shorten_for_display "$__AV1IFY_PASTED_TRIMMED"
      print -ru2 -- "  ✗ $REPLY  (見つかりません → 除外)"
    fi
  done

  if (( ${#__AV1IFY_CLIP_TARGETS[@]} == 0 )); then
    print -r -- "エラー: クリップボードから有効なパスを読み取れませんでした (使い方は av1ify --help)" >&2
    return 1
  fi

  local sum_color="green"
  (( ${#missing[@]} > 0 )) && sum_color="yellow"
  local total=$(( file_count + expanded ))
  if (( dir_count > 0 )); then
    print -ru2 -- "${_C[$sum_color]}== 対象 ${#__AV1IFY_CLIP_TARGETS[@]}行 → 処理するファイル ${total}件 (ファイル ${file_count} + ディレクトリ ${dir_count}行の配下 ${expanded}) / 除外 ${#missing[@]}行${_C_OFF}"
  else
    print -ru2 -- "${_C[$sum_color]}== 対象 ${total}件 / 除外 ${#missing[@]}件${_C_OFF}"
  fi
  if (( __AV1IFY_DELETE_ORIGIN )); then
    print -ru2 -- "${_C_RED}⚠️ 変換に成功したファイルは元ファイルをゴミ箱へ移します (av1c / --delete-origin-if-success-and-no-ng)${_C_OFF}"
  fi

  # 破壊的な確認の前に先行入力を捨てる。stdin が端末でないとき (テスト等) は
  # 回答自体がパイプで来るので捨てない。
  [[ -t 0 ]] && __av1ify_drain_typeahead
  print -nu2 -- "これを入力にしますか? [y/N]: "
  local ans=""
  read -r ans || ans=""
  case "${ans:l}" in
    y|yes) return 0 ;;
    *)
      print -ru2 -- "✋ 中止しました (クリップボードを直して再実行してください)"
      return 130
      ;;
  esac
}

# 内部補助: バッチ処理ループ + 末尾 NG 一覧の出力
# 引数: 処理対象ファイル/ディレクトリのパスを位置引数で渡す
# 出力: 各ファイルの処理ログ + 末尾サマリ + (NG があれば) NG 一覧
# 戻り値: 0=全件OK, 1=NG あり, 130=中断
# 副作用: __AV1IFY_LAST_NG_REASON を反復ごとにクリアする。
#         NG ありで返るときは __AV1IFY_LAST_NG_REASON に集計理由をセットする
#         (バッチ内にディレクトリが混在するケースで、外側のバッチが NG として
#          集計できるようにするため)
__av1ify_run_batch() {
  local target ok=0 ng=0
  local -a ng_list=()
  local -a targets=( "$@" )
  local n=${#targets[@]} i next exit_status
  # 直前 av1ify 呼び出しから持ち越した stale PID をクリア (PID 再利用での誤 kill を避ける)
  __AV1IFY_PREFETCH_PIDS=()
  for (( i = 1; i <= n; i++ )); do
    target="${targets[i]}"
    print -r -- "---- 処理: $target"
    __AV1IFY_LAST_NG_REASON=""
    # 次のファイルを background で先読み (クラウド materialize を現エンコード中に進める)。
    # ただしファイル名/ローカル glob だけで SKIP 確定の対象 (-enc.mp4 自体や既存出力済) は
    # __av1ify_one が即座に return 0 して終わるので、materialize させる意味が無い。
    # ディレクトリ指定で大量の既変換ファイルが含まれているケースで「全部 prefetch されて
    # 不要なダウンロードが走る」事故を防ぐため、prefetch 前にゲートする。
    next="${targets[i+1]:-}"
    if [[ -n "$next" ]] && ! __av1ify_skip_by_name "$next"; then
      __av1ify_prefetch "$next"
    fi
    if __AV1IFY_INTERNAL_CALL=1 av1ify "$target"; then
      ((ok++))
    else
      exit_status=$?
      if (( exit_status == 130 || __AV1IFY_ABORT_REQUESTED )); then
        __av1ify_kill_prefetches
        print -r -- "✋ 中断: 残りのファイルをスキップします"
        return 130
      fi
      ((ng++))
      # TAB を区切りに使い、ファイルパスに改行が混じっても扱えるようにする
      ng_list+=("${target}"$'\t'"${__AV1IFY_LAST_NG_REASON:-理由不明 (上のログを参照)}")
    fi
  done
  # 視認性: NG が無ければ緑 (= 全部 OK で安心), NG があれば黄 (= 下の一覧確認)
  local sum_color="green"
  (( ng > 0 )) && sum_color="yellow"
  print -r -- "${_C[$sum_color]}== サマリ: OK=$ok / NG=$ng / ALL=$((ok+ng))${_C_OFF}"
  if (( ng > 0 )); then
    print -r -- "── NG 一覧 (${ng}件) ──"
    local entry f r
    for entry in "${ng_list[@]}"; do
      f="${entry%%$'\t'*}"
      r="${entry#*$'\t'}"
      print -r -- "  ✗ $f"
      print -r -- "    └─ $r"
    done
    # NG を exit code に反映する (bin/av1ify・Finder action・スクリプト連携が失敗を検知できる)。
    # ディレクトリがバッチに混在した場合、ネストしたバッチの NG はこの非0で外側に伝搬する。
    __AV1IFY_LAST_NG_REASON="バッチ内に NG ${ng}件 (内訳は上の NG 一覧を参照)"
    return 1
  fi
  return 0
}

av1ify() {
  local __av1ify_internal=0
  if [[ -n ${__AV1IFY_INTERNAL_CALL:-} ]]; then
    __av1ify_internal=1
    unset __AV1IFY_INTERNAL_CALL
  fi

  setopt LOCAL_OPTIONS localtraps

  # ルート呼び出しでは毎回デフォルト（内部呼び出しのみ伝搬）
  local dry_run=0
  local show_help=0
  local opt_resolution=""
  local opt_fps=""
  local opt_denoise=""
  local opt_color_tags=""
  local opt_compact=0
  local opt_force=0
  local opt_delete_origin=0
  local opt_listfile=""
  local -a positional=()
  while (( $# > 0 )); do
    case "$1" in
      --dry-run|-n)
        dry_run=1
        ;;
      -h|--help)
        (( ! __av1ify_internal )) && show_help=1
        ;;
      -c|--compact)
        opt_compact=1
        ;;
      --force)
        opt_force=1
        ;;
      --delete-origin-if-success-and-no-ng)
        opt_delete_origin=1
        ;;
      --no-delete-origin-if-success-and-no-ng)
        opt_delete_origin=0
        ;;
      -r|--resolution)
        shift
        if (( $# == 0 )); then
          print -r -- "エラー: --resolution には値が必要です" >&2
          return 1
        fi
        opt_resolution="$1"
        ;;
      --fps)
        shift
        if (( $# == 0 )); then
          print -r -- "エラー: --fps には値が必要です" >&2
          return 1
        fi
        opt_fps="$1"
        ;;
      --denoise)
        shift
        if (( $# == 0 )); then
          print -r -- "エラー: --denoise には値が必要です" >&2
          return 1
        fi
        opt_denoise="$1"
        ;;
      --color-tags)
        shift
        if (( $# == 0 )); then
          print -r -- "エラー: --color-tags には値が必要です" >&2
          return 1
        fi
        opt_color_tags="$1"
        ;;
      -f)
        # -f は他のオプションと同様に位置非依存でパースする（引数のどの位置でも指定可）。
        shift
        if (( $# == 0 )) || [[ -z "$1" ]]; then
          print -r -- "エラー: -f オプションにはファイルパスが必要です" >&2
          return 1
        fi
        opt_listfile="$1"
        ;;
      -*)
        print -r -- "エラー: 不明なオプション: $1" >&2
        return 1
        ;;
      *)
        positional+=("$1")
        ;;
    esac
    shift
  done
  set -- "${positional[@]}"

  # 処理対象の有無 (位置引数 or -f リスト)。バナー/検証/ヘルプのゲートに使う
  local have_targets=0
  { (( $# > 0 )) || [[ -n "$opt_listfile" ]]; } && have_targets=1

  # 引数がなければクリップボードを入力にする (発火条件は __av1ify_clipboard_mode_available)。
  # 大量のパスをターミナルへ貼り付けると行編集を通る途中で文字が落ちることがあるため、
  # シェルを経由せず pbpaste から直接読む。読んだ内容は必ず一覧表示して確認を取る。
  # ここでは「読む」と決めるだけで、実際の読み取りはバナーと AV1_* の fail-fast 検証を
  # 通した後に行う (一覧を見せて y を取ってから「無効な fps」で落ちるのを避ける)。
  local use_clipboard=0
  if (( ! __av1ify_internal )) && (( ! show_help )) && (( ! have_targets )) \
    && __av1ify_clipboard_mode_available; then
    use_clipboard=1
  fi

  # --compact: 720p + 30fps プリセット（明示的な -r/--fps が優先）
  if (( opt_compact )); then
    [[ -z "$opt_resolution" ]] && opt_resolution="720p"
    [[ -z "$opt_fps" ]] && opt_fps="30"
  fi

  if (( ! __av1ify_internal )); then
    __AV1IFY_DRY_RUN=$dry_run
    # CLI オプションと環境変数 (AV1_RESOLUTION / AV1_FPS / AV1_DENOISE) をここで統合
    # → 各ファイル処理 (__av1ify_one) での二重バリデーションを回避
    __AV1IFY_RESOLUTION="${opt_resolution:-${AV1_RESOLUTION:-}}"
    __AV1IFY_FPS="${opt_fps:-${AV1_FPS:-}}"
    __AV1IFY_DENOISE="${opt_denoise:-${AV1_DENOISE:-}}"
    # デフォルト "auto": h264_nvenc 等が誤って埋め込む matrix_coefficient=0 (Identity) +
    # yuv420p の組み合わせ (SVT-AV1 が拒否する不正タグ) を検出時のみ bt709 に補正する。
    # 常に上書きしたくない/一切触りたくない場合は bt709/off を明示指定する。
    __AV1IFY_COLOR_TAGS="${opt_color_tags:-${AV1_COLOR_TAGS:-auto}}"
    __AV1IFY_COMPACT=$opt_compact
    __AV1IFY_FORCE=$opt_force
    __AV1IFY_DELETE_ORIGIN=$opt_delete_origin
  else
    dry_run="${__AV1IFY_DRY_RUN:-$dry_run}"
  fi

  if (( ! __av1ify_internal )) && (( ! show_help )) && (( have_targets || use_clipboard )); then
    __av1ify_banner
  fi

  # resolution / fps / denoise の早期バリデーション (fail-fast)。無効値を検知せず黙って無視すると、
  # タイポ (例: --fps abc) でも全ファイルが意図しない設定でエンコードされてしまう。
  # 配置: バナー出力後 (解決メッセージの表示順を統一)。
  # ゲート: help 表示・処理対象なしのときは検証しない (無効な AV1_* 環境変数が残っていても
  # `av1ify --help` が読めなくなる regression を防ぐ)。
  if (( ! __av1ify_internal )) && (( ! show_help )) && (( have_targets || use_clipboard )); then
    if [[ -n "$__AV1IFY_RESOLUTION" ]]; then
      if __av1ify_resolve_resolution "$__AV1IFY_RESOLUTION"; then
        __AV1IFY_RESOLUTION="$REPLY"
        opt_resolution="$REPLY"
      else
        return 1
      fi
    fi
    if [[ -n "$__AV1IFY_FPS" ]] && ! __av1ify_validate_fps "$__AV1IFY_FPS"; then
      print -r -- "エラー: 無効なfps指定: ${__AV1IFY_FPS}（0より大きく240以下で指定してください）" >&2
      return 1
    fi
    if [[ -n "$__AV1IFY_DENOISE" ]]; then
      # 有効集合は _AV1IFY_DENOISE_PRESETS (_av1ify_encode.zsh) が単一の真実源
      if (( ${+_AV1IFY_DENOISE_PRESETS[${__AV1IFY_DENOISE:l}]} )); then
        __AV1IFY_DENOISE="${__AV1IFY_DENOISE:l}"
      else
        print -r -- "エラー: 無効なdenoise指定: ${__AV1IFY_DENOISE}（light/medium/strong から選択してください）" >&2
        return 1
      fi
    fi
    case "${__AV1IFY_COLOR_TAGS:l}" in
      auto|bt709|off)
        __AV1IFY_COLOR_TAGS="${__AV1IFY_COLOR_TAGS:l}"
        ;;
      *)
        print -r -- "エラー: 無効なcolor-tags指定: ${__AV1IFY_COLOR_TAGS}（auto/bt709/off から選択してください）" >&2
        return 1
        ;;
    esac
    # 音声再エンコード閾値のマージン。awk は非数値を 0 と解釈するため、検証しないと
    # typo (例: "abc" / "1,15") が無警告で「全ソース再エンコード」や「マージン 1.0」に化ける。
    if [[ -n "${AV1_AUDIO_REENCODE_MARGIN:-}" ]] \
      && ! __av1ify_validate_reencode_margin "$AV1_AUDIO_REENCODE_MARGIN"; then
      print -r -- "エラー: 無効なAV1_AUDIO_REENCODE_MARGIN指定: ${AV1_AUDIO_REENCODE_MARGIN}（0より大きい10進数で指定してください。例: 1.15）" >&2
      return 1
    fi
  fi

  if (( ! __av1ify_internal )) && (( ! show_help )) && (( have_targets || use_clipboard )); then
    if (( opt_compact )); then
      print -r -- "${_C_CYAN}>> compact モード: -r ${opt_resolution} --fps ${opt_fps}${_C_OFF}"
    fi
  fi

  (( ! __av1ify_internal && dry_run )) && print -r -- "[DRY-RUN] ファイルは変更しません"

  if (( ! __av1ify_internal )) && { (( show_help )) || (( ! have_targets && ! use_clipboard )); }; then
    cat <<'EOF'
av1ify — 入力された動画ファイル、またはディレクトリ内の動画ファイルをAV1形式のMP4に一括変換します。

機能:
  - 指定されたファイルまたはディレクトリを対象に処理を実行します。
  - 引数を省略すると、クリップボードの内容を「1行1パス」として読み取り、内容を表示して
    確認を取ってから処理します（大量のパスをターミナルへ貼り付けると文字が落ちることがあるため）。
    対話シェルで打ったときだけ有効です（スクリプト・Finder アクション・CI では発火しません）。
  - 出力ファイル名は `<元のファイル名>-enc.mp4` となります。
  - 既に変換済みのファイルが存在する場合は、処理をスキップします。
  - 処理中には `<出力ファイル名>.in_progress` という一時ファイルを作成し、変換成功後にリネームします。
  - 変換後に音声ストリームと音ズレを簡易チェックし、問題が見つかればファイル名末尾に注意書きを付けます。
  - 音声は「MP4へcopyできるか」「再エンコードで実際に削れるか」の2段で判定します。
    1. MP4にcopyできないコーデック (opus/vorbis/ac3/dts/pcm 等) は、ビットレートに関わらず
       AAC (既定96kbps, 最大48kHz/2ch) へ再エンコードします。
    2. copyできるコーデック (既定でAAC, ALAC, MP3) でも、ソースのビットレートが
       「ターゲット × AV1_AUDIO_REENCODE_MARGIN (既定1.15)」を超えるなら再エンコードして圧縮します。
       超えない場合は再エンコードしても削減が小さく世代劣化とCPUを払うだけなので、無劣化copyします。
    再エンコード時のビットレートはソース値を上回らないようキャップされます (最低32k)。
  - ディレクトリを指定した場合、再帰的に動画ファイル
    (avi, mkv, rm, wmv, mpg, mpeg, mov, mp4, flv, webm, 3gp, ts) を検索して変換します。
    (ファイル名の大文字・小文字は区別しません)

使い方:
  av1ify [オプション] <ファイルパス または ディレクトリパス> [<ファイルパス2> ...]
  av1ify -f <ファイルリスト>
  av1ify                       # 引数なし: クリップボードから読み取り（確認あり）

  例:
    # 単一のファイルを変換
    av1ify "/path/to/movie.avi"

    # 複数のファイルを順番に変換
    av1ify xxx.mp4 yyy.mp4 zzz.mp4

    # ファイルリストから変換（改行区切り）
    av1ify -f list.txt

    # クリップボードにコピーしたパス（改行区切り）を入力にする
    #   1行1パスとして読み取り、内容を一覧表示して [y/N] の確認を取ります（既定は No）。
    #   前後の空白・引用符・`\ ` エスケープ・先頭の ~ は自動で解決し、見つからないパスは
    #   ✗ 表示のうえ対象から除外します（1件も見つからなければエラー）。
    #   表示は「実際に読み書き・削除されるパス」に揃えます（絶対パス化 + symlink 解決。
    #   貼った文字列と違う場合は (貼付: ...) を併記）。ディレクトリ行は配下の動画の件数を
    #   出します（1行が N 件に展開されるため）。
    #   ⚠️ 発火するのは「対話シェルで人が打ったとき」だけです。Finder アクション・CI・
    #   bin/av1ify や bin/av1c の直叩き（= 非対話）では発火せず、このヘルプを表示します。
    #   対話シェルの av1c は関数なので発火します（成功時に元ファイルをゴミ箱へ移すため、
    #   確認は赤字の削除警告つきです）。
    av1ify

    # ディレクトリ内のすべての動画ファイルを変換
    av1ify "/path/to/dir"

    # CRF値を指定して画質を調整
    AV1_CRF=35 av1ify "/path/to/movie.mp4"

    # 720pに解像度を変更して変換（アスペクト比は維持）
    av1ify -r 720p "/path/to/movie.mp4"

    # 24fpsに変更して変換
    av1ify --fps 24 "/path/to/movie.mp4"

    # 解像度とfpsを両方指定
    av1ify -r 1080p --fps 30 "/path/to/movie.mp4"

    # ノイズ除去で圧縮率を上げる（ノイジーな素材に効果的）
    av1ify --denoise medium "/path/to/movie.mp4"

    # 720p + ノイズ除去の組み合わせ
    av1ify -r 720p --denoise light "/path/to/movie.mp4"

    # ソースの色空間タグ (matrix_coefficient) を常に bt709 へ強制上書き
    av1ify --color-tags bt709 "/path/to/movie.mp4"

    # 保存用プリセット（720p + 30fps）
    av1ify --compact "/path/to/movie.mp4"

    # --compact + 解像度だけ上書き（480p + 30fps）
    av1ify --compact -r 480p "/path/to/movie.mp4"

オプション:
  -h, --help: このヘルプメッセージを表示します。
  -n, --dry-run: 実行内容のみを表示し、ファイルを変更しません。
  -f <ファイル>: 改行区切りでファイルパスが記載されたリストファイルを読み込んで処理します。
      引数のどの位置でも指定でき、通常のファイル引数と併用できます。
  -r, --resolution <値>: 出力解像度（縦）を指定します。アスペクト比は維持されます。
      480p / 720p / 1080p / 1440p / 4k または数値（例: 540）
  --fps <値>: 出力フレームレートを指定します（例: 24, 30, 60）。
  -c, --compact: 保存用プリセット（720p + 30fps）。-r や --fps で個別に上書き可能。
  --denoise <レベル>: ノイズ除去を適用します。圧縮率が向上しますが、ディテールが失われます。
      light: 軽度（hqdn3d=2:2:3:3）
      medium: 中程度（hqdn3d=4:4:6:6）
      strong: 強め（hqdn3d=6:6:9:9）
  --color-tags <値>: 出力の色空間 matrix (ffmpeg の -colorspace) の扱いを指定します。
      auto (デフォルト): ソースの matrix_coefficient が Identity (gbr) の場合のみ bt709 に補正します。
        (h264_nvenc 等が yuv420p と非互換な Identity タグを埋め込むケースで、
         SVT-AV1 が "Identity matrix may be used only with 4:4:4" エラーで
         エンコード自体を拒否するのを防ぐための自動補正です)
      bt709: ソースの値によらず常に bt709 へ上書きします。
      off: 一切上書きしません（ソースのタグをそのままコピー）。
      補正するのは matrix だけで、color_primaries / color_trc は変更しません。
      また補正値は解像度によらず bt709 固定です。SD (480p/576p 等) 素材では
      bt601 相当で解釈した場合と色が変わりますが、元タグが壊れている時点で
      真の色空間は失われているため、どちらを選んでも復元の保証はありません。
  --force: 入力ファイルの健全性チェックに失敗してもエンコードを続行します。
      軽微なA/V音ズレなど、許容できる問題がある場合に使用してください。
  --delete-origin-if-success-and-no-ng: 変換成功かつpostcheckでNG無しの場合、元ファイルを削除します。
      av1c (compactショートハンド) ではデフォルトで有効です。
      --no-delete-origin-if-success-and-no-ng で明示的に無効化できます。

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

  AV1_AAC_BITRATE (デフォルト: 96k)
    音声をAACへ再エンコードする際のターゲットビットレートを指定します（例: 128k）。
    ソースのビットレートがこれより低い場合はソース値までキャップされます（最低 32k）。

  AV1_AUDIO_REENCODE_MARGIN (デフォルト: 1.15)
    copy可能なコーデックを「あえて再エンコードして圧縮する」かどうかの閾値マージンです。
    ソースのビットレートが AV1_AAC_BITRATE × このマージン を超えたときだけ再エンコードします。
    1.0 に近づけるほど積極的に再エンコードしますが、削減幅の小さい領域で
    世代劣化とCPUを払うだけになります。大きくすると copy 寄りになります。
    (例: 既定では 96k × 1.15 = 110400bps 超のソースが再エンコード対象)

  AV1_RESOLUTION (デフォルト: なし)
    出力解像度を指定します。--resolution オプションと同等です。
    CLIオプションが優先されます。

  AV1_FPS (デフォルト: なし)
    出力フレームレートを指定します。--fps オプションと同等です。
    CLIオプションが優先されます。

  AV1_DENOISE (デフォルト: なし)
    ノイズ除去レベルを指定します。--denoise オプションと同等です。
    light / medium / strong から選択。CLIオプションが優先されます。

  AV1_COLOR_TAGS (デフォルト: auto)
    色空間タグの上書き方法を指定します。--color-tags オプションと同等です。
    auto / bt709 / off から選択。CLIオプションが優先されます。

  AV1IFY_FRAME_TOLERANCE (デフォルト: 24)
    変換前後のフレーム数差がこの値以下であれば警告しません。
    再エンコード時の数フレームの差異は通常無害なため、既定値は24（約1秒分）です。

  AV1IFY_SYNC_TOLERANCE (デフォルト: 2.0)
    encode 前後で「音声 - 映像 duration」の関係差がこの値[秒]以下であれば警告しません。
    ソース時点で音ズレしている素材 (末尾無音映像が残る MKV 等) を encode 由来と
    誤判定しないよう、絶対値ではなく enc 前後の差分のみを評価します。
    MKV など stream duration を出さないコンテナでは packet PTS を走査して
    真の duration を測ります (5GB クラスで数秒オーダーの追加コスト)。

  AV1IFY_DURATION_TOLERANCE (デフォルト: 2.0)
    変換前後のコンテナ再生時間 (format duration) の差がこの値[秒]以下であれば警告しません。

  AV1IFY_MIN_SIZE_RATIO (デフォルト: 0.001)
    出力サイズ / 入力サイズ がこの比率を下回ると「ファイルサイズ異常」として警告します。
EOF
    return 0
  fi

  # ここまでで「処理へ進む」と確定した。クリップボード経路はここで初めて端末を触る
  # (バナー → AV1_* 検証 → 一覧 → 確認 の順を、引数あり呼び出しと揃えるため)。
  if (( use_clipboard )); then
    __av1ify_targets_from_clipboard || return $?
    set -- "${__AV1IFY_CLIP_TARGETS[@]}"
    have_targets=1
  fi

  local __av1ify_is_root=0
  if (( ! __av1ify_internal )); then
    __av1ify_is_root=1
    __AV1IFY_ABORT_REQUESTED=0
    __AV1IFY_CURRENT_TMP=""
    trap '__av1ify_on_interrupt' INT TERM HUP
  fi

  set -o pipefail

  if [[ -n "$opt_listfile" ]]; then
    if [[ ! -f "$opt_listfile" ]]; then
      print -r -- "エラー: ファイルが見つかりません: $opt_listfile" >&2
      return 1
    fi

    local -a list_files=()
    local line
    while IFS= read -r line; do
      # CRLF のリストファイル (Windows / スプレッドシート由来) の \r を除去
      line="${line%$'\r'}"
      [[ -z "$line" || "$line" == \#* ]] && continue
      list_files+=("$line")
    done < "$opt_listfile"

    if (( ${#list_files[@]} == 0 )) && (( $# == 0 )); then
      print -r -- "（対象ファイルなし: $opt_listfile）"
      return 0
    fi
    set -- "${list_files[@]}" "$@"
  fi

  if (( $# > 1 )); then
    __av1ify_run_batch "$@"
    return $?
  fi

  local target="$1"
  if [[ -d "$target" ]]; then
    setopt LOCAL_OPTIONS extended_glob null_glob
    unsetopt LOCAL_OPTIONS SH_WORD_SPLIT
    # 列挙は __av1ify_find_videos が単一の出典 (クリップボード確認の件数表示と共有)
    local -a files=()
    __av1ify_find_videos "$target"
    files=("${reply[@]}")
    if (( ${#files[@]} == 0 )); then
      print -r -- "（対象ファイルなし: $target）"; return 0
    fi
    # 各ファイルは av1ify() を通して単体処理ルートを再利用（直列実行）
    __av1ify_run_batch "${files[@]}"
  else
    __av1ify_one "$target"
  fi
}

# av1c — av1ify --compact + 「変換成功 & postcheck NG なしなら元ファイルをゴミ箱へ」
#
# プリセットの中身はここだけに置く。bin/av1c (Finder アクション用) と _zshrc の
# lazy-reload ラッパー (対話シェル用) の両方がこの関数を呼ぶ。フラグを両方に書くと
# 片方だけ変わる。
#
# ⚠️ 対話シェルで引数なしの av1c を打つとクリップボード入力が発火する。
#    これは「マッチ 0 件の glob で引数が消えた `av1c **/*.mkv(N)` が、削除つきの
#    クリップボード確認に化ける」経路を承知の上で開けている (ユーザーの明示要求)。
#    引数が消えたこと自体は検出できないため、防波堤は __av1ify_targets_from_clipboard 側の
#    ①一覧表示 ②赤字の削除警告 ③既定 No ④先行入力の drain の 4 点だけ。この 4 点を
#    削る変更を入れるときは、この経路が「y 一発でゴミ箱行き」になることを踏まえて判断する。
av1c() {
  av1ify --compact --delete-origin-if-success-and-no-ng "$@"
}
