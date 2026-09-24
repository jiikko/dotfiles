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
	Outputs    []string // PG の出力の文 (古い順)
	LastAt     time.Time
	// LastNew は PG の出力のうち、末尾の中でそれまでに無かった文が最後に出た時刻 (watchdog の「進捗」。同じ出力を繰り返すループは数えない)
	LastNew time.Time
	// PendingSince は結果がまだ返っていないツール呼び出しのうち、最後のものの時刻 (長いコマンドの実行中。無ければゼロ)
	PendingSince time.Time
	Restarts     []time.Time // Claude Code がプロセスの死から自動で再開した時刻 (RestartNote を含む user レコード。古い順)
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
	Timestamp   string                 `json:"timestamp"`
	AITitle     string                 `json:"aiTitle"`
	CustomTitle string                 `json:"customTitle"`
	LastPrompt  string                 `json:"lastPrompt"`
	Origin      *struct{ Kind string } `json:"origin"`
	Message     *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
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

func parse(data []byte) Transcript {
	var t Transcript
	seen := map[string]bool{}
	pending := map[string]time.Time{} // tool_use の id → 呼んだ時刻 (結果が返ったら消す)
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		var r record
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue // 読めない行は飛ばす (書きかけの最後の行など)
		}
		at, _ := time.Parse(time.RFC3339Nano, r.Timestamp)
		if at.After(t.LastAt) {
			t.LastAt = at
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
			}
			if r.Message != nil {
				for _, id := range toolResults(r.Message.Content) {
					delete(pending, id)
				}
			}
			if r.Origin != nil && r.Origin.Kind == "human" && r.Message != nil {
				if s := text(r.Message.Content); s != "" {
					t.Prompts = append(t.Prompts, Prompt{At: at, Text: s})
				}
			}
		case "assistant":
			if r.Message != nil {
				if s := text(r.Message.Content); s != "" {
					t.Outputs = append(t.Outputs, s)
				}
				// 進捗は、文かツール呼び出し (名前 + 引数) のうち、それまでに無かったものが出たこと (同じ呼び出しの繰り返しは数えない)
				for _, sig := range signatures(r.Message.Content) {
					if !seen[sig] {
						seen[sig] = true
						t.LastNew = at
					}
				}
				for _, id := range toolUses(r.Message.Content) {
					pending[id] = at
				}
			}
		}
	}
	for _, at := range pending {
		if at.After(t.PendingSince) {
			t.PendingSince = at
		}
	}
	return t
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

func toolUses(raw json.RawMessage) []string {
	var ids []string
	for _, p := range parts(raw) {
		if p.Type == "tool_use" && p.ID != "" {
			ids = append(ids, p.ID)
		}
	}
	return ids
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
