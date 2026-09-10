# _av1ify_lock.zsh — av1ify の「同じ動画ファイルへの二重実行」排他 (lockman 利用)
#
# 🚨 **このファイルを分けている理由は shellcheck**。排他の実装は zsh 専用構文
# (`always` ブロック / `setopt local_options`) を使うため shellcheck が解析できず、
# _av1ify_encode.zsh に置くと**ファイル全体の検査が落ちる** (SC1073)。
# 分離して、このファイルだけを Makefile の ZSH_SYNTAX_FILES (zsh 例外) に登録し、
# 本体 1000 行超の shellcheck 検査を残している。
# 登録を外すと `make test-shellcheck` が落ちる。

# --- 対象ファイル単位の排他 (lockman) ---------------------------------------
#
# なぜ要るか: 一時出力 <stem>-enc.mp4.in_progress は**入力パスから決まる決定論的な
# 名前**で、ロックが無い。同じファイルに 2 プロセスが当たると、片方が起動時の
# 「残骸削除」でもう片方の**作業中ファイルを rm し**、生き残った側が mv -f →
# postcheck → validate → 元ファイルを trash まで走る (元が消えて出力は壊れている)。
#
# 到達経路は 1 つではない (2026-09-08 の敵対的レビューで再現):
#   - parallel-each の排他は -F の入力ファイル単位。**別のリスト**から同じ動画を
#     指せば 2 プロセスとも起動する (既定リスト名が cwd 相対なので、別ディレクトリで
#     起動するだけで成立する)
#   - 1 プロセス内でも、綴り違い (a.mkv / ./a.mkv / A.mkv。APFS は既定でケース非依存)
#     は生文字列の重複検査を素通りして同時に走る
#   - 人が別の端末で av1c を叩く
# どれも「同じ 1 ファイルに 2 つ」に帰着するので、**対象そのもの**を単位に排他する。
#
# 置き場所は ~/.lockman/av1c/<key>/ に集約する (AV1IFY_LOCK_ROOT で変更可)。
# 動画の隣に置くと、処理した本数ぶんの隠しディレクトリが素材フォルダに残り、
# しかも安全に自動削除できない (解放した瞬間から他プロセスが取得できるので、
# こちらが畳んでいる最中に相手の probe/tmp を消すと相手の lease が飛ぶ)。
# 1 箇所に集めれば、アイドル時に `rm -rf ~/.lockman/av1c` の 1 回で片付く。
#
# 🚨 これは**同一マシン内の排他**になる。lockman 自体は SMB 越しの複数マシンを
# 想定して作られているが、ロックをローカル HOME に置く以上、別マシンからの
# 競合には効かない。今回塞ぐと決めた経路 (別ディレクトリからの二重起動 /
# 綴り違い / 手で叩いた av1c) はすべて同一マシン内なので、その範囲での判断。
# 複数マシンから同じ共有を触る運用に変えるときは、AV1IFY_LOCK_ROOT を
# 共有上のパスへ向ける。
#
# 🚨 キーは**入力ではなく「出力の一時ファイル名」から導く**。実際に競合する資源は
# <stem>-enc.mp4.in_progress であって入力ファイルではない。入力で鍵を作ると、
# movie.mkv と movie.mp4 のように**別入力が同じ出力名を共有する**組み合わせが
# 別キーになり、両方が取得に成功して同じ一時ファイルを奪い合う
# (2026-09-08 の codex 敵対レビュー P1。ハードリンクで作った同 stem も同型)。
#
# 導出は「出力ディレクトリの実パス (symlink 解決済み) + basename を ASCII 小文字化」
# の SHA-256。
#   - basename では足りない: 別ディレクトリの同名ファイルが同じロックになる
#   - 小文字化するのは APFS が既定で大小を区別しないため。A.mkv と a.mkv は同じ
#     ファイルなのでキーも同じでなければならない
#   - 🚨 小文字化は **LC_ALL=C 固定の ASCII 変換**で行う。zsh の ${x:l} はロケール
#     依存で、同じ文字列でも LC_ALL=C と en_US.UTF-8 で別キーになる (同レビューで
#     実測。非 ASCII の大小違いは畳めなくなるが、環境で答えが変わる方が危険)
#
# 🚨 acquire は __av1ify_one に**入る前**に取る。これで関数内の「残骸削除」も
# ロックの下に入り、他プロセスの作業中ファイルを消す経路が構造的に消える。
# held = lockman lease を保持中、nolock = 明示的な opt-out で排他なし、
# 空文字 = 未取得または解放済み。共有 tmp の削除可否はこの状態を根拠にする。
typeset -g __AV1IFY_LOCK_MODE=""
typeset -g __AV1IFY_LOCK_DIR=""
typeset -g __AV1IFY_LOCK_TOKENFILE=""
typeset -g __AV1IFY_LOCK_RENEWER=""
typeset -g __AV1IFY_LOCK_WARNED=0

# TTL は下限 30s。長尺は数時間かかるので renew 前提で 30m を取り、TTL/3 ごとに更新する
# (lockman with と同じ割り方)。異常終了しても 30 分で他者が引き継げる。
typeset -g __AV1IFY_LOCK_TTL=30m
typeset -gi __AV1IFY_LOCK_RENEW_SEC=600

# 戻り値 0 のとき REPLY にロックディレクトリ。ハッシュを取れなければ 1。
#
# 入力パスを受け取り、その入力が書く**一時出力の名前**を鍵にする
# (__av1ify_one の stem 導出と同じ式: "${in%.*}-enc.mp4.in_progress")。
__av1ify_lock_dir_for() {
  # 🚨 **入力ファイルの symlink を解決しない**。__av1ify_one は受け取った綴りのまま
  # "${in%.*}-enc.mp4.in_progress" へ書くので、ここで :A を掛けるとロックする資源と
  # 実際に書く資源が食い違う。in/movie.mkv -> ../src/original.mkv の配置では、
  # ロックは src/original-enc… を守るのに書き込みは in/movie-enc… へ行き、
  # in/movie.mp4 との二重実行を許していた (2026-09-08 の codex 敵対レビュー P1)。
  # 正規化するのは**親ディレクトリだけ** (相対 / ./ / 別名を畳むため。ここは実在する)。
  local in="$1"
  local tmpout="${in%.*}-enc.mp4.in_progress"
  local dir="${tmpout:h}" base="${tmpout:t}"
  # :P = physical path。symlink を解決してから `..` を処理するので、実際に出力を
  # 開くときの親ディレクトリと一致する。入力ファイル自身には適用しない。
  dir="${dir:P}"
  # ASCII 小文字化をロケールから切り離す (${base:l} は LC_ALL で結果が変わる)。
  # 🚨 失敗を握り潰さない。tr が落ちると空文字を hash して「同じディレクトリの
  # 別ファイルが同一キー」になる (同レビュー P1)。
  #
  # 🚨 **下の 3 つの検証 ([[ -n "$folded" ]] / 64 桁 / hex のみ) を「pipefail が
  # あるから冗長」と読んで外さないこと。** 監査が一度「途中段の失敗検出は PIPE_FAIL 依存」と
  # 指摘したが、**前提が誤りだった** (issues/done/340-risk-av1ify-lock-unverified-residuals.md 項目 5 で却下)。実測 2026-09-09:
  # `zsh -f` + `unsetopt pipefail` で PATH 先頭に失敗 shim を置くと、tr 失敗 / shasum 失敗 /
  # **shasum が rc=0 で短い出力を返す**の 3 形すべてで rc=1 になる。3 形目は pipefail では
  # 原理的に検出できない (途中段が成功しているため) ので、担い手はこの出力検証の側。
  local folded
  folded="$(LC_ALL=C printf '%s' "$base" | LC_ALL=C tr 'ABCDEFGHIJKLMNOPQRSTUVWXYZ' 'abcdefghijklmnopqrstuvwxyz')" || return 1
  [[ -n "$folded" ]] || return 1
  local h
  h="$(printf '%s' "$dir/$folded" | shasum -a 256 2>/dev/null | cut -d' ' -f1)" || return 1
  # 64 桁の 16 進であることまで見る (途中で落ちた出力を鍵にしない)。
  # 🚨 `(#c64)` のような extendedglob 依存の書き方をしない。呼び出し元の setopt に
  # 依存し、無効なら**常に不一致 = キー生成が常に失敗**する。実測 2026-09-09: これで
  # 全経路が排他なしへ倒れ、しかも自作の確認テストは「両方とも空文字なので一致」で
  # 緑になっていた (偽陽性)。
  [[ ${#h} -eq 64 ]] || return 1
  [[ -z "${h//[0-9a-f]/}" ]] || return 1
  REPLY="$(__av1ify_lock_root)/$h"
  return 0
}

# ロックの置き場。**呼ばれるたびに環境変数を読む**。source 時に 1 度だけ写すと、
# 関数を読み込んだ後に AV1IFY_LOCK_ROOT を指定しても効かない (同レビュー P2)。
__av1ify_lock_root() {
  print -r -- "${AV1IFY_LOCK_ROOT:-$HOME/.lockman/av1c}"
}

# 排他の準備そのものが失敗したときの分岐。
#
# 🚨 **既定は中止 (fail-closed)**。以前は「元ファイルを残す設定なら排他なしで続行」に
# していたが、それは自分が失うものだけを数えた判断だった。共有名の一時出力
# (<stem>-enc.mp4.in_progress) へ書く以上、**削除設定に関係なく他プロセスの作業中
# ファイルを壊せる** — lockman を持つ端末 A が av1c で走っている最中に、lockman の
# 無い端末 B が av1ify を実行すると、B の「残骸削除」が A の出力を消す
# (2026-09-08 の codex 敵対レビュー P1 が実証)。
#
# 排他なしで走らせたい環境 (lockman を用意できない / テスト) は
# AV1IFY_ALLOW_NO_LOCK=1 を明示すること。黙って無防備になるより、明示的に選ばせる。
# 戻り値: 0 = 排他なしで続行してよい / 1 = 中止
__av1ify_lock_unavailable() {
  local reason="$1" allow_value="${AV1IFY_ALLOW_NO_LOCK:-}"
  if [[ "$allow_value" == 1 ]]; then
    __AV1IFY_LOCK_MODE="nolock"
    if (( ! __AV1IFY_LOCK_WARNED )); then
      __AV1IFY_LOCK_WARNED=1
      print -ru2 -- "${_C_YELLOW}⚠️ $reason。AV1IFY_ALLOW_NO_LOCK=${allow_value} が指定されているので排他なしで続行します${_C_OFF}"
    fi
    return 0
  fi
  print -ru2 -- "${_C_RED}❌ $reason。同じファイルへの二重実行を防げないため中止します${_C_OFF}"
  print -ru2 -- "   (承知のうえで実行するなら AV1IFY_ALLOW_NO_LOCK=1 を付ける)"
  return 1
}

# 戻り値: 0 = 先へ進んでよい / 3 = 他が保持中 (呼び出し側は SKIP) / 1 = エラー
__av1ify_lock_acquire() {
  local in="$1"
  __AV1IFY_LOCK_MODE=""
  __AV1IFY_LOCK_DIR=""; __AV1IFY_LOCK_TOKENFILE=""; __AV1IFY_LOCK_RENEWER=""

  # dry-run は何も書かないので排他も要らない (ロックディレクトリも作らない)。
  (( ${__AV1IFY_DRY_RUN:-0} )) && return 0

  if ! whence -p lockman >/dev/null; then
    __av1ify_lock_unavailable "lockman が PATH にありません"
    return $?
  fi

  # 更新用の所有者 PID は lease を取る前に確定させる。ここで失敗するなら取得を
  # 始めないので、取得後の release 巻き戻しや、削除済み token で finalize が renew
  # する経路を作らない。$$ には頼らず、サブシェル自身の PID を使う。
  if ! zmodload -F zsh/system p:sysparams 2>/dev/null || [[ -z "${sysparams[pid]-}" ]]; then
    __av1ify_lock_unavailable "zsh/system モジュールが読めず、更新の所有者を特定できません"
    return $?
  fi
  local owner="${sysparams[pid]}"

  if ! __av1ify_lock_dir_for "$in"; then
    __av1ify_lock_unavailable "ロックキーを作れません (shasum 不在?)"
    return $?
  fi
  local dir="$REPLY"
  if ! mkdir -p -- "$dir" 2>/dev/null; then
    __av1ify_lock_unavailable "ロックディレクトリを作れません: $dir"
    return $?
  fi

  # トークンは所有権の証明なので、共有されるロックディレクトリには置かない。
  local tok
  if ! tok="$(mktemp "${TMPDIR:-/tmp}/av1ify-lock.XXXXXX")"; then
    __av1ify_lock_unavailable "トークンファイルを作れません (TMPDIR=${TMPDIR:-/tmp})"
    return $?
  fi

  lockman acquire "$dir" --ttl "$__AV1IFY_LOCK_TTL" --token-file "$tok" \
    --label "av1ify pid=$$ $in" >/dev/null
  local rc=$?
  if (( rc == 3 )); then
    rm -f -- "$tok"
    return 3     # 他が保持中。呼び出し側は SKIP する
  fi
  if (( rc != 0 )); then
    # 取得そのものに失敗した (lockman のビルド失敗・壊れた lock・権限など)。
    # 「排他を用意できない」ので __av1ify_lock_unavailable に委ねる。既定は中止し、
    # AV1IFY_ALLOW_NO_LOCK=1 を明示した場合だけ排他なしで続行する。
    rm -f -- "$tok"
    __av1ify_lock_unavailable "lockman acquire が失敗しました (rc=$rc)"
    return $?
  fi

  __AV1IFY_LOCK_DIR="$dir"
  __AV1IFY_LOCK_TOKENFILE="$tok"
  __AV1IFY_LOCK_MODE="held"

  # 自動更新。親が消えたら道連れで止める (孤児が lease を持ち続けると、
  # 次の実行が 30 分待たされる)。
  #
  # 🚨 標準入出力を /dev/null へ落とす。落とさないと、この裏プロセスが**呼び出し元の
  # stdout パイプを掴んだまま**になる。parallel-each の --no-log はジョブの出力を
  # パイプで受けるので、掴まれると「本体は終わったのに EOF が来ない」= 子孫が
  # 居座る形になり、drain の猶予ぶん待たされたうえで SIGPIPE で殺される。
  # 実測 2026-09-08: 落とす前は、解放後もパイプが閉じず 120 秒のコマンドが完走しなかった。
  # 🚨 監視する PID は $$ ではなく sysparams[pid]。zsh の $$ は**サブシェルでも
  # 外側のシェルの PID**を返すので、$$ で見張ると「処理していたサブシェルが死んだのに
  # 対話シェルが生きている間ずっと更新し続ける孤児」ができ、後続が SKIP され続ける
  # (2026-09-08 の codex 敵対レビューが実測)。
  # 🚨 $$ へフォールバックしない。$$ はサブシェルでも外側を指すので、フォールバックすると
  # 「処理が終わっても更新し続ける孤児」が復活する。
  {
    local parent="$owner"
    while sleep "$__AV1IFY_LOCK_RENEW_SEC"; do
      kill -0 "$parent" 2>/dev/null || exit 0
      lockman renew "$dir" --token-file "$tok" >/dev/null 2>&1 || exit 1
    done
  } </dev/null >/dev/null 2>&1 &
  __AV1IFY_LOCK_RENEWER=$!
  # `&!` (= & + disown) は shellcheck が構文として解析できず、ファイル全体の検査が
  # 落ちる (SC1073)。$! を採ってから disown すれば同じ効果で、検査も通る。
  disown %% 2>/dev/null
  return 0
}

# まだ自分が保持しているか。renew の成否で見る (check は参考値で根拠にならない、と
# lockman の usage が明記している)。
__av1ify_lock_still_held() {
  case "${__AV1IFY_LOCK_MODE:-}" in
    nolock)
      return 0
      ;;
    held)
      [[ -n "$__AV1IFY_LOCK_DIR" && -n "$__AV1IFY_LOCK_TOKENFILE" ]] || return 1
      lockman renew "$__AV1IFY_LOCK_DIR" --token-file "$__AV1IFY_LOCK_TOKENFILE" >/dev/null 2>&1
      ;;
    *)
      return 1
      ;;
  esac
}

__av1ify_lock_release() {
  if [[ -n "$__AV1IFY_LOCK_RENEWER" ]]; then
    # 🚨 子 (sleep) を先に落とす。renewer 本体だけ kill すると sleep が親を失って
    # 残り、最大 renew 間隔ぶん生き続ける (実測 2026-09-08: ppid=1 の sleep が残った)。
    #
    # 🚨 **列挙と kill のあいだの競合 / PID 再利用は残っている。直さないと決めた**
    # (issues/done/340-risk-av1ify-lock-unverified-residuals.md 項目 2)。
    # pgrep で並べてから kill するまでに子が入れ替わる / 終了済み PID が再利用される形は
    # コード上ありうるが、**再現できていない**。閉じるには親子間の生存通知 (パイプ等) が要り、
    # shell の範囲では重い。取りこぼした sleep は最大 renew 間隔 (10 分) で自然に消え、
    # stdio は /dev/null なので呼び出し元のパイプも掴まない。
    # 再開の trigger: 孤児 renewer が lease を持ち続けて後続が SKIP され続ける事象を観測したとき。
    local _kid
    for _kid in ${(f)"$(pgrep -P "$__AV1IFY_LOCK_RENEWER" 2>/dev/null)"}; do
      [[ -n "$_kid" ]] && kill "$_kid" 2>/dev/null
    done
    kill "$__AV1IFY_LOCK_RENEWER" 2>/dev/null
    __AV1IFY_LOCK_RENEWER=""
  fi
  if [[ -n "$__AV1IFY_LOCK_DIR" ]]; then
    lockman release "$__AV1IFY_LOCK_DIR" --token-file "$__AV1IFY_LOCK_TOKENFILE" >/dev/null 2>&1
    __AV1IFY_LOCK_DIR=""
  fi
  if [[ -n "$__AV1IFY_LOCK_TOKENFILE" ]]; then
    rm -f -- "$__AV1IFY_LOCK_TOKENFILE"
    __AV1IFY_LOCK_TOKENFILE=""
  fi
  __AV1IFY_LOCK_MODE=""
}

# 共有名の一時出力 (<stem>-enc.mp4.in_progress) を消す唯一の入口。
#
# 🚨 この名前は入力から決まる**共有名**なので、lease を失った側が消すと、引き継いだ側の
# 作業中ファイルを壊す。finalize の喪失分岐だけを直しても、ffmpeg 失敗経路と割り込み
# ハンドラから同じ削除が復活していた (2026-09-08 の codex 敵対レビュー P1)。
# 排他なしで走っている場合 (__AV1IFY_LOCK_MODE=nolock) だけ無条件に消す。
# 未取得・解放済み (空文字) は、古いマーカーが残っていても消さない。
__av1ify_rm_own_tmp() {
  local tmp="$1"
  [[ -n "$tmp" && -e "$tmp" ]] || return 0
  case "${__AV1IFY_LOCK_MODE:-}" in
    nolock)
      local rm_rc
      if rm -f -- "$tmp"; then
        return 0
      else
        rm_rc=$?
        return "$rm_rc"
      fi
      ;;
    held)
      if __av1ify_lock_still_held; then
        local rm_rc
        if rm -f -- "$tmp"; then
          return 0
        else
          rm_rc=$?
          return "$rm_rc"
        fi
      fi
      print -ru2 -- "🚨 排他を失っているため一時ファイルを消しません (引き継いだ側が書いている可能性): $tmp"
      ;;
    *)
      print -ru2 -- "🚨 排他を取得していないか、すでに解放済みのため一時ファイルを消しません: $tmp"
      ;;
  esac
  return 1
}

# __av1ify_one を排他の下で実行する。呼び出し側はこちらを使うこと
# (__av1ify_one には return が多数あるので、解放は always ブロックに寄せる)。
__av1ify_one_locked() {
  # 🚨 err_exit をこの関数内で無効にする。呼び出し元が setopt err_exit していると、
  # acquire や __av1ify_one が非 0 を返した時点でシェルごと終了し、always ブロックに
  # 入らない = 解放も renewer の停止もトークン削除も取りこぼす (2026-09-08 の codex
  # 敵対レビューが実測)。rc は下で明示的に回収して return するので、呼び出し元から
  # 見た成否は変わらない。
  setopt local_options no_err_exit
  local in="$1"
  __av1ify_lock_acquire "$in"
  local rc=$?
  case $rc in
    0) ;;
    3) print -r -- "→ SKIP 他のプロセスが処理中です: $in"; return 0 ;;
    *) print -ru2 -- "${_C_RED}❌ 排他を取得できませんでした (rc=$rc): $in${_C_OFF}"; return 1 ;;
  esac
  {
    __av1ify_one "$in"
  } always {
    # __av1ify_one の早期 return が残したマーカーを、解放後の別状態で解釈させない。
    # 先に空にしてから lease と token を解放する。
    __AV1IFY_CURRENT_TMP=""
    __av1ify_lock_release
  }
}
