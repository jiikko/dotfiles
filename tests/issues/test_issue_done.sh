#!/usr/bin/env bash
# scripts/issue_done.sh の検査。
#
# 対象は「issue を done/ へ送る 4 手順を 1 本に寄せた道具」で、**自分で新設した安全機構**
# (着手前の baseline / 失敗時の rollback) を持つ。したがって正常系だけでなく
#   ① 手順を 1 つずつ外す変異で、対応する検査が赤くなり **元の状態へ戻る**
#   ② 着手前に既に赤いときは何も動かさない
#   ③ 書き換えロジックが壊れたら canary が触る前に落とす
# を実験で作って確かめる (_claude/rules/adversarial-review-own-safeguards.md §1 / §1.5)。
#
# ## 変異の当て方
#
# 変異は**使い捨ての fake root** で当てる (repo の scripts/ を書き換えない)。
#   $fake/scripts/issue_done.sh  = sed で 1 手順を潰したコピー
#   $fake/tests -> <repo>/tests  = 検査は本物を通す (stub にすると「検査が赤くなること」を
#                                  検査できず、変異検証が自己言及になる)
# 🚨 段ごとに 1 つずつ外す (全部同時に外すとどれか 1 つで赤くなり、残りが無検査でも見えない)。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR" || exit 1

SCRIPT="$ROOT_DIR/scripts/issue_done.sh"
[ -x "$SCRIPT" ] || { printf '✗ scripts/issue_done.sh が無い / 実行できない\n' >&2; exit 1; }

work="$(mktemp -d)"
trap 'chmod -R u+rwX "$work" 2>/dev/null || true; rm -rf "$work"' EXIT

fail() { printf '✗ %s\n' "$1" >&2; exit 1; }

# fixture: global issue 1 件 (347) / group issue 1 件 (901) / 参照元 2 本 / claim symlink 2 本
make_fixture() {
  local d="$1"
  mkdir -p "$d/issues/done" "$d/issues/next" "$d/issues/epic/900/done" "$d/issues/epic/900/next" \
           "$d/_claude/rules" "$d/docs"
  : > "$d/_claude/rules/x.md"
  : > "$d/docs/y.md"
  : > "$d/issues/done/311-b.md"
  cat > "$d/issues/347-refactor-a.md" <<'EOF'
# a
- 上向き: [rule](../_claude/rules/x.md)
- 下向き: [old](done/311-b.md)
- アンカー: [doc](../docs/y.md#sec)
- インライン: `](path.md)` は書式の説明
```sh
[fenced](../_claude/rules/x.md)
```
EOF
  cat > "$d/issues/345-retro-c.md" <<'EOF'
- [x](347-refactor-a.md) を起票
EOF
  cat > "$d/issues/done/310-d.md" <<'EOF'
- [x](../347-refactor-a.md) 参照
EOF
  cat > "$d/issues/epic/900/901-feat-e.md" <<'EOF'
- [rule](../../../_claude/rules/x.md)
EOF
  # issues/ の外からの参照 (P1-2: ここを走査しないと黙って切れる)
  mkdir -p "$d/docs"
  cat > "$d/docs/g.md" <<'EOF'
外から: [x](../issues/347-refactor-a.md)
EOF
  # 🚨 repo root 直下からの参照。relbase が BASE_DIR 自身で絶対パスを返していたため、
  # ここだけ張り直されないのに「stale 0 件」で緑になっていた (2 周目の敵対レビュー P1-1)
  cat > "$d/README.md" <<'EOF'
root から: [x](issues/347-refactor-a.md)
EOF
  ln -s ../347-refactor-a.md "$d/issues/next/347-refactor-a.md"
  ln -s ../901-feat-e.md "$d/issues/epic/900/next/901-feat-e.md"
  git -C "$d" init -q
  git -C "$d" add -A
  git -C "$d" -c user.email=t@t -c user.name=t commit -qm init
}

# claim symlink を **追跡外**にした fixture (glogx の `n` が作る通常の状態)。
# git rm を無条件に打つと rc=128 で落ち、set -e で移動済みのまま死ぬ (P1-1)
make_fixture_untracked_claim() {
  local d="$1"
  make_fixture "$d"
  git -C "$d" rm -q --cached -- issues/next/347-refactor-a.md
  git -C "$d" -c user.email=t@t -c user.name=t commit -qm untrack
}

# 変異を当てた script を置いた fake root を作る ($1=作業名 $2=sed 式。空なら無変異)
make_fake_root() {
  # 🚨 `local a="$1" b="$work/$a"` と書かない。bash は local の引数を**先に全部展開**するので
  # $a はまだ未割り当てで、set -u の下では unbound で落ちる (呼び出し元に同名の local が
  # 在ると dynamic scope でたまたま通るため、テストの後段でだけ露出する)。
  local name expr fake
  name="$1"; expr="$2"; fake="$work/$name"
  mkdir -p "$fake/scripts"
  ln -s "$ROOT_DIR/tests" "$fake/tests"
  if [ -n "$expr" ]; then sed "$expr" "$SCRIPT" > "$fake/scripts/issue_done.sh"
  else cp "$SCRIPT" "$fake/scripts/issue_done.sh"; fi
  chmod +x "$fake/scripts/issue_done.sh"
  printf '%s' "$fake"
}

# ---------------------------------------------------------------------------
# 1. 正常系: 4 手順すべてが効く (global / group)
# ---------------------------------------------------------------------------
d="$work/ok"; mkdir -p "$d"; make_fixture "$d"
ISSUES_DIR="$d/issues" "$SCRIPT" 347 901 > "$work/ok.log" 2>&1 ||
  { cat "$work/ok.log" >&2; fail "正常系が落ちた"; }

[ -f "$d/issues/done/347-refactor-a.md" ] || fail "① 347 が done/ へ移っていない"
[ ! -e "$d/issues/347-refactor-a.md" ] || fail "① 元の位置に残っている"
[ ! -e "$d/issues/next/347-refactor-a.md" ] || fail "② claim symlink が残っている"
[ -f "$d/issues/epic/900/done/901-feat-e.md" ] || fail "① group issue の移動先が違う"
[ ! -e "$d/issues/epic/900/next/901-feat-e.md" ] || fail "② group の claim symlink が残っている"

grep -qF '](../../_claude/rules/x.md)' "$d/issues/done/347-refactor-a.md" || fail "③ 上向きリンクが 1 段深くなっていない"
grep -qF '](311-b.md)' "$d/issues/done/347-refactor-a.md" || fail "③ 下向きリンク (done/311) が畳まれていない"
grep -qF '](../../docs/y.md#sec)' "$d/issues/done/347-refactor-a.md" || fail "③ アンカー付きが張り直されていない"
grep -qF '`](path.md)`' "$d/issues/done/347-refactor-a.md" || fail "③ インラインコードを書き換えている"
grep -qF '[fenced](../_claude/rules/x.md)' "$d/issues/done/347-refactor-a.md" || fail "③ フェンス内を書き換えている"
grep -qF '](done/347-refactor-a.md)' "$d/issues/345-retro-c.md" || fail "④ 直下からの参照が張り直されていない"
grep -qF '](347-refactor-a.md)' "$d/issues/done/310-d.md" || fail "④ done/ からの参照が張り直されていない"
grep -qF '](../../../../_claude/rules/x.md)' "$d/issues/epic/900/done/901-feat-e.md" || fail "③ group の深さ (4 段) が違う"
grep -qF '](../issues/done/347-refactor-a.md)' "$d/docs/g.md" || fail "④ issues/ の外 (docs/) からの参照が張り直されていない"
grep -qF '](issues/done/347-refactor-a.md)' "$d/README.md" || fail "④ repo root 直下 (README.md) からの参照が張り直されていない"

"$ROOT_DIR/tests/issues/test_issue_links_valid.sh" "$d/issues" >/dev/null 2>&1 || fail "移動後にリンク検査が赤"
"$ROOT_DIR/tests/issues/test_next_links_valid.sh" "$d/issues" >/dev/null 2>&1 || fail "移動後に next 検査が赤"

# ---------------------------------------------------------------------------
# 2. 実体が無い / 複数ある / done 済み は着手しない
# ---------------------------------------------------------------------------
d2="$work/missing"; mkdir -p "$d2"; make_fixture "$d2"
if ISSUES_DIR="$d2/issues" "$SCRIPT" 999 >/dev/null 2>&1; then fail "実体の無い番号を受け付けた"; fi
if ISSUES_DIR="$d2/issues" "$SCRIPT" 12 >/dev/null 2>&1; then fail "3 桁でない番号を受け付けた"; fi

# 2.5 追跡外の claim symlink でも通る (glogx の `n` が作る通常の状態。P1-1)
d25="$work/untracked"; mkdir -p "$d25"; make_fixture_untracked_claim "$d25"
ISSUES_DIR="$d25/issues" "$SCRIPT" 347 > "$work/untracked.log" 2>&1 ||
  { cat "$work/untracked.log" >&2; fail "追跡外の claim symlink で落ちた (git rm を無条件に打っている)"; }
[ -f "$d25/issues/done/347-refactor-a.md" ] || fail "追跡外 claim: 移動できていない"
[ ! -e "$d25/issues/next/347-refactor-a.md" ] || fail "追跡外 claim: symlink が残っている"

# 2.6 epic は固定 2 段。予約外の深い置き場は着手しない (P2-3)
d26="$work/deepepic"; mkdir -p "$d26/issues/epic/900/blocked"
printf '# a\n' > "$d26/issues/epic/900/blocked/347-a.md"
git -C "$d26" init -q; git -C "$d26" add -A
git -C "$d26" -c user.email=t@t -c user.name=t commit -qm init
if ISSUES_DIR="$d26/issues" "$SCRIPT" 347 >/dev/null 2>&1; then
  fail "epic/<name>/<予約外>/ の issue を動かした (契約に無い深さの done/ ができる)"
fi
[ -f "$d26/issues/epic/900/blocked/347-a.md" ] || fail "拒否したのに動かしていた"

# 2.7 `--help` は番号より先に効く (「347 を処理せず rc=0」= 成功に見える無操作を作らない)
d27="$work/help"; mkdir -p "$d27"; make_fixture "$d27"
ISSUES_DIR="$d27/issues" "$SCRIPT" 347 --help >/dev/null 2>&1 || fail "--help が rc≠0"
[ -f "$d27/issues/347-refactor-a.md" ] || fail "--help なのに移動した"

# ---------------------------------------------------------------------------
# 3. 着手前に既に赤いなら何も動かさない (自分の変更のせいに見える赤を作らない)
# ---------------------------------------------------------------------------
d3="$work/redbase"; mkdir -p "$d3"; make_fixture "$d3"
printf -- '- [broken](../_claude/rules/nonexistent.md)\n' >> "$d3/issues/345-retro-c.md"
if ISSUES_DIR="$d3/issues" "$SCRIPT" 347 > "$work/redbase.log" 2>&1; then
  fail "baseline が赤なのに着手した"
fi
[ -f "$d3/issues/347-refactor-a.md" ] || fail "baseline 赤なのに移動していた"
[ -L "$d3/issues/next/347-refactor-a.md" ] || fail "baseline 赤なのに claim を消していた"

# ---------------------------------------------------------------------------
# 4. 変異: 手順を 1 つずつ外すと、対応する検査が赤くなり **元へ戻る**
# ---------------------------------------------------------------------------
# 戻ったことの判定は「fixture を作り直した直後との全ファイル差分ゼロ」で見る
# (ファイルの有無だけを見ると、本文だけ書き換わって戻っていない形を素通しする)
snapshot() {
  ( cd "$1" || exit 1
    find issues -print | sort | while IFS= read -r p; do
      if [ -L "$p" ]; then printf '%s|L|%s\n' "$p" "$(readlink "$p")"
      elif [ -f "$p" ]; then printf '%s|F|%s\n' "$p" "$(shasum -a 256 < "$p" | awk '{print $1}')"
      else printf '%s|D|\n' "$p"
      fi
    done )
}

check_mutant() {  # $1=名前 $2=sed 式 $3=赤くなるべき検査のキーワード
  local name expr want fake dm before after
  name="$1"; expr="$2"; want="$3"
  dm="$work/mut-$name"; mkdir -p "$dm"; make_fixture "$dm"
  before="$(snapshot "$dm")"
  fake="$(make_fake_root "root-$name" "$expr")"
  # 🚨 変異が当たっていることを先に確かめる。当たっていない変異の緑を「守られていない」と
  # 読むと、効いている手順を作り替える方向に走る (mutation-verify-new-tests.md 手順 1.6)
  if diff -q "$SCRIPT" "$fake/scripts/issue_done.sh" >/dev/null; then
    fail "変異 [$name] が当たっていない (sed の当て先がずれた)"
  fi
  bash -n "$fake/scripts/issue_done.sh" 2>/dev/null ||
    fail "変異 [$name] が構文エラー (red でも green でもない。当て直すこと)"
  if ISSUES_DIR="$dm/issues" "$fake/scripts/issue_done.sh" 347 > "$work/mut-$name.log" 2>&1; then
    fail "変異 [$name] が緑のまま通った (この手順は誰も守っていない)"
  fi
  grep -q "$want" "$work/mut-$name.log" ||
    { cat "$work/mut-$name.log" >&2; fail "変異 [$name] は赤くなったが、想定した検査 ($want) ではない"; }
  after="$(snapshot "$dm")"
  [ "$before" = "$after" ] ||
    { diff <(printf '%s\n' "$before") <(printf '%s\n' "$after") >&2 || true
      fail "変異 [$name] で rollback が効いていない (半端な状態で終わった)"; }
}

# ② claim symlink の削除を外す → next の目印が dangling になる
check_mutant no-unclaim 's|^  if \[ -L "$claim" \]; then$|  if false; then|' \
  '目印の指す先が通常ファイルとして存在しない'
# ③ 移した本文の張り直しを外す → 本文の上向きリンクが 1 段足りない
check_mutant no-rebase-self 's|^\( *\)rebase_file "$dest" .*|\1:|' \
  'リンクが解決しない'
# ④ 他ファイルからの参照の張り直しを外す → 参照元のリンクが切れる
check_mutant no-rebase-inbound 's|if rebase_file "$other" .*; then|if false; then|' \
  'リンクが解決しない'
# ④ の走査を issues/ に狭める → issues/ の外 (docs/) の参照が切れるが、2 本のリンク検査は
#    そこを見ないので **事後条件だけが**これを捕まえる (P1-2 のオラクル)
check_mutant narrow-scan \
  "s|done < <(scan_candidates \"\$base\")|done < <(find \"\$ISSUES_DIR\" -type f -name '*.md' -print \| sort)|" \
  '移動前のパスを指したままの参照'
# 判定より前で異常終了する → trap が rollback を回す (P1-1 / P1-3 の共通形)
check_mutant abort-before-verdict 's|^  verdict=0$|  exit 3|' \
  '判定の前に中断された'
# 母集合の走査を空にする → **触る前に** canary が落とす。
# 🚨 これが無いと、手順 4 も事後条件も「何もせず 0 件 = 緑」になる。オラクル側の awk にだけ
# canary があり、母集合の破損は誰も見ていなかった (2 周目の敵対レビュー P1-2)
check_mutant empty-scan 's|^  grep -rlF --binary-files=without-match .*|  return 0|' \
  '母集合の走査が壊れている'

# ---------------------------------------------------------------------------
# 5. 変異: 書き換えロジックそのものを壊すと canary が **触る前に** 落とす
# ---------------------------------------------------------------------------
dc="$work/mut-canary"; mkdir -p "$dc"; make_fixture "$dc"
before_c="$(snapshot "$dc")"
fake_c="$(make_fake_root root-canary 's|^  return nl frag$|  return lnk|')"
if ISSUES_DIR="$dc/issues" "$fake_c/scripts/issue_done.sh" 347 > "$work/mut-canary.log" 2>&1; then
  fail "変異 [canary] が緑のまま通った"
fi
grep -q '✗ canary' "$work/mut-canary.log" ||
  { cat "$work/mut-canary.log" >&2; fail "canary ではなく後段で落ちた (触る前に止まっていない)"; }
[ "$before_c" = "$(snapshot "$dc")" ] || fail "canary で落ちたのに fixture が変わっている"

printf '✓ issue_done.sh: 正常系 (global / group / repo root からの参照 / 追跡外 claim / epic の深い置き場を拒否 / --help) / baseline 赤の拒否 / 変異 7 本 (claim 削除・本文の張り直し・参照の張り直し・走査範囲・判定前の中断・母集合の空振り・canary) すべて red かつ rollback 済み\n'
