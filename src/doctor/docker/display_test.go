package docker

import (
	"context"
	"strings"
	"testing"
	"time"
)

// 🚨 Name は「提示コマンドが指す先」なので、無害化して表示せず落とす
// (無害化して出すと、細工した名前の資源を持つ側が「別の資源を消せ」と案内させられる)。
func TestSanitizeDropsUnsafeNameKeepsDetail(t *testing.T) {
	rep := Report{Groups: []Group{{
		Kind: KindImages, Label: "使われていないイメージ", Size: 300,
		Items: []Item{
			{Name: "evil\x1b[2Jname", Size: 100, Command: "docker rmi evil"},
			{Name: "good:1", Detail: "説明\x1b]0;title\x07", Size: 200, Command: "docker rmi good"},
		},
	}}}
	out := SanitizeForDisplay(rep)
	g := out.Groups[0]
	if len(g.Items) != 1 || g.Items[0].Name != "good:1" {
		t.Fatalf("残った候補が %+v", g.Items)
	}
	if strings.ContainsRune(g.Items[0].Detail, 0x1b) {
		t.Fatalf("Detail に ESC が残っている: %q", g.Items[0].Detail)
	}
	// 落とした分を合計から引く (画面の「回収できる量」が実体と食い違わない)
	if g.Size != 200 {
		t.Fatalf("合計が %d (200 のはず)", g.Size)
	}
	// 🚨 落としたことは Notes に出す。Unavailable (= 診断できなかった印) を汚さない
	if out.Dropped != 1 || !strings.Contains(strings.Join(out.Notes, "\n"), "一覧から外しました") {
		t.Fatalf("落としたことが残っていない: dropped=%d notes=%v", out.Dropped, out.Notes)
	}
	if out.Unavailable != "" {
		t.Fatalf("一部を落としただけで診断できず扱いになった: %q", out.Unavailable)
	}
}

// Command が識別子として読めない行も落とす (Name だけ見ない)。
func TestSanitizeDropsUnsafeCommand(t *testing.T) {
	out := SanitizeForDisplay(Report{Groups: []Group{{
		Items: []Item{{Name: "ok", Size: 100, Command: "docker rmi ok\x1b[2J"}},
	}}})
	if len(out.Groups[0].Items) != 0 || out.Dropped != 1 {
		t.Fatalf("提示コマンドに制御文字が残った: %+v", out.Groups[0].Items)
	}
}

func TestSanitizeKeepsExistingUnavailable(t *testing.T) {
	out := SanitizeForDisplay(Report{Unavailable: "daemon が\x1b[31m落ちている"})
	if strings.ContainsRune(out.Unavailable, 0x1b) {
		t.Fatalf("Unavailable に ESC が残っている: %q", out.Unavailable)
	}
}

// 未知の状態のコンテナは候補にせず、件数を注記に出す (提示コマンドとの差を隠さない)。
func TestUnknownContainerStateIsNotedNotCounted(t *testing.T) {
	f := &fakeDocker{df: dfJSON("", `{"ID":"a","Names":"x","State":"brand-new","Size":"1GB","CreatedAt":"`+stamp(60*24*time.Hour)+`"}`, "", ""), vol: "[]"}
	g := group(t, Scan(context.Background(), opts(f)), KindContainers)
	if len(g.Items) != 0 {
		t.Fatalf("未知の状態を候補にした: %+v", g.Items)
	}
	if !strings.Contains(strings.Join(g.Notes, "\n"), "知らない状態のコンテナが 1 件") {
		t.Fatalf("注記が無い: %v", g.Notes)
	}
}

func TestHumanSizeMatchesDockerBase1000(t *testing.T) {
	for in, want := range map[int64]string{0: "0B", 999: "999B", 1000: "1.0kB", 51_810_000_000: "51.8GB"} {
		if got := HumanSize(in); got != want {
			t.Errorf("HumanSize(%d) = %q, want %q", in, got, want)
		}
	}
}

// 🚨 見出しに出す数字を取り違えないための錨。Estimate は候補の単純合計 (共有レイヤーを
// 重複計上して上振れする)、DockerReclaimable は docker 自身の申告。入れ替えても
// 気づかない状態にしない (敵対レビュー 2026-09-04: どちらもテストに一度も出ていなかった)。
func TestEstimateAndDockerReclaimableAreDifferentNumbers(t *testing.T) {
	rep := Report{Groups: []Group{
		{Kind: KindImages, Size: 38_300_000_000, Reclaimable: 27_120_000_000},
		{Kind: KindVolumes, Size: 3_132_000_000, Reclaimable: 3_132_000_000},
	}}
	if got := rep.Estimate(); got != 41_432_000_000 {
		t.Fatalf("Estimate = %d", got)
	}
	if got := rep.DockerReclaimable(); got != 30_252_000_000 {
		t.Fatalf("DockerReclaimable = %d", got)
	}
	if rep.Estimate() == rep.DockerReclaimable() {
		t.Fatalf("2 つが同じ値を返している (取り違えを検出できない)")
	}
}

func TestCandidatesCountsAllGroups(t *testing.T) {
	rep := Report{Groups: []Group{
		{Items: []Item{{Name: "a"}, {Name: "b"}}},
		{Items: []Item{{Name: "c"}}},
	}}
	if got := rep.Candidates(); got != 3 {
		t.Fatalf("Candidates = %d", got)
	}
}
