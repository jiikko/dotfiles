package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

// metadata は meta.json の中身。キーの並びは Python 版と同じ。errors / usage / thread_id と commands の各値は、
// codex が出した JSON をそのまま出し直す (json.RawMessage。数の桁・キーの順を保つ)。
type metadata struct {
	ThreadID        json.RawMessage   `json:"thread_id"`
	TurnCompleted   bool              `json:"turn_completed"`
	Errors          []json.RawMessage `json:"errors"`
	ParseErrors     []parseError      `json:"parse_errors"`
	Usage           []json.RawMessage `json:"usage"`
	Commands        []command         `json:"commands"`
	ResponseSource  *string           `json:"response_source"`
	ResponsePresent bool              `json:"response_present"`
	Complete        bool              `json:"complete"`
}

type parseError struct {
	Line  int    `json:"line"`
	Error string `json:"error"`
}

type command struct {
	ID       json.RawMessage `json:"id"`
	Command  json.RawMessage `json:"command"`
	ExitCode json.RawMessage `json:"exit_code"`
	Status   json.RawMessage `json:"status"`
}

// readFailure は、events か応答を読めなかったときの meta.json (Python 版の read_error の経路)。
type readFailure struct {
	Complete  bool   `json:"complete"`
	ReadError string `json:"read_error"`
}

var emptyObject = json.RawMessage("{}")

// inspectEvents は events.jsonl を読んで判定し、meta.json の中身と応答の本文を返す。読めなければ error。
func inspectEvents(eventsPath, responsePath string) (*metadata, string, error) {
	text, err := readText(eventsPath)
	if err != nil {
		return nil, "", err
	}
	md := &metadata{Errors: []json.RawMessage{}, ParseErrors: []parseError{}, Usage: []json.RawMessage{}, Commands: []command{}}
	var messages []string
	for i, line := range pySplitlines(text) {
		lineNo := i + 1
		if pyStrip(line) == "" {
			continue
		}
		event, perr := decodeObject(line, "event must be an object")
		if perr != "" {
			md.ParseErrors = append(md.ParseErrors, parseError{lineNo, perr})
			continue
		}
		switch kind, _ := stringField(event, "type"); kind {
		case "thread.started":
			md.ThreadID = event["thread_id"] // 無ければ nil = null (Python の event.get)
		case "turn.started":
			md.TurnCompleted = false
		case "turn.completed":
			md.TurnCompleted = true
			usage, ok := event["usage"]
			if !ok {
				usage = emptyObject
			}
			md.Usage = append(md.Usage, usage)
		case "turn.failed", "error":
			md.Errors = append(md.Errors, json.RawMessage(line))
		case "item.completed":
			raw, ok := event["item"]
			if !ok {
				raw = emptyObject
			}
			item, perr := decodeObject(string(raw), "item must be an object")
			if perr != "" {
				md.ParseErrors = append(md.ParseErrors, parseError{lineNo, "item must be an object"})
				continue
			}
			itemType, _ := stringField(item, "type")
			if text, ok := stringField(item, "text"); itemType == "agent_message" && ok {
				messages = append(messages, text)
			} else if itemType == "command_execution" {
				md.Commands = append(md.Commands, command{item["id"], item["command"], item["exit_code"], item["status"]})
			}
		}
	}
	response := ""
	if st, err := os.Stat(responsePath); err == nil && st.Mode().IsRegular() { // Python の Path.is_file (stat の失敗は「無い」)
		if response, err = readText(responsePath); err != nil {
			return nil, "", err
		}
	}
	source := "output_last_message"
	if pyStrip(response) == "" && len(messages) > 0 {
		response = messages[len(messages)-1]
		source = "agent_message_event"
	}
	md.ResponsePresent = pyStrip(response) != ""
	if md.ResponsePresent {
		md.ResponseSource = &source
	}
	md.Complete = md.TurnCompleted && md.ResponsePresent && len(md.Errors) == 0 && len(md.ParseErrors) == 0
	return md, response, nil
}

// decodeObject は 1 つの JSON の値を読み、オブジェクトならキーごとの生の値を返す。読めなければ JSON のエラー、
// オブジェクトでなければ notObject を 2 つ目に返す。
func decodeObject(s, notObject string) (map[string]json.RawMessage, string) {
	var v json.RawMessage
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, err.Error()
	}
	var m map[string]json.RawMessage
	if t := bytes.TrimLeft(v, " \t\r\n"); len(t) == 0 || t[0] != '{' || json.Unmarshal(v, &m) != nil {
		return nil, notObject
	}
	return m, ""
}

// stringField は m[key] が JSON の文字列ならその値を返す。
func stringField(m map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := m[key]
	// 🚨 Go は null を string へ読んでもエラーにしない (空のまま)。Python 版の isinstance(…, str) と同じく、文字列の値だけを採る
	if !ok || len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// readText は Python の Path.read_text と同じく、UTF-8 として読み、改行を \n にそろえる (\r\n と \r を \n に)。
// UTF-8 でなければ error (Python の UnicodeDecodeError)。
func readText(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(b) {
		return "", fmt.Errorf("%s: UTF-8 でない", path)
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n"), nil
}

// pySplitlines は Python の str.splitlines (区切りを含めない)。\n 以外の行の区切り (\v \f \x1c-\x1e \x85 U+2028 U+2029) でも割る。
// 最後の区切りの後の空は要素にしない。行番号 (line) を Python 版とそろえるために要る。
func pySplitlines(s string) []string {
	var out []string
	start := 0
	for i, r := range s {
		switch r {
		case '\n', '\r', '\v', '\f', '\x1c', '\x1d', '\x1e', '\u0085', ' ', ' ':
			out = append(out, s[start:i])
			start = i + utf8.RuneLen(r)
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// pyStrip は Python の str.strip (空白は str.isspace。unicode.IsSpace に \x1c-\x1f を足したもの)。
func pyStrip(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) })
}

// pyPathStr は Python の str(Path(p)) (POSIX)。連続した / と . の要素を除き、末尾の / を落とす。先頭のちょうど 2 つの / は残す。
// log の「see <meta.json>」の表記を Python 版とそろえるために要る。
func pyPathStr(p string) string {
	root := ""
	if strings.HasPrefix(p, "/") {
		root = "/"
		if strings.HasPrefix(p, "//") && !strings.HasPrefix(p, "///") {
			root = "//"
		}
	}
	var parts []string
	for _, c := range strings.Split(p, "/") {
		if c != "" && c != "." {
			parts = append(parts, c)
		}
	}
	if s := root + strings.Join(parts, "/"); s != "" {
		return s
	}
	return "."
}

// writeJSON は Python の json.dumps(v, ensure_ascii=False, indent=2) + "\n" と同じ形で書く (HTML の記号をエスケープしない)。
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func inspectMain(eventsPath, responsePath, metaPath, logPath string, stderr io.Writer) int {
	md, response, err := inspectEvents(eventsPath, responsePath)
	var out any = md
	complete := err == nil && md.Complete
	if err != nil {
		out, response = readFailure{ReadError: err.Error()}, ""
	}
	var buf bytes.Buffer
	if err := writeJSON(&buf, out); err != nil {
		fmt.Fprintf(stderr, "codex-events: meta.json を作れない: %v\n", err)
		return 1
	}
	if err := os.WriteFile(metaPath, buf.Bytes(), 0o666); err != nil {
		fmt.Fprintf(stderr, "codex-events: %v\n", err)
		return 1
	}
	// 人と merger が読む従来のテキストのログにも残す (生の JSONL は別のファイル)
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o666)
	if err != nil {
		fmt.Fprintf(stderr, "codex-events: %v\n", err)
		return 1
	}
	var log strings.Builder
	log.WriteString("\n--- Codex JSONL result ---\n")
	if pyStrip(response) != "" {
		log.WriteString(response + "\n")
	}
	if !complete {
		log.WriteString("Incomplete JSONL run; see " + pyPathStr(metaPath) + "\n")
	}
	_, werr := f.WriteString(log.String())
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		fmt.Fprintf(stderr, "codex-events: %v\n", werr)
		return 1
	}
	if !complete {
		return 1
	}
	return 0
}

// isObjectMain はファイルの中身が JSON のオブジェクトなら 0、そうでなければ (読めない・JSON でない・オブジェクトでない) 1。
func isObjectMain(path string, stderr io.Writer) int {
	text, err := readText(path) // UTF-8 でなければ止める (Go の JSON は文字列の中の不正なバイトを U+FFFD にして受けてしまう)
	if err != nil {
		fmt.Fprintf(stderr, "codex-events: %v\n", err)
		return 1
	}
	if _, perr := decodeObject(text, "JSON のオブジェクトでない"); perr != "" {
		fmt.Fprintf(stderr, "codex-events: %s: %s\n", path, perr)
		return 1
	}
	return 0
}
