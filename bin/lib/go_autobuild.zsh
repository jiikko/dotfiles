#!/usr/bin/env zsh
# go_autobuild.zsh — bin/<tool> ラッパー共通の「ソースが新しければ再ビルド」機構。
# bin/glogx, bin/git-popup, bin/parallel-each, bin/disassemble_excel が source する。
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

# lock の持ち主 pid を REPLY で返す ("" = lock が無い / pid 未記入)。
#
# ⚠️ 存在確認してから読む。`$(<file)` は zsh の特殊形 (fork しない代わりに) 内側の 2>/dev/null が
# 効かず、不在時に "no such file or directory" を漏らす (実測 2026-08-01)。この関数の出力先は
# .autobuild.log で、そこは不具合追跡の唯一の手がかりなので、意味のないエラーで汚さない。
# ⚠️ 戻り値で成否を伝えない (常に 0)。呼び出し側は REPLY が空かどうかで判断する。
# 非 0 を返す設計にすると、将来ラッパーが set -e を足した瞬間に「lock が読めないだけ」で
# ビルドごと中断する地雷になる (現在の 4 つのラッパーはいずれも set -u のみ)。
_go_autobuild_lock_owner() {  # $1=lock dir
  REPLY=
  [[ -f "$1/pid" ]] || return 0
  REPLY=$(<"$1/pid")
  REPLY=${REPLY%%$'\n'*}
  return 0
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
  # started は「このビルドが見たソースの時点」の目印。⚠️ 名前は .autobuild.new.* の掃除に載せる
  # (途中死したときに残さないため)。成果物の mtime をこれに合わせるので、ビルド実行中に着地した
  # 編集は次の stale 判定で拾われる。install した時刻にすると、その編集の mtime < 成果物の mtime に
  # なり「ソースの方が新しい」が二度と成立しない = その編集は永久に取り込まれない。
  # (この repo の開発ループ = glogx を触りながら popup で起動する、がまさにこれを踏む)
  local started="$src_dir/.autobuild.new.$REPLY.at"
  command touch "$started" 2>/dev/null
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
    command rm -f "$tmp" "$started" 2>/dev/null
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
    command rm -f "$tmp" "$started" 2>/dev/null
    print -u2 -- "$name: lock を奪われたため install を中止 (別の builder が入れている)"
    return 0
  fi
  # 走行中に、より新しいバイナリが入っていたら踏まない。上の lock 判定では足りない: 同期ビルド
  # (GO_AUTOBUILD_SYNC=1 = 「今すぐ新版が欲しい」) は lock を取らないので、走行中の builder から
  # 見て「lock は自分のまま」になり、ユーザーの復旧操作を古い成果物で黙って巻き戻す。
  if [[ -e "$bin" && "$bin" -nt "$started" ]]; then
    command rm -f "$tmp" "$started" 2>/dev/null
    print -u2 -- "$name: 走行中に新しいバイナリが入ったため install を中止"
    return 0
  fi
  command mv -f "$tmp" "$bin" || {
    command rm -f "$tmp" "$started" 2>/dev/null
    print -u2 -- "$name: install failed"
    return 1
  }
  # 成果物の mtime を「ビルドが見たソースの時点」へ揃える (started の doc)。
  # ⚠️ 巻き戻しても旧バイナリの mtime より必ず後なので、走っている glogx の「新版が入った」検知
  # (src/glogx/autobuild.go の classifyAutobuild = バイナリ mtime が増えたか) は壊れない。
  command touch -r "$started" "$bin" 2>/dev/null
  command rm -f "$started" 2>/dev/null
  command rm -f "$src_dir/.autobuild.failed" 2>/dev/null
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
    # 前回の途中死が残した一時ファイルを掃除する
    command rm -f "$src_dir"/.autobuild.new.*(N) 2>/dev/null
    print -r -- "--- $(strftime '%Y-%m-%d %H:%M:%S' $EPOCHSECONDS) $name (pid=$pid)"
    if ! _go_autobuild_build "$src_dir" "$name" 1 "$lock" "$pid"; then
      # 失敗を記録する。これが無いと stale が解消されないまま毎回起動ごとに
      # 落ちるビルドを撒き続ける (popup を開くたび go build が湧く)。
      # ソースが更新されるまで再挑戦しない = mtime 比較を stamp にそのまま流用する。
      command touch "$src_dir/.autobuild.failed" 2>/dev/null
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

  if [[ ! -x "$bin" ]]; then
    # 走らせるものが無い初回だけは同期でビルドする (async にできない)
    _go_autobuild_build "$src_dir" "$name" 0 || exit 1
  elif _go_autobuild_sources_newer_than "$bin" "$src_dir"; then
    if (( async )); then
      # 失敗記録より新しいソースが無ければ再挑戦しない (fail-open で旧版のまま進む)。
      # ただし記録が古びていれば一時的な失敗とみなして再挑戦する (_go_autobuild_failed_expired)。
      if _go_autobuild_sources_newer_than "$src_dir/.autobuild.failed" "$src_dir" \
        || _go_autobuild_failed_expired "$src_dir/.autobuild.failed"; then
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
