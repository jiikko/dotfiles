#!/usr/bin/env bash
# 新しい Go プロジェクトが「CI レーン無し」で入るのを止める検査 (issue 203 候補 B)。
#
# なぜ: 出典は issue 080 / 087。以前は Makefile の GO_PROJECT_DIRS を手で列挙しており、
# 追記を忘れたプロジェクトが **無音で lint / test から外れた**。Makefile 側は
# `wildcard src/*/go.mod` で解決済み (Makefile:392) だが、**CI の paths filter と
# プロジェクト側の Makefile target は今も手で用意する**ので、同じ穴が残っている。
#
# 検査する不変条件 (src/*/go.mod を出典に、6 つ):
#   1. src/<name>/Makefile に lint: と test: の両方がある
#      (Makefile の run_go_projects が `make -C <dir> lint` / `test` を呼ぶ)
#   2. .github/workflows/src_<name>.yml がある (paths filter つきの薄い caller)
#   3. その workflow の paths が src/<name>/ を含む (含まないと push で 1 度も起動しない)
#   4. go.mod が replace で取り込む共有 module の src/<dir>/ も paths に含む
#   5. src/<name>/go.sum がある (依存が無ければ空でよい)。Go は依存の無い module に go.sum を作らないが、
#      _go-project.yml の setup-go は cache-dependency-path に <dir>/go.sum を渡すので、無いと
#      `Restore cache failed` の警告だけでキャッシュが毎回効かない (ジョブは緑のまま = 気づけない)
#   6. workflow に _go-project.yml を呼ぶ job が 1 つ以上あり、その全部の `dir` が src/<name>
#      (CI が lint / test するのは caller が渡す dir。写し間違えると、paths は合っていて起動もするのに
#       別の project を検査し、この project は誰にも検査されない。1〜5 は src/<name> を前提にしているので気づけない)
#      脅威モデルは「既存の src_*.yml をコピーしたときの書き換え忘れ」。検出しない形 (review の担当):
#      追加 job の go-version-file / cache-dependency-path / env に残った古い project 名・job id の衝突・
#      引用符で囲んだ job id や with 以外のキーの下の dir: (後勝ちで上書きされる)・steps の中の uses。
#      `dir: ./src/<name>` は動くが赤になる (安全側に倒す。今の書き方に揃える)
#
# 🚨 検査できなかったときに緑を返さない。依存コマンド不在・発見 0 件はすべて失敗にする
#   (`_claude/rules/adversarial-review-own-safeguards.md` の false green)。
# 🚨 paths の検査は「行として現れるか」の静的検査。YAML として解釈しないので、
#   コメントアウトされた paths も通ってしまう。取りこぼすより出す側へ倒す方針は変えない
#   (誤って通す形は下の quoted/unquoted 両対応で狭めてある)。
set -uo pipefail

# 一時ファイルの後始末は EXIT trap に載せる (issue 299)。成功パスの rm だけだと、
# 途中で落ちた / 中断されたときに $TMPDIR へ残る (同型が tmux_schedule_keys.sh に 2 つあった)。
# bash は SIGTERM でも EXIT trap を走らせる (実測 2026-09-06)。
CGL_TMPFILES=()
cgl_cleanup_tmpfiles() {
  local f
  for f in ${CGL_TMPFILES[@]+"${CGL_TMPFILES[@]}"}; do
    [ -n "$f" ] && rm -f "$f" 2>/dev/null
  done
  CGL_TMPFILES=()
}
trap cgl_cleanup_tmpfiles EXIT
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR" || { printf '✗ repo root へ移動できない\n'; exit 1; }

fail() { printf '✗ %s\n' "$1"; exit 1; }

for c in grep find sed basename; do
  command -v "$c" >/dev/null 2>&1 || fail "$c が無い。検査できないので緑にしない"
done
grep -q probe <<< 'probe' || fail "grep が正常に動作しない"

# paths_has は workflow の **trig (push / pull_request) の** `paths:` ブロックに
# want で始まる項目があるか。
#
# 🚨 トリガーを分けずに「ファイル内のどこかの paths」を見る形にしない。**片方にだけ書けば
#    通ってしまい、「push では 1 度も走らない」を検出できない** (2026-09-04 の変異検証で実測:
#    push から落とす変異が緑のまま通った)。
#
# 🚨 **`paths:` ブロックの中だけ**を見る。行だけ grep すると `paths-ignore:` の下に書かれた
#    「その dir を除外する」設定を「含む」と読んでしまい、不変条件が反転したまま緑になる
#    (敵対的レビュー 2026-09-03 の P2-3 で実測)。指定は quoted / unquoted の両方が使われている
# 🚨 判定は awk の exit status で返す。`awk … | grep -q` にすると pipefail 下で
#    「一致しているのに非 0」になりうる (issue 096 / test-pipefail-grep-q が止めている形)
paths_has() {
  awk -v want="$2" -v trig="$3" '
    # on: の中だけを見る。**トリガー名を列挙しない**: 知らないキー (pull_request_target /
    # 行末コメント付き) で intrig が落ちないと、直前のトリガーの判定が次のブロックへ漏れる
    # (実測 2026-09-04: `pull_request:   # PR でも回す` で push の判定が true になった)
    /^on:/ { inon = 1; next }
    /^[^[:space:]#]/ { inon = 0; intrig = 0; inblk = 0 }    # トップレベルの別キー (jobs: 等)
    inon && /^[[:space:]]+[A-Za-z_][A-Za-z_0-9-]*:/ {
      ind = match($0, /[^ ]/)
      if (onind == 0) onind = ind                          # on: 直下の深さを最初のキーで覚える
      if (ind == onind) {                                  # その深さのキー = トリガー名
        t = $0; sub(/^[[:space:]]*/, "", t); sub(/:.*$/, "", t)
        intrig = (t == trig); inblk = 0; next
      }
    }
    /^[[:space:]]*paths:[[:space:]]*$/ { inblk = intrig; next }
    /^[[:space:]]*paths-ignore:/ { inblk = 0; next }
    inblk {
      if ($0 ~ /^[[:space:]]*#/) next                      # コメント行は読み飛ばす
      if ($0 !~ /^[[:space:]]*-/) { inblk = 0; next }      # リストが終わったらブロック終了
      line = $0
      gsub(/^[[:space:]]*-[[:space:]]*/, "", line)
      gsub(/^['"'"'"]|['"'"'"]$/, "", line)
      if (index(line, want) == 1) { found = 1; exit }
    }
    END { exit !found }
  ' "$1"
}

# replace_dirs は go.mod の `replace <mod> => ../<dir>` から <dir> を取り出す
# (単行と括弧ブロックの両方)。
#
# 🚨 `../` で始まる相対 replace だけを見る。バージョン置換 (`=> example.com/x v1.2.3`) は
#    ローカルの dir 依存ではないので CI の paths とは関係しない
replace_dirs() {
  awk '
    /^[[:space:]]*replace[[:space:]]*\(/ { inblk = 1; next }
    inblk && /^[[:space:]]*\)/ { inblk = 0; next }
    {
      line = $0
      sub(/[[:space:]]*\/\/.*$/, "", line)                  # 行末コメントを落とす
      gsub(/["'"'"']/, "", line)                            # go.mod は引用付きの path も許す
      if (!inblk) {
        if (line !~ /^[[:space:]]*replace[[:space:]]/) next
        sub(/^[[:space:]]*replace[[:space:]]+/, "", line)
      }
      if (line !~ /=>[[:space:]]*\.\.\//) next
      sub(/.*=>[[:space:]]*\.\.\//, "", line)
      sub(/[[:space:]].*$/, "", line)
      sub(/\/+$/, "", line)
      # 🚨 多段の ../ (= src/ の外) は「src/<dir>」に写せない。黙って変な行を出さず落とす
      if (line ~ /(^|\/)\.\.(\/|$)/) { print "__OUTSIDE_SRC__"; next }
      if (line != "") print line
    }
  ' "$1"
}

# go_project_dirs は workflow の jobs: の下で _go-project.yml を呼ぶ job ごとに、渡している dir を 1 行ずつ出す
# (dir を渡していない job は空行)。呼ぶ job が無ければ何も出さない。
#
# 🚨 job の境目は jobs: 直下の深さのキーで決める (トリガー名と同じく job 名は列挙しない)。
#    dir は block (`with:` の下の `dir: x`) と flow (`with: {dir: x}`) の両方で拾い、引用符・末尾の / と行末コメントを落とす。
#    _go-project.yml を呼ばない job の dir は見ない (別用途の入力名と取り違えない)
go_project_dirs() {
  awk '
    function flush() { if (injob && uses) print dir; uses = 0; dir = "" }
    /^jobs:/ { injobs = 1; next }
    /^[^[:space:]#]/ { if (injobs) flush(); injobs = 0; injob = 0; next }
    !injobs { next }
    /^[[:space:]]*#/ { next }
    /^[[:space:]]+[A-Za-z_][A-Za-z_0-9-]*:/ {
      ind = match($0, /[^ ]/)
      if (jobind == 0) jobind = ind
      if (ind == jobind) { flush(); injob = 1; next }
    }
    injob {
      line = $0
      sub(/[[:space:]]+#.*$/, "", line)
      if (line ~ /^[[:space:]]*-?[[:space:]]*uses:/ && line ~ /_go-project\.yml/) uses = 1
      if (match(line, /(^|[{,[:space:]])dir:[[:space:]]*[^,}[:space:]]+/)) {
        v = substr(line, RSTART, RLENGTH)
        sub(/^.*dir:[[:space:]]*/, "", v)
        gsub(/["'"'"']/, "", v)
        sub(/\/+$/, "", v)
        dir = v
      }
    }
    END { if (injobs) flush() }
  ' "$1"
}

# 🚨 canary: 抽出が空を返すと「呼ぶ job が無い」として赤にはなるが、dir の取り違えは
#    「別の値を返す」形で壊れる。block / flow / 引用符 / コメント / 呼ばない job を合成入力で先に固定する
gd_probe=$(mktemp) || fail "mktemp できない"
CGL_TMPFILES+=("$gd_probe")
cat > "$gd_probe" <<'GDYML'
on:
  push:
jobs:
  a:
    uses: ./.github/workflows/_go-project.yml
    with:
      dir: 'src/x/'   # 末尾の / と引用符とコメント
  other:
    runs-on: macos-15
    with:
      dir: src/notgo
  b:
    uses: ./.github/workflows/_go-project.yml
    with: {dir: "src/y"}
  c:
    uses: ./.github/workflows/_go-project.yml
  d:
    uses: ./.github/workflows/other.yml   # _go-project.yml の写しではない (コメントの中の名前で拾わない)
    with:
      dir: src/notgo2
GDYML
gd_got=$(go_project_dirs "$gd_probe" | tr '\n' '|')
rm -f "$gd_probe"
[ "$gd_got" = "src/x|src/y||" ] || fail "go_project_dirs の canary が壊れている: got=[$gd_got] want=[src/x|src/y||]"

# 🚨 canary: paths_has の壊れ方は**非対称**で、「見つからなくなる」は違反として大声で出るが
#    「常に見つかる」は完全に無音。負例 (push には無く pull_request にだけ在る / paths-ignore の下 /
#    コメント行) を pin しないと、トリガーの取り違えを検出できない (敵対的レビュー 2026-09-04)
ph_probe=$(mktemp) || fail "mktemp できない"
CGL_TMPFILES+=("$ph_probe")
cat > "$ph_probe" <<'PHYML'
on:
  push:
    branches: [master]
    paths:
      - 'src/inpush/**'
  pull_request:   # 行末コメント付き (知らないキーで intrig が落ちるかを見る)
    paths:
      - 'src/inpr/**'
  workflow_dispatch:
jobs:
  x:
    runs-on: macos-15
    paths-ignore:
      - 'src/ignored/**'
PHYML
ph_fail=""
paths_has "$ph_probe" "src/inpush/" "push"       || ph_fail="$ph_fail push に在るものを見つけられない;"
paths_has "$ph_probe" "src/inpr/" "pull_request" || ph_fail="$ph_fail pull_request に在るものを見つけられない;"
paths_has "$ph_probe" "src/inpr/" "push"         && ph_fail="$ph_fail pull_request だけの項目を push で見つけた;"
paths_has "$ph_probe" "src/ignored/" "push"      && ph_fail="$ph_fail jobs 配下の paths-ignore を拾った;"
rm -f "$ph_probe"
[ -z "$ph_fail" ] || fail "paths_has の canary が壊れている:$ph_fail"

# 🚨 canary: 抽出が壊れると「replace 0 件 = 違反 0 件」で**緑のまま素通り**する。
#    本走査と同じ関数へ既知の入力を通して先に確かめる (合成入力なので repo の実体には依存しない)。
canary_got=$(printf 'module x\n\nreplace foo => ../bar\n\nreplace (\n\tbaz => ../qux\n\tver => example.com/v v1.0.0\n)\n' \
  | replace_dirs /dev/stdin | tr '\n' ' ')
[ "$canary_got" = "bar qux " ] || fail "replace_dirs の canary が壊れている: got=[$canary_got] want=[bar qux ]"

# 出典は Makefile と同じ `src/*/go.mod` (手で列挙しない)。
# 🚨 glob + `-f` で見る。`find` は symlink を辿らないので、`src/<name>` が symlink のとき
#    make の `wildcard` は見えるのに検査は見えない = 「出典は Makefile と同じ」が崩れる
#    (敵対的レビュー 2026-09-03 の P3-10 で実測)
projects=""
for d in src/*/; do
  [ -f "$d/go.mod" ] || continue
  projects="$projects$(basename "$d")
"
done
projects=$(printf '%s' "$projects" | sort)
[ -n "$projects" ] || fail "src/*/go.mod が 1 件も見つからない (発見の仕方が壊れている)"

bad=0 n=0 deps=0
while IFS= read -r name; do
  [ -n "$name" ] || continue
  n=$((n + 1))
  mk="src/$name/Makefile"
  wf=".github/workflows/src_$name.yml"

  if [ ! -f "$mk" ]; then
    printf '✗ %s: %s が無い (make -C で lint / test を呼べない)\n' "$name" "$mk"; bad=$((bad + 1)); continue
  fi
  for t in lint test; do
    # 🚨 行頭の target 定義だけを見る (`.PHONY: lint test` の行や `test-foo:` に当てない)。
    #    `^$t:` だけだと **make の変数代入 `lint:=x` にも当たる** (敵対的レビュー 2026-09-03 の P2-8)
    if ! grep -qE "^$t:([[:space:]]|\$)" "$mk"; then
      printf '✗ %s: %s に %s: target が無い\n' "$name" "$mk" "$t"; bad=$((bad + 1)); continue
    fi
    # 🚨 名前だけあって recipe が空だと「lint が走る」を守れない (同 P3-9)。
    #    target 行の次の行がタブ始まり (recipe) か、依存を持つことを求める
    if ! awk -v t="$t" '
          $0 ~ "^" t ":" { got = 1; deps = $0; sub(/^[^:]*:[[:space:]]*/, "", deps); next }
          got { if ($0 ~ /^\t/ || deps != "") { print "ok"; exit } ; got = 0 }
        ' "$mk" | grep -q ok; then
      printf '✗ %s: %s の %s: が空 (recipe も依存も無い = 実際には何も走らない)\n' "$name" "$mk" "$t"; bad=$((bad + 1))
    fi
  done

  if [ ! -f "src/$name/go.sum" ]; then
    printf '✗ %s: src/%s/go.sum が無い (CI のキャッシュが復元されない。依存が無ければ空のファイルを置く)\n' "$name" "$name"
    bad=$((bad + 1))
  fi

  if [ ! -f "$wf" ]; then
    printf '✗ %s: %s が無い (push しても CI が起動しない)\n' "$name" "$wf"; bad=$((bad + 1)); continue
  fi
  for trig in push pull_request; do
    if ! paths_has "$wf" "src/$name/" "$trig"; then
      printf '✗ %s: %s の %s.paths が src/%s/ を含まない (この変更では CI が走らない)\n' \
        "$name" "$wf" "$trig" "$name"; bad=$((bad + 1))
    fi
  done
  # 6. CI が実際に検査する dir が src/<name> か
  gp_n=0
  while IFS= read -r gp_dir; do
    gp_n=$((gp_n + 1))
    if [ "$gp_dir" != "src/$name" ]; then
      printf '✗ %s: %s が _go-project.yml に渡す dir が [%s] (src/%s でない。CI は別の場所を検査する)\n' \
        "$name" "$wf" "$gp_dir" "$name"; bad=$((bad + 1))
    fi
  done < <(go_project_dirs "$wf")
  if [ "$gp_n" -eq 0 ]; then
    printf '✗ %s: %s に _go-project.yml を呼ぶ job が無い (lint / test が走らない)\n' "$name" "$wf"; bad=$((bad + 1))
  fi

  # 4. replace で取り込む共有 module も paths に入っているか (issue 251)
  #    go.mod の `replace X => ../<dir>` は「その dir が変わったらこの project の CI が要る」という依存。
  #    paths に無いと、**共有 module だけを変えた push でこの project の CI が 1 度も起動しない**
  #    (2026-09-04 に termsafe を切り出したときは手で入れており、忘れても誰も止めなかった)
  while IFS= read -r dep; do
    [ -n "$dep" ] || continue
    if [ "$dep" = "__OUTSIDE_SRC__" ]; then
      printf '✗ %s: go.mod の replace 先が src/ の外を指している (paths と対応づけられない)\n' "$name"
      bad=$((bad + 1)); continue
    fi
    deps=$((deps + 1))
    for trig in push pull_request; do
      if ! paths_has "$wf" "src/$dep/" "$trig"; then
        printf '✗ %s: %s の %s.paths が src/%s/ を含まない (go.mod が replace で取り込む共有 module)\n' \
          "$name" "$wf" "$trig" "$dep"; bad=$((bad + 1))
      fi
    done
  done <<< "$(replace_dirs "src/$name/go.mod")"
done <<< "$projects"

[ "$n" -gt 0 ] || fail "検査対象が 0 件 (発見が壊れている)"
# 🚨 canary は replace_dirs の正しさを合成入力で保証するが、「実際の go.mod を読めているか」は
#    保証しない (パスの typo / 読めないファイルは空を返し、deps は 0 のまま緑になる)
[ "$deps" -gt 0 ] || fail "replace 依存が 1 件も見つからない (実 go.mod を読めていない可能性)"
if [ "$bad" -gt 0 ]; then
  printf '  直し方: src/<name>/Makefile に lint:/test: を足し、.github/workflows/src_<name>.yml を\n'
  printf '  既存の src_*.yml に倣って作る (paths に src/<name>/** と _go-project.yml を含める)\n'
  printf '  go.mod が replace で共有 module を取り込むなら、その src/<dir>/** も paths に足す\n'
  printf '  go.sum が無ければ空で置く (: > src/<name>/go.sum。依存を足せば go が中身を書く)\n'
  printf '  src_<name>.yml の with: の dir は src/<name> にする (コピー元の名前を残さない)\n'
  exit 1
fi
printf '✓ Go プロジェクト %s 件すべてに lint/test target・go.sum・CI レーン (paths と dir つき) がある (replace 依存 %s 件も paths に在り)\n' "$n" "$deps"
