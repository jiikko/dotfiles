package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// defaultSummary は docker system df の実出力と同じ JSON Lines。
const defaultSummary = `{"Type":"Images","TotalCount":"42","Active":"15","Size":"65.46GB","Reclaimable":"27.12GB (41%)"}
{"Type":"Containers","TotalCount":"15","Active":"6","Size":"4.334GB","Reclaimable":"255.7MB (5%)"}
{"Type":"Local Volumes","TotalCount":"9","Active":"2","Size":"5.329GB","Reclaimable":"3.132GB (58%)"}
{"Type":"Build Cache","TotalCount":"154","Active":"0","Size":"51.81GB","Reclaimable":"14.35GB"}`

var now = time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

// stamp は now から d だけ前の、docker 形式の時刻。
func stamp(d time.Duration) string { return now.Add(-d).Format("2006-01-02 15:04:05 -0700 MST") }

// fakeDocker は docker CLI の差し替え。df は `system df` の stdout、vol は `volume inspect` の stdout。
type fakeDocker struct {
	df, vol    string
	summary    string // `docker system df --format json` (空なら既定の 4 行)
	summaryRC  int
	summaryErr error
	dfRC       int
	dfErr      error
	volRC      int
	calls      []string
	dfStderr   string
	volStderr  string
	volMissing bool
}

func (f *fakeDocker) run(ctx context.Context, name string, args ...string) (string, string, int, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	switch {
	case len(args) >= 3 && args[0] == "system" && args[1] == "df" && args[2] == "-v":
		return f.df, f.dfStderr, f.dfRC, f.dfErr
	case len(args) >= 2 && args[0] == "system" && args[1] == "df":
		if f.summary == "" {
			return defaultSummary, "", f.summaryRC, f.summaryErr
		}
		return f.summary, "", f.summaryRC, f.summaryErr
	case len(args) >= 2 && args[0] == "volume" && args[1] == "inspect":
		if f.volMissing {
			return "", f.volStderr, 1, nil
		}
		return f.vol, f.volStderr, f.volRC, nil
	}
	return "", "", 0, errors.New("想定外の呼び出し: " + name + " " + strings.Join(args, " "))
}

func opts(f *fakeDocker) Options {
	return Options{
		Run: f.run, Now: func() time.Time { return now },
		LookPath:  func(string) (string, error) { return "/usr/local/bin/docker", nil },
		AppExists: func() bool { return true },
	}
}

func group(t *testing.T, rep Report, k Kind) Group {
	t.Helper()
	for _, g := range rep.Groups {
		if g.Kind == k {
			return g
		}
	}
	t.Fatalf("群 %s が無い", k)
	return Group{}
}

// dfJSON は 4 種を 1 つの JSON にまとめる (実物と同じ形)。
func dfJSON(images, containers, volumes, cache string) string {
	return fmt.Sprintf(`{"Images":[%s],"Containers":[%s],"Volumes":[%s],"BuildCache":[%s]}`,
		images, containers, volumes, cache)
}

func TestScanNotInstalled(t *testing.T) {
	rep := Scan(context.Background(), Options{
		LookPath:  func(string) (string, error) { return "", errors.New("not found") },
		AppExists: func() bool { return false },
	})
	if rep.Installed {
		t.Fatalf("docker が無い環境で Installed=true")
	}
	if rep.Unavailable != "" {
		t.Fatalf("入っていないのは「診断できず」ではない: %q", rep.Unavailable)
	}
}

func TestScanAppWithoutCLIIsUnavailableNotEmpty(t *testing.T) {
	rep := Scan(context.Background(), Options{
		LookPath:  func(string) (string, error) { return "", errors.New("not found") },
		AppExists: func() bool { return true },
	})
	if !rep.Installed || rep.Unavailable == "" {
		t.Fatalf("app だけある環境を 0 件に畳んだ: %+v", rep)
	}
}

// 🚨 daemon 停止・タイムアウトを「候補なし」に畳まない (fail-closed)。
func TestScanDaemonDownIsUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    *fakeDocker
	}{
		{"rc非0", &fakeDocker{dfRC: 1, dfStderr: "Cannot connect to the Docker daemon\n"}},
		{"起動できず", &fakeDocker{dfErr: errors.New("context deadline exceeded")}},
		{"JSONが壊れている", &fakeDocker{df: "{"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rep := Scan(context.Background(), opts(tc.f))
			if rep.Unavailable == "" {
				t.Fatalf("診断できずになっていない: %+v", rep)
			}
			if len(rep.Groups) != 0 {
				t.Fatalf("診断できていないのに群がある: %d", len(rep.Groups))
			}
		})
	}
}

func TestContainersOnlyStoppedAndOld(t *testing.T) {
	f := &fakeDocker{df: dfJSON("", strings.Join([]string{
		`{"ID":"aaa","Names":"old-stopped","State":"exited","Status":"Exited (0) 3 weeks ago","Image":"golang:1.25","Size":"223MB","CreatedAt":"` + stamp(30*24*time.Hour) + `"}`,
		`{"ID":"bbb","Names":"fresh-stopped","State":"exited","Status":"Exited (0) 2 days ago","Image":"mysql:8.4","Size":"10MB","CreatedAt":"` + stamp(2*24*time.Hour) + `"}`,
		`{"ID":"ccc","Names":"running-old","State":"running","Status":"Up 3 weeks","Image":"redis","Size":"5MB","CreatedAt":"` + stamp(60*24*time.Hour) + `"}`,
		`{"ID":"ddd","Names":"unknown-state","State":"brand-new-state","Status":"?","Image":"x","Size":"5MB","CreatedAt":"` + stamp(60*24*time.Hour) + `"}`,
	}, ","), "", ""), vol: "[]"}
	rep := Scan(context.Background(), opts(f))
	g := group(t, rep, KindContainers)
	if g.Total != 4 {
		t.Fatalf("全件が %d (4 のはず)", g.Total)
	}
	if len(g.Items) != 1 || g.Items[0].Name != "old-stopped" {
		t.Fatalf("候補が想定外: %+v", g.Items)
	}
	if g.Size != 223_000_000 {
		t.Fatalf("候補の合計が %d", g.Size)
	}
	if !strings.Contains(g.Command, "until=336h") {
		t.Fatalf("提示コマンドが OldAfter と揃っていない: %q", g.Command)
	}
}

func TestImagesOnlyUnreferencedAndOld(t *testing.T) {
	f := &fakeDocker{df: dfJSON(strings.Join([]string{
		`{"ID":"sha256:0f12ea390fb6094d","Repository":"golang","Tag":"1.25","Containers":"0","Size":"848MB","UniqueSize":"847.7MB","CreatedAt":"` + stamp(30*24*time.Hour) + `"}`,
		`{"ID":"sha256:1111111111111111","Repository":"mysql","Tag":"8.4","Containers":"2","Size":"812MB","UniqueSize":"812MB","CreatedAt":"` + stamp(90*24*time.Hour) + `"}`,
		`{"ID":"sha256:2222222222222222","Repository":"<none>","Tag":"<none>","Containers":"0","Size":"1GB","UniqueSize":"1GB","CreatedAt":"` + stamp(90*24*time.Hour) + `"}`,
	}, ","), "", "", ""), vol: "[]"}
	g := group(t, Scan(context.Background(), opts(f)), KindImages)
	if len(g.Items) != 2 {
		t.Fatalf("候補が %d 件: %+v", len(g.Items), g.Items)
	}
	// dangling は repo:tag ではなく短い ID で出す (<none>:<none> は識別子にならない)
	if g.Items[0].Name != "222222222222" {
		t.Fatalf("dangling の名前が %q", g.Items[0].Name)
	}
	if g.Items[1].Name != "golang:1.25" {
		t.Fatalf("名前が %q", g.Items[1].Name)
	}
	// 共有レイヤーを二重に数えない (UniqueSize を使う)
	if g.Items[1].Size != 847_700_000 {
		t.Fatalf("サイズが %d", g.Items[1].Size)
	}
}

func TestBuildCacheUsesLastUsedAndSkipsInUse(t *testing.T) {
	f := &fakeDocker{df: dfJSON("", "", "", strings.Join([]string{
		`{"ID":"c1","InUse":"false","Size":"2GB","Description":"pulled from docker.io/library/ruby","LastUsedAt":"` + stamp(60*24*time.Hour) + `","CreatedAt":"` + stamp(90*24*time.Hour) + `"}`,
		`{"ID":"c2","InUse":"false","Size":"1GB","Description":"recent","LastUsedAt":"` + stamp(24*time.Hour) + `","CreatedAt":"` + stamp(90*24*time.Hour) + `"}`,
		`{"ID":"c3","InUse":"true","Size":"1GB","Description":"in use","LastUsedAt":"` + stamp(90*24*time.Hour) + `","CreatedAt":"` + stamp(90*24*time.Hour) + `"}`,
		`{"ID":"c4","InUse":"false","Size":"3GB","Description":"never used","LastUsedAt":"","CreatedAt":"` + stamp(90*24*time.Hour) + `"}`,
	}, ",")), vol: "[]"}
	g := group(t, Scan(context.Background(), opts(f)), KindBuildCache)
	names := make([]string, 0, len(g.Items))
	for _, it := range g.Items {
		names = append(names, it.Name)
	}
	if strings.Join(names, ",") != "c4,c1" {
		t.Fatalf("候補が %v (c4,c1 のはず)", names)
	}
	if !strings.Contains(g.Command, "unused-for=336h") {
		t.Fatalf("提示コマンドが %q", g.Command)
	}
}

func TestVolumesUnusedWithCreatedAt(t *testing.T) {
	f := &fakeDocker{
		df: dfJSON("", "", strings.Join([]string{
			`{"Name":"old_unused","Links":"0","Size":"3GB"}`,
			`{"Name":"fresh_unused","Links":"0","Size":"1GB"}`,
			`{"Name":"in_use","Links":"2","Size":"5GB"}`,
		}, ","), ""),
		vol: `[{"Name":"old_unused","CreatedAt":"2023-11-18T05:28:12Z"},{"Name":"fresh_unused","CreatedAt":"2026-09-03T05:28:12Z"}]`,
	}
	rep := Scan(context.Background(), opts(f))
	g := group(t, rep, KindVolumes)
	if len(g.Items) != 1 || g.Items[0].Name != "old_unused" {
		t.Fatalf("候補が %+v", g.Items)
	}
	if g.Items[0].Command != "docker volume rm old_unused" {
		t.Fatalf("個別コマンドが %q", g.Items[0].Command)
	}
	// 🚨 群のまとめコマンドは出さない (docker volume prune -a はデータを消して戻せない)
	if g.Command != "" {
		t.Fatalf("ボリュームにまとめコマンドが出ている: %q", g.Command)
	}
	if !g.Confirm {
		t.Fatalf("Confirm が立っていない")
	}
	// `--` を置いて名前がフラグとして読まれる余地を消す
	var volCall string
	for _, c := range f.calls {
		if strings.Contains(c, "volume inspect") {
			volCall = c
		}
	}
	if !strings.Contains(volCall, "json -- old_unused") {
		t.Fatalf("volume inspect の引数が %q", volCall)
	}
}

// 作成日が取れなくても候補を 0 件に畳まない (「見えていない」を「無い」にしない)。
func TestVolumesKeepItemsWhenCreatedAtUnavailable(t *testing.T) {
	f := &fakeDocker{
		df:         dfJSON("", "", `{"Name":"unknown_age","Links":"0","Size":"3GB"}`, ""),
		volMissing: true, volStderr: "Error response from daemon: boom",
	}
	g := group(t, Scan(context.Background(), opts(f)), KindVolumes)
	if len(g.Items) != 1 || g.Items[0].AgeKnown {
		t.Fatalf("候補が %+v", g.Items)
	}
	if !strings.Contains(strings.Join(g.Notes, "\n"), "作成日を取れませんでした") {
		t.Fatalf("取れなかったことが注記に無い: %v", g.Notes)
	}
}

// 名前が識別子として読めないボリュームは提示コマンドに載せず、落とした件数を残す。
func TestVolumeNameAllowlist(t *testing.T) {
	f := &fakeDocker{
		df:  dfJSON("", "", `{"Name":"bad name;rm -rf /","Links":"0","Size":"3GB"},{"Name":"-flagish","Links":"0","Size":"1GB"}`, ""),
		vol: "[]",
	}
	rep := Scan(context.Background(), opts(f))
	g := group(t, rep, KindVolumes)
	if len(g.Items) != 0 {
		t.Fatalf("読めない名前を候補にした: %+v", g.Items)
	}
	if rep.Dropped != 2 {
		t.Fatalf("Dropped が %d (2 のはず)", rep.Dropped)
	}
	for _, c := range f.calls {
		if strings.Contains(c, "volume inspect") {
			t.Fatalf("読めない名前しかないのに inspect を呼んだ: %q", c)
		}
	}
}

func TestParseSize(t *testing.T) {
	for in, want := range map[string]int64{
		"848MB": 848_000_000, "51.81GB": 51_810_000_000, "0B": 0, "N/A": 0, "": 0,
		"255.7MB (5%)": 255_700_000, "こわれた": 0, "12": 0,
	} {
		if got := parseSize(in); got != want {
			t.Errorf("parseSize(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseDockerTime(t *testing.T) {
	for _, s := range []string{"2026-08-14 04:34:32 +0900 JST", "2025-11-07 02:05:40.521062297 +0000 UTC", "2023-11-18T05:28:12Z"} {
		if _, ok := parseDockerTime(s); !ok {
			t.Errorf("読めない: %q", s)
		}
	}
	if _, ok := parseDockerTime("いつか"); ok {
		t.Errorf("読めない形を読めたことにした")
	}
}

// 時計が巻き戻っていても「古い」側へ倒さない。
func TestNegativeAgeIsNotOld(t *testing.T) {
	f := &fakeDocker{df: dfJSON("", `{"ID":"a","Names":"future","State":"exited","Size":"1GB","CreatedAt":"`+stamp(-100*24*time.Hour)+`"}`, "", ""), vol: "[]"}
	if g := group(t, Scan(context.Background(), opts(f)), KindContainers); len(g.Items) != 0 {
		t.Fatalf("未来の作成日を古い扱いにした: %+v", g.Items)
	}
}

// 🚨 合計と回収可能量は docker 自身の申告を使う (共有レイヤーの勘定を自前で作り直さない)。
func TestTotalsComeFromDockerNotOurSum(t *testing.T) {
	f := &fakeDocker{df: dfJSON(
		`{"ID":"sha256:aaaaaaaaaaaa","Repository":"a","Tag":"1","Containers":"0","Size":"1GB","UniqueSize":"1GB","CreatedAt":"`+stamp(60*24*time.Hour)+`"}`,
		"", "", ""), vol: "[]"}
	g := group(t, Scan(context.Background(), opts(f)), KindImages)
	if g.TotalSize != 65_460_000_000 || g.Reclaimable != 27_120_000_000 {
		t.Fatalf("docker の申告を使っていない: total=%d reclaimable=%d", g.TotalSize, g.Reclaimable)
	}
	if g.Size != 1_000_000_000 {
		t.Fatalf("候補の見積もりが %d", g.Size)
	}
}

// 集計が取れない / 書式が変わったら「診断できず」へ倒す (0 件に畳まない)。
func TestSummaryFailureIsUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    *fakeDocker
	}{
		{"rc非0", &fakeDocker{summaryRC: 1}},
		{"既知の種別が無い", &fakeDocker{summary: `{"Type":"Somethig New","Size":"1GB","Reclaimable":"1GB"}`}},
		{"JSONが壊れている", &fakeDocker{summary: "{"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.f.df, tc.f.vol = dfJSON("", "", "", ""), "[]"
			rep := Scan(context.Background(), opts(tc.f))
			if rep.Unavailable == "" || len(rep.Groups) != 0 {
				t.Fatalf("診断できずになっていない: %+v", rep)
			}
		})
	}
}
