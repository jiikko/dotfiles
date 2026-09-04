#!/usr/bin/env bash
# _claude/hooks/retro-open.sh (SessionStart で未決着の retro issue を注入する hook) の
# unit テスト。合成した hook JSON を stdin で流し、報告内容を pin する。
#
# なぜ: この hook は「振り返りが読まれずに溜まる」を止めるための検査で、壊れても静かに
# 黙るだけなので気づけない (= retro が誰にも見えない状態に戻る)。human-tasks-due.sh で
# 実測された欠陥と同型のもの (カテゴリの部分一致誤検出 / 「読めなかった」の沈黙 /
# pending の取りこぼし) を回帰として固定する。
# 規範: issues/README.md「`retro`」、~/.claude/CLAUDE.md「Issue管理」
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/retro-open.sh"
fails=0

if ! command -v jq >/dev/null 2>&1; then
  echo "SKIP: jq が無い環境 (hook 自体は素の stdout へフォールバックする)"
  exit 77
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/retro-open.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

repo="$WORK/repo"
mkdir -p "$repo/issues/pending" "$repo/issues/done"
git -C "$repo" init -q .

mkissue() { # $1=issues/ 以下の相対パス $2=起票日メタ行 (空可)
  printf '# t\n\n%s\n\n- [ ] 残課題\n' "$2" >"$repo/issues/$1"
}

raw() { # 追加の env は呼び出し側で env ... を前置する
  printf '{"cwd":"%s"}' "$repo" | "$@" "$HOOK" 2>/dev/null || echo "__ERROR__"
}

report() {
  local out
  out="$(raw "$@")" || { echo "__ERROR__"; return; }
  [ -n "$out" ] || return 0
  printf '%s' "$out" | jq -r '.hookSpecificOutput.additionalContext // "__NOJSON__"'
}

check() { # $1=説明 $2=期待パターン (grep -E、空なら無出力を期待) $3=本文
  local desc="$1" want="$2" got="$3"
  if [ -z "$want" ]; then
    [ -z "$got" ] && return 0
    echo "NG: $desc — 何も出さないはずが出力された:"; printf '%s\n' "$got"; fails=$((fails + 1)); return
  fi
  # 🚨 パイプに戻さないこと (pipefail + grep -q の EPIPE で判定が反転する)。
  # 経緯は tests/claude/test_human_tasks_due.sh の同じ箇所のコメント。
  grep -Eq "$want" <<<"$got" && return 0
  echo "NG: $desc — /$want/ が出力に無い:"; printf '%s\n' "${got:-(無出力)}"; fails=$((fails + 1))
}

# --- 1. 対象なし: retro が無ければ黙る (毎セッションのノイズにしない) ---
mkissue "010-docs-something.md" "起票日: 2026-08-01"
check "対象なしで黙る" "" "$(report env)"

# --- 2. カテゴリはファイル名 position 2 で判定する (スラッグ側の retro を誤検出しない) ---
mkissue "011-docs-retro-format-notes.md" "起票日: 2026-08-01"
check "スラッグ側の retro を誤検出しない" "" "$(report env)"

# --- 3. done/ は対象外 (状態の正本はファイルの位置) ---
printf '# t\n' >"$repo/issues/done/012-retro-settled-2026-08-01.md"
check "done/ は数えない" "" "$(report env)"

# --- 4. 未決着 1 件を件数とパスつきで報告する ---
yesterday="$(date -v-1d +%F 2>/dev/null || date -d '-1 day' +%F)"
mkissue "013-retro-session-${yesterday}.md" "起票日: ${yesterday}"
out="$(report env)"
check "件数を出す" "未決着の retro issue: 1 件" "$out"
check "パスを出す" "issues/013-retro-session-${yesterday}\.md" "$out"
check "経過日数を出す" "1 日前" "$out"
check "切り出し先を示す" "_claude/rules/" "$out"

# --- 5. 日付が読めないものを黙って捨てない (「新しい retro」と区別できなくなる) ---
mkissue "014-retro-nodate.md" ""
check "日付不明として列挙する" "日付不明.*issues/014-retro-nodate\.md" "$(report env)"

# --- 6. pending/ の retro も取りこぼさない ([保留] とラベルする) ---
printf '# t\n\n起票日: 2026-08-01\n' >"$repo/issues/pending/015-retro-held-2026-08-01.md"
check "pending も拾う" "issues/pending/015-retro-held-2026-08-01\.md \[保留\]" "$(report env)"

# --- 6b. epic の直下と group 内 next/ は open として走査・集計する ---
mkdir -p "$repo/issues/epic/foo/next"
mkissue "epic/foo/016-retro-epic-${yesterday}.md" "起票日: ${yesterday}"
mkissue "epic/foo/next/017-retro-epic-next-${yesterday}.md" "起票日: ${yesterday}"
out="$(report env)"
check "epic 直下の retro を一覧に出す" "issues/epic/foo/016-retro-epic-${yesterday}\.md" "$out"
check "epic 直下の retro を件数に数える" "未決着の retro issue: 4 件" "$out"
check "epic 内 next/ の retro を一覧に出す" "issues/epic/foo/next/017-retro-epic-next-${yesterday}\.md" "$out"
check "epic 内 next/ の retro を件数に数える" "未決着の retro issue: 4 件" "$out"

# --- 7. 古い方が先に出る (溜まった retro が末尾に沈まない) ---
out="$(report env)"
older="$(printf '%s' "$out" | grep -n '015-retro-held' | cut -d: -f1)"
newer="$(printf '%s' "$out" | grep -n '013-retro-session' | cut -d: -f1)"
if [ -z "$older" ] || [ -z "$newer" ] || [ "$older" -ge "$newer" ]; then
  echo "NG: 古い retro を先に出す — 015 (古) が 013 (新) より後 (015=$older 013=$newer):"
  printf '%s\n' "$out"; fails=$((fails + 1))
fi

# --- 8. hook 配線の契約 (event 名と suppressOutput) を pin する ---
# ここが違うと Claude Code が注入を丸ごと無視する = 完全な沈黙になるが、additionalContext
# だけを見るテストでは検出できない (実測 2026-08-21 の敵対的レビュー指摘)
meta="$(raw env | jq -r '[.hookSpecificOutput.hookEventName, (.suppressOutput|tostring)] | join(",")' 2>/dev/null)"
check "SessionStart として emit する" "^SessionStart,true$" "$meta"

# --- 9. lib が読めなければ黙らずに省略した旨を出す ---
# (set -e が無いので source 失敗は黙って exit 0 になりやすい)
broken_dir="$WORK/nolib"
mkdir -p "$broken_dir"
cp "$HOOK" "$broken_dir/retro-open.sh"
out="$(printf '{"cwd":"%s"}' "$repo" | "$broken_dir/retro-open.sh" 2>/dev/null)"
check "lib 不在で黙らない" "点検を省略した" "$out"

# --- 10. 未来日付を「最古」として先頭に出さない ---
mkissue "016-retro-future-2099-01-01.md" ""
out="$(report env)"
check "未来日付をラベルする" "起票日が未来.*016-retro-future" "$out"
if [ "$(printf '%s' "$out" | sed -n '3p')" != "${out#*$'\n'}" ]; then :; fi
head_line="$(printf '%s' "$out" | sed -n '3p')"
grep -q '016-retro-future' <<<"$head_line" && {
  echo "NG: 未来日付が先頭に居座る: $head_line"; fails=$((fails + 1))
}
rm -f "$repo/issues/016-retro-future-2099-01-01.md"

# --- 11. 4 桁以上の issue 番号を黙って捨てない ---
mkissue "1000-retro-fourdigit-2026-08-01.md" ""
check "4 桁番号も拾う" "issues/1000-retro-fourdigit" "$(report env)"
rm -f "$repo/issues/1000-retro-fourdigit-2026-08-01.md"

# --- 12. 読めないファイルを「日付不明」に化けさせない (権限事故を見分ける) ---
mkissue "017-retro-noperm.md" ""
chmod 000 "$repo/issues/017-retro-noperm.md"
check "読み取り不可を区別する" "読み取り不可.*017-retro-noperm" "$(report env)"
chmod 644 "$repo/issues/017-retro-noperm.md"
rm -f "$repo/issues/017-retro-noperm.md"

# --- 13. date 実装差: BSD 形式 (-j -f) が使えない環境でも日数を出す ---
# GNU date だけの Linux を PATH スタブで再現する。フォールバックが消えると「日付不明」に落ちる
stub="$WORK/stub"; mkdir -p "$stub"
cat >"$stub/date" <<'STUB'
#!/bin/sh
# BSD 形式 (-j -f) を拒否し、GNU 形式 (-d) だけを受ける date。中身は host の date で実装する
case "$1" in
  -j) exit 1 ;;
  # exec で呼ぶと最初の /bin/date にプロセスが置き換わり、失敗しても || の後段が走らない
  # (host が GNU date の Linux では -j が非 0 で終わり、スタブが常に失敗していた)
  -d)
    # 🚨 時刻つきの形を先に試す。BSD の -f は寛容で、'%Y-%m-%d' に
    # "2026-09-01 00:00:00" を渡すと**日付だけ読んで時刻は今の時刻で埋める**ため、
    # 順序を逆にすると GNU date のふりをしながら壁時計を混ぜてしまう
    out=$(/bin/date -j -f '%Y-%m-%d %H:%M:%S' "$2" +%s 2>/dev/null) \
      || out=$(/bin/date -j -f '%Y-%m-%d' "$2" +%s 2>/dev/null) \
      || out=$(/bin/date -d "$2" +%s 2>/dev/null) || exit 1
    printf '%s\n' "$out"
    exit 0
    ;;
esac
exec /bin/date "$@"
STUB
chmod +x "$stub/date"
check "BSD date が無くても日数を出す" "[0-9]+ 日前 +issues/013-retro-session" \
  "$(report env "PATH=$stub:$PATH")"

# --- 14. grep が壊れていても「日付不明」と誤報しない (依存故障を見分ける) ---
# 起票日を本文にしか持たない issue で、grep を rc>1 で失敗させる
mkissue "018-retro-bodydate.md" "起票日: 2026-08-05"
cat >"$stub/grep" <<'STUB'
#!/bin/sh
exit 2
STUB
chmod +x "$stub/grep"
check "grep 故障時も列挙は続ける" "issues/018-retro-bodydate" "$(report env "PATH=$stub:$PATH")"
rm -f "$stub/grep"
check "grep が生きていれば本文の起票日を使う" "[0-9]+ 日前 +issues/018-retro-bodydate" "$(report env)"
rm -f "$repo/issues/018-retro-bodydate.md"

# --- 15. 並び順が locale に依存しない (日付不明の位置が反転しない) ---
for loc in C en_US.UTF-8 ja_JP.UTF-8; do
  o="$(report env "LC_ALL=$loc")"
  n_unknown="$(printf '%s' "$o" | grep -n '日付不明' | cut -d: -f1)"
  n_dated="$(printf '%s' "$o" | grep -n '013-retro-session' | cut -d: -f1)"
  if [ -z "$n_unknown" ] || [ -z "$n_dated" ] || [ "$n_dated" -ge "$n_unknown" ]; then
    echo "NG: locale=$loc で並び順が崩れた (dated=$n_dated unknown=$n_unknown):"; printf '%s\n' "$o"
    fails=$((fails + 1))
  fi
done

# --- 16. 経過日数は数値順 (文字列順だと 9 日前が 20 日前より上に来る) ---
d9="$(date -v-9d +%F 2>/dev/null || date -d '-9 days' +%F)"
d20="$(date -v-20d +%F 2>/dev/null || date -d '-20 days' +%F)"
mkissue "019-retro-nine-${d9}.md" ""
mkissue "020-retro-twenty-${d20}.md" ""
out="$(report env)"
n9="$(printf '%s' "$out" | grep -n '019-retro-nine' | cut -d: -f1)"
n20="$(printf '%s' "$out" | grep -n '020-retro-twenty' | cut -d: -f1)"
if [ -z "$n9" ] || [ -z "$n20" ] || [ "$n20" -ge "$n9" ]; then
  echo "NG: 数値順に並んでいない (20 日前=$n20 が 9 日前=$n9 より後):"; printf '%s\n' "$out"
  fails=$((fails + 1))
fi
rm -f "$repo/issues/019-retro-nine-${d9}.md" "$repo/issues/020-retro-twenty-${d20}.md"

# --- 17. 番号のないファイルは issue として扱わない (命名規約は NNN- が必須) ---
# 先頭トークンが数字でない = 命名規約外。category 位置だけ retro に見えるものを拾わない
mkissue "nonum-retro-slug.md" ""
check "番号なしは拾わない" "" "$(report env | grep 'nonum-retro-slug' || true)"
rm -f "$repo/issues/nonum-retro-slug.md"

# --- 18. jq が無い環境でも .cwd を捨てず、素のテキストで報告する ---
# 🚨 .cwd を捨てて $PWD に落ちると「別 repo の issues を報告する」= 黙って間違う
nojq="$WORK/nojq"; mkdir -p "$nojq"
# bash / env も入れる (hook の shebang が `env bash` なので PATH から消すと 127 になる)
for c in bash env dirname cat git date grep sed awk tr sort cut basename; do
  real="$(command -v "$c" 2>/dev/null)" && ln -sf "$real" "$nojq/$c"
done
other="$WORK/other"; mkdir -p "$other/issues"; git -C "$other" init -q .
printf '# t\n' >"$other/issues/030-retro-elsewhere-2026-08-01.md"
plain="$(cd "$other" && printf '{"cwd":"%s"}' "$repo" | env -i "PATH=$nojq" HOME="$HOME" "$HOOK" 2>/dev/null)"
if command -v jq >/dev/null 2>&1 && grep -q '"hookSpecificOutput"' <<<"$plain"; then
  echo "NG: jq を PATH から除いたつもりが JSON が出た (スタブ不備):"; printf '%s\n' "$plain"
  fails=$((fails + 1))
fi
check "jq 不在でも黙らない" "未決着の retro issue" "$plain"
check "jq 不在でも .cwd 側の repo を報告する" "issues/013-retro-session" "$plain"
check "jq 不在で PWD 側の repo を報告しない" "" "$(printf '%s' "$plain" | grep 'retro-elsewhere' || true)"

# --- 19. epic/ が無い repo でも未展開 glob をエラーにしない ---
noepic="$WORK/no-epic"
mkdir -p "$noepic/issues"
git -C "$noepic" init -q .
printf '# t\n\n起票日: 2026-08-01\n' >"$noepic/issues/091-retro-no-epic.md"
noepic_out="$(printf '{"cwd":"%s"}' "$noepic" | "$HOOK" 2>/dev/null)"
noepic_ctx="$(printf '%s' "$noepic_out" | jq -r '.hookSpecificOutput.additionalContext // ""')"
check "epic 無しでも正常に走査する" '091-retro-no-epic' "$noepic_ctx"

# --- 16. 壁時計が進んでも経過日数は暦日で数える (実測 2026-09-02 の flake) ---
# hook は today_epoch を先に取り、あとで各 issue の日付を解釈する。BSD の
# `date -j -f '%Y-%m-%d'` は時刻を 00:00:00 にせず**実行時点の時刻**を埋めるため、その 2 回の
# 呼び出しに 1 秒でも差があると引き算が 86400 を割り、「1 日前」が「0 日前」に落ちていた。
# 単体実行では通り抜け、負荷のかかった `make test` でだけ落ちる flake だったので、
# 「呼ばれるたびに進む時計」で決定論的に固定する。
clock="$WORK/stub-clock"; mkdir -p "$clock"
tick="$WORK/tick"
base_day="$(date +%F)"
# 正午を基準にする (境界をまたがせない: 深夜に走らせたとき進んだぶんで日付が変わると、
# この回帰テスト自身が壁時計依存になる)
base_epoch="$(date -j -f '%Y-%m-%d %H:%M:%S' "$base_day 12:00:00" +%s)"
yday="$(date -v-1d +%F 2>/dev/null || date -d '-1 day' +%F)"
cat >"$clock/date" <<STUB
#!/bin/sh
# 呼ばれるたびに 5 秒進む時計 + BSD date の「時刻を埋める」挙動の再現
n=\$(cat "$tick" 2>/dev/null || echo 0); n=\$((n + 1)); printf '%s' "\$n" > "$tick"
now=\$(( $base_epoch + n * 5 ))
if [ "\$1" = "-j" ] && [ "\$2" = "-f" ]; then
  case "\$3" in
    '%Y-%m-%d')
      # 本物の BSD date と同じく 00:00:00 にせず「今の時刻」を埋める (これが罠の本体)
      exec /bin/date -j -f '%Y-%m-%d %H:%M:%S' "\$4 \$(/bin/date -r \$now +%H:%M:%S)" +%s ;;
    *) exec /bin/date -j -f "\$3" "\$4" +%s ;;
  esac
fi
case "\$*" in
  '+%s') printf '%s\n' "\$now" ;;
  '+%F') /bin/date -r "\$now" +%F ;;
  *) exec /bin/date "\$@" ;;
esac
STUB
chmod +x "$clock/date"
: > "$tick"
mkissue "019-retro-clockdrift.md" "起票日: ${yday}"
check "壁時計が進んでも暦日で数える" "1 日前 +issues/019-retro-clockdrift" \
  "$(report env "PATH=$clock:$PATH")"

if [ "$fails" -gt 0 ]; then
  echo "FAIL: retro-open.sh ($fails 件)"; exit 1
fi
echo "OK: retro-open.sh (30 観点)"
