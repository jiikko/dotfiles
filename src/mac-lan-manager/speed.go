package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"mac-lan-manager/lanmgr"
	"mac-lan-manager/ledger"
	"mac-lan-manager/netinfo"
	"mac-lan-manager/speedtest"
	"mac-lan-manager/sshdest"
)

// speedReport は speedtest の 1 台分 (--json の要素)。
type speedReport struct {
	Host       string  `json:"host"`
	Name       string  `json:"name"`
	IP         string  `json:"ip,omitempty"`
	Link       string  `json:"link,omitempty"` // "有線" / "無線" / "" (分からない)
	UpBps      float64 `json:"up_bps"`
	DownBps    float64 `json:"down_bps"`
	Capped100M bool    `json:"capped_100m"` // 有線で 100 Mbps 付近の頭打ち (途中に 100M の機器・ケーブルの疑い)
	Error      string  `json:"error,omitempty"`
	Skipped    string  `json:"skipped,omitempty"` // 測らなかった理由 (鍵で入れない・ユーザー名が分からない等)
	// ScanSettled は裏の読み込み (Bonjour の走査・有線の名前など) が --wait までに終わったか。false なら途中の一覧で宛先を決めた
	// (有線の IP がまだ見えず Wi-Fi で測った・経路が分からない、がありうる。link を見る)
	ScanSettled bool `json:"scan_settled"`
	// ScanError は Bonjour の走査そのものの失敗 (dns-sd を起こせない等)。走査は「終わった」扱いになるので scan_settled では分からない
	ScanError string `json:"scan_error,omitempty"`
}

// runSpeed は `mac-lan-manager speedtest`: 管理対象の相手 (引数を渡せばその相手だけ) との通信速度を ssh 越しに 1 台ずつ測る。
// 宛先・経路・測らない相手の規則は画面の t と同じ (lanmgr.SpeedTargets)。画面と違って確認の板を出さない (コマンドを打つことが確認)。
// rc: 0 = 全部測れた / 1 = 測れない相手がいた (1 台でも) / 2 = 引数の誤り・知らない相手 / 130 = 止められた。
func runSpeed(args []string, stdout, stderr io.Writer, now func() time.Time, stop <-chan struct{}, newModel func() (*lanmgr.Model, error),
	test func(context.Context, sshdest.Dest, func() time.Time) speedtest.Result) int {
	fs := flag.NewFlagSet("speedtest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "JSON で出す")
	wait := fs.Duration("wait", 8*time.Second, "Bonjour の走査を待つ上限")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	m, err := newModel()
	if err != nil {
		fmt.Fprintln(stderr, "mac-lan-manager speedtest:", err)
		return 1
	}
	defer m.Close()
	ok, settled := settle(m, now, now().Add(*wait), stop, nil)
	if !ok {
		return 130
	}
	if !settled && !*asJSON {
		fmt.Fprintln(stderr, "mac-lan-manager speedtest: 🚨 裏の読み込み (Bonjour の走査・有線の名前など) が --wait までに終わらなかった (途中の一覧で宛先を決めた。経路を確かめる / --wait を延ばす)")
	}
	scanErr := m.Report().ScanError
	rc := 0
	if scanErr != "" { // 見えない相手は測れず、測れた相手も経路が分からないことがある。ヘルスチェックとしては不調
		rc = 1
		if !*asJSON {
			fmt.Fprintln(stderr, "mac-lan-manager speedtest: 🚨 Bonjour で探せない:", scanErr)
		}
	}
	targets, err := m.SpeedTargets()
	if err != nil {
		fmt.Fprintln(stderr, "mac-lan-manager speedtest:", err)
		return 1
	}
	if want := fs.Args(); len(want) > 0 {
		picked := make([]lanmgr.SpeedTarget, 0, len(want))
		for _, w := range want {
			t, why := pickTarget(targets, speedID, w)
			if why != "" {
				fmt.Fprintln(stderr, "mac-lan-manager speedtest:", why)
				return 2
			}
			if !slices.ContainsFunc(picked, func(p lanmgr.SpeedTarget) bool { return p.Host == t.Host }) { // 同じ相手を 2 回測らない
				picked = append(picked, t)
			}
		}
		targets = picked
	}
	if len(targets) == 0 {
		fmt.Fprintln(stderr, "mac-lan-manager speedtest: 管理対象の Mac が無い (台帳に登録してから)")
		return 1
	}
	reports := make([]speedReport, 0, len(targets))
	for _, t := range targets { // 1 台ずつ (同時に流すと互いの帯域を食って値が意味を失う)
		r := speedReport{Host: t.Host, Name: t.Name, IP: t.Dest.IP, Link: t.Link.Label(), ScanSettled: settled, ScanError: scanErr}
		switch {
		case t.Refusal != "":
			r.Skipped, rc = t.Refusal, 1
		default:
			var res speedtest.Result
			if untilStop(stop, func(ctx context.Context) { res = test(ctx, t.Dest, now) }) {
				return 130
			}
			r.UpBps, r.DownBps = res.UpBps, res.DownBps
			if res.Err != nil {
				r.Error, rc = res.Err.Error(), 1
			} else {
				r.Capped100M = t.Link == netinfo.LinkWired && speedtest.Capped100M(res)
			}
		}
		reports = append(reports, r)
		if !*asJSON {
			printSpeed(stdout, r)
		}
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(reports); err != nil {
			fmt.Fprintln(stderr, "mac-lan-manager speedtest:", err)
			return 1
		}
	}
	return rc
}

func speedID(t lanmgr.SpeedTarget) (host, name string) { return t.Host, t.Name }

// pickTarget は名前 w の相手を選ぶ (speedtest と ping が共有する。id は相手の台帳の鍵と別名)。ホスト名にちょうど一致すればそれ。そうでなければ .local を落とした名前か別名で選び、2 台以上に
// 当たれば断る (先に当たった方を黙って測ると、別名と短いホスト名が別の Mac で重なったときに違う相手を測った)。
// ホスト名は台帳の鍵の形 (ledger.Key: 小文字・末尾の . 無し) に揃えてから比べる。
func pickTarget[T any](targets []T, id func(T) (host, name string), w string) (T, string) {
	var zero T
	k := ledger.Key(w)
	if i := slices.IndexFunc(targets, func(t T) bool { h, _ := id(t); return h == k }); i >= 0 {
		return targets[i], ""
	}
	var hit []T
	var hosts []string
	for _, t := range targets {
		if h, n := id(t); n == w || strings.TrimSuffix(h, ".local") == k {
			hit, hosts = append(hit, t), append(hosts, h)
		}
	}
	switch len(hit) {
	case 0:
		return zero, fmt.Sprintf("管理対象に %q が無い (台帳のホスト名か別名で指定する)", w)
	case 1:
		return hit[0], ""
	}
	return zero, fmt.Sprintf("%q は %s に当たる (ホスト名で指定する)", w, strings.Join(hosts, " / "))
}

func printSpeed(w io.Writer, r speedReport) {
	head := r.Name
	if r.Name != r.Host {
		head += " (" + r.Host + ")"
	}
	switch {
	case r.Skipped != "":
		fmt.Fprintf(w, "%s  - 測らない: %s\n", head, r.Skipped)
	case r.Error != "":
		fmt.Fprintf(w, "%s  ✗ %s  [%s %s]\n", head, strings.ReplaceAll(r.Error, "\n", " / "), r.IP, or(r.Link, "経路不明"))
	default:
		fmt.Fprintf(w, "%s  ↑ %s  ↓ %s  [%s %s]", head, speedtest.Format(r.UpBps), speedtest.Format(r.DownBps), r.IP, or(r.Link, "経路不明"))
		if r.Capped100M {
			fmt.Fprint(w, "  🚨 100 Mbps 付近で頭打ち (途中に 100M の機器・ケーブル?)")
		}
		fmt.Fprintln(w)
	}
}
