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

_go_autobuild_age() {  # REPLY = 経過秒 (取得不能なら空)
  local -a st
  REPLY=
  [[ -n "${EPOCHSECONDS-}" ]] || return 1   # zsh/datetime 不在時は age 不明として扱う
  zstat -A st +mtime -- "$1" 2>/dev/null || return 1
  REPLY=$(( EPOCHSECONDS - st[1] ))
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
    holder=$(<"$lock/pid") 2>/dev/null
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
  local src_dir="$1" name="$2" quiet="$3" lock="${4-}" lock_pid="${5-}"
  # lock と同じ実 pid で一時ファイルを一意にする
  # (別シェルからの stale takeover が実行中 builder の一時ファイルを消すのを避ける)
  _go_autobuild_self_pid
  local bin="$src_dir/$name" tmp="$src_dir/.autobuild.new.$REPLY"
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
  if ! (cd "$src_dir" && go build -o "$tmp" .); then
    command rm -f "$tmp" 2>/dev/null
    print -u2 -- "$name: build failed"
    return 1
  fi
  # lock を奪われていたら install しない。奪った側は自分より新しいソースでビルドしており、
  # ここで上書きすると古い成果物が「バイナリ mtime = 今」で入って stale 判定を欺く
  # (= 黙って古い版に固定される)。失敗ではないので .autobuild.failed も作らない。
  if [[ -n "$lock" && "$(<"$lock/pid" 2>/dev/null)" != "$lock_pid" ]]; then
    command rm -f "$tmp" 2>/dev/null
    print -u2 -- "$name: lock を奪われたため install を中止 (別の builder が入れている)"
    return 0
  fi
  command mv -f "$tmp" "$bin" || {
    command rm -f "$tmp" 2>/dev/null
    print -u2 -- "$name: install failed"
    return 1
  }
  command rm -f "$src_dir/.autobuild.failed" 2>/dev/null
  return 0
}

_go_autobuild_spawn() {  # $1=src_dir $2=name
  local src_dir="$1" name="$2"
  local lock="$src_dir/.autobuild.lock" log="$src_dir/.autobuild.log"
  (
    # ignore された disposition は fork/exec を越えて go build にも継承される。popup を閉じた
    # ときに process group へ飛ぶ TERM/HUP で巻き添えにされないため (nohup は HUP しか防がない)。
    trap '' HUP TERM INT
    _go_autobuild_self_pid
    local pid=$REPLY
    _go_autobuild_take_lock "$lock" "$pid" || exit 0
    trap 'command rm -rf "$lock" 2>/dev/null' EXIT
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
      # 失敗記録より新しいソースが無ければ再挑戦しない (fail-open で旧版のまま進む)
      if _go_autobuild_sources_newer_than "$src_dir/.autobuild.failed" "$src_dir"; then
        _go_autobuild_spawn "$src_dir" "$name"
        # 起動するツールへ「裏でビルド中」を伝える。旧版で exec するため、ツール側からは
        # 新版の完成もビルド失敗も観測できず無言だった (失敗すると気づかないまま旧版に固定
        # される)。読む側は任意で、今は glogx がこれを見て決着をトースト通知する
        # (src/glogx/autobuild.go)。名前を変えるなら読む側も直すこと。
        export GO_AUTOBUILD_PENDING=1
      fi
    else
      _go_autobuild_build "$src_dir" "$name" 0 || exit 1
    fi
  fi

  exec "$bin" "$@"
}
