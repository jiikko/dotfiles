#!/usr/bin/env bash

# PreToolUse(Bash / Grep): Swiftのsymbol検索と、Goを明示したsymbol検索に対応するdupscanコマンドを案内する。
#
# なぜ: Swift の 4 repo の直近 7 日の 27 セッションで、Swift のパスを対象にした grep は 2,882 回、dupscan は 13 回だった (921 の実測)。
# skill dupscan の 2 節に「grep の前に」と書いてあっても使われない。grep はコメント・文字列・呼び出し側の行が混ざって返る量が多い。
#
# 🚨 **止めない (additionalContext だけ)**。grep は文字列・コメント・設定ファイルを探すのに要る。permissionDecision を返さない。
# 🚨 **出しすぎない**: 同session/言語ではNUDGE_INTERVAL秒に1回。状態は$TMPDIRに置く (OSが片付ける)。
#
# 判定 (粗い。偽陽性は 1 行の案内で済み、偽陰性は今までどおり grep が走るだけ):
#   - Bash: コマンドに grep / rg の語が無ければ即座に終わる (毎回の Bash の待ちを増やさない)。あれば heredoc の本文を落とし、
#     python3 の shlex で `;` `&` `|` で分けた segment ごとに grep / rg / git grep のパターン (-e の値か、最初のオプションでない引数) を取る
#   - Grep: tool_input.pattern
#   - パターンがシンボルらしい: 先頭の宣言の語 (func / struct / class / enum / protocol / extension / actor / init / var / let / case / typealias)・
#     `\b` `\<` `\>`・末尾の `(` を外した残りが識別子 (`A.b` も可) で、次のどれか:
#       宣言の語が付いている / PascalCase か camelCase (大文字を含み、全部が大文字ではない) / 検索対象の言語を明示 (Swift、または対応するGo selector)
#   - repo: cwdのgitが案内対象の.swift/.goを追跡する。Goは明示selector・file/glob指定だけ。dirの中身から言語を推測しない。
#   - パイプの後ろの grep (`make test | grep FooBar`) は出力を絞っているだけなので案内しない
#
# 検出しない形 (silent):
#   - 全部が小文字の語で言語を明示していない grep (`grep -n refresh config.yml`)・全部が大文字の語 (`TODO`)・正規表現 (`foo.*bar`)
#   - grep の語が変数・エイリアス・`sh -c "..."` の中にあるもの / `-C` や cd・`$W/…` で別の repo を指したもの (cwd の repo で判定する)
#   - パイプの後ろの grep (stdin を絞る形)・`A|B` の alternation のパターン
#   - `-i` (大文字小文字を無視する) の grep (def / refs は完全一致なので、案内しても引けない名前になりうる)
#   - 対象が対応するsource以外のファイルだけのgrep (`grep -c Error build.log`・`grep -n Foo .gitignore`)
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
# 言語と名前を返す。対象dirの内容を調べて言語を推測しない。
match=$(HOOK_TOOL="$tool" HOOK_CMD="$cmd" HOOK_INPUT="$input" HOOK_CWD="$cwd" python3 -I - <<'PY' 2>/dev/null
import json, os, re, shlex

DECL = r'(?:func|struct|class|enum|protocol|extension|actor|init|var|let|case|typealias)'
IDENT = r'[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?'

def symbol(pattern, swift_target, declarations=DECL):
    p = pattern.strip()
    p = re.sub(r'\\[b<>]', '', p)
    m = re.fullmatch(r'(?:(' + declarations + r')\s+)?(' + IDENT + r')\s*(?:\\?\()?', p)
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

def glob_languages(value):
    if value.startswith('!'):
        return set()
    return {lang for lang in ('go', 'swift') if re.search(r'\.' + lang + r'($|[?*{}\[\]])', value)}

def selector_languages(rest):
    # Positive selectors only. Patterns, excludes, and arguments after -- are not language hints.
    langs = set()
    insensitive = False
    k = 0
    while k < len(rest):
        w = rest[k]
        if w == '--':
            break
        kind, value = None, ''
        if w in ('-t', '--type', '-g', '--glob', '--iglob', '--include'):
            kind = 'type' if w in ('-t', '--type') else 'glob'
            value = rest[k + 1] if k + 1 < len(rest) else ''
            k += 1
        elif w.startswith('--type='):
            kind, value = 'type', w.split('=', 1)[1]
        elif w.startswith(('--glob=', '--iglob=', '--include=')):
            kind, value = 'glob', w.split('=', 1)[1]
        elif re.fullmatch(r'-[nHhlLqvcowsFxUu0i]*t(?:=?(?:go|swift))?', w):
            bundled = re.fullmatch(r'-([nHhlLqvcowsFxUu0i]*)t=?(go|swift)?', w)
            kind = 'type'
            value = bundled.group(2) or (rest[k + 1] if k + 1 < len(rest) else '')
            insensitive = insensitive or 'i' in bundled.group(1)
            if bundled.group(2) is None:
                k += 1
        elif w.startswith('-g') and not w.startswith('--'):
            kind, value = 'glob', w[2:]
            if value.startswith('='):
                value = value[1:]
        elif w in VALUE_OPTIONS or w in ('-e', '--regexp'):
            k += 1
        if kind == 'type' and value in ('go', 'swift'):
            langs.add(value)
        elif kind == 'glob':
            langs.update(glob_languages(value))
        k += 1
    return langs, insensitive

def named_languages(paths):
    cwd = os.environ.get('HOOK_CWD', '')
    return {lang for p in paths for lang in ('go', 'swift')
            if p.endswith('.' + lang) and os.path.isfile(p if os.path.isabs(p) else os.path.join(cwd, p))}

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
    # Swift を対象にしているか: `.swift` の拡張子 (glob を含む) か `-t swift` / `-tswift` / `--type=swift` (`.swiftlint.yml` には当てない)
    swift = any(re.search(r'\.swift($|[^A-Za-z0-9_])', w) or w == 'swift' or re.fullmatch(r'(-t|--type=)swift', w) for w in rest)
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
        elif w in ('-i', '--ignore-case') or (re.fullmatch(r'-[A-Za-z]*i[A-Za-z]*', w) and not re.search(r'[tgefmABCMj]', w)):
            return None  # 大文字小文字を無視する検索 (値をくっつけた -tswift・-gFoo は -i と読まない)
        elif re.fullmatch(r'-[A-Za-z]*[ABCmMjtgf]', w):
            k += 2  # 束ねた短いオプションの最後が値を取る (`-nA 3`)
            continue
        elif not w.startswith('-'):
            if pattern is None:
                pattern = w
            else:
                paths.append(w)
        k += 1
    if pattern is None:
        return None
    langs, insensitive = selector_languages(rest)
    if 'go' in langs:
        if insensitive or 'swift' in langs or not_source_files(paths, '.go'):
            return None
        name = symbol(pattern, True, r'(?:func|type|var|const)')
        return 'go\t' + name if name else None
    path_langs = named_languages(paths)
    for path in paths:
        if not path.startswith(('!', ':')) and any(c in path for c in '*?['):
            path_langs.update(glob_languages(path))
    if not langs and path_langs == {'go'}:
        name = symbol(pattern, True, r'(?:func|type|var|const)')
        return 'go\t' + name if name else None
    if not_source_files(paths):
        return None
    name = symbol(pattern, swift)
    return 'swift\t' + name if name else None

def not_source_files(paths, extension=".swift"):
    # 全対象が指定言語以外の実在fileなら、設定やログ検索として案内を控える。
    # (`..`・`MyPkg.swiftpm` のような dir をファイルと読まない)。在ることを確かめられない対象が 1 つでもあれば判定しない
    cwd = os.environ.get('HOOK_CWD', '')
    if not paths or not cwd:
        return False
    for p in paths:
        full = p if os.path.isabs(p) else os.path.join(cwd, p)
        if not os.path.isfile(full) or p.endswith(extension):
            return False
    return True

tool = os.environ['HOOK_TOOL']
if tool == 'Grep':
    ti = json.loads(os.environ['HOOK_INPUT']).get('tool_input') or {}
    glob, typ, path = (str(ti.get(k) or '') for k in ('glob', 'type', 'path'))
    ignored_case = ti.get('-i') is True
    langs = glob_languages(glob) | ({typ} if typ in ('go', 'swift') else set())
    go_target = langs == {'go'} or (not glob and not typ and named_languages([path]) == {'go'})
    if go_target:
        ignored = ignored_case or (glob and 'go' not in glob_languages(glob)) or (typ and typ != 'go') or (path and not_source_files([path], '.go'))
        name = '' if ignored else symbol(str(ti.get('pattern') or ''), True, r'(?:func|type|var|const)')
        print('go\t' + name if name else '')
    elif 'go' in langs:
        print('')
    else:
        ignored = ignored_case or (glob and 'swift' not in glob) or (typ and typ != 'swift') or (path and not_source_files([path]))
        name = '' if ignored else symbol(str(ti.get('pattern') or ''), 'swift' in glob or typ == 'swift' or path.endswith('.swift'))
        print('swift\t' + name if name else '')
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
[ -n "$match" ] || exit 0
language=${match%%$'\t'*}
name=${match#*$'\t'}
case "$language" in go|swift) ;; *) exit 0 ;; esac
# headの後のSIGPIPEを失敗扱いにしない。追跡sourceのある言語だけ案内する。
[ -n "$(git -C "$root" ls-files -- "*.$language" 2>/dev/null | head -n 1)" ] || exit 0

# Goの通知で既存Swift通知の間隔を消費しない。session IDの末尾とも衝突しないnamespaceにする。
stamp_prefix=claude-dupscan-nudge
[ "$language" != go ] || stamp_prefix=claude-dupscan-go-nudge
stamp="${TMPDIR:-/tmp}/${stamp_prefix}-${session//[^A-Za-z0-9_-]/_}"
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
if [ "$language" = go ]; then
  companion="$(dirname "$(readlink -f "$0")")/../../bin/dupscan"
  capabilities=$("$companion" -no-build capabilities -format json 2>/dev/null) || capabilities=''
  if ! printf '%s' "$capabilities" | jq -e '
    .tool == "dupscan" and .schema_version == 1 and
    ([.commands[] | select(.name == "index" or .name == "callers") | select(.languages | index("go")) | .name] | unique | length == 2)
  ' >/dev/null 2>&1; then
    msg="dupscan の Go 対応状況を手動で確認できる: \`dupscan capabilities\`。通知では build やソース解析を起こさない。未build・古い版なら通常起動で準備する (skill dupscan)。"
  else
  msg="dupscan で Go の候補・呼び出し元を引ける: \`dupscan index -name $short\` (定義の候補) / \`dupscan callers '$short'\` (名前による近似)。def / refs / context は Go 非対応。型・同名の別receiverは解決しない (skill dupscan の 2 節)"
  fi
else
msg="dupscan で Swift の定義・参照を引ける: \`dupscan def $name\` (定義を本体つき・型はメンバーの一覧) / \`dupscan refs $short\` (参照の一覧。[型の位置] / [式の位置] と囲む関数、宣言名・コメント・文字列を除く) / \`dupscan callers '$short'\` (呼び出し元)。grep は文字列・コメント・設定ファイルを探すときに使う (skill dupscan の 2 節)"
fi
if [ "${#msg}" -gt 1024 ] || [ "$(printf '%s' "$msg" | wc -c)" -gt 1024 ]; then
  msg='dupscan capabilities で対応するコマンドを確認できる。長い名前は手動で渡す。文字列・コメント・設定にはgrepを使う。'
fi
jq -n --arg c "$msg" '{hookSpecificOutput: {hookEventName: "PreToolUse", additionalContext: $c}}'
