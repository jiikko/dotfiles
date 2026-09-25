package live

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/store"
)

// 今走っているもの (issue 473): 結果待ちの道具の呼び出しは引数の要約つきで呼んだ順に、裏のサブエージェントは終わりの知らせ
// (task-notification) が来るまで残る。知らせは queue-operation / attachment のどちらに載っても拾い、PG の出力が引用しただけでは消さない。
// プロセスが死んで再開したら裏のサブエージェントは消えている。
func TestParseTranscriptCallsAndAgents(t *testing.T) {
	data := `{"type":"assistant","timestamp":"2026-09-26T01:00:00Z","message":{"content":[{"type":"tool_use","id":"a1","name":"Agent","input":{"description":"敵対的レビュー","prompt":"壊して"}}]}}
{"type":"user","timestamp":"2026-09-26T01:00:01Z","message":{"content":[{"type":"tool_result","tool_use_id":"a1","content":"launched"}]},"toolUseResult":{"isAsync":true,"status":"async_launched","agentId":"ag1","description":"敵対的レビュー"}}
{"type":"assistant","timestamp":"2026-09-26T01:00:02Z","message":{"content":[{"type":"tool_use","id":"a2","name":"Agent","input":{"description":"変異の検証"}}]}}
{"type":"user","timestamp":"2026-09-26T01:00:03Z","message":{"content":[{"type":"tool_result","tool_use_id":"a2","content":"launched"}]},"toolUseResult":{"isAsync":true,"agentId":"ag2","description":"変異の検証"}}
{"type":"user","timestamp":"2026-09-26T01:00:04Z","message":{"content":[{"type":"tool_result","tool_use_id":"x","content":"err"}]},"toolUseResult":"Error: 文字列の結果"}
{"type":"assistant","timestamp":"2026-09-26T01:00:05Z","message":{"content":[{"type":"text","text":"<task-notification><task-id>ag2</task-id><status>completed</status></task-notification> を待つ"}]}}
{"type":"queue-operation","timestamp":"2026-09-26T01:00:06Z","content":"<task-notification>\n<task-id>ag1</task-id>\n<status>completed</status>\n</task-notification>"}
{"type":"assistant","timestamp":"2026-09-26T01:00:07Z","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"make   test"}},{"type":"tool_use","id":"t2","name":"WebFetch","input":{"url":"https://example.com"}}]}}
`
	tr := parse([]byte(data))
	if len(tr.Agents) != 1 || tr.Agents[0].ID != "ag2" || tr.Agents[0].Text != "変異の検証" {
		t.Fatalf("終わりの知らせの来ていない裏のサブエージェントだけ残るはず (PG の出力の引用では消さない): %+v", tr.Agents)
	}
	if len(tr.Calls) != 2 || tr.Calls[0].Name != "Bash" || tr.Calls[0].Text != "make test" || tr.Calls[1].Text != "https://example.com" ||
		!tr.Calls[0].At.Equal(time.Date(2026, 9, 26, 1, 0, 7, 0, time.UTC)) {
		t.Fatalf("結果待ちの呼び出しを呼んだ順に要約つきで: %+v", tr.Calls)
	}
	att := `{"type":"attachment","timestamp":"2026-09-26T01:00:08Z","attachment":{"prompt":"<task-notification>\n<task-id>ag2</task-id>\n<status>failed</status>\n</task-notification>"}}` + "\n"
	if tr := parse([]byte(data + att)); len(tr.Agents) != 0 {
		t.Fatalf("attachment に載った知らせでも終わったと読むはず: %+v", tr.Agents)
	}
	running := `{"type":"attachment","timestamp":"2026-09-26T01:00:08Z","attachment":{"prompt":"<task-notification><task-id>ag2</task-id><status>running</status></task-notification>"}}` + "\n"
	if tr := parse([]byte(data + running)); len(tr.Agents) != 1 {
		t.Fatalf("status が running の知らせでは終わったと読まない: %+v", tr.Agents)
	}
	restart := `{"type":"user","timestamp":"2026-09-26T01:00:09Z","message":{"content":"` + RestartNote + `"}}` + "\n"
	if tr := parse([]byte(data + restart)); len(tr.Agents) != 0 || len(tr.Calls) != 0 {
		t.Fatalf("再開の後は裏のサブエージェントも結果待ちの呼び出しも残らない: %+v / %+v", tr.Agents, tr.Calls)
	}
}

// 画面は dispatcher が集めた様子 (store.DoingFile) を読んでカードに足すだけ。読めなければ理由を違反の行に出す (0 件と区別する)。
func TestRefreshAttachesCollectedDoing(t *testing.T) {
	b, _ := testBackend(t, sessions[:1], nil)
	runningCard(t, b, "C-001", "bbbbbbbb")
	at := time.Date(2026, 9, 24, 10, 59, 55, 0, time.UTC)
	if err := store.SaveDoing(b.dir, store.Doing{At: at, Cards: map[string][]card.Doing{"C-001": {{Kind: card.DoingProcess, Text: "make test", Since: at}}}}); err != nil {
		t.Fatal(err)
	}
	b.Refresh(context.Background())
	s := b.Poll()
	if len(s.Cards) != 1 || len(s.Cards[0].Doing) != 1 || s.Cards[0].Doing[0].Text != "make test" || !s.Cards[0].DoingAt.Equal(at) {
		t.Fatalf("集めた様子をカードに足さない: %+v", s.Cards)
	}
	if err := os.WriteFile(filepath.Join(b.dir, store.DoingFile), []byte("{壊れた"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.Refresh(context.Background())
	if s := b.Poll(); len(s.Violations) == 0 || len(s.Cards[0].Doing) != 0 {
		t.Fatalf("読めないのに理由を出さない / 古い様子を残した: %+v", s)
	}
}
