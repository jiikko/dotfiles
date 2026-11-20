#!/usr/bin/env bash
# devtool/bin/comment-provenance の self-test: 語彙ごとの検出、コメント外の語を見ないこと、版の比較の除外、
# 判定不能 (対象 0 件・読めない file・無いパス・--root が無い) を fixture で pin する。
# obaket の macOS/libexec/check/check-comment-provenance-test から移した (vlc-multi-video-player issue 588 の A)。
# 各 repo の呼び口の配線 (--root・--label) は各 repo の gate が本物の repo を走査して見る。

set -euo pipefail
unset CDPATH
# fixture の repo を git init する。継承した GIT_DIR が残っていると、その gitdir を init し直す
# shellcheck disable=SC2046
unset $(git rev-parse --local-env-vars) GIT_NAMESPACE GIT_QUARANTINE_PATH

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
GATE="$ROOT_DIR/devtool/bin/comment-provenance"
[ -x "$GATE" ] || { echo "NG: devtool/bin/comment-provenance が無い"; exit 1; }
TMP="$(mktemp -d "${TMPDIR:-/tmp}/comment-provenance-test.XXXXXX")"
UNREADABLE_FILE=""
cleanup() {
  if [[ -n "$UNREADABLE_FILE" && -e "$UNREADABLE_FILE" ]]; then
    chmod 600 "$UNREADABLE_FILE"
  fi
  rm -rf "$TMP"
}
trap cleanup EXIT

passed=0
failed=0

# expect <want rc> <名前> <出力に含むべき文字列 (空なら見ない)> <path...>
expect() {
  local want="$1" name="$2" needle="$3"
  shift 3
  local output rc
  set +e
  output="$("$GATE" --root "$TMP" "$@" 2>&1)"
  rc=$?
  set -e
  if [[ "$rc" -ne "$want" ]]; then
    echo "FAIL: $name: rc=$rc (want $want)"
    printf '%s\n' "$output" | sed 's/^/    /'
    failed=$((failed + 1))
    return
  fi
  if [[ -n "$needle" ]] && ! grep -qF -- "$needle" <<< "$output"; then
    echo "FAIL: $name: 出力に '$needle' が無い"
    printf '%s\n' "$output" | sed 's/^/    /'
    failed=$((failed + 1))
    return
  fi
  passed=$((passed + 1))
}

# fixture を 1 件ずつ別ディレクトリに作る (ケース間で状態を共有しない)
# `d="$(fixture ...)"` は subshell で走るので、番号を数えるとケースどうしが同じディレクトリを使い回す。mktemp で分ける
fixture() {
  local dir
  dir="$(mktemp -d "$TMP/case.XXXXXX")"
  printf '%s\n' "$@" > "$dir/Fixture.swift"
  printf '%s' "$dir"
}

# 語彙ごとに 1 件ずつ検出する (1 語だけ落ちる退行を、他の語の検出で隠さない)
for term in 'codex' 'Codex review P2' 'red team P2-1' 'red-team' 'design review' 'adversarial review' \
            '敵対レビュー' '敵対的レビュー' '設計レビュー' '実装レビュー' 'レビュー指摘' 'レビュー発見' \
            'P1-C' 'P2-12' '以前は X だった'; do
  d="$(fixture 'struct A {' "    let x = 1 // 理由 ($term)" '}')"
  expect 1 "行末コメントの '$term' を検出する" "Fixture.swift:2:" "$d"
done

d="$(fixture '/// 説明 (codex 4c P3)' 'struct A {}')"
expect 1 "doc コメントを検出する" "Fixture.swift:1: [codex]" "$d"

d="$(fixture 'struct A {' '    // 説明。' '    // 2 行目 (敵対レビュー)' '}')"
expect 1 "当たった行の行番号を出す" "Fixture.swift:3: [敵対レビュー]" "$d"

d="$(fixture '/// 不具合が出る macOS の major の版。macOS 26 以前は超過を許す。' 'struct A {}')"
expect 0 "版の比較の「26 以前は」は見ない" "違反 0 件" "$d"

d="$(fixture 'let s = "codex review // 敵対レビュー"' 'let u = "https://example.com" // 接続先' 'let codexValue = 1 // 値を持つ')"
expect 0 "文字列リテラルと識別子の中の語は見ない" "違反 0 件" "$d"

d="$(fixture 'let s = "https://example.com/a" // codex review')"
expect 1 "文字列の後ろのコメントは見る" "[codex]" "$d"

d="$(fixture '// P1 / P2 の優先度はここでは使わない' '// preview を出す' 'struct A {}')"
expect 0 "番号の無い P1 と preview は見ない" "違反 0 件" "$d"

d="$(fixture '// 説明 "codex review" を引く')"
expect 1 "コメントの中の引用符で囲んだ語も見る" "[codex]" "$d"

d="$(fixture 'let text = #"a" // codex"#' 'let regex = #/https://codex.com/#' 'let two = ##"x"# // codex"##')"
expect 0 "raw string と正規表現リテラルの中の // はコメントと読まない" "違反 0 件" "$d"

d="$(fixture 'let text = #"a"# // codex')"
expect 1 "raw string の後ろのコメントは見る" "[codex]" "$d"

for text in '// P1-Cの指摘に対応' '// 指摘P1-C' '// design reviewで発見'; do
  d="$(fixture "$text")"
  expect 1 "日本語に続けて書いたラベル ($text) を検出する" "Fixture.swift:1:" "$d"
done

d="$(fixture '// XP1-C と PP2-3x は番号ではない' '// redesign review ではない')"
expect 0 "英字に続く綴りはラベルと読まない" "違反 0 件" "$d"

d="$(mktemp -d "$TMP/case.XXXXXX")"
printf '// \xff codex\n' > "$d/Broken.swift"
printf '%s\n' 'struct A {}' > "$d/Fine.swift"
expect 2 "UTF-8 として読めない file は判定不能" "UTF-8 として読めない" "$d"

d="$(mktemp -d "$TMP/case.XXXXXX")"
printf '// ordinary\rlet codexValue = 1\r' > "$d/CR.swift"
expect 0 "CR だけの改行の次の行をコメントと読まない" "違反 0 件" "$d"
d="$(mktemp -d "$TMP/case.XXXXXX")"
printf 'let a = 1\r// codex\r' > "$d/CR.swift"
expect 1 "CR だけの改行の後ろのコメントは見る" "CR.swift:2:" "$d"

d="$(fixture 'struct A {}')"
expect 0 "違反の無い file は通る" "Swift file 1 件 / 読み取り失敗 0 件 / 違反 0 件" "$d"

mkdir -p "$TMP/empty"
expect 2 "対象 0 件は判定不能" "Swift file 0 件" "$TMP/empty"

expect 2 "無いパスは判定不能" "対象が無い" "$TMP/does-not-exist"

# 読める file を隣に置く (読めない file だけだと「対象 0 件」で先に判定不能になり、読めないことの扱いを見ない)
d="$(fixture 'struct A {}')"
printf '%s\n' '// codex' > "$d/Unreadable.swift"
UNREADABLE_FILE="$d/Unreadable.swift"
chmod 000 "$UNREADABLE_FILE"
if [[ -r "$UNREADABLE_FILE" ]]; then
  echo "SKIP: 読めない file のケース (この環境では chmod 000 の file が読める)"
else
  expect 2 "読めない file は判定不能" "READ-FAIL" "$d"
fi

# 引数なし (--root の repo 全体) の走査は、継承した GIT_DIR を捨ててから git を読む。どちらの形も、捨て忘れると
# 別の repo の index を読んで件数が 1 件以下になる (走査する repo は Swift を 5 件追跡する)。
expect_scans_real_repo() {
  local name="$1" output rc count
  shift
  set +e
  output="$(env "$@" "$GATE" --root "$REAL" --label lbl 2>&1)"
  rc=$?
  set -e
  count="$(sed -n 's/.*Swift file \([0-9][0-9]*\) 件.*/\1/p' <<< "$output" | tail -1)"
  # 件数は本物の repo の追跡数と一致すること。下限だけだと、一部しか見ない退行や異常終了を通す
  if [[ "$rc" -ne 0 && "$rc" -ne 1 ]] || [[ "$count" != "$REAL_SWIFT_COUNT" ]]; then
    echo "FAIL: $name: rc=$rc count=${count:-?} (want rc 0/1, count $REAL_SWIFT_COUNT)"
    printf '%s\n' "$output" | tail -5 | sed 's/^/    /'
    failed=$((failed + 1))
    return
  fi
  passed=$((passed + 1))
}
REAL="$TMP/real"
git init -q "$REAL"
for n in 1 2 3 4 5; do printf '%s\n' "struct S$n {}" > "$REAL/S$n.swift"; done
git -C "$REAL" add .
REAL_SWIFT_COUNT=5
# 共有 checkout の形: core.worktree 付きの gitdir (中身の無い work tree を指す)
git init -q "$TMP/shared-form"
mkdir -p "$TMP/shared-form-wt"
git -C "$TMP/shared-form" config core.worktree "$TMP/shared-form-wt"
expect_scans_real_repo "共有 checkout の形の GIT_DIR を継承しない" GIT_DIR="$TMP/shared-form/.git"
# linked worktree の形: 別の repo の index (Swift が 1 件だけ) を読ませる
git init -q "$TMP/linked-form"
printf '%s\n' 'struct A {}' > "$TMP/linked-form/Only.swift"
git -C "$TMP/linked-form" add Only.swift
expect_scans_real_repo "linked worktree の形の GIT_DIR を継承しない" GIT_DIR="$TMP/linked-form/.git"

# --label で出力の先頭の名前を変える (呼び口の名前を保つ)。--root が無ければ判定不能
set +e
label_out="$("$GATE" --root "$REAL" --label tt-check-comment-provenance 2>&1)"; label_rc=$?
noroot_out="$("$GATE" "$TMP/empty" 2>&1)"; noroot_rc=$?
set -e
if [[ "$label_rc" -eq 0 ]] && grep -qF 'tt-check-comment-provenance: Swift file 5 件' <<< "$label_out"; then passed=$((passed + 1))
else echo "FAIL: --label の名前で出す: rc=$label_rc $label_out"; failed=$((failed + 1)); fi
if [[ "$noroot_rc" -eq 2 ]] && grep -qF -- '--root が要る' <<< "$noroot_out"; then passed=$((passed + 1))
else echo "FAIL: --root が無いと判定不能: rc=$noroot_rc $noroot_out"; failed=$((failed + 1)); fi

echo "test-comment-provenance: ${passed} passed / ${failed} failed"
[[ "$failed" -eq 0 ]]
[[ "$failed" -eq 0 ]]
