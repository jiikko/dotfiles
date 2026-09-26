package live

// transcript (~/.claude/projects/*/<sessionId>.jsonl) の末尾から、カードに出す情報を読む。
//
// 形式は Claude Code 2.1.281 で実測した (issue 424): Desktop の対話 session と claude --bg の session は同じ形式で、
// 1 行 1 レコードの JSON。題名は ai-title (対話) か custom-title (bg の -n)、最後に人間が打った文は last-prompt に、
// 会話の途中で何度も書き直されるので、末尾だけ読めば最新が取れる (対話の transcript は 14MB を超えるので全体は読まない)。
// 人間の発言は user レコードの origin.kind == "human"、PG の出力は assistant レコードの text。

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// tailBytes は末尾から読む量。ai-title は実測でおよそ 46KB に 1 回書かれるので、その数倍を読む。
const tailBytes = 512 << 10

// Transcript は末尾から読めた情報。読めなかった欄は空。
type Transcript struct {
	Title      string
	LastPrompt string
	Prompts    []Prompt // 人間の発言 (古い順)
	Outputs    []string // PG の出力の文 (古い順)。改行を残した原文 (markdown。rawText)
	LastAt     time.Time
	// LastNew は PG の出力のうち、末尾の中でそれまでに無かった文が最後に出た時刻 (watchdog の「進捗」。同じ出力を繰り返すループは数えない)
	LastNew time.Time
	// PendingSince は結果がまだ返っていないツール呼び出しのうち、最後のものの時刻 (長いコマンドの実行中。無ければゼロ)
	PendingSince time.Time
	Restarts     []time.Time // Claude Code がプロセスの死から自動で再開した時刻 (RestartNote を含む user レコード。古い順)
	// Calls は結果がまだ返っていない道具の呼び出し (呼んだ順)。Agents は裏で走らせたサブエージェントのうち、終わりの知らせ
	// (task-notification) がまだ末尾に無いもの (起こした順)。どちらも issue 473 の「今走っているもの」
	Calls  []Call
	Agents []Call
	// Uses は末尾にある道具の呼び出しすべて (結果が返ったものも。呼んだ順)。PM が今の turn でどのカードを扱っているか (issue 480)
	Uses []Call
}

// Call は道具の呼び出し 1 つ。
type Call struct {
	ID   string // tool_use の id (サブエージェントは agentId)
	Name string // 道具の名前 (Bash / Agent / Read …)
	Text string // 引数の要約 (1 行。Bash はコマンド、Agent は説明)
	// Target は引数のうち説明ではない対象 (コマンド・ファイル・URL・パターン。1 行)。扱っているカードの ID をここから拾う
	// (説明には ID が無いことが多い。書く中身 (content) は拾わない: issue の本文に並ぶ別のカードの ID を扱っていると取り違える)
	Target string
	At     time.Time
}

// RestartNote は、プロセスが死んだ session を Claude Code が自動で再開したときに会話へ足す文の一部 (2.1.281 で実測。issue 425 結果 1)。
// origin を実測していないので、発言者を問わず user レコードの文の**先頭**で探す (依頼の原文などに引用された文は数えない)。版が変わって文が変わると、落ちた回数を数えられなくなる
// (そのときは外から操作された疑いとして知らせる側に倒れる)
const RestartNote = "Continue from where you left off. Note: this session was automatically restarted after its process exited unexpectedly"

// Prompt は人間の発言 1 つ。
type Prompt struct {
	At   time.Time
	Text string
}

type record struct {
	Type        string                 `json:"type"`
	UUID        string                 `json:"uuid"`
	Timestamp   string                 `json:"timestamp"`
	AITitle     string                 `json:"aiTitle"`
	CustomTitle string                 `json:"customTitle"`
	LastPrompt  string                 `json:"lastPrompt"`
	Origin      *struct{ Kind string } `json:"origin"`
	Message     *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	// ToolUseResult は道具の結果の付帯情報。裏のサブエージェントを起こした結果は isAsync と agentId を持つ (2.1.282 で実測)。
	// 🚨 RawMessage で受ける: 文字列のこともあり (エラーの結果)、構造体で受けると行ごと読めなくなる
	ToolUseResult json.RawMessage `json:"toolUseResult"`
}

// asyncAgent は裏のサブエージェントを起こした結果の付帯情報。
type asyncAgent struct {
	IsAsync     bool   `json:"isAsync"`
	AgentID     string `json:"agentId"`
	Description string `json:"description"`
}

// ReadTail は path の末尾を読む。
func ReadTail(path string) (Transcript, error) {
	f, err := os.Open(path)
	if err != nil {
		return Transcript{}, err
	}
	defer func() { _ = f.Close() }() // 読むだけなので閉じる失敗は結果に影響しない
	st, err := f.Stat()
	if err != nil {
		return Transcript{}, err
	}
	off := max(st.Size()-tailBytes, 0)
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return Transcript{}, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return Transcript{}, err
	}
	if off > 0 { // 途中から読んだ最初の行は壊れているので捨てる
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	return parse(data), nil
}

// HumanPrompts は path の transcript の**全体**から、from 以降 to 以前の人間の発言を古い順に返す (attach の間に打った指示。issue 428)。
// 末尾だけ (ReadTail) だと、長く attach していた間の先頭の発言を落とす。
func HumanPrompts(path string, from, to time.Time) ([]Prompt, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // 読むだけなので閉じる失敗は結果に影響しない
	var out []Prompt
	for _, p := range parseFrom(f).Prompts {
		if !p.At.Before(from) && !p.At.After(to) {
			out = append(out, p)
		}
	}
	return out, nil
}

func parse(data []byte) Transcript { return parseFrom(bytes.NewReader(data)) }

func parseFrom(rd io.Reader) Transcript {
	var t Transcript
	seen := map[string]bool{}
	pending := map[string]Call{} // tool_use の id → 呼び出し (結果が返ったら消す)
	var order []string           // pending の呼んだ順
	agents := map[string]Call{}  // agentId → 裏のサブエージェント (終わりの知らせで消す)
	var agentOrder []string
	// 🚨 bufio.Scanner にしない: 行の長さに上限があり、それを超える行 (大きなツールの結果) で読むのを黙って止め、後の発言を落とす
	br := bufio.NewReader(rd)
	for line, err := br.ReadBytes('\n'); len(line) > 0 || err == nil; line, err = br.ReadBytes('\n') {
		var r record
		if json.Unmarshal(line, &r) != nil {
			continue // 読めない行は飛ばす (書きかけの最後の行など)
		}
		at, _ := time.Parse(time.RFC3339Nano, r.Timestamp)
		if at.After(t.LastAt) {
			t.LastAt = at
		}
		if r.Type != "assistant" {
			for _, id := range finishedTasks(line) {
				delete(agents, id)
			}
		}
		switch r.Type {
		case "ai-title":
			if r.AITitle != "" {
				t.Title = r.AITitle
			}
		case "custom-title":
			if r.CustomTitle != "" {
				t.Title = r.CustomTitle
			}
		case "last-prompt":
			if r.LastPrompt != "" {
				t.LastPrompt = r.LastPrompt
			}
		case "user":
			if r.Message != nil && strings.HasPrefix(text(r.Message.Content), RestartNote) {
				t.Restarts = append(t.Restarts, at)
				clear(agents) // プロセスが死んだので、裏のサブエージェントも一緒に消えている
			}
			var a asyncAgent
			if json.Unmarshal(r.ToolUseResult, &a) == nil && a.IsAsync && a.AgentID != "" {
				agents[a.AgentID] = Call{ID: a.AgentID, Name: "Agent", Text: oneLine(a.Description), At: at}
				agentOrder = append(agentOrder, a.AgentID)
			}
			if r.Message != nil {
				if results := toolResults(r.Message.Content); len(results) > 0 {
					for _, id := range results {
						delete(pending, id)
					}
				} else if text(r.Message.Content) != "" {
					// ツールの結果ではない user 行 (再開の文・人間の発言) の前の呼び出しは置き去り (落ちて結果が書かれなかった等)。
					// 🚨 「後に PG の出力が続いたか」で判定しない: 並列の呼び出しは 1 件ずつ時刻の違う別の行に書かれる (実 transcript で確認)
					clear(pending)
				}
			}
			if r.Origin != nil && r.Origin.Kind == "human" && r.Message != nil {
				if s := text(r.Message.Content); s != "" && !strings.HasPrefix(s, RestartNote) { // 再開の文は人間の発言ではない (origin は未実測)
					t.Prompts = append(t.Prompts, Prompt{At: at, Text: s})
				}
			}
		case "assistant":
			if r.Message != nil {
				if s := text(r.Message.Content); s != "" {
					t.Outputs = append(t.Outputs, rawText(r.Message.Content)) // 画面が markdown として描くので改行を残す (486)
				}
				// 進捗は、文かツール呼び出し (名前 + 引数) のうち、それまでに無かったものが出たこと (同じ呼び出しの繰り返しは数えない)
				for _, sig := range signatures(r.Message.Content) {
					if !seen[sig] {
						seen[sig] = true
						t.LastNew = at
					}
				}
				for _, c := range toolUses(r.Message.Content) {
					c.At = at
					pending[c.ID] = c
					order = append(order, c.ID)
					t.Uses = append(t.Uses, c)
				}
			}
		}
	}
	for _, id := range order {
		if c, ok := pending[id]; ok {
			delete(pending, id) // 同じ id が 2 度出ても 1 度だけ
			t.Calls = append(t.Calls, c)
			if c.At.After(t.PendingSince) {
				t.PendingSince = c.At
			}
		}
	}
	for _, id := range agentOrder {
		if c, ok := agents[id]; ok {
			delete(agents, id)
			t.Agents = append(t.Agents, c)
		}
	}
	return t
}

var (
	notificationRe = regexp.MustCompile(`<task-notification>.*?</task-notification>`)
	taskIDRe       = regexp.MustCompile(`<task-id>([^<]+)</task-id>`)
	taskStatusRe   = regexp.MustCompile(`<status>([^<]+)</status>`)
)

// finishedTasks は行 (JSON の 1 レコードの生の文字) に載った終わりの知らせ (task-notification) の task-id。status が running のものは除く。
// 知らせは queue-operation / attachment / user のどのレコードにも載りうる (2.1.282 で実測。版で変わる) ので、種類を問わず生の文字から探す。
// 🚨 assistant の行では探さない (PG が知らせの文を書いただけで終わったことにしない)。道具の結果に載った知らせ (PG が別の transcript を
// 読んだ出力など) は拾ってしまうが、消えるのはその id の作業だけ
func finishedTasks(line []byte) []string {
	if !bytes.Contains(line, []byte("<task-notification>")) { // 末尾 512KB の全行で正規表現を回さない
		return nil
	}
	var ids []string
	for _, n := range notificationRe.FindAll(line, -1) {
		id, st := taskIDRe.FindSubmatch(n), taskStatusRe.FindSubmatch(n)
		if id == nil || (st != nil && string(st[1]) == "running") {
			continue
		}
		ids = append(ids, string(id[1]))
	}
	return ids
}

type part struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
}

func parts(raw json.RawMessage) []part {
	var ps []part
	if json.Unmarshal(raw, &ps) != nil {
		return nil
	}
	return ps
}

// signatures は assistant の出力 1 件の中身の識別 (文 / ツールの名前 + 引数)。
func signatures(raw json.RawMessage) []string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if s = oneLine(s); s != "" {
			return []string{"text:" + s}
		}
		return nil
	}
	var out []string
	for _, p := range parts(raw) {
		switch p.Type {
		case "text":
			if t := oneLine(p.Text); t != "" {
				out = append(out, "text:"+t)
			}
		case "tool_use":
			out = append(out, "tool:"+p.Name+":"+string(p.Input))
		}
	}
	return out
}

func toolUses(raw json.RawMessage) []Call {
	var cs []Call
	for _, p := range parts(raw) {
		if p.Type == "tool_use" && p.ID != "" {
			cs = append(cs, Call{ID: p.ID, Name: p.Name, Text: callText(p.Input), Target: callTarget(p.Input)})
		}
	}
	return cs
}

// callText は道具の引数の要約 (1 行)。説明 (description) があればそれ、無ければコマンド・ファイル・URL・パターンの順に最初にあるもの。
func callText(input json.RawMessage) string {
	return firstArg(input, "description", "command", "file_path", "url", "pattern", "prompt")
}

// callTarget は道具の引数のうち、説明ではない対象 (Call.Target)。コマンドは 1 行目だけ (heredoc の本文 = 書く中身を拾わない)。
func callTarget(input json.RawMessage) string {
	var in struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(input, &in) == nil && strings.TrimSpace(in.Command) != "" {
		head, _, _ := strings.Cut(strings.TrimSpace(in.Command), "\n")
		return oneLine(head)
	}
	return firstArg(input, "file_path", "url", "pattern")
}

// firstArg は道具の引数 keys のうち、最初にある空でない文字列 (1 行)。
func firstArg(input json.RawMessage, keys ...string) string {
	var in map[string]any
	if json.Unmarshal(input, &in) != nil {
		return ""
	}
	for _, k := range keys {
		if s, ok := in[k].(string); ok && strings.TrimSpace(s) != "" {
			return oneLine(s)
		}
	}
	return ""
}

func toolResults(raw json.RawMessage) []string {
	var ids []string
	for _, p := range parts(raw) {
		if p.Type == "tool_result" && p.ToolUseID != "" {
			ids = append(ids, p.ToolUseID)
		}
	}
	return ids
}

// text は message.content (文字列か、type:text を含む配列) の文を 1 行にして返す。ツールの呼び出しと結果は含めない。
func text(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return oneLine(s)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b []string
	for _, p := range parts {
		if p.Type == "text" && strings.TrimSpace(p.Text) != "" {
			b = append(b, p.Text)
		}
	}
	return oneLine(strings.Join(b, " "))
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// rawText は text と同じ中身を、改行を残したまま返す (PG の出力 = Outputs 用。486)。複数の文の部分は空行でつなぐ。
// 🚨 text (1 行に潰す) を変えない: 人間の発言・再開の文の判定・watchdog の進捗の数え方 (signatures) が 1 行の形に依存している
func rawText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b []string
	for _, p := range parts {
		if p.Type == "text" && strings.TrimSpace(p.Text) != "" {
			b = append(b, strings.TrimSpace(p.Text))
		}
	}
	return strings.Join(b, "\n\n")
}
