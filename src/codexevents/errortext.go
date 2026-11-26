package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// errorTextMain は inspect が書いた meta.json から、失敗の原因の文を出す。出せたら 0、文が無ければ 1、meta.json を読めなければ 2。
// 文を持つ error / turn.failed のイベントがあれば、その文を起きた順に 1 行ずつ出す (続けて同じ文は 1 行)。無ければ最初の parse_errors、無ければ read_error を 1 行。
// codex-fanout は最後の行で利用上限かを判定し (上限の文の後に別の原因が続く run を上限と読まない)、候補には全部の行を出す。
// 🚨 最後の 1 行だけを出す形にしない: 本物の codex が落ちたときの -J のイベントの並びは未実測 (2026-11-26 時点) で、
// error が具体的な文を、後の turn.failed が汎用の文 ("failure") だけを持つ形なら、最後の 1 行は原因を失う。
// 上限の判定を最後の行で行うのは、codex exec の JSONL の出力が最後の error の文を turn.failed にそのまま写すという仮説による
// (upstream の codex-rs の exec の実装の記憶。手元の実ログでは未確認)。この仮説の下では、上の形は本物では起きない。
// 再提起するなら、本物の -J の上限の run で turn.failed の文が error と違う events.jsonl を添える。そのときは判定を「上限の文がどこかの行に在り、
// その後に別の具体的な原因が無い」に変える。
func errorTextMain(metaPath string, stdout, stderr io.Writer) int {
	text, err := readText(metaPath)
	if err != nil {
		fmt.Fprintf(stderr, "codex-events: %v\n", err)
		return 2
	}
	var meta struct {
		Errors      []json.RawMessage `json:"errors"`
		ParseErrors []parseError      `json:"parse_errors"`
		ReadError   string            `json:"read_error"`
	}
	if err := json.Unmarshal([]byte(text), &meta); err != nil {
		fmt.Fprintf(stderr, "codex-events: %s: %v\n", metaPath, err)
		return 2
	}
	var lines []string
	for _, raw := range meta.Errors {
		// 1 行の要約に載せるので、改行・タブを畳む
		if s := strings.Join(strings.Fields(eventErrorText(raw)), " "); s != "" && (len(lines) == 0 || lines[len(lines)-1] != s) {
			lines = append(lines, s)
		}
	}
	if len(lines) == 0 && len(meta.ParseErrors) > 0 {
		p := meta.ParseErrors[0]
		lines = append(lines, strings.Join(strings.Fields(fmt.Sprintf("events の %d 行目を読めない: %s", p.Line, p.Error)), " "))
	}
	if len(lines) == 0 && meta.ReadError != "" {
		lines = append(lines, strings.Join(strings.Fields("events か応答を読めない: "+meta.ReadError), " "))
	}
	if len(lines) == 0 {
		return 1
	}
	fmt.Fprintln(stdout, strings.Join(lines, "\n"))
	return 0
}

// eventErrorText は error / turn.failed のイベントの文を返す (error は .message、turn.failed は .error.message。無ければ空)。
func eventErrorText(raw json.RawMessage) string {
	event, perr := decodeObject(string(raw), "")
	if perr != "" || event == nil {
		return ""
	}
	if s, ok := stringField(event, "message"); ok && pyStrip(s) != "" {
		return s
	}
	inner, perr := decodeObject(string(event["error"]), "")
	if perr != "" || inner == nil {
		return ""
	}
	if s, ok := stringField(inner, "message"); ok && pyStrip(s) != "" {
		return s
	}
	return ""
}
