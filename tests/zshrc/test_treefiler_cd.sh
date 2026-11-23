#!/usr/bin/env zsh
# zshlib/_treefiler_cd.zsh の tf (treefiler を q で閉じたら cd する) のテスト。
# 実体の tf を、--cwd-file に FAKE_TF_WRITE の中身を書く偽物に差し替え、呼んだ後の cwd・rc・一時ファイルを見る。

set -uo pipefail
unset CDPATH

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)

TMP_ROOT=$(mktemp -d)
cleanup() { rm -rf "$TMP_ROOT"; }
trap cleanup EXIT
TMP_ROOT="${TMP_ROOT:A}"

mkdir -p "$TMP_ROOT/bin" "$TMP_ROOT/tmp" "$TMP_ROOT/start" "$TMP_ROOT/dest dir"
cat > "$TMP_ROOT/bin/tf" <<'EOF'
#!/usr/bin/env zsh
# 偽の tf: 引数を記録し、--cwd-file の先に FAKE_TF_WRITE を書く (unset なら書かない)。rc は FAKE_TF_RC
print -r -- "$*" > "$FAKE_TF_ARGS"
[[ $1 == --cwd-file ]] || exit 97
(( ${+FAKE_TF_WRITE} )) && print -rn -- "$FAKE_TF_WRITE" > "$2"
exit ${FAKE_TF_RC:-0}
EOF
chmod +x "$TMP_ROOT/bin/tf"
path=("$TMP_ROOT/bin" $path)
export TMPDIR="$TMP_ROOT/tmp"
export FAKE_TF_ARGS="$TMP_ROOT/args"

source "$ROOT_DIR/zshlib/_treefiler_cd.zsh"

fails=0
ok() { print "✓ $1"; }
ng() { print -u2 "✗ $1"; fails=$(( fails + 1 )); }
leftovers() { local -a l=("$TMP_ROOT"/tmp/*(N)); print ${#l}; }

# q で閉じた: 書かれたフォルダ (空白入り) へ cd し、引数をそのまま渡し、一時ファイルを残さない
cd "$TMP_ROOT/start" || exit 1
FAKE_TF_WRITE="$TMP_ROOT/dest dir" tf some/dir 2>"$TMP_ROOT/err1"; rc=$?
if [[ $PWD == "$TMP_ROOT/dest dir" && $rc == 0 ]]; then ok "書かれたフォルダへ cd する"; else ng "cd しない: PWD=$PWD rc=$rc"; fi
if [[ $(<"$FAKE_TF_ARGS") == "--cwd-file $TMP_ROOT/tmp/treefiler-cd."*" some/dir" ]]; then ok "引数を --cwd-file の後へ渡す"; else ng "引数: $(<"$FAKE_TF_ARGS")"; fi
if [[ $(leftovers) == 0 ]]; then ok "一時ファイルを残さない"; else ng "一時ファイルが残った: $(ls "$TMP_ROOT/tmp")"; fi

# esc で閉じた (何も書かない): cd しない・rc を返す
cd "$TMP_ROOT/start" || exit 1
unset FAKE_TF_WRITE
FAKE_TF_RC=3 tf 2>"$TMP_ROOT/err2"; rc=$?
if [[ $PWD == "$TMP_ROOT/start" && $rc == 3 ]]; then ok "書かれていなければ cd せず rc を返す"; else ng "PWD=$PWD rc=$rc"; fi
if [[ ! -s $TMP_ROOT/err2 ]]; then ok "書かれていなければ何も言わない"; else ng "stderr: $(<"$TMP_ROOT/err2")"; fi

# 書かれたフォルダが消えていた: cd せず知らせる
FAKE_TF_WRITE="$TMP_ROOT/gone" tf 2>"$TMP_ROOT/err3"; rc=$?
if [[ $PWD == "$TMP_ROOT/start" && $rc == 1 ]] && grep -q 'cd 先がありません' "$TMP_ROOT/err3"; then ok "無いフォルダへは cd せず知らせ、rc=1 を返す"; else ng "PWD=$PWD rc=$rc err=$(<"$TMP_ROOT/err3")"; fi
if [[ $(leftovers) == 0 ]]; then ok "失敗しても一時ファイルを残さない"; else ng "一時ファイルが残った"; fi

# 名前が改行で終わるフォルダ ($(<f) で読むと末尾の改行が落ちて別のパスになる)
nl_dir="$TMP_ROOT/nl"$'\n'
mkdir -p "$nl_dir"
FAKE_TF_WRITE="$nl_dir" tf 2>"$TMP_ROOT/err4"
if [[ $PWD == "$nl_dir" ]]; then ok "改行で終わる名前のフォルダへも cd する"; else ng "PWD=${(q)PWD} err=$(<"$TMP_ROOT/err4")"; fi

if (( fails )); then
  print -u2 "FAIL: $fails 件"
  exit 1
fi
print "PASS"
