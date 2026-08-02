#!/usr/bin/env zsh
# go_autobuild.zsh — bin/<tool> ラッパー共通の「ソースが変わっていれば再ビルド」機構。
# bin/glogx, bin/git-popup, bin/parallel-each, bin/disassemble_excel が source する。
#
# 再ビルドするかは「ビルド入力の指紋」で決める (_go_autobuild_fingerprint / _go_autobuild_stale)。
# 前回ビルドした指紋を .autobuild.built に残し、起動のたびに今の指紋と比べるだけ (実測 0.66ms)。
# ⚠️ 「ソースの mtime > バイナリの mtime か」という順序比較には戻さない。順序で見ると、ビルド中に
# 着地した編集を拾うために「成果物の mtime にソースの状態を代表させる」必要が生じ、目印ファイル・
# touch -r・参照先不在時の -nt の挙動という壊れやすい仕掛けが芋づるで要る (2026-08-01 にそこから
# 2 件のバグが出て、翌日この方式へ移行した)。
# ⚠️ git の commit / tree hash を判定に使わない。コミット済みの内容しか見えないので、未コミットの
# 編集 (= ソースを触りながら起動する開発ループ) を拾えない。tree hash は .autobuild.rev に
# 「どこから作ったか」として記録するだけで、判定には使わない (_go_autobuild_record_rev)。
#
# 使い方:
#   source "${0:A:h}/lib/go_autobuild.zsh"
#   go_autobuild_exec [--async] <src_dir> <name> -- "$@"
#
# --async: 既存バイナリで即 exec し、再ビルドはバックグラウンドで走らせる (次回起動から反映)。
#   tmux popup から起動するツール (glogx / git-popup) 向け。ビルド出力を人に見せないため。
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
_go_autobuild_sources_newer_than() {  # 0 = 新しいソースがある (ref 不在も含む)
  local ref="$1" src_dir="$2" f
  [[ -e "$ref" ]] || return 0
  for f in "$src_dir"/**/*.go(N) "$src_dir"/go.mod(N) "$src_dir"/go.sum(N); do
    [[ "$f" == *_test.go ]] && continue
    [[ "$f" -nt "$ref" ]] && return 0
  done
  return 1
}

# 失敗記録が古びていれば再挑戦を許す。
#
# ⚠️ これが無いと「一度の一時的な失敗で再ビルドが永久に止まる」: 失敗記録は pull の後に書かれる
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
# ⚠️ 存在確認してから読む。`$(<file)` は zsh の特殊形 (fork しない代わりに) 内側の 2>/dev/null が
# 効かず、不在時に "no such file or directory" を漏らす (実測 2026-08-01)。出力先は
# .autobuild.log で、そこは不具合追跡の唯一の手がかりなので、意味のないエラーで汚さない。
# ⚠️ 戻り値で成否を伝えない (常に 0)。呼び出し側は REPLY が空かどうかで判断する。
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
# ⚠️ 「ソースの mtime > バイナリの mtime か」という順序比較はしない。順序で見ると、ビルド中に
# 着地した編集を拾うために「成果物の mtime にソースの状態を代表させる」必要が生じ、そこから
# 目印ファイル・touch -r・参照先不在時の -nt の挙動、という壊れやすい仕掛けが芋づるで要る
# (実際そこから 2 件のバグが出た 2026-08-01)。記録した指紋と今の指紋を比べるだけなら、その
# 仕掛けが丸ごと不要になる。
# ⚠️ 指紋は mtime を信じている点は順序比較と同じ (rsync -a 等で mtime ごと巻き戻されると
# 騙される)。そこは glogx 側の stale 判定 (src/glogx/autobuild.go) が受け持つ。
# zstat は +<field> を 1 つしか取れないので 2 回に分ける。どちらも全ファイル一括 (実測 0.75ms)。
_go_autobuild_fingerprint() {  # $1=src_dir
  local src_dir="$1" i
  local -a files mt sz
  REPLY=
  # ⚠️ 入力集合はここが唯一の定義。`//go:embed` で .go 以外を焼き込むようになったら、その
  # アセットもここへ足すこと (足さないと、テンプレや静的ファイルだけを直しても再ビルドされない)。
  # 2026-08-02 時点では src/ 配下のどのツールも go:embed / go.work / vendor を使っていない。
  files=("$src_dir"/**/*.go(N) "$src_dir"/go.mod(N) "$src_dir"/go.sum(N))
  files=(${files:#*_test.go})   # go build の入力ではない (テスト編集で再ビルドを起こさない)
  (( $#files )) || return 0
  zstat -A mt +mtime -- "${files[@]}" 2>/dev/null || return 0
  zstat -A sz +size  -- "${files[@]}" 2>/dev/null || return 0
  local out=
  for (( i = 1; i <= $#files; i++ )); do
    out+="${files[i]#$src_dir/} $mt[i] $sz[i]"$'\n'
  done
  # ⚠️ 末尾に改行を残さない。読み出し側の `$(<file)` は末尾改行を落とすので、残すと
  # 「書いた指紋」と「読んだ指紋」が毎回食い違い、常に stale = 起動ごとに再ビルドになる。
  REPLY=${out%$'\n'}
  return 0
}

# .autobuild.built を読み、reply=(ビルド開始時刻 指紋) を返す (不在なら (0 ""))。
#
# ⚠️ 指紋だけでなく開始時刻も持つ。install の可否は「順序」で決める必要があるため: 内容一致
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

# 再ビルドが要るか (0 = 要る)。⚠️ 「いつ再ビルドするか」の判定はこの関数だけが持つ。
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
# ⚠️ ただし TTL を超えていれば挑戦する。これが無いと「一度の一時的な失敗で再ビルドが永久に
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
  # ⚠️ :a で絶対パスに正規化してから使う。下の go build は -C で src_dir へ移動するので、
  # 相対パスのままだと -o の出力先が移動後の cwd 基準になり、意図しない場所へ書く。
  # (現在の呼び出しは全て ${0:A:h} 由来で絶対だが、-C を使う限りこれは前提であって偶然ではない)
  # lock と同じ実 pid で一時ファイルを一意にする
  # (別シェルからの stale takeover が実行中 builder の一時ファイルを消すのを避ける)
  _go_autobuild_self_pid
  local bin="$src_dir/$name" tmp="$src_dir/.autobuild.new.$REPLY"
  # このビルドが「何を入力にしたか」を開始時点で確定させる。成功時にこれを記録するので、
  # ビルド実行中に着地した編集は次回の指紋比較で必ず差として出る (_go_autobuild_fingerprint の doc)。
  local built_fp started_at=${EPOCHREALTIME-0}
  _go_autobuild_fingerprint "$src_dir"; built_fp=$REPLY
  local local_go required_go
  local_go=$(go env GOVERSION 2>/dev/null) || local_go=unknown
  required_go=$(awk '$1 == "go" {print $2; exit}' "$src_dir/go.mod" 2>/dev/null)
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
  # ⚠️ この go build を「サブシェルの中」や「バックグラウンドジョブ」にしない。zsh は fork した
  # サブシェル `( ... )` と `&` のジョブで trap をリセットするので、_go_autobuild_spawn が張った
  # `trap '' HUP TERM INT` の ignore がそこで失われる (bash は POSIX どおり ignore を継承するため
  # この罠は zsh 固有)。失われると、popup を閉じた瞬間に process group へ飛ぶ HUP で go build が
  # exit 129 (=128+SIGHUP) で死に、失敗記録が残って TTL が切れるまで旧版に固定される
  # (= 「古い版で動いています」が出続けてビルドされない)。ignore が届くのは、trap を張ったシェル
  # 自身が exec する foreground コマンドだけ。詳細と実測表: rules/zsh-trap-not-inherited.md
  # -C なら cd 用の fork が要らないのでこの条件を満たす (go 1.20 以降・かつ最初の引数)。
  go build -C "$src_dir" -o "$tmp" . || rc=$?
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
  # 自分より「あとに始まった」ビルドが既に入っていたら踏まない。上の lock 判定では足りない:
  # 同期ビルド (GO_AUTOBUILD_SYNC=1 = 「今すぐ新版が欲しい」) は lock を取らないので、走行中の
  # builder から見て「lock は自分のまま」になり、ユーザーの復旧操作を古い成果物で巻き戻す。
  #
  # ⚠️ 「入っているか」でなく「あとに始まったか」で見る。完走の順で決めると、先に終わった古い
  # 入力のビルドが勝ってしまい、最新の入力でビルドした方が捨てられる。あとに始まった方が新しい
  # 入力を見ているので、開始の順が入力の新しさの順になる。
  _go_autobuild_read_built "$src_dir"
  if (( reply[1] > started_at )); then
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
  return 0
}

# ビルド元の tree hash を .autobuild.rev へ記録する (診断専用)。
#
# ⚠️ 再ビルドの判定には使わない。tree hash はコミット済みの内容しか見ないので、未コミットの
# 編集を拾えない (= glogx を触りながら起動する開発ループが再ビルドされなくなる)。記録だけなら
# 嘘をつかない: 未コミットの変更があれば +dirty を添える。
# これがあると「古い版で動いています」を「動いているのは tree abc1234 / 今は def5678」と言える。
# git を 3 回 fork するが、走るのはビルド成功時だけなので起動のホットパスには乗らない。
_go_autobuild_record_rev() {  # $1=src_dir
  local src_dir="$1" top rel rev
  top=$(command git -C "$src_dir" rev-parse --show-toplevel 2>/dev/null) || return 0
  rel=${src_dir#$top/}
  [[ "$rel" == "$src_dir" ]] && rel=""   # src_dir が repo のルートそのもの
  rev=$(command git -C "$src_dir" rev-parse "HEAD:$rel" 2>/dev/null) || return 0
  # ⚠️ diff でなく status で見る。diff は追跡対象しか比較しないので、まだ git add していない
  # 新規 .go を見落とす。それは go build の入力に入る (= その版はコミットに存在しないコードで
  # 動いている) ので、記録が clean を名乗ると診断が嘘になる (自己レビューで検出 2026-08-02)。
  # ⚠️ 対象を指紋と同じ入力集合に絞る (*_test.go を除く .go + go.mod + go.sum)。
  # ディレクトリ全体を見ると、成果物や作業ファイルを .gitignore していない repo で常に
  # +dirty になる。*_test.go を含めても同じことが起きる: テストは go build の入力ではないので
  # 成果物はコミットの内容そのものなのに、テストを常時いじるこの repo では +dirty が
  # 出っぱなしになり記録が何も言わなくなる (自己レビューで検出 2026-08-02)。
  [[ -n "$(command git -C "$src_dir" status --porcelain \
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
    # ⚠️ 守れるのは HUP と INT だけで、TERM は守れない。Go ランタイムは継承した SIG_IGN を
    # HUP / INT については尊重するが、TERM には自前ハンドラを張り直すため、trap を張っていても
    # go build は exit 143 (=128+SIGTERM) で死ぬ (実測 2026-08-01)。TERM を並べているのは
    # builder シェル自身と、Go でない子 (将来足すかもしれない) を守るため。
    # ⚠️ ここに TERM 対策を足さない: popup / pane を閉じる経路が送るのは pty 切断による HUP で、
    # TERM を送る主体は現状いない。居ない相手向けの防御コードは、効くかどうかも確かめられない。
    #
    # ⚠️ zsh はサブシェルとバックグラウンドジョブで trap を既定へ戻すので、この下でどちらかを
    # 掘ると ignore がそこで切れる (rules/zsh-trap-not-inherited.md)。
    trap '' HUP TERM INT
    _go_autobuild_self_pid
    local pid=$REPLY
    _go_autobuild_take_lock "$lock" "$pid" || exit 0
    # ⚠️ 解放してよいのは自分が持ち主のときだけ。timeout 超過で奪われた後に無条件で rm -rf すると
    # 「奪った側の lock」まで消し、以後その src は無施錠になって多重ビルドが漏れる。
    trap '_go_autobuild_lock_owner "$lock"; [[ "$REPLY" == "$pid" ]] && command rm -rf "$lock" 2>/dev/null' EXIT
    # 前回の途中死が残した作業ファイルを掃除する。
    #
    # ⚠️ 生きている builder のものは消さない。同期ビルド (GO_AUTOBUILD_SYNC=1 / バイナリ不在の
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

go_autobuild_exec() {
  local async=0
  while (( $# )); do
    case "$1" in
      --async) async=1; shift ;;
      *) break ;;
    esac
  done
  local src_dir="$1" name="$2"; shift 2
  [[ "${1-}" == "--" ]] && shift
  local bin="$src_dir/$name"
  [[ -n "${GO_AUTOBUILD_SYNC-}" ]] && async=0

  local fp
  _go_autobuild_fingerprint "$src_dir"
  fp=$REPLY

  if [[ ! -x "$bin" ]]; then
    # 走らせるものが無い初回だけは同期でビルドする (async にできない)
    _go_autobuild_build "$src_dir" "$name" 0 || exit 1
  elif _go_autobuild_stale "$src_dir" "$bin" "$fp"; then
    if (( async )); then
      # 前回と同じ入力で落ちているなら再挑戦しない (fail-open で旧版のまま進む)
      if _go_autobuild_should_retry "$src_dir" "$fp"; then
        _go_autobuild_spawn "$src_dir" "$name"
        # 起動するツールへ「裏でビルド中」を伝える。旧版で exec するため、ツール側からは
        # 新版の完成もビルド失敗も観測できず無言だった (失敗すると気づかないまま旧版に固定
        # される)。読む側は任意で、今は glogx がこれを見て決着をトースト通知する
        # (src/glogx/autobuild.go)。名前を変えるなら読む側も直すこと。
        export GO_AUTOBUILD_PENDING=1
      fi
      # 再挑戦しない場合 (前回の失敗が backoff で効いている) は何も渡さない。ツール側は
      # 「.autobuild.failed が自バイナリより新しいか」で同じ結論に達せるため (glogx の
      # autobuildStaleBinary)。env で伝えるとこの分岐を通った瞬間しか伝わらず、TTL 超過での
      # 再挑戦・shim を経ない起動・別セッションの失敗を取りこぼす。
    else
      _go_autobuild_build "$src_dir" "$name" 0 || exit 1
    fi
  fi

  exec "$bin" "$@"
}
