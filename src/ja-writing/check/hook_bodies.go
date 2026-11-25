package check

// 本家 yomiyasu_hook.py の、文から原稿と投稿の本文を取り出す所: _read_text・_paths・_bash_command・_shell_segments・_bodies。
// シェルの文は実行しない (区切りを数えて、引数は shlex で読むだけ)。

import (
	"io"
	"slices"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyos"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pyshlex"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// hookReadText は _read_text (通常のファイルを 2 MiB まで読み、UTF-8 (BOM 可) で読めることを確かめる)。
// 誤りは ValueError("text_file")・ValueError("text_size")・UnicodeDecodeError・ファイルの誤り (呼び出し側はどれも同じに扱う)。
func hookReadText(path string) ([]byte, error) {
	if !hookIsFile(path) {
		return nil, valueError("text_file")
	}
	file, err := pyos.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, hookMaxText+1))
	if err != nil {
		return nil, err
	}
	if len(data) > hookMaxText {
		return nil, valueError("text_size")
	}
	if _, err := TextSHA256Of(data); err != nil {
		return nil, err
	}
	return data, nil
}

// hookTextPattern は _paths の、拡張子で原稿の名前を拾う正規表現。
var hookTextPattern = pyre.NewLazy(`[^\s"'「」（）()]+\.(?:md|markdown|txt|text|rst|adoc|html?)`, 0)

// paths は _paths (依頼の文で最初に名指した、在る文書のファイル 1 つ。後の名前 (最終稿・メモ) は登録しない)。
func (h *hookRun) paths(args string) []string {
	words, err := pyshlex.Split(args)
	if err != nil {
		words = splitWhitespace(args)
	}
	type candidate struct {
		position int
		word     string
	}
	var candidates []candidate
	runes := pyunicode.Runes(args)
	for _, word := range words {
		position := runeIndex(runes, pyunicode.Runes(word))
		if position < 0 {
			position = len(runes)
		}
		candidates = append(candidates, candidate{position, word})
	}
	matches, err := hookTextPattern.Get().Finditer(pyre.NewText(args), 0, pyre.NoEnd)
	if err != nil {
		panic(failure{err})
	}
	for _, m := range matches {
		s, _ := m.Group(0)
		candidates = append(candidates, candidate{m.Start(0), s})
	}
	slices.SortStableFunc(candidates, func(a, b candidate) int { return a.position - b.position })
	seen := map[string]bool{}
	for _, c := range candidates {
		word := strings.TrimPrefix(c.word, "@")
		if word == "" {
			continue
		}
		path := hookAbsolute(visibility.PathJoin(h.cwd, word))
		key := hookPlace(path)
		if key == nil || seen[*key] {
			continue
		}
		seen[*key] = true
		if _, err := hookReadText(path); err != nil {
			continue
		}
		return []string{path}
	}
	return nil
}

// runeIndex は str.find (コードポイントの添字。無ければ -1)。
func runeIndex(s, sub []rune) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if slices.Equal(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

// bashCommand は _bash_command (tool_input.command が文字列か、文字列の並び)。
func bashCommand(value pyjson.Value) string {
	switch v := value.(type) {
	case string:
		return v
	case []pyjson.Value:
		words := make([]string, 0, len(v))
		for _, w := range v {
			s, ok := w.(string)
			if !ok {
				return ""
			}
			words = append(words, s)
		}
		if len(words) >= 3 && (words[1] == "-c" || words[1] == "-lc") {
			return words[2]
		}
		return pyshlex.Join(words)
	}
	return ""
}

var hookHeredocPattern = pyre.NewLazy(`<<(-?)\s*(["']?)([A-Za-z_][A-Za-z0-9_]*)\2`, 0)

// shellSegments は _shell_segments (引用符・置換の外の ; | && || 改行で切る。ヒアドキュメントの本文はその命令に付けたままにする)。
func shellSegments(command string) []string {
	cmd := pyunicode.Runes(command)
	n := len(cmd)
	startsWith := func(prefix string, at int) bool {
		p := []rune(prefix)
		return at+len(p) <= n && slices.Equal(cmd[at:at+len(p)], p)
	}
	type pendingDoc struct {
		delimiter string
		tabs      bool
	}
	type substitution struct {
		depth int
		quote rune
	}
	var result []string
	var pending []pendingDoc
	var substitutions []substitution
	start, index := 0, 0
	var quote rune // 0 は引用符の外
	escaped := false
	text := pyre.NewText(command)
	for index < n {
		char := cmd[index]
		if escaped {
			escaped = false
			index++
			continue
		}
		if char == '\\' && quote != '\'' {
			escaped = true
			index++
			continue
		}
		if quote == '\'' || quote == '`' {
			if char == quote {
				quote = 0
			}
			index++
			continue
		}
		if startsWith("$(", index) {
			substitutions = append(substitutions, substitution{1, quote})
			quote = 0
			index += 2
			continue
		}
		if quote == '"' {
			if char == '"' {
				quote = 0
			}
			index++
			continue
		}
		if char == '\'' || char == '"' || char == '`' {
			quote = char
			index++
			continue
		}
		if len(substitutions) > 0 {
			last := &substitutions[len(substitutions)-1]
			switch char {
			case '(':
				last.depth++
			case ')':
				last.depth--
				if last.depth == 0 {
					quote = last.quote
					substitutions = substitutions[:len(substitutions)-1]
				}
			}
			index++
			continue
		}
		if startsWith("<<", index) {
			m, err := hookHeredocPattern.Get().Match(text, index, pyre.NoEnd)
			if err != nil {
				panic(failure{err})
			}
			if m != nil {
				dash, _ := m.Group(1)
				name, _ := m.Group(3)
				pending = append(pending, pendingDoc{name, dash != ""})
				index = m.End(0)
				continue
			}
		}
		if char == '\n' && len(pending) > 0 {
			end := index + 1
			for _, doc := range pending {
				pattern := `(?m)^`
				if doc.tabs {
					pattern += `\t*`
				}
				pattern += pyre.Escape(doc.delimiter) + `\r?$`
				re, err := pyre.Compile(pattern, 0)
				if err != nil {
					panic(failure{err})
				}
				rest := pyre.NewText(pyunicode.FromRunes(cmd[end:]))
				m, err := re.Search(rest, 0, pyre.NoEnd)
				if err != nil {
					panic(failure{err})
				}
				if m == nil {
					end = n
					break
				}
				end += m.End(0)
				if end < n && cmd[end] == '\n' {
					end++
				}
			}
			result = append(result, pyunicode.FromRunes(cmd[start:end]))
			start, index = end, end
			pending = nil
			continue
		}
		if char == ';' || char == '|' || char == '\n' || startsWith("&&", index) {
			if len(pending) == 0 {
				result = append(result, pyunicode.FromRunes(cmd[start:index]))
				if startsWith("&&", index) || startsWith("||", index) {
					index += 2
				} else {
					index++
				}
				start = index
				continue
			}
		}
		index++
	}
	if start < n {
		result = append(result, pyunicode.FromRunes(cmd[start:]))
	}
	return result
}

// hookBody は _bodies の 1 件 (本文のバイト列か nil、本文を読んだファイルか nil、元のコマンド)。
type hookBody struct {
	body    []byte
	path    *string
	command string
}

var bodyKeys = []string{"body", "text", "content", "description", "message", "notes"}

// hookBodies は _bodies (投稿のツールの入力から、投稿の本文を取り出す。シェルの文は実行しない)。
func hookBodies(data *pyjson.Object, cwd string) []hookBody {
	name, nameOK := data.Field("tool_name").(string)
	tool, toolOK := data.Field("tool_input").(*pyjson.Object)
	if !nameOK || !toolOK {
		return nil
	}
	var bodies []hookBody
	if strings.HasPrefix(name, "mcp__") {
		var scan func(value *pyjson.Object, depth int)
		scan = func(value *pyjson.Object, depth int) {
			for _, key := range value.Keys() {
				item := value.Field(key)
				if text, ok := item.(string); ok && slices.Contains(bodyKeys, key) {
					bodies = append(bodies, hookBody{body: append([]byte{}, text...), command: ""})
				} else if inner, ok := item.(*pyjson.Object); ok && depth < 2 {
					scan(inner, depth+1)
				}
			}
		}
		scan(tool, 1)
		return bodies
	}
	if name != "Bash" {
		return nil
	}
	command := bashCommand(tool.Field("command"))
	for _, context := range shellSegments(command) {
		unsafe := strings.Contains(context, "$(") || strings.Contains(context, "`") || strings.Contains(context, "<<")
		words, err := pyshlex.PunctuationWords(context)
		if err != nil {
			words = splitWhitespace(context)
			unsafe = true
		}
		found := -1
		for n, word := range words {
			base := strings.ReplaceAll(word, "\\", "/")
			if i := strings.LastIndex(base, "/"); i >= 0 {
				base = base[i+1:]
			}
			if base == "gh" || base == "gh.exe" {
				found = n
				break
			}
		}
		if found < 0 {
			continue
		}
		words = words[found+1:]
		api := len(words) > 0 && words[0] == "api"
		public := len(words) >= 2 && ((words[0] == "pr" && slices.Contains([]string{"create", "edit", "comment", "review"}, words[1])) ||
			(words[0] == "issue" && slices.Contains([]string{"create", "edit", "comment"}, words[1])) ||
			(words[0] == "release" && slices.Contains([]string{"create", "edit"}, words[1])))
		if !api && !public {
			continue
		}
		if unsafe {
			bodies = append(bodies, hookBody{command: context})
			continue
		}
		flags := map[string]bool{"--body": false, "-b": false, "--body-file": true, "-F": true}
		if words[0] == "release" {
			flags = map[string]bool{"--notes": false, "-n": false, "--notes-file": true, "-F": true}
		}
		if api {
			flags = map[string]bool{"-f": false, "--raw-field": false, "-F": true, "--field": true, "--input": true}
		}
		index := 2
		if api {
			index = 1
		}
		for index < len(words) {
			word := words[index]
			flag, value, equal := strings.Cut(word, "=")
			fileArg, known := flags[flag]
			if !known {
				index++
				continue
			}
			if !equal {
				index++
				value = ""
				if index < len(words) {
					value = words[index]
				}
			}
			if api {
				if flag == "--input" {
					bodies = append(bodies, hookBody{command: context})
					index++
					continue
				}
				key, field, sep := strings.Cut(value, "=")
				if !slices.Contains([]string{"body", "text", "notes"}, key) || !sep {
					index++
					continue
				}
				value = field
				fileArg = fileArg && strings.HasPrefix(value, "@")
				if fileArg {
					value = value[1:]
				}
			}
			if fileArg {
				var path *string
				var raw []byte
				if value != "" && value != "-" {
					p := hookAbsolute(visibility.PathJoin(cwd, value))
					path = &p
					if data, err := hookReadText(p); err == nil {
						raw = data
					}
				}
				bodies = append(bodies, hookBody{body: raw, path: path, command: context})
			} else {
				bodies = append(bodies, hookBody{body: append([]byte{}, value...), command: context})
			}
			index++
		}
	}
	return bodies
}
