package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// summaryFor は `docker system df` の出力を作る。件数は -v の fixture から数える
// (本物の docker は 2 つのコマンドで同じ件数を返すので、fake もそう振る舞わせる。
// 件数を固定値にすると、突合の検査が全テストを巻き込んで落ちる)。
func summaryFor(dfv string) string {
	var d struct{ Images, Containers, Volumes, BuildCache []json.RawMessage }
	if err := json.Unmarshal([]byte(dfv), &d); err != nil {
		return "" // 壊れた fixture は summary も出せない (テスト側の事故を隠さない)
	}
	return fmt.Sprintf(`{"Type":"Images","TotalCount":"%d","Size":"65.46GB","Reclaimable":"27.12GB (41%%)"}
{"Type":"Containers","TotalCount":"%d","Size":"4.334GB","Reclaimable":"255.7MB (5%%)"}
{"Type":"Local Volumes","TotalCount":"%d","Size":"5.329GB","Reclaimable":"3.132GB (58%%)"}
{"Type":"Build Cache","TotalCount":"%d","Size":"51.81GB","Reclaimable":"14.35GB"}`,
		len(d.Images), len(d.Containers), len(d.Volumes), len(d.BuildCache))
}

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
			return summaryFor(f.df), "", f.summaryRC, f.summaryErr
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
		{"JSONが壊れている", &fakeDocker{df: "{", summary: summaryFor(dfJSON("", "", "", ""))}},
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
	if g.Items[0].Command != "docker volume rm -- old_unused" {
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

// 🚨 `-v` の JSON はキー名が変わっても構文が正しければ Unmarshal に成功し、全群 0 件 = 緑になる。
// docker 自身が出している件数と突合して、食い違いは診断できずへ倒す。
func TestVerboseShapeChangeIsUnavailableNotGreen(t *testing.T) {
	f := &fakeDocker{
		// キー名が変わった (= このツールからは 0 件に見える) が、docker の集計は 1 件と言っている。
		// 🚨 大小の違いだけでは再現しない — Go の Unmarshal はフィールド名を大小無視で当てる
		df:      `{"ImageSummary":[{"ID":"a"}],"Containers":[],"Volumes":[],"BuildCache":[]}`,
		summary: summaryFor(dfJSON(`{"ID":"a"}`, "", "", "")),
		vol:     "[]",
	}
	rep := Scan(context.Background(), opts(f))
	if rep.Unavailable == "" || len(rep.Groups) != 0 {
		t.Fatalf("形が変わったのに緑になった: %+v", rep)
	}
}

// 集計に無い種別があれば診断できずへ倒す (0 件に畳まない)。
func TestSummaryMissingKindIsUnavailable(t *testing.T) {
	f := &fakeDocker{df: dfJSON("", "", "", ""), vol: "[]",
		summary: `{"Type":"Images","TotalCount":"0","Size":"0B","Reclaimable":"0B"}`}
	if rep := Scan(context.Background(), opts(f)); rep.Unavailable == "" {
		t.Fatalf("種別が欠けているのに緑: %+v", rep)
	}
}

// build cache の InUse は allowlist。読めない値は候補にせず注記に出す
// (「true でなければ未使用」だと、表記が変わった瞬間に使用中を全部候補にする)。
func TestBuildCacheUnknownInUseIsNotCandidate(t *testing.T) {
	f := &fakeDocker{df: dfJSON("", "", "", `{"ID":"c1","InUse":"1","Size":"9GB","LastUsedAt":"`+stamp(90*24*time.Hour)+`"}`), vol: "[]"}
	g := group(t, Scan(context.Background(), opts(f)), KindBuildCache)
	if len(g.Items) != 0 {
		t.Fatalf("読めない InUse を未使用扱いにした: %+v", g.Items)
	}
	if !strings.Contains(strings.Join(g.Notes, "\n"), "使用中かどうかを読めなかった") {
		t.Fatalf("注記が無い: %v", g.Notes)
	}
}

// 🚨 提示は **ID**。repo:tag にしない。
//
// 実測 2026-09-04 (docker 29.2.1): `docker rmi <多タグの ID>` は rc=1 で
// "image is referenced in multiple repositories" を返す (見える失敗) が、
// `docker rmi <タグ>` は rc=0 で "Untagged:" だけを出し **1 バイトも減らない** (静かな成功)。
// 画面は UniqueSize を「回収できる量」として出しているので、後者だと嘘の成功報告になる。
//
// あわせて `df -v` の形も pin する: **ID ごとに 1 行**で、行の Tag はその ID に付いた
// 複数のタグのうちの 1 つでしかない (`docker images` は tag ごとに 1 行で、こちらとは違う)。
func TestImagesProposeIDNotTag(t *testing.T) {
	f := &fakeDocker{df: dfJSON(
		`{"ID":"sha256:abcabcabcabc","Repository":"app","Tag":"v1","Containers":"0","Size":"2GB","UniqueSize":"2GB","CreatedAt":"`+stamp(60*24*time.Hour)+`"}`,
		"", "", ""), vol: "[]"}
	g := group(t, Scan(context.Background(), opts(f)), KindImages)
	if len(g.Items) != 1 {
		t.Fatalf("候補が %+v", g.Items)
	}
	if g.Items[0].Name != "app:v1" {
		t.Errorf("表示名が %q (読める名前で出す)", g.Items[0].Name)
	}
	if g.Items[0].Command != "docker rmi -- abcabcabcabc" {
		t.Errorf("提示コマンドが %q (ID を指すこと)", g.Items[0].Command)
	}
}

// 🚨 `df -v` が tag ごとの行を返す形に変わったら、行数が summary の TotalCount (= ID の件数) と
// 食い違うので**診断できずへ倒れる**。0 件にも二重計上にもしない。
func TestPerTagRowsWouldBeCaughtByCountCheck(t *testing.T) {
	row := func(tag string) string {
		return `{"ID":"sha256:abcabcabcabc","Repository":"app","Tag":"` + tag + `","Containers":"0","Size":"2GB","UniqueSize":"2GB","CreatedAt":"` + stamp(60*24*time.Hour) + `"}`
	}
	f := &fakeDocker{
		df: dfJSON(row("v1")+","+row("v2")+","+row("v3"), "", "", ""), vol: "[]",
		// docker の集計は ID の件数 = 1 と言っている
		summary: summaryFor(dfJSON(row("v1"), "", "", "")),
	}
	rep := Scan(context.Background(), opts(f))
	if rep.Unavailable == "" || len(rep.Groups) != 0 {
		t.Fatalf("形が変わったのに診断できずへ倒れない: %+v", rep)
	}
}

// コンテナ名もボリュームと同じ allowlist を通す (termsafe は制御文字しか弾かない)。
func TestContainerNameAllowlist(t *testing.T) {
	f := &fakeDocker{df: dfJSON("", `{"ID":"aaa","Names":"web; rm -rf ~","State":"exited","Size":"1GB","CreatedAt":"`+stamp(60*24*time.Hour)+`"}`, "", ""), vol: "[]"}
	rep := Scan(context.Background(), opts(f))
	if g := group(t, rep, KindContainers); len(g.Items) != 0 {
		t.Fatalf("シェルのメタ文字を含む名前を提示コマンドに載せた: %+v", g.Items)
	}
	if rep.Dropped != 1 {
		t.Fatalf("Dropped が %d", rep.Dropped)
	}
}

// 作成日を取れないボリュームは候補にしない (不可逆な群を診断の劣化で広げない)。
func TestVolumesWithoutCreatedAtAreNotCandidates(t *testing.T) {
	f := &fakeDocker{
		df:         dfJSON("", "", `{"Name":"unknown_age","Links":"0","Size":"3GB"}`, ""),
		volMissing: true, volStderr: "Error response from daemon: boom",
	}
	g := group(t, Scan(context.Background(), opts(f)), KindVolumes)
	if len(g.Items) != 0 {
		t.Fatalf("作成日不明を候補にした: %+v", g.Items)
	}
	notes := strings.Join(g.Notes, "\n")
	if !strings.Contains(notes, "作成日を取れませんでした") || !strings.Contains(notes, "候補から外しました") {
		t.Fatalf("注記が足りない: %v", g.Notes)
	}
}

// volume inspect が 1 行 1 JSON (NDJSON) を返す版でも読む。
func TestVolumeInspectAcceptsNDJSON(t *testing.T) {
	f := &fakeDocker{
		df:  dfJSON("", "", `{"Name":"old_unused","Links":"0","Size":"3GB"}`, ""),
		vol: `{"Name":"old_unused","CreatedAt":"2023-11-18T05:28:12Z"}`,
	}
	g := group(t, Scan(context.Background(), opts(f)), KindVolumes)
	if len(g.Items) != 1 {
		t.Fatalf("NDJSON を読めていない: %+v (%v)", g.Items, g.Notes)
	}
}

// 提示コマンドの filter は候補の範囲より広くならない (切り上げる)。
func TestHoursRoundsUp(t *testing.T) {
	f := &fakeDocker{df: dfJSON("", "", "", ""), vol: "[]"}
	o := opts(f)
	o.OldAfter = 36*time.Hour + 30*time.Minute
	rep := Scan(context.Background(), o)
	if !strings.Contains(group(t, rep, KindContainers).Command, "until=37h") {
		t.Fatalf("filter が切り捨てられている: %q", group(t, rep, KindContainers).Command)
	}
}

// system prune はボリュームを消さない — その事実を持って回る。
func TestSystemPruneNoteMentionsVolumes(t *testing.T) {
	f := &fakeDocker{df: dfJSON("", "", "", ""), vol: "[]"}
	rep := Scan(context.Background(), opts(f))
	if !strings.Contains(rep.SystemPruneNote, "ボリュームは含みません") {
		t.Fatalf("SystemPruneNote が %q", rep.SystemPruneNote)
	}
}

// 🚨 診断できなかった理由には docker の stderr が入る。早期 return の経路こそ関門が要る。
func TestUnavailableFromStderrIsSanitized(t *testing.T) {
	f := &fakeDocker{summaryRC: 1}
	f.df, f.vol = dfJSON("", "", "", ""), "[]"
	// summaryRC=1 の経路は stderr を読まないので、-v 側の rc!=0 で測る
	f2 := &fakeDocker{df: "", dfRC: 1, dfStderr: "boom\x1b[2J\x1b]0;title\x07", summary: summaryFor(dfJSON("", "", "", ""))}
	rep := Scan(context.Background(), opts(f2))
	if rep.Unavailable == "" {
		t.Fatalf("診断できずになっていない")
	}
	if strings.ContainsRune(rep.Unavailable, 0x1b) {
		t.Fatalf("stderr の制御文字が素通りした: %q", rep.Unavailable)
	}
	_ = f
}

// 🚨 提示コマンドを**完全一致**で固定する。部分一致 ("until=336h" を含むか) だと、
// -f や -a の有無が変わっても緑のまま通る (実測 2026-09-04: 実際に通り抜けた)。
//
// -f: prune は対話プロンプトを出すので、TTY 無しの実行では何も消さずに rc=0 で返る。
// -a (builder): 付けないと dangling なキャッシュしか消えず、候補 (InUse=false 全件) と食い違う。
func TestPruneCommandsArePinnedExactly(t *testing.T) {
	f := &fakeDocker{df: dfJSON("", "", "", ""), vol: "[]"}
	rep := Scan(context.Background(), opts(f))
	want := map[Kind]string{
		KindContainers: "docker container prune --filter until=336h -f",
		KindImages:     "docker image prune -a --filter until=336h -f",
		KindBuildCache: "docker builder prune -a --filter unused-for=336h -f",
		KindVolumes:    "", // まとめて消すコマンドは出さない (戻らないため)
	}
	for _, g := range rep.Groups {
		if g.Command != want[g.Kind] {
			t.Errorf("%s の提示コマンドが %q (期待 %q)", g.Kind, g.Command, want[g.Kind])
		}
	}
	if rep.SystemPrune != "docker system prune -a --filter until=336h -f" {
		t.Errorf("まとめて回収するコマンドが %q", rep.SystemPrune)
	}
}

// 個別のコマンドは 1 件だけを指す (prune の filter と混ぜない)。
func TestPerItemCommandsArePinnedExactly(t *testing.T) {
	f := &fakeDocker{
		df: dfJSON(
			`{"ID":"sha256:abcabcabcabc","Repository":"app","Tag":"old","Containers":"0","Size":"2GB","UniqueSize":"2GB","CreatedAt":"`+stamp(60*24*time.Hour)+`"}`,
			`{"ID":"c1","Names":"old-web","State":"exited","Size":"10MB","CreatedAt":"`+stamp(60*24*time.Hour)+`"}`,
			`{"Name":"old_data","Links":"0","Size":"3GB"}`, ""),
		vol: `[{"Name":"old_data","CreatedAt":"2020-01-01T00:00:00Z"}]`,
	}
	rep := Scan(context.Background(), opts(f))
	want := map[Kind]string{
		KindContainers: "docker rm -- old-web",
		KindImages:     "docker rmi -- abcabcabcabc",
		KindVolumes:    "docker volume rm -- old_data",
	}
	for _, g := range rep.Groups {
		w, ok := want[g.Kind]
		if !ok || len(g.Items) == 0 {
			continue
		}
		if g.Items[0].Command != w {
			t.Errorf("%s の個別コマンドが %q (期待 %q)", g.Kind, g.Items[0].Command, w)
		}
	}
}

// 🚨 `docker volume inspect` は名前をまとめて渡すので、走査中に 1 個消えただけで rc=1 になる。
// **stdout を捨てない** — 捨てると他の全ボリュームが「作成日不明」= 候補 0 件に落ち、
// ボリューム群が常時空になる (敵対レビュー 2 周目 P3-3)。
func TestVolumeInspectPartialFailureKeepsOthers(t *testing.T) {
	f := &fakeDocker{
		df: dfJSON("", "", `{"Name":"old_a","Links":"0","Size":"1GB"},{"Name":"gone_b","Links":"0","Size":"2GB"}`, ""),
		// old_a だけ読めて、gone_b は消えている (rc=1 + stderr)
		vol: `[{"Name":"old_a","CreatedAt":"2020-01-01T00:00:00Z"}]`, volRC: 1, volStderr: "Error: No such volume: gone_b",
	}
	g := group(t, Scan(context.Background(), opts(f)), KindVolumes)
	if len(g.Items) != 1 || g.Items[0].Name != "old_a" {
		t.Fatalf("読めた分まで落とした: %+v (%v)", g.Items, g.Notes)
	}
	notes := strings.Join(g.Notes, "\n")
	if !strings.Contains(notes, "一部のボリュームの作成日を取れませんでした") {
		t.Errorf("rc が注記に残っていない: %v", g.Notes)
	}
	if !strings.Contains(notes, "候補から外しました") {
		t.Errorf("読めなかった 1 件を候補から外したことが残っていない: %v", g.Notes)
	}
}
