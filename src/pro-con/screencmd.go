package main

// pro-con screen — 人間の画面に今出ているものを外から読む口 (issue 443。親の設計は 441)。画面が描くたびに置く最新の 1 枚 (package relay) を読む。
//
// 🚨 読み取りだけ (441 の守ること 1)。状態の置き場に何も書かず、socket も使わない (TestScreenDoesNotWrite が固定する)。
// 入力欄に打ちかけの文も伏せずに見せる (2026-09-25 にユーザーが決めた。445 に記録。置き場は同じユーザーだけが読める)。

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"pro-con/dispatcher"
	"pro-con/relay"
)

const screenUsage = `usage: pro-con screen [--screen <id>] [--all] [--ansi] [--json] [--follow [--timeout <長さ>]] [--e2e <置き場>]
  開いている画面が 1 つならその画面を出す。複数なら一覧を出すので --screen <id> で選ぶ (--all は閉じた画面の残りも並べる)
  --ansi は色つきのまま、--json は機械が読む形、--follow は画面が描き直されるたびに出す (既定の上限 1m、画面が閉じたら終わる)
  --e2e <置き場> は e2e モードの画面を読む。どれも読むだけ (状態の置き場に何も書かない)`

// screenPoll は --follow で最新の 1 枚を読み直す間隔。
var screenPoll = 200 * time.Millisecond

func runScreen(args []string, home string, now func() time.Time, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pro-con screen", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	pick := fs.String("screen", "", "")
	all := fs.Bool("all", false, "")
	withANSI := fs.Bool("ansi", false, "")
	asJSON := fs.Bool("json", false, "")
	follow := fs.Bool("follow", false, "")
	timeout := fs.Duration("timeout", time.Minute, "")
	e2eRoot := fs.String("e2e", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *timeout <= 0 {
		_, _ = fmt.Fprintln(stderr, screenUsage)
		return 2
	}
	dir := liveDir(home)
	if *e2eRoot != "" {
		root, err := filepath.Abs(*e2eRoot)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "pro-con screen:", err)
			return 2
		}
		dir = dispatcher.E2E{Root: root}.StateDir()
	}
	screens, err := relay.List(dir)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con screen:", err)
		return 1
	}
	var shown []relay.Screen
	for _, s := range screens {
		if s.Open || *all {
			shown = append(shown, s)
		}
	}
	var target relay.Screen
	switch {
	case *pick != "":
		ok := false
		for _, s := range screens {
			if s.ID == *pick {
				target, ok = s, true
			}
		}
		if !ok {
			_, _ = fmt.Fprintf(stderr, "pro-con screen: 画面 %q の中継が無い\n", *pick)
			return 1
		}
	case len(shown) == 1 && !*all:
		target = shown[0]
	case len(shown) == 0:
		_, _ = fmt.Fprintln(stderr, "pro-con screen: 開いている画面が無い (中継を置いた画面が 1 つも無い)")
		return 1
	default:
		return listScreens(shown, now(), *asJSON, stdout, stderr)
	}
	if !*follow {
		if target.Path == "" {
			_, _ = fmt.Fprintf(stderr, "pro-con screen: 画面 %s はまだ 1 枚も描いていない\n", target.ID)
			return 1
		}
		f, err := relay.Read(target.Path)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "pro-con screen:", err)
			return 1
		}
		return printFrame(f, target.Open, now(), *withANSI, *asJSON, stdout, stderr)
	}
	return followScreen(dir, target, *timeout, now, *withANSI, *asJSON, stdout, stderr)
}

// screenEntry は一覧の 1 行 (--json の形)。
type screenEntry struct {
	ID     string            `json:"id"`
	Open   bool              `json:"open"`
	View   bool              `json:"view"`
	At     time.Time         `json:"at,omitzero"`
	Width  int               `json:"width,omitempty"`
	Height int               `json:"height,omitempty"`
	State  map[string]string `json:"state,omitempty"`
}

func listScreens(ss []relay.Screen, now time.Time, asJSON bool, stdout, stderr io.Writer) int {
	out := []screenEntry{}
	for _, s := range ss {
		e := screenEntry{ID: s.ID, Open: s.Open}
		if s.Path != "" {
			if f, err := relay.Read(s.Path); err == nil {
				e.View, e.At, e.Width, e.Height, e.State = f.View, f.At, f.Width, f.Height, f.State
			}
		}
		out = append(out, e)
	}
	if asJSON {
		return writeJSON(stdout, stderr, out)
	}
	_, _ = fmt.Fprintf(stdout, "画面が %d 個ある。--screen <id> で選ぶ\n", len(out))
	for _, e := range out {
		_, _ = fmt.Fprintln(stdout, "  "+screenHeader(e, now))
	}
	return 0
}

func screenHeader(e screenEntry, now time.Time) string {
	kind := "普通の画面"
	if e.View {
		kind = "見ているだけ (--view)"
	}
	if !e.Open {
		kind += "・閉じた画面の残り"
	}
	h := fmt.Sprintf("%s  %s", e.ID, kind)
	if !e.At.IsZero() {
		h += fmt.Sprintf("  %dx%d  最後に変わったのは %s 前  mode=%s", e.Width, e.Height, fmtAge(now.Sub(e.At)), orDashCLI(e.State["mode"]))
		if d := e.State["drawer"]; d != "" {
			h += "  詳細=" + d
		}
	} else {
		h += "  (まだ描いていない)"
	}
	return h
}

func printFrame(f relay.Frame, open bool, now time.Time, withANSI, asJSON bool, stdout, stderr io.Writer) int {
	if asJSON {
		if !withANSI {
			f.ANSI = "" // 既定は色を落とした文字だけ (Claude が読む)。--ansi で両方
		}
		return writeJSON(stdout, stderr, f)
	}
	e := screenEntry{ID: f.ID, Open: open, View: f.View, At: f.At, Width: f.Width, Height: f.Height, State: f.State}
	_, _ = fmt.Fprintln(stdout, "--- "+screenHeader(e, now))
	body := f.Plain
	if withANSI {
		body = f.ANSI + "\x1b[0m"
	}
	_, _ = fmt.Fprintln(stdout, body)
	return 0
}

// followScreen は画面の中身が変わるたびに出す (1 枚目がまだなら待つ)。画面が閉じたら (中継のファイルが消えた / flock が外れた) rc=0、
// 上限でも rc=0 で、どちらで終わったかは stderr に書く。
func followScreen(dir string, s relay.Screen, timeout time.Duration, now func() time.Time, withANSI, asJSON bool, stdout, stderr io.Writer) int {
	deadline := time.Now().Add(timeout)
	path := filepath.Join(dir, relay.Dir, s.ID+".frame")
	var last time.Time
	seen := false
	for {
		f, err := relay.Read(path)
		switch {
		case errors.Is(err, os.ErrNotExist) && !seen: // まだ 1 枚も描いていない
		case errors.Is(err, os.ErrNotExist):
			_, _ = fmt.Fprintf(stderr, "pro-con screen: 画面 %s が閉じた\n", s.ID)
			return 0
		case err != nil:
			_, _ = fmt.Fprintln(stderr, "pro-con screen:", err)
			return 1
		case !f.At.Equal(last):
			last, seen = f.At, true
			if rc := printFrame(f, true, now(), withANSI, asJSON, stdout, stderr); rc != 0 {
				return rc
			}
		}
		if !stillOpen(dir, s.ID) {
			_, _ = fmt.Fprintf(stderr, "pro-con screen: 画面 %s が閉じた\n", s.ID)
			return 0
		}
		if time.Now().After(deadline) {
			_, _ = fmt.Fprintf(stderr, "pro-con screen: 上限 %v に達したので終わる (画面 %s は開いたまま)\n", timeout, s.ID)
			return 0
		}
		time.Sleep(screenPoll)
	}
}

func stillOpen(dir, id string) bool {
	ss, err := relay.List(dir)
	if err != nil {
		return false
	}
	for _, s := range ss {
		if s.ID == id {
			return s.Open
		}
	}
	return false
}
