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
パスは repo root からの相対 (git status が出す形。先頭の ./ は付いていても
剥がして扱う)。存在しないパス (削除したファイル) も写像対象になる。
空白・改行を含むファイル名は非対応 (make の $(PATHS) 展開で分割される。
tests/ の run_tests と同じ repo 既存の前提)。

  --dry-run   実行せず、選ばれたターゲットだけを表示する
              (make 経由は DRY_RUN=1。1 との厳密一致のみ有効で、DRY_RUN=0 は無効)
  --help      このヘルプを表示する

写像 (前方一致・上から先勝ち):
  src/<proj>/...              → make -C src/<proj> lint test  (CI の _go-project.yml と同等)
  .github/...                 → test-actionlint test-yaml
  nvim/ _nviminit.lua         → test-nvim
  _tmux.conf                  → test-tmux (+ shell 系 lint)
  tests/<dir>/...             → make test-dir DIR=tests/<dir>  (そのディレクトリのテストを直接実行)
  mac/karabiner.json          → test-json test-karabiner
  *.json                      → test-json
  *.yml / *.yaml              → test-yaml
  _gitconfig                  → test-gitconfig
  Brewfile _pryrc _gemrc      → test-ruby-syntax
  _claude/hooks/ _claude/**.sh → shell 系 lint + make test-dir DIR=tests/claude
  _claude/CLAUDE.md _claude/(agents|rules|skills|references|commands)/
                              → make test-dir DIR=tests/claude (skill 参照表・symlink 健全性)
  bin/ scripts/ zshlib/ と、場所を問わない *.sh / _z*
                              → test-syntax test-shellcheck test-zsh-syntax test-zshrc
  *.md *.txt LICENSE issues/ docs/ vendor/
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

targets=""    # make ターゲット (スペース区切り、重複は add_target が除去)
go_dirs=""    # make -C で回す Go プロジェクトディレクトリ
test_dirs=""  # make test-dir DIR=... で回す tests/ 配下ディレクトリ

add_target() {
  case " $targets " in *" $1 "*) ;; *) targets="$targets $1" ;; esac
}

add_go_dir() {
  case " $go_dirs " in *" $1 "*) ;; *) go_dirs="$go_dirs $1" ;; esac
}

add_test_dir() {
  case " $test_dirs " in *" $1 "*) ;; *) test_dirs="$test_dirs $1" ;; esac
}

# shell/zsh ソースに対する共通セット。lint (sh 系 + zsh 系の両方; どちらの構文かは
# ファイルごとに Makefile 側の振り分けが決めるため両方回す) + zshrc 実行テスト
add_shell_targets() {
  add_target test-syntax
  add_target test-shellcheck
  add_target test-zsh-syntax
  add_target test-zshrc
}

# 変更されたスクリプトを **名前で参照しているテストディレクトリ**も回す。
# ⚠️ ここを「どのディレクトリを回すか」の登録表にしないこと。この repo は「登録なしで対象に
# なる」を全域で徹底しており (SHELLCHECK_FILES は discover_shell_scripts、テストは test-dir の
# 自動発見)、写像を手で列挙すると必ず腐る。実際 issues/done/060 は tests/claude の写像漏れを
# 直したが「tests/tmux / tests/bin にも同種の漏れがあるかは未網羅」と開いたまま残しており、
# その穴を監査 071 が指摘して反証にも耐えた (shell 系のどのパスも lint 4 種へ潰れ、
# tests/tmux 等に一度も到達していなかった)。
#
# ⚠️ grep の rc は 3 分岐する: 0 = 参照あり / 1 = 参照なし / >=2 = 検査できなかった。
# 「検査できなかった」を「参照なし」に畳むと、テストを回さない方向へ倒れる (= 未検証なのに
# green)。ここでは安全側 = **回す方**へ倒す (余分なテストが走るだけで害がない)。
# ⚠️ POSIX sh なので local は使えない (SC3043)。変数名を _ref_ 接頭辞で衝突回避する。
add_test_dirs_referencing() {  # $1=変更されたパス
  _ref_base="${1##*/}"
  [ -n "$_ref_base" ] || return 0
  for _ref_d in tests/*/; do
    _ref_d="${_ref_d%/}"
    [ -d "$_ref_d" ] || continue
    grep -rqlF -- "$_ref_base" "$_ref_d" 2>/dev/null
    _ref_rc=$?
    case "$_ref_rc" in
      0) add_test_dir "$_ref_d" ;;
      1) : ;;
      # ⚠️ この分岐はテストで pin できていない (grep を確実に rc>=2 にする状況を写像テストの
      # 中で作れないため。変異させても緑のまま = 守られていない)。消すと「検査できなかった」が
      # 「参照なし」に畳まれ、未検証なのにテストを回さない方向へ倒れるので、意図をここに残す。
      *) add_test_dir "$_ref_d" ;;   # 検査できなかった = 回す方へ倒す
    esac
  done
}

fail=0
notest=""
for p in "$@"; do
  p="${p#./}"  # git status は ./ を付けないが、人が付けても同じ写像に落とす
  case "$p" in
    src/*/*)
      # 2 番目のセグメントをプロジェクト名として取り出す。`src/../x` のような
      # トラバーサルは make -C src/.. (= repo root 全体) に化けるため弾く
      proj=$(echo "$p" | cut -d/ -f2)
      case "$proj" in
        ''|.|..|-*)
          echo "✗ 不正な src プロジェクト名: $p" >&2; fail=1 ;;
        *) add_go_dir "src/$proj" ;;
      esac ;;
    .github/*)
      add_target test-actionlint; add_target test-yaml ;;
    _nviminit.lua|nvim/*)
      add_target test-nvim ;;
    _tmux.conf)
      add_target test-tmux; add_shell_targets ;;
    # tests/<dir>/ 配下はそのディレクトリのテストを直接回す (make test-dir)。
    # 名前付きターゲット (test-nvim 等) が無い tests/claude, tests/bin 等も
    # 取りこぼさない。tests/ 直下のファイルは tests 全体
    tests/*/*)
      add_test_dir "tests/$(echo "$p" | cut -d/ -f2)"; add_target test-lint-tests ;;
    tests/*)
      add_test_dir "tests"; add_target test-lint-tests ;;
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
    # _claude 配下の shell は lint に加えて tests/claude も回す (issue 060):
    # test_deny_bare_tmux_kill.sh が hooks を、test_statusline.sh が
    # statusline-command.sh を実テストしている
    _claude/hooks/*|_claude/*.sh)
      add_shell_targets; add_test_dir "tests/claude"
      add_test_dirs_referencing "$p" ;;
    # shell/zsh ソース。ディレクトリ前方一致に加え、置き場所を問わない *.sh /
    # zsh dotfile (_z*) も拾う (shellcheck/zsh -n の対象は discover_shell_scripts が
    # repo 全体から発見するため、ここも場所で絞らない)
    bin/*|scripts/*|zshlib/*|*.sh|_z*)
      add_shell_targets
      add_test_dirs_referencing "$p" ;;
    # _claude の設定群は「テスト対象なし」ではない (issue 060): tests/claude が
    # skill 参照表 (CLAUDE.md ↔ skills の相互整合) と ~/.claude 側 symlink の
    # dangling (rename/削除の置き去り) を検証している
    _claude/CLAUDE.md|_claude/agents/*|_claude/rules/*|_claude/skills/*|_claude/references/*|_claude/commands/*)
      add_test_dir "tests/claude" ;;
    # issue ファイルは「テスト対象なし」ではない: tests/issues が NNN の一意性を検証する
    # (追加・改番がそのまま検査のトリガー。2026-08-28 に 127 と 133 が同時に衝突した)
    issues/*)
      add_test_dir "tests/issues" ;;
    # テスト対象なし (明示写像)。ドキュメント・vendor・データファイルは対応する
    # テストが存在しないので何も回さないが、黙って落とすのではなく報告する
    *.md|*.txt|LICENSE|docs/*|vendor/*|kinesis*)
      notest="$notest $p" ;;
    *)
      echo "✗ 写像に無いパス: $p (make test で全体を回すか、scripts/test_changed.sh に写像を足すこと)" >&2
      fail=1 ;;
  esac
done
# notest の報告は fail 判定より先に出す (fail と混在しても「テスト対象なしと
# 判定された」事実が読めるように)
[ -z "$notest" ] || echo "[test-changed] テスト対象なし (ドキュメント等):$notest"
[ "$fail" -eq 0 ] || exit 1

echo "[test-changed] targets:${targets:-" (なし)"}${test_dirs:+ / tests:$test_dirs}${go_dirs:+ / go:$go_dirs}"
if [ "$dry_run" -eq 1 ]; then
  echo "[test-changed] dry-run: テストは実行していません"
  exit 0
fi

# ⚠️ **1 本目の失敗で残りを隠さない** (issue 130)。触った範囲の検証が途中で止まると、
#   「直したら別の赤が出た」を繰り返すことになる。全部走らせてから、失敗をまとめて返す。
failed=""
for t in $targets; do
  make "$t" || failed="$failed $t"
done
for d in $test_dirs; do
  make test-dir DIR="$d" || failed="$failed test-dir:$d"
done
for d in $go_dirs; do
  # CI (_go-project.yml) は lint と test を**別 job** で回すので、ここも 2 回に分けて呼ぶ。
  # `make -C "$d" lint test` の 1 make 2 goal だと lint が落ちた時点で test が走らない
  make -C "$d" lint || failed="$failed $d:lint"
  make -C "$d" test || failed="$failed $d:test"
done
if [ -n "$failed" ]; then
  printf '\n✗ [test-changed] 失敗:%s\n' "$failed" >&2
  exit 1
fi
