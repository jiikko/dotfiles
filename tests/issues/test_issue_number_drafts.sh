#!/usr/bin/env bash
# scripts/issue_number_drafts.sh の検査 (issue 530)。
#
# 使い捨ての repo に origin/master 相当の ref を作り、
#   ① 番号は working tree と ref の両方の最大 + 1 から振られる (ref にだけある番号を跨ぐ)
#   ② 改名・見出し・他のファイルからの参照が新しい名前になる
#   ③ 止める形では何も動かさない
# を確かめる。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/issue_number_drafts.sh"
[ -x "$SCRIPT" ] || { printf '✗ scripts/issue_number_drafts.sh が無い / 実行できない\n' >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

fail() { printf '✗ %s\n' "$1" >&2; exit 1; }

g() { git -C "$1" -c user.email=t@t -c user.name=t "${@:2}"; }

# fixture: working tree の最大は 520、ref (= 他の session が先に push した master) の最大は 530。
# 仮の名前の issue 2 本と、それを参照する既存の issue と docs
make_fixture() {
  local d="$1"
  mkdir -p "$d/issues/epic/415" "$d/issues/done" "$d/docs"
  : > "$d/issues/done/520-bug-old.md"
  g "$d" init -q
  g "$d" add -A
  g "$d" commit -qm base
  : > "$d/issues/epic/415/530-bug-other.md"
  g "$d" add -A
  g "$d" commit -qm other
  g "$d" update-ref refs/remotes/origin/master HEAD
  g "$d" reset -q --hard HEAD~1
  cat > "$d/issues/epic/415/new-bug-first.md" <<'EOF'
# new (bug): 1 本目

- [2 本目](../../new-feat-second.md)
EOF
  cat > "$d/issues/new-feat-second.md" <<'EOF'
# new (feat): 2 本目

- [1 本目](epic/415/new-bug-first.md)
EOF
  cat > "$d/docs/x.md" <<'EOF'
- `issues/epic/415/new-bug-first.md` を参照
EOF
  g "$d" add -A
  g "$d" commit -qm drafts
}

# ① ② 正常系
d="$work/ok"
make_fixture "$d"
out="$(cd "$d" && "$SCRIPT")" || fail "正常系で失敗した: $out"
[ -f "$d/issues/epic/415/531-bug-first.md" ] || fail "パスの辞書順で最初の仮の名前が 531 になっていない (ref の 530 を跨いでいない?): $(find "$d/issues" -name '*.md')"
[ -f "$d/issues/532-feat-second.md" ] || fail "2 本目が 532 になっていない"
[ -z "$(find "$d/issues" -name 'new-*')" ] || fail "仮の名前が残っている"
head -1 "$d/issues/epic/415/531-bug-first.md" | grep -qxF '# 531 (bug): 1 本目' || fail "見出しの番号が書き換わっていない"
grep -qF '](../../532-feat-second.md)' "$d/issues/epic/415/531-bug-first.md" || fail "改名した issue の中の参照が張り替わっていない"
grep -qF '](epic/415/531-bug-first.md)' "$d/issues/532-feat-second.md" || fail "他の issue からの参照が張り替わっていない"
grep -qF 'issues/epic/415/531-bug-first.md' "$d/docs/x.md" || fail "issues/ の外からの参照が張り替わっていない"
[ -z "$(git -C "$d" grep -lF 'new-' || true)" ] || fail "旧名の参照が残っている"
git -C "$d" diff --cached --name-status | grep -q '^R' || fail "git mv で改名していない (index に rename が無い)"

# 仮の名前が無ければ何もしない
out="$(cd "$d" && "$SCRIPT")" || fail "仮の名前が無いときに失敗した"
[ -z "$out" ] || fail "仮の名前が無いのに何か出した: $out"

# ③ 止める形: 何も動かさない
refuse() {  # $1=名前 $2=fixture を崩すコマンド
  local d="$work/$1"
  make_fixture "$d"
  (cd "$d" && eval "$2")
  local before; before="$(cd "$d" && find issues | LC_ALL=C sort)"
  if (cd "$d" && "$SCRIPT" >/dev/null 2>&1); then fail "$1 で止まらなかった"; fi
  [ "$before" = "$(cd "$d" && find issues | LC_ALL=C sort)" ] || fail "$1 で止まったのにファイルを動かした"
}
refuse no-ref 'git update-ref -d refs/remotes/origin/master'
refuse dup 'mkdir -p issues/done && cp issues/new-feat-second.md issues/done/new-feat-second.md'
refuse bad-name 'mv issues/new-feat-second.md issues/new-Feat_second.md'
refuse claim 'mkdir -p issues/next && ln -s ../new-feat-second.md issues/next/new-feat-second.md'

printf '✓ issue_number_drafts.sh\n'
