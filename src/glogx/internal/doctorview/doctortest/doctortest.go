// Package doctortest は doctor 画面のテストが共有する部品 (テスト用の DoctorView・fake の Options・描画の取り出し)。
// main のテスト (browseModel と組み合わせるもの) と internal/doctorview の外部テスト (doctorview_test) の両方から使う。
// 🚨 本番のコードから import しない (testing を import する)。doctorview の中のテスト (package doctorview) からは
// import できない (import の循環)。そちらで使う部品は doctorview の中のテストに置く (issue 742)。
package doctortest

import (
	"context"
	"doctor/disk"
	"doctor/docker"
	"doctor/ssd"
	"doctor/svc"
	"encoding/json"
	"errors"
	"fmt"
	"glogx/internal/doctorview"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// progAt はテスト用の進捗。表示の中身が主題でないテストの穴埋めに使う。
func ProgAt(i, total int, label string, ph disk.DeletePhase) doctorview.DoctorProgress {
	return doctorview.DoctorProgress{I: i, Total: total, Label: label, Phase: ph, Known: true}
}

func DoctorPanelText(v *doctorview.DoctorView, page int) string {
	return strings.Join(v.Lines(DoctorTestOpts(page)), "\n")
}

// 記憶の置き場をテスト用に隔離する。
func WithDoctorStateDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
}

// fakeSSDDiskutil / fakeSSDSmartctl は実機の出力 (2026-09-29) を必要な欄に絞ったもの。
// smartctl の serial_number はわざと残す (画面・コピー文へ漏れないことの検査に使う)。
const (
	fakeSSDDiskutil = "   Device / Media Name:       APPLE SSD AP1024Z\n   SMART Status:              Verified\n   Device Location:           Internal\n"
	fakeSSDSmartctl = `{"smartctl":{"exit_status":4},"serial_number":"FAKESERIAL0000","smart_status":{"passed":true},` +
		`"nvme_smart_health_information_log":{"critical_warning":0,"available_spare":100,"available_spare_threshold":99,` +
		`"percentage_used":7,"media_errors":0,"data_units_written":234413480,"power_on_hours":3993,"unsafe_shutdowns":11}}`
)

// fakeSSDOptions は本物の diskutil / smartctl を叩かない ssd.Options (issue 419 と同じ理由)。
// withSmartctl=false は「smartctl が入っていない」。
func FakeSSDOptions(withSmartctl bool) func() ssd.Options {
	return func() ssd.Options {
		return ssd.Options{
			Run: func(_ context.Context, name string, _ ...string) (string, string, int, error) {
				if strings.HasSuffix(name, "smartctl") {
					return fakeSSDSmartctl, "", 4, nil
				}
				return fakeSSDDiskutil, "", 0, nil
			},
			LookPath: func(string) (string, error) {
				if !withSmartctl {
					return "", errors.New("not found")
				}
				return "/opt/homebrew/bin/smartctl", nil
			},
		}
	}
}

// writeDoctorSnapshot は snapshot をキャッシュ位置へ書く (TTL 内 / 外の前提を作る)。
// 同じ 4〜10 行がこのファイルに 8 回あったのを 1 行にしたもの (issue 199)。
//
// 🚨 壊れた JSON を書くテスト (TestDoctorCacheCorruptAndAtomic) と、書き込み先を
// 直接触るテスト (os.Remove / os.Chmod) は path 自体が要るので寄せていない。
// そちらは doctorview.DoctorSnapshotPath() を直接呼ぶ。
func WriteDoctorSnapshot(t *testing.T, sn doctorview.DoctorSnapshot) {
	t.Helper()
	data, err := json.Marshal(sn)
	if err != nil {
		t.Fatal(err)
	}
	writeDoctorSnapshotRaw(t, data)
}

// writeDoctorSnapshotRaw は生のバイト列を書く (壊れた JSON を置くテスト用)。
func writeDoctorSnapshotRaw(t *testing.T, data []byte) {
	t.Helper()
	path, err := doctorview.DoctorSnapshotPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func DoctorTestView(t *testing.T) *doctorview.DoctorView {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	target := filepath.Join(home, "Library", "Caches", "thing")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "f"), make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := func(context.Context, string, ...string) (string, string, int, error) {
		// 段落 (空行) を 2 箇所入れる: 「(N 行) を len(detail) で数える」と「非空行で数える」と
		// 「len(lines) で数える」が全部違う数になる形にして、数え方の変異を検出できるようにする
		// (敵対レビュー 2026-09-03: 空行の無い fixture では 3 つの式が区別できず vacuous だった)。
		// detail=6 / 非空行=5 / len(lines)=7
		return "", "Warning: Some installed casks are deprecated or disabled.\nYou should find replacements:\n  foo\n\nUninstall them with `brew uninstall --cask`\n\nSee also: brew help uninstall\n", 1, nil
	}
	v := &doctorview.DoctorView{
		DiskOpts: func() disk.Options {
			return disk.Options{
				Env: disk.Env{Home: home, TmpDir: home + "/", Getenv: func(string) string { return "" }, AppDirs: nil},
				Run: fake,
				Catalog: []disk.Entry{{ID: "thing", Label: "Thing キャッシュ", Tier: 1, Risk: disk.RiskSafe, DeleteVia: "rm",
					Recover: "再生成されます", Paths: []string{"~/Library/Caches/thing"}}},
				BootTime: func() (time.Time, error) { return time.Now(), nil },
			}
		},
		SvcOpts: func() svc.Options {
			return svc.Options{Dirs: []svc.LaunchDir{{Path: filepath.Join(home, "LaunchAgents"), Domain: "gui/501"}},
				Run: func(context.Context, string, ...string) (string, string, int, error) {
					return "PID\tStatus\tLabel\n", "", 0, nil
				}}
		},
		BrewRun:    fake,
		DockerOpts: func() docker.Options { return FakeDockerOptions(FakeDockerDF) },
		SsdOpts:    FakeSSDOptions(true),
	}
	return v
}

// fakeDockerDF は `docker system df -v` の fixture (実物と同じ形。1 群 1 件ずつ)。
const FakeDockerDF = `{"Images":[{"ID":"sha256:abcabcabcabc","Repository":"app","Tag":"old","Containers":"0","Size":"2GB","UniqueSize":"2GB","CreatedAt":"2020-01-01 00:00:00 +0000 UTC"}],` +
	`"Containers":[{"ID":"c1","Names":"old-web","State":"exited","Status":"Exited (0) 1 year ago","Image":"app:old","Size":"10MB","CreatedAt":"2020-01-01 00:00:00 +0000 UTC"}],` +
	`"Volumes":[{"Name":"old_data","Links":"0","Size":"3GB"}],` +
	`"BuildCache":[{"ID":"bc1","InUse":"false","Size":"1GB","Description":"[2/3] COPY . /app","LastUsedAt":"2020-01-01 00:00:00 +0000 UTC","CreatedAt":"2020-01-01 00:00:00 +0000 UTC"}]}`

// fakeDockerOptions は docker CLI を差し替えた Options。件数は df -v の fixture から数える
// (本物の docker は 2 コマンドで同じ件数を返すので、fake もそう振る舞わせる。固定値にすると
// doctor/docker の件数突合に引っかかって全テストが「診断できず」になる)。
func FakeDockerOptions(dfv string) docker.Options {
	var d struct {
		Images, Containers, BuildCache []json.RawMessage
		Volumes                        []struct{ Name string }
	}
	if err := json.Unmarshal([]byte(dfv), &d); err != nil {
		panic("fixture が壊れている: " + err.Error())
	}
	// 🚨 **docker 申告の回収可能量を、候補の単純合計 (2GB+10MB+3GB+1GB = 6.01GB) と
	// 違う値にする。** 一致させると「見積もりと申告を取り違える」変異が検出できない
	// (実測 2026-09-04: 同じ値にしていたので取り違えの変異が緑で通った)。
	// この summary なら申告の合計は 1GB+5MB+3GB+500MB = 4.5GB
	summary := fmt.Sprintf(`{"Type":"Images","TotalCount":"%d","Size":"2GB","Reclaimable":"1GB"}
{"Type":"Containers","TotalCount":"%d","Size":"10MB","Reclaimable":"5MB"}
{"Type":"Local Volumes","TotalCount":"%d","Size":"3GB","Reclaimable":"3GB"}
{"Type":"Build Cache","TotalCount":"%d","Size":"1GB","Reclaimable":"500MB"}`,
		len(d.Images), len(d.Containers), len(d.Volumes), len(d.BuildCache))
	return docker.Options{
		LookPath:  func(string) (string, error) { return "/usr/local/bin/docker", nil },
		AppExists: func() bool { return true },
		Run: func(_ context.Context, _ string, args ...string) (string, string, int, error) {
			switch {
			case len(args) >= 3 && args[0] == "system" && args[1] == "df" && args[2] == "-v":
				return dfv, "", 0, nil
			case len(args) >= 2 && args[0] == "system" && args[1] == "df":
				return summary, "", 0, nil
			case len(args) >= 2 && args[0] == "volume" && args[1] == "inspect":
				// 🚨 fixture のボリュームから作る。固定名にすると、別の fixture を使う
				// テストで全件が「作成日不明」になり、検査対象が空になる
				parts := make([]string, 0, len(d.Volumes))
				for _, v := range d.Volumes {
					parts = append(parts, `{"Name":"`+v.Name+`","CreatedAt":"2020-01-01T00:00:00Z"}`)
				}
				return "[" + strings.Join(parts, ",") + "]", "", 0, nil
			}
			return "", "想定外の docker 呼び出し", 1, nil
		},
	}
}

// noDockerOptions は「Docker Desktop が入っていない」環境 (タブごと出ない)。
func NoDockerOptions() docker.Options {
	return docker.Options{
		LookPath:  func(string) (string, error) { return "", errors.New("not found") },
		AppExists: func() bool { return false },
	}
}

// runDoctorCmds は Cmd (Batch 含む) を実行して Msg を集め、view へ届ける。disk は完了まで再アームする。
func RunDoctorCmds(t *testing.T, v *doctorview.DoctorView, cmd tea.Cmd) {
	t.Helper()
	pending := []tea.Cmd{cmd}
	deadline := time.Now().Add(20 * time.Second)
	for len(pending) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("doctor の走査が終わらない")
		}
		c := pending[0]
		pending = pending[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, sub := range msg {
				pending = append(pending, sub)
			}
		case doctorview.DoctorDiskMsg:
			if next := v.ReceiveDisk(msg); next != nil {
				pending = append(pending, next)
			}
		case doctorview.DoctorSvcMsg:
			v.ReceiveSvc(msg)
		case doctorview.DoctorBrewMsg:
			v.ReceiveBrew(msg)
		case doctorview.DoctorDockerMsg:
			v.ReceiveDocker(msg)
		case doctorview.DoctorSSDMsg:
			v.ReceiveSSD(msg)
		case nil:
		default:
			t.Fatalf("知らない Msg: %T", msg)
		}
	}
}

func DoctorTestOpts(page int) doctorview.DoctorRenderOpts {
	return doctorview.DoctorRenderOpts{Width: 100, Page: page, Colored: false, Spinner: "⠋", Now: time.Date(2026, 9, 2, 12, 0, 0, 0, time.Local)}
}

func DoctorText(v *doctorview.DoctorView, page int) string {
	return strings.Join(v.Lines(DoctorTestOpts(page)), "\n")
}
