package dispatcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/store"
)

// 担い手は 設定 > config.toml > 既定 (claude)。様子に出どころつきで書く (pro-con config show と画面が読む)。
func TestReviewPrecedence(t *testing.T) {
	r := newUsageRig(t)
	check := func(mode, from string) {
		t.Helper()
		r.tick(t)
		if st, _, _ := store.LoadDispatcherState(r.d.Dir); st.Review != mode || st.ReviewFrom != from {
			t.Fatalf("担い手が %s (%s) でない: %+v", mode, from, st)
		}
	}
	check(store.ReviewClaude, ReviewFromDefault)
	r.d.ReviewDefault = store.ReviewCodex
	check(store.ReviewCodex, ReviewFromConfig)
	putSetting(t, r.d.Dir, store.SettingReview, store.ReviewClaude) // 設定は config.toml より勝つ
	check(store.ReviewClaude, ReviewFromSetting)
	putSetting(t, r.d.Dir, store.SettingReview, "")
	check(store.ReviewCodex, ReviewFromConfig)
}

// 既定 (claude) の PG への指示は 514 の前と同じ。testdata/prompt-before-514.txt は 514 を入れる前の master (86dd3ca3。PG は push しない) の Prompt が同じカードに出した文
// (PG の規律を直したら、この比べ方では落ちる。そのときは 514 の前と同じかではなく、claude のとき codex の行が無いことだけを見る形へ直す)。
func TestPromptClaudeUnchanged(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "prompt-before-514.txt"))
	if err != nil {
		t.Fatal(err)
	}
	c := card.Card{ID: "C-007", Title: "直す", Request: "色を直して", After: []string{"C-001"}, Issues: []card.IssueRef{{Repo: "dotfiles", Number: 514}}}
	for _, rv := range []Review{{}, {Mode: store.ReviewClaude, From: ReviewFromDefault, Codex: "/opt/codex"}} {
		if p := Prompt(c, rv); p != string(want) {
			t.Fatalf("%+v: claude の指示が 514 の前と違う:\n%s\n--- 前:\n%s", rv, p, want)
		}
	}
}

// codex のとき、PG の起動の指示に実体の絶対パス・枠の確かめ・証拠と代わりに回した記録の添付を書く。
func TestPromptCodexTellsPathAndFallback(t *testing.T) {
	r := newUsageRig(t)
	r.d.Codex = Tool{Path: "/opt/x/codex"}
	putSetting(t, r.d.Dir, store.SettingReview, store.ReviewCodex)
	r.tick(t)
	if len(r.l.prompts) == 0 {
		t.Fatal("PG を起動していない")
	}
	p := r.l.prompts[0]
	for _, want := range []string{"`/opt/x/codex`", "ratelimit -source codex -check", "codex-review/SKILL.md", "pro-con card attach C-001",
		"codex の敵対的レビュー", "Claude のサブエージェントで代わりに回した"} {
		if !strings.Contains(p, want) {
			t.Fatalf("codex の指示に %q が無い:\n%s", want, p)
		}
	}
	if i, j := strings.Index(p, "敵対的レビュー"), strings.Index(p, "pro-con card review C-001"); i < 0 || i > j {
		t.Fatalf("敵対的レビューの行が「終えたら review」より後にある:\n%s", p)
	}
}

// codex の実体を解けなかったら、その理由を渡して Claude で代わりに回させる (codex を呼ぶ行は書かない)。
func TestPromptCodexUnresolvedFallsBack(t *testing.T) {
	p := Prompt(card.Card{ID: "C-007", Title: "t"}, Review{Mode: store.ReviewCodex, CodexErr: "codex が PATH に無い"})
	if !strings.Contains(p, "codex が PATH に無い") || !strings.Contains(p, "代わりに回した") || strings.Contains(p, "ratelimit") {
		t.Fatalf("解けない codex の代わりの指示が違う:\n%s", p)
	}
}

// 取り込みの係への知らせは、今の設定ではなく PG を起動したときの担い手で書く。claude で起動した PG を、後で codex に変えた設定で差し戻させない。
func TestIntegratorNoticeUsesLaunchReview(t *testing.T) {
	r := newIntRig(t) // C-001 の PG は claude の設定で起動した
	if c := states(t, r.dir)["C-001"]; c.ReviewBy != store.ReviewClaude {
		t.Fatalf("起動のときの担い手を残していない: %q", c.ReviewBy)
	}
	putSetting(t, r.dir, store.SettingReview, store.ReviewCodex)
	submit(t, r.dir, store.Request{Kind: "review", CardID: "C-001"})
	r.tick(t)
	if p := r.l.prompts[len(r.l.prompts)-1]; !strings.Contains(p, "レビュー待ち C-001") || strings.Contains(p, "codex") {
		t.Fatalf("claude で起動した PG に codex の確かめを頼んだ: %q", p)
	}
	c := card.Card{ID: "C-009", Title: "t", State: card.Review, Since: t0, ReviewBy: store.ReviewCodex}
	k, _ := integratorKey(c)
	if n := integratorNotice(&Dispatcher{}, []card.Card{c}, []string{k}, []string{k}); !strings.Contains(n, "PG を起動したカード: C-009") || !strings.Contains(n, "差し戻す") {
		t.Fatalf("codex で起動した PG の確かめを頼んでいない: %q", n)
	}
}

// 担い手が codex の設定で起動した PG のカードには codex を残す (取り込みの係が確かめる印)。
func TestMarkRecordsReviewBy(t *testing.T) {
	r := newUsageRig(t)
	putSetting(t, r.d.Dir, store.SettingReview, store.ReviewCodex)
	r.tick(t)
	if c := states(t, r.d.Dir)["C-001"]; c.ReviewBy != store.ReviewCodex {
		t.Fatalf("codex で起動したのに印が %q", c.ReviewBy)
	}
}

// codex の実体は担い手が codex になった最初の Tick に 1 回だけ解く (claude のままなら解かない = 既定の起動を遅くしない)。
func TestResolveCodexLazily(t *testing.T) {
	r := newUsageRig(t)
	calls := 0
	r.d.ResolveCodex = func(context.Context) (Tool, error) { calls++; return Tool{Path: "/opt/x/codex", Version: "0.1"}, nil }
	r.tick(t)
	if calls != 0 {
		t.Fatalf("claude なのに codex を解いた (%d 回)", calls)
	}
	putSetting(t, r.d.Dir, store.SettingReview, store.ReviewCodex)
	notes := r.tick(t)
	r.tick(t)
	if calls != 1 || !hasEvent(notes, eventlog.KindLaunch, "/opt/x/codex") {
		t.Fatalf("codex を 1 回だけ解いて出来事に残していない: calls=%d %+v", calls, notes)
	}
	if st, _, _ := store.LoadDispatcherState(r.d.Dir); st.Codex != "/opt/x/codex" {
		t.Fatalf("解いた codex を様子に書いていない: %+v", st)
	}
}

// 手で書き換えた settings.json の review (Codex 等) は担い手にしない。codex と見せて Claude が回す形にしない。
func TestInvalidReviewSettingIgnored(t *testing.T) {
	r := newUsageRig(t)
	r.d.ReviewDefault = store.ReviewCodex
	if err := os.WriteFile(filepath.Join(r.d.Dir, store.SettingsFile), []byte(`{"review":"Codex"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	st, _, _ := store.LoadDispatcherState(r.d.Dir)
	if st.Review != store.ReviewCodex || st.ReviewFrom != ReviewFromConfig || !strings.Contains(st.Why, "review") {
		t.Fatalf("不正な review を使った / 理由を出さない: %+v", st)
	}
}

// 偽の codex (--version が失敗する = 認証切れ・壊れた実体) は解けたことにしない (PG に渡すと、呼んでから失敗を知る)。
func TestResolveCodexRefusesBrokenBinary(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\necho 'not logged in' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin:/bin")
	if cx, err := ResolveCodex(context.Background(), t.TempDir()); err == nil {
		t.Fatalf("壊れた codex を解けたことにした: %+v", cx)
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	if _, err := ResolveCodex(context.Background(), t.TempDir()); err == nil || !strings.Contains(err.Error(), "codex が PATH に無い") {
		t.Fatalf("codex が無いのに理由が違う: %v", err)
	}
}
