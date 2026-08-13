#!/bin/sh
# test_changed.sh — 変更したパスから「回すべき既存テストターゲット」を導出して実行する。
#
# make test-changed PATHS="..." の実行本体。CI の paths filter (src_*.yml) の
# ローカル対応物で、「触った範囲だけ検証する」を Makefile の既存ターゲットへの
# 写像として提供する。共有 working tree では git の dirty に並行セッションの変更が
# 混ざるため、対象パスの自動推定はせず必ず引数で受け取る (pathspec commit と同じ規律)。
#
# 写像はディレクトリ前方一致の粗い粒度に留める (詳細は --help)。どの写像にも
# 該当しないパスは fail にする — 黙って skip すると「未検証なのに green」になり、
# このスクリプトの存在価値 (検証範囲の保証) が壊れるため。その場合は root の
# `make test` で全体を回すか、この写像に新しい対応を足す。
set -u

usage() {
  cat <<'EOF'
usage: test_changed.sh [--dry-run] <path>...
       make test-changed PATHS="<path> <path>..." [DRY_RUN=1]

変更したファイルのパスから、回すべきテストターゲットを導出して実行する。
パスは repo root からの相対 (git status が出す形)。存在しないパス (削除した
ファイル) も写像対象になる。

  --dry-run   実行せず、選ばれたターゲットだけを表示する
  --help      このヘルプを表示する

写像 (前方一致・上から先勝ち):
  src/<proj>/...              → make -C src/<proj> test   (Go プロジェクト単位)
  .github/...                 → test-actionlint test-yaml
  tests/nvim/ _nviminit.lua   → test-nvim
  tests/tmux/ _tmux.conf      → test-tmux (+ shell 系 lint)
  tests/setup/...             → test-setup
  mac/karabiner.json          → test-json test-karabiner
  *.json                      → test-json
  *.yml / *.yaml              → test-yaml
  _gitconfig                  → test-gitconfig
  Brewfile _pryrc _gemrc      → test-ruby-syntax
  bin/ scripts/ zshlib/ tests/ _claude/hooks/ と、場所を問わない *.sh / _z*
                              → test-syntax test-shellcheck test-zsh-syntax test-zshrc
  *.md *.txt LICENSE issues/ docs/ vendor/ _claude/(agents|rules|skills|references|commands)/
                              → テスト対象なし (何も回さず、その旨を報告して成功)

どれにも該当しないパスはエラーで止まる (黙って skip しない)。その変更は
`make test` で全体を回すか、この写像に対応を追加してから使うこと。

例:
  make test-changed PATHS="_claude/hooks/tmux-pane-state.sh _claude/settings.json"
  make test-changed PATHS="src/glogx/tui.go src/glogx/render.go"
  make test-changed PATHS="zshlib/_concat.zsh" DRY_RUN=1
EOF
}

dry_run=0
case "${1:-}" in
  --help|-h) usage; exit 0 ;;
  --dry-run) dry_run=1; shift ;;
esac

if [ $# -eq 0 ]; then
  echo "✗ 対象パスが指定されていません" >&2
  echo >&2
  usage >&2
  exit 1
fi

targets=""   # make ターゲット (スペース区切り、重複は add_target が除去)
go_dirs=""   # make -C で回す Go プロジェクトディレクトリ

add_target() {
  case " $targets " in *" $1 "*) ;; *) targets="$targets $1" ;; esac
}

add_go_dir() {
  case " $go_dirs " in *" $1 "*) ;; *) go_dirs="$go_dirs $1" ;; esac
}

# shell/zsh ソースに対する共通セット。lint (sh 系 + zsh 系の両方; どちらの構文かは
# ファイルごとに Makefile 側の振り分けが決めるため両方回す) + zshrc 実行テスト
add_shell_targets() {
  add_target test-syntax
  add_target test-shellcheck
  add_target test-zsh-syntax
  add_target test-zshrc
}

fail=0
notest=""
for p in "$@"; do
  case "$p" in
    src/*/*)
      add_go_dir "src/$(echo "$p" | cut -d/ -f2)" ;;
    .github/*)
      add_target test-actionlint; add_target test-yaml ;;
    tests/nvim/*|_nviminit.lua|nvim/*)
      add_target test-nvim ;;
    tests/tmux/*|_tmux.conf)
      add_target test-tmux; add_shell_targets ;;
    tests/setup/*)
      add_target test-setup ;;
    mac/karabiner.json)
      add_target test-json; add_target test-karabiner ;;
    *.json)
      add_target test-json ;;
    *.yml|*.yaml)
      add_target test-yaml ;;
    _gitconfig)
      add_target test-gitconfig ;;
    Brewfile|_pryrc|_gemrc)
      add_target test-ruby-syntax ;;
    # shell/zsh ソース。ディレクトリ前方一致に加え、置き場所を問わない *.sh /
    # zsh dotfile (_z*) も拾う (shellcheck/zsh -n の対象は discover_shell_scripts が
    # repo 全体から発見するため、ここも場所で絞らない)
    bin/*|scripts/*|zshlib/*|tests/*|_claude/hooks/*|*.sh|_z*)
      add_shell_targets ;;
    # テスト対象なし (明示写像)。ドキュメント・vendor・データファイルは対応する
    # テストが存在しないので何も回さないが、黙って落とすのではなく報告する
    *.md|*.txt|LICENSE|issues/*|docs/*|vendor/*|kinesis*|_claude/agents/*|_claude/rules/*|_claude/skills/*|_claude/references/*|_claude/commands/*)
      notest="$notest $p" ;;
    *)
      echo "✗ 写像に無いパス: $p (make test で全体を回すか、scripts/test_changed.sh に写像を足すこと)" >&2
      fail=1 ;;
  esac
done
[ "$fail" -eq 0 ] || exit 1
[ -z "$notest" ] || echo "[test-changed] テスト対象なし (ドキュメント等):$notest"

echo "[test-changed] targets:${targets:-" (なし)"}${go_dirs:+ / go:$go_dirs}"
[ "$dry_run" -eq 1 ] && exit 0

for t in $targets; do
  make "$t" || exit 1
done
for d in $go_dirs; do
  make -C "$d" test || exit 1
done
