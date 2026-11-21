# ~/src の下で、ディレクトリから上へ `.src-env` を探し、書いてある `KEY=パス` を export する (direnv の allow の要らない版)。
# 外へ出たら元の値に戻す (元が無ければ unset)。例: my-products の root の `.src-env` に `UMBRELLA_ROOT=$DIR` と書くと、
# その下 (submodule の中も) で $UMBRELLA_ROOT が umbrella の root になる。
#
# なぜ allow が要らないか: ファイルを実行せず、`KEY=値` として読むだけにしている。direnv が allow を求めるのは
# `.envrc` をシェルのコードとして実行するから。読むだけでも、変数によってはコマンドを走らせられる (GIT_SSH_COMMAND・PATH・DYLD_*) ので、
# 値はパス (`$DIR` で始まるか `/` で始まる) に限り、下の DENY の名前は受け付けない。範囲は SRC_ENV_ROOT (指定しないときは ~/src) の下だけ。
# 範囲の外では何もしない (clone しただけの他人の repo を ~/src の外に置く限り、そのファイルは読まれない)。
#
# ファイルの書式 (1 行 1 つ。空行と # で始まる行は飛ばす):
#   KEY=$DIR              ($DIR / ${DIR} は、その .src-env があるディレクトリ)
#   KEY=$DIR/sub/dir
#   KEY=/absolute/path
# KEY は [A-Z_][A-Z0-9_]*。上のディレクトリと同じ KEY を下のディレクトリが書いたら、下 (今いる場所に近い方) が勝つ。
# 受け付けない行は、同じファイルの同じ行につき 1 回だけ stderr に知らせて飛ばす。
#
# 対話のシェルの chpwd でだけ動く。make・Xcode・cron から起こされるスクリプトには届かないので、スクリプトは今までどおり
# 自分で親のディレクトリを辿って root を探す。
# fork しない (chpwd は cd のたびに走る。zshlib/_git_prompt.zsh と同じく [[ -f ]] と組み込みの read だけを使う)。

typeset -gA _SRC_ENV_SAVED     # KEY -> 書き換える前の値
typeset -gA _SRC_ENV_HAD       # KEY -> 1 (書き換える前に値があった) / 0 (無かった)
typeset -gA _SRC_ENV_WARNED    # "<file>:<行番号>" -> 1
typeset -ga _SRC_ENV_APPLIED   # 今 export している KEY

# _src_env_deny は KEY を受け付けないとき 0 を返す
_src_env_deny() {
  case "$1" in
    PATH|FPATH|MANPATH|INFOPATH|HOME|SHELL|ZDOTDIR|PWD|OLDPWD|TMPDIR|USER|LOGNAME|EDITOR|VISUAL|PAGER) return 0 ;;
    DYLD_*|LD_*|GIT_*|SSH_*|SRC_ENV_*|PYTHON*|NODE_*|RUBY*|PERL*|BASH_*|ZSH_*) return 0 ;;
  esac
  return 1
}

# _src_env_parse <file> <assoc の名前> は file の行を読み、受け付けた KEY=値 を assoc に入れる
_src_env_parse() {
  emulate -L zsh
  local file="$1" target="$2" dir="${1:h}" line key value n=0
  while IFS= read -r line || [[ -n "$line" ]]; do
    (( n++ ))
    line="${line##[[:space:]]#}"
    line="${line%%[[:space:]]#}"
    [[ -z "$line" || "$line" == \#* ]] && continue
    if [[ ! "$line" =~ '^([A-Z_][A-Z0-9_]*)=(.*)$' ]]; then
      _src_env_warn "$file" "$n" "KEY=値 の形でない"
      continue
    fi
    key="${match[1]}"
    value="${match[2]}"
    [[ "$value" == \"*\" && ${#value} -ge 2 ]] && value="${value[2,-2]}"
    if _src_env_deny "$key"; then
      _src_env_warn "$file" "$n" "$key は受け付けない名前"
      continue
    fi
    case "$value" in
      '$DIR'|'${DIR}') value="$dir" ;;
      '$DIR/'*) value="$dir/${value#\$DIR/}" ;;
      '${DIR}/'*) value="$dir/${value#\$\{DIR\}/}" ;;
    esac
    if [[ "$value" != /* || "$value" == *'$'* || "$value" == *'`'* ]]; then
      _src_env_warn "$file" "$n" "値は \$DIR か / で始まるパスだけ"
      continue
    fi
    eval "${target}[\$key]=\$value"
  done < "$file"
}

_src_env_warn() {
  local id="$1:$2"
  (( ${+_SRC_ENV_WARNED[$id]} )) && return
  _SRC_ENV_WARNED[$id]=1
  print -u2 "src-env: $1 の $2 行目を飛ばした ($3)"
}

# _src_env_apply は今のディレクトリに合わせて export / 戻しをする (chpwd と起動のときに呼ぶ)
_src_env_apply() {
  emulate -L zsh
  local root="${SRC_ENV_ROOT:-$HOME/src}" dir key
  local -a files
  local -A want
  root="${root:A}"
  dir="${PWD:A}"
  if [[ -d "$root" && ( "$dir" == "$root" || "$dir" == "$root"/* ) ]]; then
    while :; do
      [[ -f "$dir/.src-env" ]] && files=("$dir/.src-env" "${files[@]}")
      [[ "$dir" == "$root" || "$dir" == / ]] && break
      dir="${dir:h}"
    done
  fi
  # 外側から順に読み、内側の同じ KEY で上書きする
  local f
  for f in "${files[@]}"; do
    _src_env_parse "$f" want
  done

  # 前に export したが、もう要らない KEY を戻す
  for key in "${_SRC_ENV_APPLIED[@]}"; do
    (( ${+want[$key]} )) && continue
    if [[ "${_SRC_ENV_HAD[$key]}" == 1 ]]; then
      export "$key=${_SRC_ENV_SAVED[$key]}"
    else
      unset "$key"
    fi
    unset "_SRC_ENV_SAVED[$key]" "_SRC_ENV_HAD[$key]"
  done
  _SRC_ENV_APPLIED=()

  for key in "${(@k)want}"; do
    if (( ! ${+_SRC_ENV_HAD[$key]} )); then
      if (( ${+parameters[$key]} )); then
        _SRC_ENV_HAD[$key]=1
        _SRC_ENV_SAVED[$key]="${(P)key}"
      else
        _SRC_ENV_HAD[$key]=0
      fi
    fi
    export "$key=${want[$key]}"
    _SRC_ENV_APPLIED+=("$key")
  done
}

# umbrella_root [<root からのパス>] は umbrella (my-products) の root (かその下のパス) を stdout に出す。root の .src-env が入れる
# UMBRELLA_ROOT を読むが、変数を直接使わずにこれを通す: umbrella の外では変数が空のまま `$UMBRELLA_ROOT/apps` が `/apps` になり、
# 気づかずに別の場所を指す。ここは空・指す先が無い・足したパスが無い、のどれでも stderr に理由を出して rc 1 を返す
# (使い方: `p="$(umbrella_root apps/obaket)" && cd "$p"`)。
# git rev-parse --show-superproject-working-tree を使わないのは、submodule の worktree と root では空を返すから
umbrella_root() {
  emulate -L zsh
  local r="${UMBRELLA_ROOT:-}" p
  if [[ -z "$r" ]]; then
    print -u2 "umbrella_root: UMBRELLA_ROOT が無い (my-products の下で使う。root の .src-env が入れる)"
    return 1
  fi
  if [[ ! -d "$r" ]]; then
    print -u2 "umbrella_root: UMBRELLA_ROOT ($r) のディレクトリが無い"
    return 1
  fi
  p="$r${1:+/$1}"
  if [[ ! -e "$p" ]]; then
    print -u2 "umbrella_root: $p が無い"
    return 1
  fi
  print -r -- "$p"
}

# cdu [<root からのパス>] は umbrella の root (かその下) へ移る。移れないときは移らずに umbrella_root の理由を出す
cdu() {
  local p
  p="$(umbrella_root "$@")" || return 1
  cd "$p"
}
