#!/usr/bin/env bash
# scripts/check_comment_provenance.sh (コメントの経緯・出所の語のラチェット) の契約を fixture の repo で固定する。
#
# 守るもの: issue / retro の番号 (open の issue も・表記ゆれも) / 経緯の語 / レビューの出所の語を、Go の `//`・shell の `#`・Lua の `--`
# のコメント行 (インデント付きも) で数える / 拡張子の無い shebang のスクリプト・dotfiles の名前・.awk・.ci・`=` を含む path も数える /
# 行末のコメント・shebang の行・YAML の `--`・issues/・vendor/・testdata/ は数えない / 一覧より増えたら落とし、減った・消えたら
# 落とさず案内する / 一覧の形が壊れていたら落とす / --update は増加を拒み、--allow-increase でだけ通す / 読めないファイルで
# 判定不能なら落とす / 走査が空なら緑にしない / Makefile から配線されている / 今の repo 全体で緑。
# shellcheck disable=SC2015
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHECK="$ROOT_DIR/scripts/check_comment_provenance.sh"
fails=0
ok()  { printf '  ✓ %s\n' "$1"; }
bad() { printf '  ✗ %s\n' "$1"; fails=$((fails + 1)); }

grep -qE '^test-comment-provenance:' "$ROOT_DIR/Makefile" && grep -qE '^\t@scripts/check_comment_provenance\.sh$' "$ROOT_DIR/Makefile" \
  && grep -qE 'run_make_targets_parallel\.sh .*test-comment-provenance' "$ROOT_DIR/Makefile" \
  || { printf '✗ Makefile の test-lint が check_comment_provenance.sh を回していない (配線が外れている)\n'; exit 1; }

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-check-comment-provenance.XXXXXX")"
trap 'chmod -R u+rwX "$WORK" 2>/dev/null; rm -rf "$WORK"' EXIT

new_repo() {
  rm -rf "$WORK/r"; mkdir -p "$WORK/r/scripts"
  git -C "$WORK/r" init -q
}
# $1=path $2=中身 を fixture の repo に足す
put() { mkdir -p "$(dirname "$WORK/r/$1")"; printf '%s\n' "$2" > "$WORK/r/$1"; git -C "$WORK/r" add -f -- "$1"; }
run() { RC=0; OUT="$(CHECK_COMMENT_PROVENANCE_ROOT="$WORK/r" CHECK_COMMENT_PROVENANCE_MIN_FILES=1 "$CHECK" "$@" 2>&1)" || RC=$?; }
# 空の一覧を作ってから、$2 のファイルに $3 を置いて検査する
expect_red() {
  new_repo; put a.sh 'echo ok'; run --update; put "$2" "$3"; run
  if [ "$RC" -ne 0 ] && grep -qF "$2:" <<< "$OUT"; then ok "$1 を落とす"; else bad "$1 を落とさない (rc=$RC): $OUT"; fi
}
expect_green() {
  new_repo; put a.sh 'echo ok'; run --update; put "$2" "$3"; run
  if [ "$RC" -eq 0 ]; then ok "$1 は通す"; else bad "$1 を通さない (rc=$RC): $OUT"; fi
}

expect_red   'Go のインデントしたコメントの番号'  x.go      $'package x\nfunc f() {\n\t// 直した (issue 802)\n}'
expect_red   'open かどうかに関わらず番号'      x.go      $'package x\n// issue 001 が直るまで残す'
expect_red   'retro の番号'                    x.go      $'package x\n// retro 999 の形'
expect_red   '大文字の Issue'                  x.go      $'package x\n// Issue 802'
expect_red   'issue-802 の形'                  y.sh      $'# issue-802 を見よ'
expect_red   'issue: 802 の形'                 y.sh      $'# issue: 802'
expect_red   'issue #802 の形'                 y.sh      $'# issue #802'
expect_red   'shell の以前は'                  y.sh      $'#!/bin/sh\n  # 以前は毎回読み直していた'
expect_red   'Lua の旧実装'                    z.lua     $'-- 旧実装は毎回読み直していた'
expect_red   'YAML のかつて'                   a.yml     $'# かつては 2 本あった\nk: v'
expect_red   '敵対的レビューの出所'             y.sh      $'# 敵対的レビューで見つかった'
expect_red   '敵対レビュー (的なし)'            y.sh      $'# 敵対レビューで見つけた'
expect_red   'codex の指摘'                    y.sh      $'# codex の指摘で直した'
expect_red   '2 行目以降の #! で始まるコメント'  y.sh      $'#!/bin/sh\n#! 以前は別の形'
expect_red   'Red-team の表記ゆれ'             y.sh      $'# Red-team が壊した'
expect_red   '指摘番号 P2-1'                   x.go      $'package x\n// 理由は別 (P2-1)'
expect_red   '拡張子の無い shebang のスクリプト' bin/tool  $'#!/usr/bin/env bash\n# issue 802 で足した'
expect_red   'dotfiles の名前のファイル'        _zshrc    $'# 以前は compinit を毎回呼んだ'
expect_red   '.awk'                           x.awk     $'# 旧実装は遅かった'
expect_red   '.ci'                            t.ci      $'# 以前は 40 件'
expect_red   '= を含む path'                   'a=b.sh'  $'# 以前は遅かった'
expect_green '行末のコメント'                   y.sh      $'echo hi # issue 802'
expect_green 'コメントでない行'                 x.go      $'package x\nvar s = "issue 802"'
expect_green 'shebang の行'                    y.sh      $'#!/bin/sh issue 802 以前は'
expect_green 'YAML の -- の行'                 a.yml     $'k: v\n-- 以前は'
expect_green 'issues/ の下'                    issues/x.sh $'# 以前は'
expect_green 'vendor/ と testdata/ の下'       vendor/x.go $'// issue 802'
expect_green 'testdata/ の下'                  src/testdata/x.sh $'# 以前は'
expect_green '1 桁の番号'                      x.go      $'package x\n// issue 2 は番号として数えない'
expect_green 'issues の複数形 (画面名の計測値)'  t.ci      $'# issues 98 / status 105'
expect_green '_ で始まる拡張子付きのファイル'     _player.html $'# 以前は'

# open の issue の番号も数える (issues/ の状態を見ない)
new_repo; put a.sh 'echo ok'; run --update; put issues/001-open.md '# 001'; put x.go $'package x\n// issue 001 が直るまで残す'; run
[ "$RC" -ne 0 ] && grep -qF 'x.go:' <<< "$OUT" && ok 'open の issue の番号も落とす' || bad "open の番号を通した (rc=$RC): $OUT"

# 一覧が無ければ落とす
new_repo; put a.sh 'echo ok'; run
[ "$RC" -ne 0 ] && grep -q 'が無い' <<< "$OUT" && ok '一覧が無ければ落とす' || bad "一覧が無いのに通した (rc=$RC): $OUT"

# タブ入りの path
new_repo; put a.sh 'echo ok'; run --update; put $'a\tb.sh' $'# 以前は'; run
[ "$RC" -ne 0 ] && grep -q 'タブ' <<< "$OUT" && ok 'タブ入りの path は判定不能で落とす' || bad "タブ入りの path を通した (rc=$RC): $OUT"
new_repo; put a.sh 'echo ok'; run --update; put $'a\nb.sh' $'# 以前は'; run
[ "$RC" -ne 0 ] && grep -q '改行' <<< "$OUT" && ok '改行入りの path は判定不能で落とす' || bad "改行入りの path を通した (rc=$RC): $OUT"
new_repo; put a.sh 'echo ok'; run --update; printf '# x\n1\tx.go\n' > "$WORK/r/scripts/comment_provenance_allowlist.txt"; run
[ "$RC" -ne 0 ] && grep -q '古い形' <<< "$OUT" && ok '2 列の古い一覧は作り直し方を案内して落とす' || bad "古い一覧の案内が無い (rc=$RC): $OUT"

# 一覧のラチェット
new_repo; put x.go $'package x\n// issue 802\n// issue 802'; run --update --allow-increase
run; [ "$RC" -eq 0 ] && grep -q '2 件' <<< "$OUT" && ok '一覧どおりなら通し、件数を出す' || bad "一覧どおりで通らない (rc=$RC): $OUT"
put x.go $'package x\n// issue 802\n// issue 802\n// 以前は'; run
[ "$RC" -ne 0 ] && grep -q '増えた' <<< "$OUT" && ok '一覧より増えたら落とす' || bad "増加を落とさない (rc=$RC): $OUT"
run --update; [ "$RC" -ne 0 ] && grep -q '作り直さない' <<< "$OUT" && ok '--update は増加を拒む' || bad "--update が増加を吸収した (rc=$RC): $OUT"
run --update --allow-increase; run; [ "$RC" -eq 0 ] && ok '--update --allow-increase で一覧を上げられる' || bad "--allow-increase が効かない (rc=$RC): $OUT"
put x.go $'package x\n// issue 802'; run
[ "$RC" -ne 0 ] && grep -q '減ったのに' <<< "$OUT" && ok '減ったのに一覧が古いと落とす (余った枠を残さない)' || bad "減少を通した (rc=$RC): $OUT"
run --update; run; [ "$RC" -eq 0 ] && ok '--update で下げれば通す' || bad "下げた後に通らない (rc=$RC): $OUT"
git -C "$WORK/r" mv x.go y.go; run
[ "$RC" -eq 0 ] && grep -q 'rename: x.go → y.go' <<< "$OUT" && ok 'rename (数えた行が同じ) は落とさず案内する' || bad "rename で落ちた / 案内が無い (rc=$RC): $OUT"
run --update; [ "$RC" -eq 0 ] && grep -q 'rename 1' <<< "$OUT" && ok '--update は rename を増加に数えない' || bad "--update が rename を拒んだ (rc=$RC): $OUT"
put z.sh $'# 以前は'; git -C "$WORK/r" mv y.go w.go; run --update
[ "$RC" -ne 0 ] && grep -q 'z.sh' <<< "$OUT" && ok 'rename と一緒に増えたファイルは --update が拒む' || bad "rename に紛れた増加を吸収した (rc=$RC): $OUT"
git -C "$WORK/r" rm -q -f z.sh; run --update
git -C "$WORK/r" rm -q -f w.go; put v.go $'package x\n// retro 777'; run
[ "$RC" -ne 0 ] && grep -q 'v.go: コメントに' <<< "$OUT" && ok '件数だけ一致する無関係なファイルは rename と認めない' || bad "件数の一致で枠を移した (rc=$RC): $OUT"
git -C "$WORK/r" rm -q -f v.go; put a.sh 'echo ok'; run --update; put q.sh 'echo q'; run
[ "$RC" -eq 0 ] && ok '一覧どおり (空) なら通す' || bad "空の一覧で通らない (rc=$RC): $OUT"
new_repo; put x.go $'package x\n// issue 802\n// issue 802'; run --update --allow-increase; put x.go $'package x'; run
[ "$RC" -ne 0 ] && grep -q '0 行に減った' <<< "$OUT" && ok '0 件まで減ったファイルも減少として落とす' || bad "0 件への減少を通した (rc=$RC): $OUT"
new_repo; put x.go $'package x\n// issue 802'; run --update --allow-increase; git -C "$WORK/r" rm -q -f x.go; put a.sh 'echo ok'; run
[ "$RC" -eq 0 ] && grep -q '無くなった' <<< "$OUT" && ok '消えたファイルは落とさず案内する' || bad "消えたファイルで落ちた / 案内が無い (rc=$RC): $OUT"

# 一覧の形
new_repo; put x.go $'package x\n// issue 802'; run --update --allow-increase
dup=$(grep -v '^#' "$WORK/r/scripts/comment_provenance_allowlist.txt"); printf '%s\n' "$dup" >> "$WORK/r/scripts/comment_provenance_allowlist.txt"; run
[ "$RC" -ne 0 ] && grep -q '2 行ある' <<< "$OUT" && ok '同じ path が 2 行ある一覧を落とす' || bad "重複した一覧を通す (rc=$RC): $OUT"
printf '# x\nabc\t0a\tx.go\n' > "$WORK/r/scripts/comment_provenance_allowlist.txt"; run
[ "$RC" -ne 0 ] && grep -q '形が壊れている' <<< "$OUT" && ok '件数が数字でない一覧を落とす' || bad "壊れた一覧を通す (rc=$RC): $OUT"

# 読めないファイル
new_repo; put a.sh 'echo ok'; put b.sh 'echo b'; put z.sh $'# 以前は'; run --update --allow-increase
chmod 000 "$WORK/r/b.sh"; run; chmod 644 "$WORK/r/b.sh"
[ "$RC" -ne 0 ] && grep -q '途中で失敗' <<< "$OUT" && ok '読めないファイルがあれば判定不能で落とす' || bad "読めないファイルで緑 (rc=$RC): $OUT"

# 走査が空なら緑にしない
new_repo; put issues/README.md 'x'; RC=0; OUT="$(CHECK_COMMENT_PROVENANCE_ROOT="$WORK/r" "$CHECK" 2>&1)" || RC=$?
[ "$RC" -ne 0 ] && grep -q 'しか無い' <<< "$OUT" && ok '検査したファイルが少なすぎれば落とす' || bad "空の走査を通す (rc=$RC): $OUT"

# --file (編集の直後の hook が使う 1 ファイルの検査)
new_repo; put a.sh 'echo ok'; run --update
printf 'package x\n// issue 802 で直した\n' > "$WORK/r/new.go"   # git に add しない (Write で作った直後の形)
run --file new.go
[ "$RC" -eq 1 ] && grep -q '^new.go:2:// issue 802' <<< "$OUT" && ok '--file は add する前のファイルも数え、数えた行を出す' || bad "--file が add 前のファイルを見ない (rc=$RC): $OUT"
put x.go $'package x\n// issue 802'; run --update --allow-increase; run --file x.go
[ "$RC" -eq 0 ] && [ -z "$OUT" ] && ok '--file は一覧の件数までなら黙る' || bad "--file が一覧どおりで出した (rc=$RC): $OUT"
put x.go $'package x\n// issue 802\n// 以前は別の形'; run --file x.go
[ "$RC" -eq 1 ] && grep -q '一覧は 1' <<< "$OUT" && ok '--file は一覧を超えたら落とす' || bad "--file が増加を通す (rc=$RC): $OUT"
printf '# issue 802\n' > "$WORK/r/note.md"; run --file note.md
[ "$RC" -eq 0 ] && [ -z "$OUT" ] && ok '--file は走査の対象外 (Markdown) を数えない' || bad "--file が対象外を数えた (rc=$RC): $OUT"
printf 'tmp/\n' > "$WORK/r/.gitignore"; mkdir -p "$WORK/r/tmp"; printf '# issue 802\n' > "$WORK/r/tmp/s.sh"; run --file tmp/s.sh
[ "$RC" -eq 0 ] && [ -z "$OUT" ] && ok '--file は gitignore されて追跡もされていないファイルを数えない (CI も見ない)' || bad "--file が ignore されたファイルを数えた (rc=$RC): $OUT"
run --file
[ "$RC" -eq 2 ] && ok '--file に path が無ければ使い方の誤り' || bad "--file の引数なしが rc=2 にならない (rc=$RC): $OUT"

# 今の repo 全体
RC=0; OUT="$("$CHECK" 2>&1)" || RC=$?
[ "$RC" -eq 0 ] && ok "repo 全体: $(tail -1 <<< "$OUT")" || bad "repo 全体が赤 (rc=$RC): $OUT"

[ "$fails" -eq 0 ] || { printf '✗ %s 件失敗\n' "$fails"; exit 1; }
printf '✓ check_comment_provenance の契約がすべて成り立つ\n'
