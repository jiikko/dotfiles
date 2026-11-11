#!/usr/bin/env bash
# bin/ja-textlint の自作の規則 (_claude/skills/ja-writing/textlint/rules/) を、本物の textlint を通して固定する。
#
# - ja-ai-vocabulary: 当たる語と、外すべき定着した語 (第2版・日本語版・出版・静かな・効果) の両方を、行と規則の組で完全に一致させる
#   (件数だけを見ると、外すべき語に当たって当たるべき語を落とす入れ替わりを通す)
# - 語彙は warning で、warning だけのファイルは rc 0 (textlintrc の severity は --rulesdir の規則に届かないので、規則の中で決めている)
# - 語彙の規則は書き換えない (--fix でファイルが変わらない)
# - ja-bold-not-rendered: 表示されない ** だけを指摘し、表示される太字とコードの中は見ない
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN="$ROOT_DIR/bin/ja-textlint"
fails=0
ok()   { printf '✓ %s\n' "$1"; }
fail() { printf '✗ %s\n' "$1" >&2; fails=$((fails + 1)); }

if ! command -v npm >/dev/null; then
  echo "skipped: npm が無い (ja-textlint は初回に npm ci で textlint を入れる)"
  exit 77
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

cat >"$work/a.md" <<'EOF'
# 試験

新しい版では直っている。
Go の版を上げる。
第2版を読む。
日本語版と英語版がある。
出版社に送る。
残さない側に倒す。
黙って無視される。
データが静かに壊れる。
静かな部屋で読む。
1 つずつ潰していく。
時間を溶かした。
設定が効く。
効果がある。
前提が崩れる。
ただし **「拡張しやすい」は先回りの抽象で買わない** と書く。
ただし**「拡張しやすい」は先回りの抽象で買わない**と書く。
**太字**は表示される。
`**/*.go` はコードなので見ない。
版画を刷る。
体験版を配る。
第２版と第二版と第2.1版を読む。
絶版と電子版と海賊版と特別版。
効き目がある。
古い方に倒す。
処理が静かに失敗する。
潰さないように書く。
x ** 2 を計算する。
効かない設定。
EOF

rc=0
"$BIN" -f json "$work/a.md" >"$work/a.json" 2>"$work/a.err" || rc=$?
[ "$rc" -eq 1 ] || fail "error を含むファイルの rc=$rc (期待 1): $(cat "$work/a.err")"
got="$(python3 - "$work/a.json" <<'PY'
import json, sys
mine = {"ja-ai-vocabulary", "ja-bold-not-rendered"}
for f in json.load(open(sys.argv[1])):
    for m in f["messages"]:
        if m["ruleId"] in mine:
            print(m["line"], m["column"], m["ruleId"], m["severity"])
PY
)"
want="3 4 ja-ai-vocabulary 1
4 5 ja-ai-vocabulary 1
8 5 ja-ai-vocabulary 1
9 1 ja-ai-vocabulary 1
10 5 ja-ai-vocabulary 1
12 6 ja-ai-vocabulary 1
13 4 ja-ai-vocabulary 1
14 4 ja-ai-vocabulary 1
16 1 ja-ai-vocabulary 1
18 4 ja-bold-not-rendered 2
18 26 ja-bold-not-rendered 2
26 3 ja-ai-vocabulary 1
27 4 ja-ai-vocabulary 1
28 1 ja-ai-vocabulary 1
29 3 ja-bold-not-rendered 2
30 1 ja-ai-vocabulary 1"
if [ "$(sort <<<"$got")" = "$(sort <<<"$want")" ]; then
  ok "当たる語・外す語・表示されない太字が、行と桁と重さまで一致する (16 件)"
else
  fail "指摘が期待と違う。got:
$got"
fi

printf '黙って無視される。\n' >"$work/w.md"
cp "$work/w.md" "$work/w.orig"
rc=0
"$BIN" "$work/w.md" >"$work/w.out" 2>&1 || rc=$?
if [ "$rc" -eq 0 ] && grep -qF 'ja-ai-vocabulary' "$work/w.out"; then
  ok "語彙の指摘だけのファイルは warning を出して rc 0"
else
  fail "語彙だけのファイル: rc=$rc (期待 0 で ja-ai-vocabulary の warning): $(cat "$work/w.out")"
fi

rc=0
"$BIN" --fix "$work/w.md" >"$work/fix.out" 2>&1 || rc=$?
if cmp -s "$work/w.md" "$work/w.orig"; then
  ok "--fix でも語彙の規則は書き換えない"
else
  fail "--fix で語彙が書き換わった (rc=$rc): $(cat "$work/w.md")"
fi

# text plugin は段落全体を 1 つの Str にするので、.txt で太字の規則を走らせると表示される太字まで当たる
printf 'feat: **太字**を足す。黙って捨てない。\n' >"$work/t.txt"
rc=0
"$BIN" "$work/t.txt" >"$work/t.out" 2>&1 || rc=$?
if grep -qF 'ja-ai-vocabulary' "$work/t.out" && ! grep -qF 'ja-bold-not-rendered' "$work/t.out"; then
  ok ".txt では太字の規則を走らせない (語彙の規則は走る = ファイルは検査されている)"
else
  fail ".txt で太字の規則が出た (rc=$rc): $(cat "$work/t.out")"
fi
rc=0
printf 'ただし**「x」**と書く。\n' | "$BIN" --stdin --stdin-filename x.md >"$work/s.out" 2>&1 || rc=$?
if [ "$rc" -eq 1 ] && grep -qF 'ja-bold-not-rendered' "$work/s.out"; then
  ok "標準入力でも x.md の名前なら太字の規則を走らせる"
else
  fail "標準入力 (x.md) で太字の規則が出ない (rc=$rc): $(cat "$work/s.out")"
fi

cat >"$work/d.md" <<'EOF'
<!-- textlint-disable ja-ai-vocabulary -->
黙って無視される。
<!-- textlint-enable ja-ai-vocabulary -->
EOF
rc=0
"$BIN" "$work/d.md" >"$work/d.out" 2>&1 || rc=$?
if [ "$rc" -eq 0 ] && ! grep -qF 'ja-ai-vocabulary' "$work/d.out"; then
  ok "無効化のコメントで語彙の規則を止められる"
else
  fail "無効化のコメントが効かない (rc=$rc): $(cat "$work/d.out")"
fi

[ "$fails" -eq 0 ] || { printf '✗ %d 件失敗\n' "$fails" >&2; exit 1; }
echo "OK: ja-textlint"
