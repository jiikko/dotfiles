#!/usr/bin/env bash
# bin/ja-textlint の自作の規則 (_claude/skills/ja-writing/textlint/rules/) を、本物の textlint を通して固定する。
#
# - ja-ai-vocabulary: 当たる語と、外すべき定着した語 (第2版・日本語版・出版・静かな・効果) の両方を、行と規則の組で完全に一致させる
#   (件数だけを見ると、外すべき語に当たって当たるべき語を落とす入れ替わりを通す)
# - 語彙は warning で、warning だけのファイルは rc 0 (textlintrc の severity は --rulesdir の規則に届かないので、規則の中で決めている)
# - 語彙の規則は書き換えない (--fix でファイルが変わらない)
# - ja-bold-not-rendered: 表示されない ** だけを指摘し、表示される太字とコードの中は見ない
# - textlint の後に ja-writing lint を掛け、rc は両方の大きい方 (表は bin/ja-textlint の冒頭)。
#   Go のある環境で ja-writing lint を飛ばして緑にならないよう、出力に ja-writing lint の行と規則名が出ることを見る
#   (go が無くビルドも無い環境では rc 3 で赤くなる。skip にはしない)
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
別版を出す。
準備ができ次第
版を上げる。
第百版を読む。
この変更は全
版に当てる。
安定版と暫定版を出す。
準備ができ次第版を上げる。この変更は全版に当てる。
第Ⅱ版と改訂版と限定版と体験版。
初版 再版 改版 原版 出版 日本語版 無料版 有料版 試用版 製品版 完全版 縮刷版 豪華版 決定版 廉価版 普及版 通常版 絶版 電子版 海賊版 第IV版 第ⅳ版 版画 版権。
新版画面と提出版と最初版と Go 言語版。
失敗を握り潰さない。
旧版画像と流出版と検出版と高原版。
本の出版と第iv版。
設定が有効かと無効かを見る。
既定値は 30 日で、既定ではオフ。
変異を当てて確かめた。
削除の印を付ける。
発電所の表を結合して、掲載情報の表の列を読む。
表にまとめる。表の見出しと印を付ける。
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
30 1 ja-ai-vocabulary 1
31 2 ja-ai-vocabulary 1
33 1 ja-ai-vocabulary 1
36 1 ja-ai-vocabulary 1
37 3 ja-ai-vocabulary 1
37 7 ja-ai-vocabulary 1
38 8 ja-ai-vocabulary 1
38 20 ja-ai-vocabulary 1
41 2 ja-ai-vocabulary 1
41 8 ja-ai-vocabulary 1
41 12 ja-ai-vocabulary 1
41 20 ja-ai-vocabulary 1
43 2 ja-ai-vocabulary 1
43 8 ja-ai-vocabulary 1
43 12 ja-ai-vocabulary 1
43 16 ja-ai-vocabulary 1
46 1 ja-ai-vocabulary 1
46 12 ja-ai-vocabulary 1
47 1 ja-ai-vocabulary 1
48 1 ja-ai-vocabulary 1
49 5 ja-ai-vocabulary 1
49 17 ja-ai-vocabulary 1"
if [ "$(sort <<<"$got")" = "$(sort <<<"$want")" ]; then
  ok "当たる語・外す語・表示されない太字が、行と桁と重さまで一致する (34 件)"
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
printf 'ただし**「x」**と書く。\n' >"$work/k.mkd"
rc=0
"$BIN" "$work/k.mkd" >"$work/k.out" 2>&1 || rc=$?
if grep -qF 'ja-bold-not-rendered' "$work/k.out"; then
  ok "markdown plugin が受ける別の拡張子 (.mkd) でも太字の規則を走らせる"
else
  fail ".mkd で太字の規則が出ない (rc=$rc): $(cat "$work/k.out")"
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

# --- ja-writing lint を合わせた形 (rc の合成の表の各行) ---
jw_ran() { grep -qF "ja-writing lint: $2" "$1" && grep -qF '表現・書式の検査を実行しました' "$1"; }

# 両方とも warn だけ → rc 0。両方の指摘が出る
rc=0
"$BIN" "$work/w.md" >"$work/jw0.out" 2>"$work/jw0.err" || rc=$?
if [ "$rc" -eq 0 ] && grep -qF 'ja-ai-vocabulary' "$work/jw0.out" && jw_ran "$work/jw0.out" "$work/w.md" && grep -qF 'metaphor_verb' "$work/jw0.out"; then
  ok "warn だけなら rc 0 で、textlint と ja-writing lint の両方の指摘が出る"
else
  fail "warn だけ: rc=$rc (期待 0 で両方の指摘): $(cat "$work/jw0.out" "$work/jw0.err")"
fi

# ja-writing lint の error (bold_not_rendered) → rc 1 に入る。textlint は語彙の warning だけになるよう太字の規則を止める
cat >"$work/jb.md" <<'EOF'
<!-- textlint-disable ja-bold-not-rendered -->
ただし**「x」**と書く。
<!-- textlint-enable ja-bold-not-rendered -->
EOF
rc=0
"$BIN" "$work/jb.md" >"$work/jb.out" 2>"$work/jb.err" || rc=$?
if [ "$rc" -eq 1 ] && ! grep -qF 'ja-bold-not-rendered' "$work/jb.out" && jw_ran "$work/jb.out" "$work/jb.md" && grep -qF '[ERROR] bold_not_rendered' "$work/jb.out"; then
  ok "ja-writing lint だけが error を出したときも rc 1"
else
  fail "ja-writing lint の error: rc=$rc (期待 1): $(cat "$work/jb.out" "$work/jb.err")"
fi

# ファイルを複数渡すと、ファイルごとに掛ける (作業フォルダの外のファイルにも掛ける)
rc=0
"$BIN" "$work/w.md" "$work/jb.md" >"$work/jm.out" 2>"$work/jm.err" || rc=$?
if [ "$rc" -eq 1 ] && jw_ran "$work/jm.out" "$work/w.md" && jw_ran "$work/jm.out" "$work/jb.md"; then
  ok "複数のファイルにファイルごとに ja-writing lint を掛け、rc は大きい方"
else
  fail "複数のファイル: rc=$rc (期待 1): $(cat "$work/jm.out" "$work/jm.err")"
fi

# 標準入力: textlint と ja-writing lint の両方が同じ文を読む
rc=0
printf '黙って無視される。\n' | "$BIN" --stdin --stdin-filename y.md >"$work/js.out" 2>"$work/js.err" || rc=$?
if [ "$rc" -eq 0 ] && grep -qF 'ja-ai-vocabulary' "$work/js.out" && jw_ran "$work/js.out" y.md && grep -qF 'metaphor_verb' "$work/js.out"; then
  ok "標準入力の文に textlint と ja-writing lint の両方を掛ける"
else
  fail "標準入力: rc=$rc: $(cat "$work/js.out" "$work/js.err")"
fi

# textlint が rc 2 (引数の誤り) でも ja-writing lint は走り、rc は 2
rc=0
"$BIN" --rule ja-textlint-no-such-rule "$work/w.md" >"$work/j2.out" 2>"$work/j2.err" || rc=$?
if [ "$rc" -eq 2 ] && jw_ran "$work/j2.out" "$work/w.md"; then
  ok "textlint が rc 2 でも ja-writing lint の指摘を出し、rc 2"
else
  fail "textlint rc 2: rc=$rc (期待 2): $(cat "$work/j2.out" "$work/j2.err")"
fi

# textlint の依存が無い (rc 3) でも ja-writing lint は走り、rc は 3
mkdir -p "$work/nocache"
rc=0
XDG_CACHE_HOME="$work/nocache" JA_TEXTLINT_NO_INSTALL=1 "$BIN" "$work/w.md" >"$work/j3.out" 2>"$work/j3.err" || rc=$?
if [ "$rc" -eq 3 ] && jw_ran "$work/j3.out" "$work/w.md" && grep -qF 'metaphor_verb' "$work/j3.out"; then
  ok "textlint の依存が無くても ja-writing lint の指摘を出し、rc 3"
else
  fail "textlint rc 3: rc=$rc (期待 3): $(cat "$work/j3.out" "$work/j3.err")"
fi

# ja-writing を起こせない (ビルドの失敗・JA_TEXTLINT_NO_INSTALL でビルドが無い) → textlint の指摘は出し、rc 3。
# go.mod の無い src/ja-writing を持つ木に ja-textlint を写して起こす (textlint の依存は本物の置き場を使う)
fake="$work/fake"
mkdir -p "$fake/bin/lib" "$fake/_claude/skills/ja-writing" "$fake/src/ja-writing"
cp "$ROOT_DIR/bin/ja-textlint" "$ROOT_DIR/bin/ja-writing" "$fake/bin/"
cp "$ROOT_DIR/bin/lib/go_autobuild.zsh" "$ROOT_DIR/bin/lib/ja_textlint_targets.cjs" "$fake/bin/lib/"
ln -s "$ROOT_DIR/_claude/skills/ja-writing/textlint" "$fake/_claude/skills/ja-writing/textlint"
for mode in build no-install; do
  rc=0
  if [ "$mode" = build ]; then
    "$fake/bin/ja-textlint" "$work/w.md" >"$work/jf.out" 2>"$work/jf.err" || rc=$?
  else
    JA_TEXTLINT_NO_INSTALL=1 "$fake/bin/ja-textlint" "$work/w.md" >"$work/jf.out" 2>"$work/jf.err" || rc=$?
  fi
  if [ "$rc" -eq 3 ] && grep -qF 'ja-ai-vocabulary' "$work/jf.out" && ! grep -qF 'ja-writing lint: ' "$work/jf.out" \
    && grep -qF 'ja-writing lint を掛けられない' "$work/jf.err"; then
    ok "ja-writing を起こせない ($mode) ときは textlint の指摘を出して rc 3"
  else
    fail "ja-writing を起こせない ($mode): rc=$rc (期待 3): $(cat "$work/jf.out" "$work/jf.err")"
  fi
done

# textlint だけで終える形: 出力の形の指定 (-f json) と --fix。標準出力に ja-writing lint を混ぜず、標準エラーに理由を出す
rc=0
"$BIN" -f json "$work/w.md" >"$work/jj.out" 2>"$work/jj.err" || rc=$?
if [ "$rc" -eq 0 ] && python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$work/jj.out" && ! grep -qF 'ja-writing lint: ' "$work/jj.out" \
  && grep -qF 'ja-writing lint は掛けない' "$work/jj.err"; then
  ok "-f json の出力は textlint の JSON だけで、ja-writing lint を掛けない旨を標準エラーに出す"
else
  fail "-f json: rc=$rc: $(cat "$work/jj.out" "$work/jj.err")"
fi
if ! grep -qF 'ja-writing lint: ' "$work/fix.out" && grep -qF 'ja-writing lint は掛けない' "$work/fix.out"; then
  ok "--fix では ja-writing lint を掛けない旨を出す"
else
  fail "--fix の出力: $(cat "$work/fix.out")"
fi

# textlint の対象でない拡張子には ja-writing lint を掛けない (textlint も見ないので rc 0 のまま)。掛けなかった件数を出す
printf '// ただし**「x」**と書く。\n' >"$work/m.go"
rc=0
"$BIN" "$work/m.go" >"$work/jg.out" 2>"$work/jg.err" || rc=$?
if [ "$rc" -eq 0 ] && ! grep -qF 'ja-writing lint: ' "$work/jg.out" && grep -qF '掛けなかったファイル: 1 件' "$work/jg.err"; then
  ok "textlint の対象でない拡張子 (.go) には ja-writing lint を掛けず rc 0"
else
  fail ".go: rc=$rc (期待 0): $(cat "$work/jg.out" "$work/jg.err")"
fi

# textlint が ignore するファイル (cwd の .textlintignore・--ignore-path) には ja-writing lint を掛けない (textlint だけなら rc 0 のまま)。
# 同じ起動で ignore されないファイルには掛ける。中身は jb.md と同じ (textlint は何も言わず、ja-writing lint だけが error を出す)
ig="$work/ig"
mkdir -p "$ig"
cp "$work/jb.md" "$ig/cwdig.md"
cp "$work/jb.md" "$ig/pathig.md"
cp "$work/jb.md" "$ig/kept.md"
printf 'cwdig.md\n' >"$ig/.textlintignore"
printf 'pathig.md\n' >"$ig/listed"
rc=0
(cd "$ig" && "$BIN" cwdig.md) >"$work/ji1.out" 2>"$work/ji1.err" || rc=$?
if [ "$rc" -eq 0 ] && ! grep -qF 'ja-writing lint: ' "$work/ji1.out" && grep -qF 'cwdig.md は textlint の対象でない' "$work/ji1.err"; then
  ok "cwd の .textlintignore に載ったファイルには ja-writing lint を掛けず rc 0"
else
  fail ".textlintignore: rc=$rc (期待 0): $(cat "$work/ji1.out" "$work/ji1.err")"
fi
rc=0
(cd "$ig" && "$BIN" --ignore-path listed pathig.md kept.md) >"$work/ji2.out" 2>"$work/ji2.err" || rc=$?
if [ "$rc" -eq 1 ] && ! grep -qF 'ja-writing lint: pathig.md' "$work/ji2.out" && grep -qF 'pathig.md は textlint の対象でない' "$work/ji2.err" \
  && jw_ran "$work/ji2.out" kept.md && [ "$(grep -c '^ja-writing lint: ' "$work/ji2.out")" -eq 1 ]; then
  ok "--ignore-path に載ったファイルには掛けず、載っていない kept.md には掛ける (rc 1)"
else
  fail "--ignore-path: rc=$rc (期待 1): $(cat "$work/ji2.out" "$work/ji2.err")"
fi
rc=0
(cd "$ig" && "$BIN" --ignore-path listed cwdig.md) >"$work/ji3.out" 2>"$work/ji3.err" || rc=$?
if [ "$rc" -eq 1 ] && jw_ran "$work/ji3.out" cwdig.md; then
  ok "--ignore-path を渡すと cwd の .textlintignore は読まず、cwdig.md に掛ける (textlint と同じ)"
else
  fail "--ignore-path で .textlintignore を読まない: rc=$rc (期待 1): $(cat "$work/ji3.out" "$work/ji3.err")"
fi

# 値を取る textlint のオプションの値をファイルと読まない
printf 'これは普通の文です。\n' >"$work/ok.md"
rc=0
"$BIN" -c "$ROOT_DIR/_claude/skills/ja-writing/textlint/textlintrc.json" "$work/ok.md" >"$work/jc.out" 2>"$work/jc.err" || rc=$?
if [ "$(grep -c '^ja-writing lint: ' "$work/jc.out")" -eq 1 ] && jw_ran "$work/jc.out" "$work/ok.md" && ! grep -qF '掛けなかった' "$work/jc.err"; then
  ok "-c の値は ja-writing lint に渡さず、ok.md だけに掛ける"
else
  fail "-c: rc=$rc: $(cat "$work/jc.out" "$work/jc.err")"
fi
rc=0
"$BIN" -f stylish "$work/w.md" >"$work/jst.out" 2>"$work/jst.err" || rc=$?
if [ "$rc" -eq 0 ] && grep -qF 'ja-ai-vocabulary' "$work/jst.out" && jw_ran "$work/jst.out" "$work/w.md"; then
  ok "-f stylish は両方を掛ける"
else
  fail "-f stylish: rc=$rc: $(cat "$work/jst.out" "$work/jst.err")"
fi
rc=0
"$BIN" --format=json "$work/w.md" >"$work/jfj.out" 2>"$work/jfj.err" || rc=$?
if python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$work/jfj.out" && grep -qF 'ja-writing lint は掛けない' "$work/jfj.err"; then
  ok "--format=json は textlint だけ"
else
  fail "--format=json: rc=$rc: $(cat "$work/jfj.out" "$work/jfj.err")"
fi

# ja-writing lint の入力の誤り (UTF-8 で読めない) は rc 2 (掛けられない 3 ではない)
printf '\xff\xfe\x00bin' >"$work/bin.md"
rc=0
"$BIN" "$work/bin.md" >"$work/jbin.out" 2>"$work/jbin.err" || rc=$?
if [ "$rc" -eq 2 ]; then
  ok "ja-writing lint の入力の誤りは rc 2"
else
  fail "入力の誤り: rc=$rc (期待 2): $(cat "$work/jbin.out" "$work/jbin.err")"
fi

# --stdin-filename が最後で値が無くても未定義の変数で落ちない
rc=0
"$BIN" --stdin --stdin-filename </dev/null >"$work/jsn.out" 2>"$work/jsn.err" || rc=$?
if ! grep -qF 'unbound variable' "$work/jsn.err"; then
  ok "--stdin-filename の値が無くても unbound で落ちない (rc=$rc)"
else
  fail "--stdin-filename 値なし: $(cat "$work/jsn.err")"
fi

# 引数は textlint (optionator) と同じに読む: `-f=json`・`-o=FILE`・`-o FILE` は textlint だけで、標準出力に ja-writing lint を混ぜない
for form in '-f=json' '-o=OUT' '-o OUT'; do
  rm -f "$work/o.txt"
  set -- ${form/OUT/$work/o.txt}
  rc=0
  "$BIN" "$@" "$work/w.md" >"$work/jo.out" 2>"$work/jo.err" || rc=$?
  set --
  if [ "$rc" -eq 0 ] && ! grep -qF 'ja-writing lint: ' "$work/jo.out" && grep -qF 'ja-writing lint は掛けない' "$work/jo.err" \
    && { [ "$form" = '-f=json' ] && python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$work/jo.out" || [ -s "$work/o.txt" ]; }; then
    ok "$form は textlint だけを掛け、標準出力に ja-writing lint を混ぜない"
  else
    fail "$form: rc=$rc: $(cat "$work/jo.out" "$work/jo.err")"
  fi
done

# 標準入力が空でなければ textlint はファイルを読まないので、ファイルには掛けない (jb.md は ja-writing lint だけが error を出す)
rc=0
printf 'これは普通の文です。\n' | "$BIN" --stdin --stdin-filename s.md "$work/jb.md" >"$work/jsf.out" 2>"$work/jsf.err" || rc=$?
if [ "$rc" -eq 0 ] && jw_ran "$work/jsf.out" s.md && ! grep -qF "ja-writing lint: $work/jb.md" "$work/jsf.out"; then
  ok "--stdin とファイルを一緒に渡すと、標準入力だけに掛ける (rc 0)"
else
  fail "--stdin とファイル: rc=$rc (期待 0): $(cat "$work/jsf.out" "$work/jsf.err")"
fi

rc=0
printf 'これは普通の文です。\n' | "$BIN" --stdin --stdin-filename s.go "$work/jb.md" >"$work/jsf2.out" 2>"$work/jsf2.err" || rc=$?
if ! grep -qF 'ja-writing lint: ' "$work/jsf2.out"; then
  ok "--stdin の名前が対象外でも、一緒に渡したファイルには掛けない (rc=$rc は textlint のまま)"
else
  fail "--stdin とファイル (対象外の名前): rc=$rc: $(cat "$work/jsf2.out" "$work/jsf2.err")"
fi

# 標準入力の名前が対象でない拡張子なら掛けない
rc=0
printf 'ただし**「x」**と書く。\n' | "$BIN" --stdin --stdin-filename x.go >"$work/jsg.out" 2>"$work/jsg.err" || rc=$?
if ! grep -qF 'ja-writing lint: ' "$work/jsg.out" && grep -qF 'x.go が textlint の対象の拡張子でない' "$work/jsg.err"; then
  ok "--stdin-filename x.go の標準入力には ja-writing lint を掛けない (rc=$rc は textlint のまま)"
else
  fail "--stdin-filename x.go: rc=$rc: $(cat "$work/jsg.out" "$work/jsg.err")"
fi

# 名前に glob の文字があると textlint は別のファイル (x.md) を読む。名前のファイル ([x].md) には掛けない
gl="$work/gl"
mkdir -p "$gl"
cp "$work/jb.md" "$gl/[x].md"
cp "$work/ok.md" "$gl/x.md"
rc=0
(cd "$gl" && "$BIN" '[x].md') >"$work/jgl.out" 2>"$work/jgl.err" || rc=$?
if [ "$rc" -eq 0 ] && ! grep -qF 'ja-writing lint: [x].md' "$work/jgl.out" && grep -qF '[x].md は textlint の対象でない' "$work/jgl.err"; then
  ok "glob の文字を含む名前 ([x].md) には、textlint が読まないので掛けない (rc 0)"
else
  fail "[x].md: rc=$rc (期待 0): $(cat "$work/jgl.out" "$work/jgl.err")"
fi

# `--` の後ろの - で始まる名前もファイルとして掛ける
cp "$work/jb.md" "$gl/-d.md"
rc=0
(cd "$gl" && "$BIN" -- -d.md) >"$work/jdd.out" 2>"$work/jdd.err" || rc=$?
if [ "$rc" -eq 1 ] && jw_ran "$work/jdd.out" -d.md; then
  ok "-- の後ろの -d.md にも掛ける (rc 1)"
else
  fail "-- -d.md: rc=$rc (期待 1): $(cat "$work/jdd.out" "$work/jdd.err")"
fi

# ディスク上と大小の違う綴り (`../GL/...`) でも、textlint が読むファイルには掛ける (字面でなく実体で比べる)
cs="$work/csdir"
mkdir -p "$cs/sub"
cp "$work/jb.md" "$cs/sub/jb.md"
rc=0
(cd "$cs/sub" && "$BIN" ../SUB/jb.md) >"$work/jcs.out" 2>"$work/jcs.err" || rc=$?
if [ "$rc" -eq 1 ] && jw_ran "$work/jcs.out" ../SUB/jb.md; then
  ok "大小の違う綴り (../SUB/jb.md) でも掛ける (rc 1)"
else
  fail "../SUB/jb.md: rc=$rc (期待 1): $(cat "$work/jcs.out" "$work/jcs.err")"
fi

# stat が ENOTDIR になる名前 (x.md/) があっても、ほかのファイルには掛ける。
# x.md/ を外すときの文は Node のバージョンで分かれる (throwIfNoEntry: false で ENOTDIR を投げるバージョンは「読めない」、黙って返すバージョンは「ファイルでない」)
rc=0
(cd "$gl" && "$BIN" -- x.md/ -d.md) >"$work/jnd.out" 2>"$work/jnd.err" || rc=$?
if [ "$rc" -eq 1 ] && jw_ran "$work/jnd.out" -d.md && grep -qE 'x\.md/ (を読めない|はファイルでない)' "$work/jnd.err"; then
  ok "読めない名前 (x.md/) があってもほかのファイル (-d.md) には掛ける (rc 1)"
else
  fail "x.md/ と -d.md: rc=$rc (期待 1): $(cat "$work/jnd.out" "$work/jnd.err")"
fi

# 対象を判定する補助が失敗したら、textlint の指摘は出し、ja-writing lint は掛けずに rc 3 (偽の緑にしない)
bad="$work/badhelper"
mkdir -p "$bad/bin/lib" "$bad/_claude/skills/ja-writing"
cp "$ROOT_DIR/bin/ja-textlint" "$ROOT_DIR/bin/ja-writing" "$bad/bin/"
cp "$ROOT_DIR/bin/lib/go_autobuild.zsh" "$bad/bin/lib/"
ln -s "$ROOT_DIR/src" "$bad/src"
ln -s "$ROOT_DIR/_claude/skills/ja-writing/textlint" "$bad/_claude/skills/ja-writing/textlint"
printf 'process.exitCode = 2;\n' >"$bad/bin/lib/ja_textlint_targets.cjs"
rc=0
"$bad/bin/ja-textlint" "$work/jb.md" >"$work/jbh.out" 2>"$work/jbh.err" || rc=$?
if [ "$rc" -eq 3 ] && ! grep -qF 'ja-writing lint: ' "$work/jbh.out" && grep -qF '判定できない' "$work/jbh.err"; then
  ok "補助が失敗したら ja-writing lint を掛けずに rc 3"
else
  fail "補助の失敗: rc=$rc (期待 3): $(cat "$work/jbh.out" "$work/jbh.err")"
fi

# textlint の依存が無いとき: 対象でない拡張子 (.go) には掛けず、オプションがあれば引数を読めないので掛けない
rc=0
XDG_CACHE_HOME="$work/nocache" JA_TEXTLINT_NO_INSTALL=1 "$BIN" "$work/w.md" "$work/m.go" >"$work/jn.out" 2>"$work/jn.err" || rc=$?
if [ "$rc" -eq 3 ] && jw_ran "$work/jn.out" "$work/w.md" && ! grep -qF "ja-writing lint: $work/m.go" "$work/jn.out"; then
  ok "textlint の依存が無いときも .go には掛けない (rc 3)"
else
  fail "依存なしの .go: rc=$rc (期待 3): $(cat "$work/jn.out" "$work/jn.err")"
fi
rc=0
XDG_CACHE_HOME="$work/nocache" JA_TEXTLINT_NO_INSTALL=1 "$BIN" -f=json "$work/w.md" >"$work/jnf.out" 2>"$work/jnf.err" || rc=$?
if [ "$rc" -eq 3 ] && ! grep -qF 'ja-writing lint: ' "$work/jnf.out" && grep -qF '引数を読めず' "$work/jnf.err"; then
  ok "textlint の依存が無くオプションがあるときは掛けない (rc 3)"
else
  fail "依存なしのオプション: rc=$rc (期待 3): $(cat "$work/jnf.out" "$work/jnf.err")"
fi

[ "$fails" -eq 0 ] || { printf '✗ %d 件失敗\n' "$fails" >&2; exit 1; }
echo "OK: ja-textlint"
