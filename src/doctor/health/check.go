// Package health は macOS 全体の健康診断 (ネットワーク・プロセス・ディスク・ハードウェアと寿命・常用アプリ) の
// 材料を集めて判定する。CLI は bin/mac-healthcheck、手順 (観点を選ぶ → レポート → 直すかを聞く) は skill mac-healthcheck。
//
// 読み取りだけで、止める・消す・設定を変える経路を持たない。直し方は Check.Fix に「人が打つコマンド」として載せるだけ。
// 項目・判定・出力の契約の正本は docs/macos-health-check.md。
package health

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"doctor/runner"
)

// Status は 1 項目の判定。
type Status string

const (
	StatusOK      Status = "ok"
	StatusWarn    Status = "warn"    // 今は壊れていないが、放置すると困る
	StatusBad     Status = "bad"     // 対処が要る
	StatusUnknown Status = "unknown" // コマンドが無い・失敗した・出力が読めない。ok に丸めない
	StatusInfo    Status = "info"    // 数字を出すだけで判定しない
)

// Category は観点。skill の最初の質問の選択肢と 1 対 1。
type Category string

const (
	CatNet    Category = "net"
	CatProc   Category = "proc"
	CatDisk   Category = "disk"
	CatHW     Category = "hw"
	CatTmux   Category = "tmux"
	CatNvim   Category = "nvim"
	CatChrome Category = "chrome"
	CatApps   Category = "apps"
)

// AllCategories は既定で回す観点 (表示の順でもある)。
var AllCategories = []Category{CatHW, CatProc, CatNet, CatDisk, CatTmux, CatNvim, CatChrome, CatApps}

// Check は 1 項目の結果。
type Check struct {
	Category Category `json:"category"`
	ID       string   `json:"id"` // 安定キー (skill が「どれを直すか」を指すのに使う)
	Label    string   `json:"label"`
	Value    string   `json:"value"`
	Status   Status   `json:"status"`
	Note     string   `json:"note,omitempty"` // 判定の理由。判定不能なら何が取れなかったか
	Fix      string   `json:"fix,omitempty"`  // 直し方 (人が打つコマンドか操作)。この package は実行しない
}

// Report は 1 回の診断の結果。
type Report struct {
	Categories []Category `json:"categories"`
	Checks     []Check    `json:"checks"`
	ScannedAt  time.Time  `json:"scanned_at"`
}

// Options は Scan の依存。テストは Run / Now / Sleep / DiagDirs / Home を差し替える。
type Options struct {
	Run        runner.Runner
	Now        func() time.Time
	Sleep      func(ctx context.Context, d time.Duration) error
	Categories []Category
	// Interval は CPU とメモリを 2 回読む間隔。ps の %CPU は直近の平均なので 1 回では張り付きと瞬間の山を区別できない。
	Interval time.Duration
	// Explicit は重い検査 (networkQuality) も回すか。既定では回さず「未検査」と出す。
	Explicit bool
	// DiagDirs は DiagnosticReports の置き場 (パニック・クラッシュ・資源超過の記録)。
	DiagDirs []string
	Home     string
}

// DefaultDiagDirs は DiagnosticReports の置き場。
func DefaultDiagDirs(home string) []string {
	return []string{"/Library/Logs/DiagnosticReports", home + "/Library/Logs/DiagnosticReports"}
}

// ParseCategories は "net,proc" の形を読む。空なら全部。
func ParseCategories(s string) ([]Category, error) {
	if strings.TrimSpace(s) == "" {
		return slices.Clone(AllCategories), nil
	}
	var out []Category
	for f := range strings.SplitSeq(s, ",") {
		c := Category(strings.TrimSpace(f))
		if !slices.Contains(AllCategories, c) {
			return nil, fmt.Errorf("知らない観点: %q (使えるもの: %s)", c, joinCats(AllCategories))
		}
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out, nil
}

func joinCats(cs []Category) string {
	ss := make([]string, len(cs))
	for i, c := range cs {
		ss[i] = string(c)
	}
	return strings.Join(ss, ",")
}

// SleepCtx は ctx で中断できる待ち (Options.Sleep の既定)。
func SleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Scan は選ばれた観点を順に調べる。
func Scan(ctx context.Context, o Options) Report {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Sleep == nil {
		o.Sleep = SleepCtx
	}
	if o.Interval <= 0 {
		o.Interval = 5 * time.Second
	}
	if len(o.Categories) == 0 {
		o.Categories = slices.Clone(AllCategories)
	}
	if o.Home == "" {
		o.Home, _ = os.UserHomeDir()
	}
	s := &scanner{o: o}
	rep := Report{Categories: o.Categories, ScannedAt: o.Now()}
	for _, c := range AllCategories { // 表示の順は固定 (選んだ順に依らない)
		if !slices.Contains(o.Categories, c) {
			continue
		}
		var cs []Check
		switch c {
		case CatHW:
			cs = s.hw(ctx)
		case CatProc:
			cs = s.proc(ctx)
		case CatNet:
			cs = s.net(ctx)
		case CatDisk:
			cs = s.disk(ctx)
		case CatTmux:
			cs = s.tmux(ctx)
		case CatNvim:
			cs = s.nvim(ctx)
		case CatChrome:
			cs = s.chrome(ctx)
		case CatApps:
			cs = s.apps(ctx)
		}
		for i := range cs {
			cs[i].Category = c
		}
		rep.Checks = append(rep.Checks, cs...)
	}
	return rep
}

// scanner は 1 回の Scan の中で共有する読み取り結果 (ps の 2 回分) を持つ。
type scanner struct {
	o       Options
	snapErr error
	snap    *psSnapshot
}

// cmd は 1 回の外部コマンド。rc と起動の失敗を分けて返す。
func (s *scanner) cmd(ctx context.Context, timeout time.Duration, name string, args ...string) (stdout, stderr string, rc int, err error) {
	return runner.WithTimeout(ctx, s.o.Run, timeout, name, args...)
}

func unknown(id, label, note string) Check {
	return Check{ID: id, Label: label, Value: "取れない", Status: StatusUnknown, Note: note}
}

// cmdNote は判定不能の理由を 1 行にする (stderr の先頭行を添える)。
func cmdNote(name string, rc int, err error, stderr string) string {
	if err != nil {
		return fmt.Sprintf("%s を起動できない: %v", name, err)
	}
	first, _, _ := strings.Cut(strings.TrimSpace(stderr), "\n")
	// stderr はパスを含むことがある (lsof の can't stat(/Users/<名前>/…))。レポートにユーザー名を出さない
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		first = regexp.MustCompile(regexp.QuoteMeta(home)+`(/|\)|$|\s)`).ReplaceAllString(first, "~$1")
	}
	if first == "" {
		return fmt.Sprintf("%s が rc=%d", name, rc)
	}
	return fmt.Sprintf("%s が rc=%d: %s", name, rc, first)
}
