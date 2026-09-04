#!/usr/bin/env zsh
# go_autobuild.zsh — bin/<tool> ラッパー共通の「ソースが変わっていれば再ビルド」機構。
# bin/glogx, bin/parallel-each, bin/disassemble_excel が source する。
#
# 再ビルドするかは「ビルド入力の指紋」で決める (_go_autobuild_fingerprint / _go_autobuild_stale)。
# 前回ビルドした指紋を .autobuild.built に残し、起動のたびに今の指紋と比べるだけ (実測 0.66ms)。
# 🚨 「ソースの mtime > バイナリの mtime か」という順序比較には戻さない。順序で見ると、ビルド中に
# 着地した編集を拾うために「成果物の mtime にソースの状態を代表させる」必要が生じ、目印ファイル・
# touch -r・参照先不在時の -nt の挙動という壊れやすい仕掛けが芋づるで要る (2026-08-01 にそこから
# 2 件のバグが出て、翌日この方式へ移行した)。
# 🚨 git の commit / tree hash を判定に使わない。コミット済みの内容しか見えないので、未コミットの
# 編集 (= ソースを触りながら起動する開発ループ) を拾えない。tree hash は .autobuild.rev に
# 「どこから作ったか」として記録するだけで、判定には使わない (_go_autobuild_record_rev)。
#
# 使い方:
#   source "${0:A:h}/lib/go_autobuild.zsh"
#   go_autobuild_exec [--async] [--pkg <rel>] <src_dir> <name> -- "$@"
#
# --pkg <rel>: 1 つの module に複数の main を置く形 (src/doctor/cmd/svcdoctor 等) 用。
#   指紋と go build は module root (<src_dir>) で取り、成果物と作業ファイルは <src_dir>/<rel> に
#   置く (main ごとに別ディレクトリなので、同じ module の別 main と .autobuild.* が衝突しない)。
#   --pkg を省いて <src_dir>/<rel> を直接渡しても、上へ go.mod を探して同じ結論になる
#   (_go_autobuild_resolve_mod。go_autobuild_spawn_if_stale を外部から呼ぶ経路のため)。
# 指紋は go.mod の `replace X => <相対パス>` の先も含む (glogx → ../doctor)。取り込み先だけを直しても
#   再ビルドされる。
#   🚨 成果物の置き場を module root にしない: 同じ .autobuild.built を複数の main が上書きし合い、
#   「入力は同じ = ビルド済み」と読んで別 main の古いバイナリを走らせる。
#
# install 成功時は、差し替えたバイナリを 1 回起こして macOS の署名検証キャッシュを温める
# (_go_autobuild_warm_signature)。これをしないと「次回の初回 exec」が 17MB の glogx で 230ms
# かかり、tmux popup 起動の律速になる (実測 2026-09-05。ファイルを読むだけでは温まらない)。
# 🚨 新しいラッパーを足すときは、そのツールが未知フラグで即終了することを確かめる (温めの起こし方)。
#
# --async: 既存バイナリで即 exec し、再ビルドはバックグラウンドで走らせる (次回起動から反映)。
#   tmux popup から起動するツール (glogx) 向け。ビルド出力を人に見せないため。
#   出力を人/スクリプトが消費するツールでは使わない (古い結果を新コードの結果と誤認させる)。
# --async なしは同期ビルド (従来どおり stderr に進捗を出す)。
#
# GO_AUTOBUILD_SYNC=1 で --async を打ち消して同期ビルドにする (「今すぐ新版が欲しい」用)。
# GO_AUTOBUILD_LOCK_TIMEOUT (秒, 既定 1800) を超えた lock は死んでいるものとして奪う。
# GO_AUTOBUILD_FAILED_TTL (秒, 既定 600) を超えた失敗記録は無視して再挑戦する (下記)。
#
# src_dir に置く作業ファイル (いずれも .gitignore 済み):
#   .autobuild.built  前回ビルドした入力の指紋 (再ビルド判定の基準)
#   .autobuild.rev    そのビルド元の tree hash (診断用。判定には使わない)
#   .autobuild.failed 失敗したときの入力の指紋 (同じ入力での再挑戦を抑える)
#   .autobuild.lock   排他 (中に持ち主の pid) / .autobuild.log 進捗と失敗の記録
#   .autobuild.new.<pid>  rename 前の一時バイナリ

zmodload zsh/datetime 2>/dev/null
zmodload -F zsh/stat b:zstat 2>/dev/null
autoload -Uz is-at-least

# 自プロセスの pid を REPLY で返す。zsh の $$ はサブシェルでも親の pid のままなので、
# lock の持ち主判定と一時ファイル名には実 pid ($sysparams[pid]) が要る。
# ${sysparams[pid]:-$$} と書けないのが要点: zsh は未定義の連想配列への添字参照を
# set -u で「parameter not set」の fatal にするため、:- のフォールバックが効かず、
# zsh/system を持たない zsh でラッパー自体が起動しなくなる。
if zmodload zsh/system 2>/dev/null; then
  _go_autobuild_self_pid() { REPLY=${sysparams[pid]} }
else
  # 縮退: サブシェルでは親 pid になる。lock で直列化されるので実害は temp 名の一意性のみ。
  _go_autobuild_self_pid() { REPLY=$$ }
fi

# ソース集合は再帰 glob で取る。サブパッケージ (usage/ ovba/ 等) を含めるため。
# *_test.go は go build の入力ではないので除外する (テスト編集で無用な再ビルドを起こさない)。
# zsh の * は / を跨がないため **/*.go~*_test.go では usage/*_test.go を取りこぼす → ループ内で弾く。
# module root と build 対象を解く。reply=(mod_dir pkg)。
#   --pkg 指定 (typeset -g _GO_AUTOBUILD_MOD_DIR / _GO_AUTOBUILD_PKG) があればそれ。無ければ src_dir から
#   上へ go.mod を探す (src_dir 自身に go.mod があれば従来どおり mod_dir=src_dir, pkg=.)。
# 🚨 上へ探すのは、外部から `go_autobuild_spawn_if_stale <cmd_dir> <name>` と呼ばれたとき (glogx が
#   走行中に呼ぶ形) にも --pkg 相当を再現するため。module 変数は別プロセスに漏れないので、
#   cmd/<name> を渡されたら自力で module root を見つける必要がある (敵対レビュー 2026-09-02)。
# 🚨 パスは :A (symlink も解決) で正規化する。指紋はパス文字列を含むので、/var/... と /private/var/...
#   (macOS の /var は symlink) が呼び方で揺れると同じ入力が別の指紋になり、常に stale になる。
_go_autobuild_resolve_mod() {  # $1=src_dir
  local src_dir="${1:A}" d
  if [[ -n "${_GO_AUTOBUILD_MOD_DIR-}" ]]; then
    reply=("${_GO_AUTOBUILD_MOD_DIR:A}" "${_GO_AUTOBUILD_PKG:-.}")
    return 0
  fi
  d="$src_dir"
  while [[ "$d" != "/" && ! -e "$d/go.mod" ]]; do d="${d:h}"; done
  if [[ "$d" == "/" || "$d" == "$src_dir" ]]; then
    reply=("$src_dir" .)
  else
    reply=("$d" "./${src_dir#$d/}")
  fi
  return 0
}

# go.mod の `replace X => <相対パス>` の先 (別 module を相対パスで取り込む形。glogx → ../doctor)。
# reply=(絶対パス...)。🚨 取り込み先を編集しても src_dir の指紋は変わらないので、ここを入力に
# 含めないと「replace 先だけ直した」変更で旧バイナリを黙って exec する (敵対レビュー 2026-09-02, P1)。
# 1 行形式 (`replace a => ../b`) とブロック形式 (`replace (` ... `a => ../b` ... `)`) の両方を見る。
# 相対パス (./ か ../ で始まる) だけが対象。バージョン指定 (`a => b v1.2.3`) は無視する。
_go_autobuild_replace_dirs() {  # $1=mod_dir
  local mod_dir="$1" line target
  reply=()
  [[ -r "$mod_dir/go.mod" ]] || return 0
  while IFS= read -r line; do
    [[ "$line" == *'=>'* ]] || continue
    target="${line##*=>}"; target="${target## }"; target="${target%% *}"; target="${target%%$'\t'*}"
    [[ "$target" == ./* || "$target" == ../* ]] || continue
    reply+=("${mod_dir}/${target}"(:A))
  done < "$mod_dir/go.mod"
  return 0
}

# ビルド入力の集合を reply へ返す (*_test.go を除く .go + 各 module root の go.mod / go.sum。
# module root と replace 先を合わせる)。🚨 入力集合はここが唯一の定義 (指紋・縮退経路・
# glogx 側 src/glogx/autobuild.go の autobuildSourcesNewer と同じ定義を保つ)。
# `//go:embed` で .go 以外を焼き込むようになったら、そのアセットもここへ足すこと。
_go_autobuild_inputs() {  # $1=src_dir
  local root
  local -a roots files
  _go_autobuild_resolve_mod "$1"; roots=("$reply[1]")
  _go_autobuild_replace_dirs "$reply[1]"; roots+=("${reply[@]}")
  files=()
  for root in "${roots[@]}"; do
    files+=("$root"/**/*.go(N) "$root"/go.mod(N) "$root"/go.sum(N))
  done
  reply=(${files:#*_test.go})   # go build の入力ではない (テスト編集で再ビルドを起こさない)
  return 0
}

_go_autobuild_sources_newer_than() {  # 0 = 新しいソースがある (ref 不在も含む)
  local ref="$1" f
  [[ -e "$ref" ]] || return 0
  _go_autobuild_inputs "$2"
  for f in "${reply[@]}"; do
    [[ "$f" -nt "$ref" ]] && return 0
  done
  return 1
}

# 失敗記録が古びていれば再挑戦を許す。
#
# 🚨 これが無いと「一度の一時的な失敗で再ビルドが永久に止まる」: 失敗記録は pull の後に書かれる
# ので、backoff の条件「ソースが失敗記録より新しいか」は二度と成立しない。実証 (2026-07-31):
# pull 相当の状態で 1 回失敗させると、失敗要因を解消した後の 3 回の起動で go build が 0 回しか
# 呼ばれず、古いバイナリのまま固定された。glogx は go.mod が要求する Go が手元より新しく
# (1.26 vs 1.25)、初回ビルドが ~90MB の toolchain 取得に依存するため、この一時失敗が現実に起きる。
#
# 元の backoff の狙い (落ちるビルドを起動ごとに撒かない) は TTL で保つ: 最悪でも TTL ごとに 1 回。
# age が取れない環境 (zstat 不在) では期限切れと判定しない = 従来どおり保守的に止める。
_go_autobuild_failed_expired() {  # 0 = 記録が古い (再挑戦してよい)
  local stamp="$1" ttl=${GO_AUTOBUILD_FAILED_TTL:-600}
  [[ -e "$stamp" ]] || return 0
  _go_autobuild_age "$stamp"
  [[ -n "$REPLY" ]] && (( REPLY >= ttl ))
}

_go_autobuild_age() {  # REPLY = 経過秒 (取得不能なら空)
  local -a st
  REPLY=
  [[ -n "${EPOCHSECONDS-}" ]] || return 1   # zsh/datetime 不在時は age 不明として扱う
  zstat -A st +mtime -- "$1" 2>/dev/null || return 1
  REPLY=$(( EPOCHSECONDS - st[1] ))
}

# ファイル全体を REPLY へ読む ("" = 不在 / 空 / 読めない)。
#
# 🚨 存在確認してから読む。`$(<file)` は zsh の特殊形 (fork しない代わりに) 内側の 2>/dev/null が
# 効かず、不在時に "no such file or directory" を漏らす (実測 2026-08-01)。出力先は
# .autobuild.log で、そこは不具合追跡の唯一の手がかりなので、意味のないエラーで汚さない。
# 🚨 戻り値で成否を伝えない (常に 0)。呼び出し側は REPLY が空かどうかで判断する。
# 非 0 を返す設計にすると、将来ラッパーが set -e を足した瞬間に「ファイルが読めないだけ」で
# ビルドごと中断する地雷になる (現在の 4 つのラッパーはいずれも set -u のみ)。
_go_autobuild_slurp() {  # $1=path
  REPLY=
  [[ -f "$1" ]] || return 0
  REPLY=$(<"$1")
  return 0
}

# lock の持ち主 pid を REPLY で返す ("" = lock が無い / pid 未記入)。
_go_autobuild_lock_owner() {  # $1=lock dir
  _go_autobuild_slurp "$1/pid"
  REPLY=${REPLY%%$'\n'*}
  return 0
}

# ビルド入力の指紋を REPLY へ返す ("" = 取得不能)。各入力ファイルの パス + mtime + サイズ。
#
# 🚨 「ソースの mtime > バイナリの mtime か」という順序比較はしない。順序で見ると、ビルド中に
# 着地した編集を拾うために「成果物の mtime にソースの状態を代表させる」必要が生じ、そこから
# 目印ファイル・touch -r・参照先不在時の -nt の挙動、という壊れやすい仕掛けが芋づるで要る
# (実際そこから 2 件のバグが出た 2026-08-01)。記録した指紋と今の指紋を比べるだけなら、その
# 仕掛けが丸ごと不要になる。
# 🚨 指紋は mtime を信じている点は順序比較と同じ (rsync -a 等で mtime ごと巻き戻されると
# 騙される)。そこは glogx 側の stale 判定 (src/glogx/autobuild.go) が受け持つ。
# zstat は +<field> を 1 つしか取れないので 2 回に分ける。どちらも全ファイル一括 (実測 0.75ms)。
_go_autobuild_fingerprint() {  # $1=src_dir (入力集合は _go_autobuild_inputs: module root + replace 先)
  local i
  local -a files mt sz
  REPLY=
  _go_autobuild_inputs "$1"; files=("${reply[@]}")
  (( $#files )) || return 0
  zstat -A mt +mtime -- "${files[@]}" 2>/dev/null || return 0
  zstat -A sz +size  -- "${files[@]}" 2>/dev/null || return 0
  local out=
  for (( i = 1; i <= $#files; i++ )); do
    out+="${files[i]} $mt[i] $sz[i]"$'\n'   # 絶対パス (replace 先は src_dir の外にある)
  done
  # 🚨 末尾に改行を残さない。読み出し側の `$(<file)` は末尾改行を落とすので、残すと
  # 「書いた指紋」と「読んだ指紋」が毎回食い違い、常に stale = 起動ごとに再ビルドになる。
  REPLY=${out%$'\n'}
  return 0
}

# .autobuild.built を読み、reply=(ビルド開始時刻 指紋) を返す (不在なら (0 ""))。
#
# 🚨 指紋だけでなく開始時刻も持つ。install の可否は「順序」で決める必要があるため: 内容一致
# だけで見ると「あとから完走した方が無条件で降りる」になり、最新の入力でビルドした方が捨て
# られる (自己レビューで検出 2026-08-02)。旧実装は成果物の mtime を巻き戻すことで順序を
# 表していたが、その仕掛けごと廃したのでここに持たせる。
_go_autobuild_read_built() {  # $1=src_dir / reply=(開始時刻 指紋)
  local raw
  _go_autobuild_slurp "$1/.autobuild.built"; raw=$REPLY
  if [[ "$raw" != *$'\n'* ]]; then
    reply=(0 "")   # 1 行しか無い = 書きかけ / 旧形式。ビルド済みとみなさない
    return 0
  fi
  reply=("${raw%%$'\n'*}" "${raw#*$'\n'}")
  [[ "$reply[1]" == <->(.<->|) ]] || reply[1]=0
  return 0
}

# 再ビルドが要るか (0 = 要る)。🚨 「いつ再ビルドするか」の判定はこの関数だけが持つ。
# 呼び出し側に条件を散らすと、片方だけ直したときに shim とツール側で結論が食い違う。
_go_autobuild_stale() {  # $1=src_dir $2=bin $3=指紋
  local src_dir="$1" bin="$2" fp="$3"
  [[ -x "$bin" ]] || return 0            # 走らせるものが無い
  if [[ -z "$fp" ]]; then
    # 指紋が取れない環境 (zstat 不在) では順序比較へ縮退する
    _go_autobuild_sources_newer_than "$bin" "$src_dir"
    return $?
  fi
  _go_autobuild_read_built "$src_dir"
  [[ "$reply[2]" != "$fp" ]]
}

# 前回の失敗を踏まえて再挑戦してよいか (0 = よい)。
#
# 同じ入力で落ちるビルドを起動ごとに撒かないため、失敗した指紋と今の指紋が同じなら見送る。
# 🚨 ただし TTL を超えていれば挑戦する。これが無いと「一度の一時的な失敗で再ビルドが永久に
# 止まる」: 失敗要因が repo の外 (toolchain 取得の失敗等) にあると入力は変わらないので、
# 指紋の一致が永久に続く (実証 2026-07-31)。
_go_autobuild_should_retry() {  # $1=src_dir $2=指紋
  local stamp="$1/.autobuild.failed" fp="$2"
  [[ -e "$stamp" ]] || return 0
  if [[ -z "$fp" ]]; then
    # 指紋が取れない環境: 従来どおりソースが記録より新しいかで見る
    _go_autobuild_sources_newer_than "$stamp" "$1" && return 0
  else
    _go_autobuild_slurp "$stamp"
    [[ "$REPLY" != "$fp" ]] && return 0   # 入力が変わった = 別の挑戦
  fi
  _go_autobuild_failed_expired "$stamp"
}

# mkdir の atomicity で排他する。lock 内の pid で「持ち主が生きているか」を判定し、
# 死んでいれば奪う。これが無いと kill された builder の lock が永久に残り、以後
# 「stale だが誰かがビルド中」と誤認して黙って旧版に固定される (loud に落ちる今より悪化する)。
_go_autobuild_take_lock() {  # $1=lock dir $2=自分の pid / 0 = 取得, 1 = 他が実行中
  local lock="$1" pid="$2" holder timeout=${GO_AUTOBUILD_LOCK_TIMEOUT:-1800}
  if command mkdir "$lock" 2>/dev/null; then
    print -r -- "$pid" >| "$lock/pid" 2>/dev/null
    return 0
  fi
  # age が取れないとき (zstat 不在等) は timeout 判定を諦め、pid 判定にだけ従う。
  # 「不明なら奪う」にすると zstat の無い環境で毎回 lock を奪い多重ビルドになる。
  _go_autobuild_age "$lock"
  if [[ -z "$REPLY" ]] || (( REPLY < timeout )); then
    _go_autobuild_lock_owner "$lock"
    holder=$REPLY
    # pid 未記入は「mkdir 直後の別プロセス」= 生存扱い (取得直後の一瞬だけ起きる)
    [[ -z "$holder" ]] && return 1
    kill -0 "$holder" 2>/dev/null && return 1
  fi
  # timeout 超過の lock は pid が生きて見えても奪う (pid 再利用での永久固定を救済する)
  command rm -rf "$lock" 2>/dev/null
  command mkdir "$lock" 2>/dev/null || return 1
  print -r -- "$pid" >| "$lock/pid" 2>/dev/null
  return 0
}

# 一時ファイルへビルドしてから rename する。実行中バイナリを直接上書きしないため、
# ビルドが途中で死んでも旧版は壊れない (= 途中死が無害になる = async の前提)。
_go_autobuild_build() {  # $1=src_dir $2=name $3=quiet(0/1) $4=lock dir $5=自分の pid (省略可)
  local src_dir="${1:a}" name="$2" quiet="$3" lock="${4-}" lock_pid="${5-}"
  # 🚨 :a で絶対パスに正規化してから使う。下の go build は -C で src_dir へ移動するので、
  # 相対パスのままだと -o の出力先が移動後の cwd 基準になり、意図しない場所へ書く。
  # (現在の呼び出しは全て ${0:A:h} 由来で絶対だが、-C を使う限りこれは前提であって偶然ではない)
  # lock と同じ実 pid で一時ファイルを一意にする
  # (別シェルからの stale takeover が実行中 builder の一時ファイルを消すのを避ける)
  _go_autobuild_self_pid
  local bin="$src_dir/$name" tmp="$src_dir/.autobuild.new.$REPLY"
  # このビルドが「何を入力にしたか」を開始時点で確定させる。成功時にこれを記録するので、
  # ビルド実行中に着地した編集は次回の指紋比較で必ず差として出る (_go_autobuild_fingerprint の doc)。
  local built_fp started_at=${EPOCHREALTIME-0} seen_at seen_fp
  _go_autobuild_fingerprint "$src_dir"; built_fp=$REPLY
  # install の可否を決めるための基準 (下の guard の doc)。開始時点の記録を控える。
  _go_autobuild_read_built "$src_dir"; seen_at=$reply[1]; seen_fp=$reply[2]
  local local_go required_go mod_dir pkg
  _go_autobuild_resolve_mod "$src_dir"; mod_dir="$reply[1]"; pkg="$reply[2]"
  local_go=$(go env GOVERSION 2>/dev/null) || local_go=unknown
  required_go=$(awk '$1 == "go" {print $2; exit}' "$mod_dir/go.mod" 2>/dev/null)
  print -u2 -- "$name: building... (go=${local_go:-unknown} / go.mod=${required_go:-?})"
  # GOTOOLCHAIN は既定 (auto) のまま。local に固定すると go.mod の要求版に足りない環境で
  # 「go.mod requires go >= X」で失敗して手動対応が必要になる。auto なら toolchain (~90MB) を
  # DL して通るので、初回だけ遅い代わりに人手が要らない。
  # 警告は「手元 go が go.mod の要求より古い」ときだけ。前置き一致で判定すると
  # go1.25.4 vs go.mod 1.25.0 (手元が新しい) でも誤発火する。
  # `is-at-least 0 0` は「関数が実際に呼べるか」の probe。autoload は fpath に実体が
  # 無くても stub を作るため $+functions では判定できず、呼び出し失敗 (非 0) が
  # 「手元 go が古い」と誤読されて popup に無用な警告が漏れる。
  if [[ -n "$required_go" && "$local_go" == go* ]] && is-at-least 0 0 2>/dev/null \
    && ! is-at-least "$required_go" "${local_go#go}"; then
    (( quiet )) || print -u2 -- "$name: 初回は Go $required_go の toolchain 取得で時間がかかることがあります"
  fi
  local rc=0
  # 🚨 この go build を「サブシェルの中」や「バックグラウンドジョブ」にしない。zsh は fork した
  # サブシェル `( ... )` と `&` のジョブで trap をリセットするので、_go_autobuild_spawn が張った
  # `trap '' HUP TERM INT` の ignore がそこで失われる (bash は POSIX どおり ignore を継承するため
  # この罠は zsh 固有)。失われると、popup を閉じた瞬間に process group へ飛ぶ HUP で go build が
  # exit 129 (=128+SIGHUP) で死に、失敗記録が残って TTL が切れるまで旧版に固定される
  # (= 「古い版で動いています」が出続けてビルドされない)。ignore が届くのは、trap を張ったシェル
  # 自身が exec する foreground コマンドだけ。詳細と実測表: rules/zsh-trap-not-inherited.md
  # -C なら cd 用の fork が要らないのでこの条件を満たす (go 1.20 以降・かつ最初の引数)。
  # --pkg のときは module root で `go build ./<rel>` (成果物 $tmp は絶対パスなので置き場は変わらない)
  go build -C "$mod_dir" -o "$tmp" "$pkg" || rc=$?
  if (( rc )); then
    command rm -f "$tmp" 2>/dev/null
    # exit code を残す: コンパイルエラーなら go build 自身が理由を上に出すが、シグナルで
    # 殺された場合 (OOM の SIGKILL = 137 等) は何も出ないため、code が唯一の手がかりになる
    # (実例 2026-07-31: ログに "build failed" だけが残り原因を追えなかった)
    print -u2 -- "$name: build failed (exit $rc)"
    return 1
  fi
  # lock を奪われていたら install しない。奪った側は自分より新しいソースでビルドしており、
  # ここで上書きすると古い成果物が「バイナリ mtime = 今」で入って stale 判定を欺く
  # (= 黙って古い版に固定される)。失敗ではないので .autobuild.failed も作らない。
  _go_autobuild_lock_owner "$lock"
  if [[ -n "$lock" && "$REPLY" != "$lock_pid" ]]; then
    command rm -f "$tmp" 2>/dev/null
    print -u2 -- "$name: lock を奪われたため install を中止 (別の builder が入れている)"
    return 0
  fi
  # install を見送ってよいのは「自分のビルド中に別のビルドが実際に記録を書き換え、かつその相手が
  # 自分より後に始まった」ときだけ。上の lock 判定では足りない: 同期ビルド
  # (GO_AUTOBUILD_SYNC=1 = 「今すぐ新版が欲しい」) は lock を取らないので、走行中の builder から
  # 見て「lock は自分のまま」になり、ユーザーの復旧操作を古い成果物で巻き戻す。
  #
  # 🚨 2 つの条件は両方要る。片方ずつで実装して 2 回とも壊した (2026-08-01 / 08-02):
  #   「書き換わったか」だけ → 順序が無く、先に終わった古い入力のビルドが勝つ。最新の入力で
  #     ビルドした方が捨てられる
  #   「後に始まったか」だけ → 記録の絶対時刻を今の時計と無条件に比べるので、時計が巻き戻ると
  #     (NTP の step / スリープ復帰 / 進んだ時計のホストからの復元) 他に 1 本も走っていなくても
  #     install を捨てる。失敗ではないので backoff も効かず毎起動フルビルド + 全捨てになり、
  #     バイナリ不在の初回経路では exec が rc=127 で死ぬ (ツールが起動しない)
  # 両方を要求すれば、時計が壊れていても・時計が無くても、単独のビルドは必ず install される。
  _go_autobuild_read_built "$src_dir"
  if [[ "$reply[1]" != "$seen_at" || "$reply[2]" != "$seen_fp" ]] && (( reply[1] > started_at )); then
    command rm -f "$tmp" 2>/dev/null
    print -u2 -- "$name: あとから始まったビルドが既に入っているため install を中止"
    return 0
  fi
  command mv -f "$tmp" "$bin" || {
    command rm -f "$tmp" 2>/dev/null
    print -u2 -- "$name: install failed"
    return 1
  }
  # 何を、いつ始めて作ったかを記録する。次回の起動は指紋を比べるだけで stale を判定し、
  # 開始時刻は上の install ガード (順序判定) が使う。
  print -rn -- "$started_at"$'\n'"$built_fp" >| "$src_dir/.autobuild.built" 2>/dev/null
  _go_autobuild_record_rev "$src_dir"
  command rm -f "$src_dir/.autobuild.failed" 2>/dev/null
  _go_autobuild_warm_signature "$bin"
  return 0
}

# 差し替えたバイナリの署名検証キャッシュを温める (次の起動の律速をここで先払いする)。
#
# macOS は「新しい inode を初めて exec する」ときだけカーネルが Mach-O の署名を検証し、
# 結果を vnode に載せる。17MB の glogx では実測 230ms で、tmux popup 起動の律速はここだった
# (実測 2026-09-05: 新規コピーの初回 exec 0.23-0.25s / 2 回目以降 0.02-0.03s、n=5 で分離)。
# 裏ビルドの中で 1 回起こしておけば、次の popup は 0.02s 側から始まる。
#
# 🚨 ファイルを読むだけでは温まらない。実測した 3 案のうち効くのは実際の exec だけ:
#   cat "$bin" >/dev/null … 0.23s (page cache は律速ではない)
#   codesign -v "$bin"    … 0.23s (userland の検証はカーネルの vnode キャッシュに載らない)
#   exec して走り切らせる … 0.02s ← これだけ
# 🚨 spawn 直後に kill -9 する形も温まらない (n=10 で 0/10)。exec の完了前に死ぬと検証されない。
#   「即 kill でも温まった」ように見えたのは、kill が競合で外れて走り切っていたときだけ。
#
# 起こし方は未知フラグ。どのツールも引数解析で落ちるので本体のロジックは走らない。
# 🚨 ここには timeout が無い。未知フラグで終了しないツールを足すと裏ビルドがここで止まる。
# その前提は tests/bin/test_go_autobuild_warmup.sh が守る: go_autobuild_exec を呼ぶ bin/* を
# 全部ビルドして --__autobuild_warmup__ で起こし、5 秒以内に終わることを確かめる
# (対象はラッパーから grep で取るので、新しいラッパーは自動で検査対象になる)。
#
# 同期ビルド経路 (GO_AUTOBUILD_SYNC=1 / バイナリ不在の初回) では直後の exec が自分で
# 温めるので、ここでの 1 回は二重になる (実測 +20ms 程度)。1.4s のビルドを払っている経路
# なので、経路ごとに分岐させず一律で温める方を採る。
_go_autobuild_warm_signature() {  # $1=バイナリの絶対パス
  [[ -x "$1" ]] || return 0
  "$1" --__autobuild_warmup__ >/dev/null 2>&1 </dev/null || true
  return 0
}

# ビルド元の tree hash を .autobuild.rev へ記録する (診断専用)。
#
# 🚨 再ビルドの判定には使わない。tree hash はコミット済みの内容しか見ないので、未コミットの
# 編集を拾えない (= glogx を触りながら起動する開発ループが再ビルドされなくなる)。記録だけなら
# 嘘をつかない: 未コミットの変更があれば +dirty を添える。
# これがあると「古い版で動いています」を「動いているのは tree abc1234 / 今は def5678」と言える。
# git を 3 回 fork するが、走るのはビルド成功時だけなので起動のホットパスには乗らない。
_go_autobuild_record_rev() {  # $1=src_dir (記録先)。tree hash と dirty は module root 基準
  local src_dir="$1" mod_dir top rel rev
  _go_autobuild_resolve_mod "$src_dir"; mod_dir="$reply[1]"
  top=$(command git -C "$mod_dir" rev-parse --show-toplevel 2>/dev/null) || return 0
  rel=${mod_dir#$top/}
  [[ "$rel" == "$mod_dir" ]] && rel=""   # module root が repo のルートそのもの
  rev=$(command git -C "$mod_dir" rev-parse "HEAD:$rel" 2>/dev/null) || return 0
  # 🚨 diff でなく status で見る。diff は追跡対象しか比較しないので、まだ git add していない
  # 新規 .go を見落とす。それは go build の入力に入る (= その版はコミットに存在しないコードで
  # 動いている) ので、記録が clean を名乗ると診断が嘘になる (自己レビューで検出 2026-08-02)。
  # 🚨 対象を指紋と同じ入力集合に絞る (*_test.go を除く .go + go.mod + go.sum)。
  # ディレクトリ全体を見ると、成果物や作業ファイルを .gitignore していない repo で常に
  # +dirty になる。*_test.go を含めても同じことが起きる: テストは go build の入力ではないので
  # 成果物はコミットの内容そのものなのに、テストを常時いじるこの repo では +dirty が
  # 出っぱなしになり記録が何も言わなくなる (自己レビューで検出 2026-08-02)。
  # 🚨 replace 先の dirty は見ない (別 module の tree なので、この記録の「どこから作ったか」には
  # 含めない。指紋には含めているので再ビルド判定は正しい)
  [[ -n "$(command git -C "$mod_dir" status --porcelain \
      -- '*.go' go.mod go.sum ':(exclude)*_test.go' 2>/dev/null)" ]] \
    && rev+=" +dirty"
  print -r -- "$rev" >| "$src_dir/.autobuild.rev" 2>/dev/null
  return 0
}

_go_autobuild_spawn() {  # $1=src_dir $2=name
  local src_dir="$1" name="$2"
  local lock="$src_dir/.autobuild.lock" log="$src_dir/.autobuild.log"
  (
    # ignore された disposition は fork/exec を越えて go build にも継承される。popup を閉じた
    # ときに process group へ飛ぶ HUP で巻き添えにされないため (これが実際に起きていた経路)。
    #
    # 🚨 守れるのは HUP と INT だけで、TERM は守れない。Go ランタイムは継承した SIG_IGN を
    # HUP / INT については尊重するが、TERM には自前ハンドラを張り直すため、trap を張っていても
    # go build は exit 143 (=128+SIGTERM) で死ぬ (実測 2026-08-01)。TERM を並べているのは
    # builder シェル自身と、Go でない子 (将来足すかもしれない) を守るため。
    # 🚨 ここに TERM 対策を足さない: popup / pane を閉じる経路が送るのは pty 切断による HUP で、
    # TERM を送る主体は現状いない。居ない相手向けの防御コードは、効くかどうかも確かめられない。
    #
    # 🚨 zsh はサブシェルとバックグラウンドジョブで trap を既定へ戻すので、この下でどちらかを
    # 掘ると ignore がそこで切れる (rules/zsh-trap-not-inherited.md)。
    trap '' HUP TERM INT
    _go_autobuild_self_pid
    local pid=$REPLY
    _go_autobuild_take_lock "$lock" "$pid" || exit 0
    # 🚨 解放してよいのは自分が持ち主のときだけ。timeout 超過で奪われた後に無条件で rm -rf すると
    # 「奪った側の lock」まで消し、以後その src は無施錠になって多重ビルドが漏れる。
    trap '_go_autobuild_lock_owner "$lock"; [[ "$REPLY" == "$pid" ]] && command rm -rf "$lock" 2>/dev/null' EXIT
    # 前回の途中死が残した作業ファイルを掃除する。
    #
    # 🚨 生きている builder のものは消さない。同期ビルド (GO_AUTOBUILD_SYNC=1 / バイナリ不在の
    # 初回) は lock を取らないのでこの掃除と直列化されず、走行中の一時ファイルを巻き込むと
    # その builder は mv 先を失って "install failed" で落ちる。持ち主の pid が生きていない
    # ものだけを消す (自己レビューで検出 2026-08-01)。
    local stale owner
    for stale in "$src_dir"/.autobuild.new.*(N); do
      owner=${${stale:t}#.autobuild.new.}
      owner=${owner%%.*}
      [[ "$owner" == <-> ]] && kill -0 "$owner" 2>/dev/null && continue
      command rm -f "$stale" 2>/dev/null
    done
    print -r -- "--- $(strftime '%Y-%m-%d %H:%M:%S' $EPOCHSECONDS) $name (pid=$pid)"
    if ! _go_autobuild_build "$src_dir" "$name" 1 "$lock" "$pid"; then
      # 失敗を記録する。これが無いと stale が解消されないまま毎回起動ごとに
      # 落ちるビルドを撒き続ける (popup を開くたび go build が湧く)。
      # 何を入力にして落ちたかを残す。同じ入力なら再挑戦しない (_go_autobuild_should_retry)。
      _go_autobuild_fingerprint "$src_dir"
      print -rn -- "$REPLY" >| "$src_dir/.autobuild.failed" 2>/dev/null
    fi
  ) >>"$log" 2>&1 </dev/null &!
}

# go_autobuild_spawn_if_stale <src_dir> <name>
#
# 「今この瞬間、再ビルドが要るか」を判定して、要るなら裏ビルドを起動する。
# 0 = 起動した / 1 = 不要 (最新) か見送り (backoff・バイナリ不在)。
#
# 起動時の判定 (go_autobuild_exec) と同じ規準を、走行中のツールから使うための入口。
# glogx が pull 後に呼ぶ: pull で自分のソースが更新されたら、その場でビルドを始めて完成したら
# 再起動を提案する (ツールを手で起動し直す手間をなくすため。ユーザー要望 2026-08-05)。
#
# 🚨 判定をツール側に写経させない。stale の規準 (指紋) と backoff (同じ入力での再挑戦抑制) は
# ここが正本で、二重に実装すると必ずずれる。多重起動は _go_autobuild_spawn の lock が防ぐ。
go_autobuild_spawn_if_stale() {
  local src_dir="$1" name="$2" bin="$src_dir/$name" fp
  [[ -x "$bin" ]] || return 1   # 走らせるものが無い = この入口の前提外 (exec 側の同期ビルドの領分)
  _go_autobuild_fingerprint "$src_dir"
  fp=$REPLY
  _go_autobuild_stale "$src_dir" "$bin" "$fp" || return 1
  # 前回と同じ入力で落ちているなら再挑戦しない (fail-open で旧版のまま進む)
  _go_autobuild_should_retry "$src_dir" "$fp" || return 1
  _go_autobuild_spawn "$src_dir" "$name"
}

go_autobuild_exec() {
  local async=0 pkg=""
  while (( $# )); do
    case "$1" in
      --async) async=1; shift ;;
      --pkg)
        # 🚨 値が無いと zsh の shift 2 は何も shift せず、この while が --pkg を永久に見る (無限ループ +
        # stderr 洪水。敵対レビュー 2026-09-02)。ラッパーの書き間違いは loud に止める
        (( $# >= 2 )) || { print -u2 -- "go_autobuild_exec: --pkg には値が要る"; exit 2 }
        pkg="$2"; shift 2 ;;
      *) break ;;
    esac
  done
  local src_dir="$1" name="$2"; shift 2
  if [[ -n "$pkg" ]]; then
    # module root は指紋と go build に、<rel> は成果物と作業ファイルに使う (冒頭の doc)。
    # 下の関数群は src_dir しか受けないので、module root と package は module 変数で渡す。
    # spawn のサブシェルにも継承される (export は要らない。子プロセスの go には渡さない)。
    typeset -g _GO_AUTOBUILD_MOD_DIR="${src_dir:a}" _GO_AUTOBUILD_PKG="./${pkg#./}"
    src_dir="$src_dir/${pkg#./}"
  else
    unset _GO_AUTOBUILD_MOD_DIR _GO_AUTOBUILD_PKG   # 同一シェルで前の呼び出しの値を引きずらない
  fi
  [[ "${1-}" == "--" ]] && shift
  local bin="$src_dir/$name"
  [[ -n "${GO_AUTOBUILD_SYNC-}" ]] && async=0

  local fp
  _go_autobuild_fingerprint "$src_dir"
  fp=$REPLY

  if [[ ! -x "$bin" ]]; then
    # 走らせるものが無い初回だけは同期でビルドする (async にできない)
    _go_autobuild_build "$src_dir" "$name" 0 || exit 1
  elif (( async )); then
    # 判定と spawn は go_autobuild_spawn_if_stale が持つ (走行中のツールからも同じ入口を使う)。
    if go_autobuild_spawn_if_stale "$src_dir" "$name"; then
      # 起動するツールへ「裏でビルド中」を伝える。旧版で exec するため、ツール側からは
      # 新版の完成もビルド失敗も観測できず無言だった (失敗すると気づかないまま旧版に固定
      # される)。読む側は任意で、今は glogx がこれを見て決着をトースト通知する
      # (src/glogx/autobuild.go)。名前を変えるなら読む側も直すこと。
      export GO_AUTOBUILD_PENDING=1
    fi
    # 起動しなかった場合 (最新 / 前回の失敗が backoff で効いている) は何も渡さない。ツール側は
    # 「.autobuild.failed が自バイナリより新しいか」で同じ結論に達せるため (glogx の
    # autobuildStaleBinary)。env で伝えるとこの分岐を通った瞬間しか伝わらず、TTL 超過での
    # 再挑戦・shim を経ない起動・別セッションの失敗を取りこぼす。
  elif _go_autobuild_stale "$src_dir" "$bin" "$fp"; then
    _go_autobuild_build "$src_dir" "$name" 0 || exit 1
  fi

  exec "$bin" "$@"
}
