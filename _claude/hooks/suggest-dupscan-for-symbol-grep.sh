#!/usr/bin/env bash

# PreToolUse(Bash / Grep) フック: Swift の repo (git が .swift を追跡している) で、Swift の定義・呼び出し元を探す形の
# grep / rg / git grep を打とうとしたら、dupscan の def / refs / callers を 1 行案内する (issue 921)。
#
# なぜ: Swift の 4 repo の直近 7 日の 27 セッションで、Swift のパスを対象にした grep は 2,882 回、dupscan は 13 回だった (921 の実測)。
# skill dupscan の 2 節に「grep の前に」と書いてあっても使われない。grep はコメント・文字列・呼び出し側の行が混ざって返る量が多い。
#
# 🚨 **止めない (additionalContext だけ)**。grep は文字列・コメント・設定ファイルを探すのに要る。permissionDecision を返さない。
# 🚨 **出しすぎない**: 同じ session では NUDGE_INTERVAL 秒に 1 回まで。状態は $TMPDIR の session ごとの 1 ファイル (OS が片付ける)。
#
# 判定 (粗い。偽陽性は 1 行の案内で済み、偽陰性は今までどおり grep が走るだけ):
#   - Bash: コマンドに grep / rg の語が無ければ即座に終わる (毎回の Bash の待ちを増やさない)。あれば heredoc の本文を落とし、
#     python3 の shlex で `;` `&` `|` で分けた segment ごとに grep / rg / git grep のパターン (-e の値か、最初のオプションでない引数) を取る
#   - Grep: tool_input.pattern
#   - パターンがシンボルらしい: 先頭の宣言の語 (func / struct / class / enum / protocol / extension / actor / init / var / let / case / typealias)・
#     `\b` `\<` `\>`・末尾の `(` を外した残りが識別子 (`A.b` も可) で、次のどれか:
#       宣言の語が付いている / PascalCase か camelCase (大文字を含み、全部が大文字ではない) / 検索の対象が Swift (`.swift`・`-t swift`・glob・type)
#   - repo: cwd (Bash の `-C` は見ない) の git が .swift を 1 つ以上追跡している (dupscan は .dupscan.json が無くても動く)
#   - パイプの後ろの grep (`make test | grep FooBar`) は出力を絞っているだけなので案内しない
#
# 検出しない形 (silent):
#   - 全部が小文字の語で Swift を対象にしていない grep (`grep -n refresh config.yml`)・全部が大文字の語 (`TODO`)・正規表現 (`foo.*bar`)
#   - grep の語が変数・エイリアス・`sh -c "..."` の中にあるもの / `-C` や cd・`$W/…` で別の repo を指したもの (cwd の repo で判定する)
#   - パイプの後ろの grep (stdin を絞る形)・`A|B` の alternation のパターン
#   - `-i` (大文字小文字を無視する) の grep (def / refs は完全一致なので、案内しても引けない名前になりうる)
#   - 対象が Swift 以外のファイルだけの grep (`grep -c Error build.log`・`grep -n Foo .gitignore`)
#   - grep / rg がコマンドの先頭の語でないもの (`xargs grep …`・`find … -exec grep …`・`time grep …`)
#   - python3 / jq / git が無い環境 (fail-open)
#   - 1 つ目のパターンだけを見る (`-e a -e b` の b は見ない)

set -uo pipefail

NUDGE_INTERVAL=${DUPSCAN_NUDGE_INTERVAL:-900}

# shellcheck source=_claude/hooks/lib/hook_io.sh
. "$(dirname "$0")/lib/hook_io.sh" 2>/dev/null || exit 0
command -v jq >/dev/null 2>&1 || exit 0
command -v python3 >/dev/null 2>&1 || exit 0

input=$(cat)
tool=$(printf '%s' "$input" | jq -r '.tool_name // ""' 2>/dev/null) || exit 0
cwd=$(printf '%s' "$input" | jq -r '.cwd // ""' 2>/dev/null) || exit 0
session=$(printf '%s' "$input" | jq -r '.session_id // "unknown"' 2>/dev/null) || exit 0

case "$tool" in
  Bash)
    cmd=$(printf '%s' "$input" | jq -r '.tool_input.command // ""' 2>/dev/null) || exit 0
    [[ "$cmd" =~ (^|[^A-Za-z0-9_-])(grep|rg)([^A-Za-z0-9_-]|$) ]] || exit 0
    cmd=$(hook_strip_heredocs "$cmd")
    ;;
  Grep)
    cmd=""
    ;;
  *) exit 0 ;;
esac

[ -n "$cwd" ] && [ -d "$cwd" ] || exit 0
root=$(git -C "$cwd" rev-parse --show-toplevel 2>/dev/null) || exit 0
# 🚨 パイプの終わりの rc で判定しない: .swift が多い repo (obaket は 100KB 超) では head の後に git が SIGPIPE で落ち、pipefail で
# 失敗扱いになって黙る。コマンド置換の出力の有無で見る (置換の rc は使わない)
[ -n "$(git -C "$root" ls-files -- '*.swift' 2>/dev/null | head -n 1)" ] || exit 0

# 名前を 1 つ返す (案内しないなら空)。Bash は cmd、Grep は tool_input を読む
name=$(HOOK_TOOL="$tool" HOOK_CMD="$cmd" HOOK_INPUT="$input" python3 -I - <<'PY' 2>/dev/null
import json, os, re, shlex

DECL = r'(?:func|struct|class|enum|protocol|extension|actor|init|var|let|case|typealias)'
IDENT = r'[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?'

def symbol(pattern, swift_target):
    p = pattern.strip()
    p = re.sub(r'\\[b<>]', '', p)
    m = re.fullmatch(r'(?:(' + DECL + r')\s+)?(' + IDENT + r')\s*(?:\\?\()?', p)
    if not m:
        return ''
    decl, name = m.group(1), m.group(2)
    base = name.split('.')[-1]
    if len(base) < 3 or base.isupper():
        return ''
    camel = any(c.isupper() for c in name)  # Type.member の Type の大文字も数える
    if decl or camel or swift_target:
        return name
    return ''

# 値を取るオプション (次の語をパターンやパスと読まない)。grep と rg で意味が違う短いオプション (-r: grep は再帰 / rg は置換、-E・-T など)
# は、grep で値を取らないので入れない (入れると `grep -r Foo .` の Foo を読み飛ばす)。rg のそれらは長い形だけ数える
VALUE_OPTIONS = {
    '-t', '--type', '--type-not', '-g', '--glob', '--iglob', '--include', '--exclude', '--exclude-dir',
    '-A', '-B', '-C', '-m', '--max-count', '-f', '--file', '--sort', '--sortr', '-M', '--max-columns',
    '-j', '--threads', '--encoding', '--replace', '--context-separator', '--path-separator', '--colors',
}

def from_segment(words):
    # grep / rg / git grep の後ろから、パターンと Swift を対象にしているかを取る
    i = 0
    while i < len(words) and re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*=.*', words[i]):
        i += 1
    if i < len(words) and words[i] == 'git':
        j = i + 1
        while j < len(words) and words[j].startswith('-'):
            j += 2 if words[j] in ('-C', '-c') else 1
        if j < len(words) and words[j] == 'grep':
            i = j
        else:
            return None
    if i >= len(words) or os.path.basename(words[i]) not in ('grep', 'rg', 'egrep'):
        return None
    rest = words[i + 1:]
    # Swift を対象にしているか: `.swift` の拡張子 (glob を含む) か `-t swift` (`.swiftlint.yml` には当てない)
    swift = any(re.search(r'\.swift($|[^A-Za-z0-9_])', w) or w == 'swift' for w in rest)
    pattern = None
    paths = []
    k = 0
    while k < len(rest):
        w = rest[k]
        if w in ('-e', '--regexp'):
            if k + 1 < len(rest) and pattern is None:
                pattern = rest[k + 1]
            k += 2
            continue
        if w.startswith('--regexp='):
            pattern = pattern or w.split('=', 1)[1]
        elif w in VALUE_OPTIONS:
            k += 2
            continue
        elif w in ('-i', '--ignore-case') or re.fullmatch(r'-[A-Za-z]*i[A-Za-z]*', w):
            return None  # 大文字小文字を無視する検索
        elif not w.startswith('-'):
            if pattern is None:
                pattern = w
            else:
                paths.append(w)
        k += 1
    if pattern is None:
        return None
    # 対象のファイルを名指ししていて、どれも Swift でない (拡張子つき・dot file) なら、設定やログを探している
    named = [p for p in paths if re.search(r'(^|/)\.[^/]+$|\.[A-Za-z0-9]+$', p)]
    if named and len(named) == len(paths) and not any(p.endswith('.swift') for p in named):
        return None
    return symbol(pattern, swift)

tool = os.environ['HOOK_TOOL']
if tool == 'Grep':
    ti = json.loads(os.environ['HOOK_INPUT']).get('tool_input') or {}
    target = ' '.join(str(ti.get(k) or '') for k in ('glob', 'type', 'path'))
    print(symbol(str(ti.get('pattern') or ''), 'swift' in target))
else:
    cmd = os.environ['HOOK_CMD']
    # 区切りを残して分け、パイプ (`|`) の直後の segment は stdin を絞る grep なので見ない
    parts = re.split(r'(\|\||&&|;|\n|\|)', cmd)
    prev = None
    for seg in parts:
        if seg in ('||', '&&', ';', '\n', '|'):
            prev = seg
            continue
        after_pipe = prev == '|'
        prev = None
        if after_pipe:
            continue
        try:
            words = shlex.split(seg, comments=True)
        except ValueError:
            continue
        got = from_segment(words)
        if got:
            print(got)
            break
PY
) || exit 0
[ -n "$name" ] || exit 0

stamp="${TMPDIR:-/tmp}/claude-dupscan-nudge-${session//[^A-Za-z0-9_-]/_}"
now=$(date +%s)
if [ -f "$stamp" ]; then
  last=$(cat "$stamp" 2>/dev/null || echo 0)
  case "$last" in ''|*[!0123456789]*) last=0 ;; esac
  [ "${#last}" -le 12 ] || last=0
  [ $((now - last)) -ge "$NUDGE_INTERVAL" ] || exit 0
fi
# 書けない TMPDIR でも stderr に漏らさない (`> f 2>/dev/null` の順だと、リダイレクトの失敗は stderr に出る)
{ printf '%s\n' "$now" > "$stamp"; } 2>/dev/null || true

short=${name##*.}
msg="dupscan で Swift の定義・参照を引ける: \`dupscan def $name\` (定義を本体つき・型はメンバーの一覧) / \`dupscan refs $short\` (参照の一覧。[型の位置] / [式の位置] と囲む関数、宣言名・コメント・文字列を除く) / \`dupscan callers '$short'\` (呼び出し元)。grep は文字列・コメント・設定ファイルを探すときに使う (skill dupscan の 2 節)"
jq -n --arg c "$msg" '{hookSpecificOutput: {hookEventName: "PreToolUse", additionalContext: $c}}'
