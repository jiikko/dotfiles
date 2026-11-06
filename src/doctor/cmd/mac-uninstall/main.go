package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"doctor/exitcode"
	"doctor/runner"
	"doctor/uninstall"
	"github.com/jiikko/dotfiles/src/termsafe"
	"safefs"
)

// dupscan:allow intentional main は run への 1 行の委譲 (run が出力先を引数で受け、テストから呼べる形)
func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

const usage = `usage:
  mac-uninstall list [-json]
  mac-uninstall plan -out <dir> [-app <bundle ID,...>] [-data <bundle ID,...>] [-formula <名前,...>] [-json]
  mac-uninstall apply -plan <dir>/plan-<ID>.json -approve <ID> [-json]

list と plan は何も動かさない。plan は <dir>/plan-<ID>.json を書き、apply はその ID の承認がある計画だけを実行する。
-data に挙げたアプリだけ、設定とデータ (Application Support・Preferences・Containers) も片付ける (既定は残す)。
brew の cask / formula は brew uninstall で、それ以外はゴミ箱へ移す (rm はしない)。手順は skill mac-uninstall。

終了コード:
  0  成功 (apply: 計画のものを全部片付けた)
  1  apply: 片付けられなかった・止めたものがある (結果の skipped / failed)
  2  引数が不正 / 計画を作れない・使えない (動いている・古い・ID が違う) / 一覧の一部が取れない
  3  実行環境・出力の失敗
`

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return exitcode.Undiagnosed
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	home, err := os.UserHomeDir()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "mac-uninstall:", err)
		return exitcode.EnvFailure
	}
	o := uninstall.Options{Run: runner.Exec, Home: home, AppDirs: uninstall.DefaultAppDirs(home)}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { _, _ = fmt.Fprint(stderr, usage) }
	jsonOut := fs.Bool("json", false, "JSON で出力する")
	switch args[0] {
	case "list":
		if fs.Parse(args[1:]) != nil || fs.NArg() > 0 {
			return exitcode.Undiagnosed
		}
		inv := uninstall.SanitizeInventory(uninstall.List(ctx, o))
		rc := exitcode.NoFindings
		if len(inv.Notes) > 0 {
			rc = exitcode.Undiagnosed
		}
		return emit(stdout, stderr, *jsonOut, inv, func() string { return uninstall.FormatInventory(inv, time.Now()) }, rc)
	case "plan":
		out := fs.String("out", "", "計画を書く dir")
		apps := fs.String("app", "", "片付けるアプリの bundle ID (カンマ区切り)")
		data := fs.String("data", "", "設定とデータも片付けるアプリの bundle ID (-app の部分集合)")
		formulae := fs.String("formula", "", "片付ける formula (カンマ区切り)")
		if fs.Parse(args[1:]) != nil || fs.NArg() > 0 || *out == "" {
			_, _ = fmt.Fprint(stderr, usage)
			return exitcode.Undiagnosed
		}
		p, err := uninstall.BuildPlan(ctx, o, uninstall.Selection{Apps: split(*apps), Data: split(*data), Formulae: split(*formulae)})
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "mac-uninstall: 計画を作れない:", termsafe.PlainLine(err.Error()))
			return exitcode.Undiagnosed
		}
		path, err := uninstall.WritePlan(*out, p)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "mac-uninstall:", err)
			return exitcode.EnvFailure
		}
		_, _ = fmt.Fprintf(stderr, "plan: %s\nid: %s\n", path, p.ID)
		disp := uninstall.SanitizePlan(p)
		return emit(stdout, stderr, *jsonOut, disp, func() string { return uninstall.FormatPlan(disp) }, exitcode.NoFindings)
	case "apply":
		planPath := fs.String("plan", "", "計画のファイル")
		approve := fs.String("approve", "", "承認した計画の ID")
		if fs.Parse(args[1:]) != nil || fs.NArg() > 0 || *planPath == "" || *approve == "" {
			_, _ = fmt.Fprint(stderr, usage)
			return exitcode.Undiagnosed
		}
		p, err := uninstall.ReadPlan(*planPath, *approve, time.Now())
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "mac-uninstall: 止めた:", termsafe.PlainLine(err.Error()))
			return exitcode.Undiagnosed
		}
		res := uninstall.SanitizeResult(uninstall.Apply(ctx, uninstall.ApplyOptions{Options: o, TrashRoot: home + "/.Trash", Guard: safefs.ProductionGuard}, p))
		rc := exitcode.NoFindings
		if len(res.Skipped)+len(res.Failed) > 0 {
			rc = exitcode.Findings
		}
		return emit(stdout, stderr, *jsonOut, res, func() string { return formatResult(res) }, rc)
	}
	_, _ = fmt.Fprintf(stderr, "mac-uninstall: 知らないサブコマンド %q\n%s", args[0], usage)
	return exitcode.Undiagnosed
}

func split(s string) []string {
	var out []string
	for f := range strings.SplitSeq(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// emit は出力の分岐の外で終了コードを決める (text と -json で同じ rc)。
func emit(stdout, stderr io.Writer, jsonOut bool, v any, text func() string, rc int) int {
	if jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(v); err != nil {
			_, _ = fmt.Fprintln(stderr, "mac-uninstall:", err)
			return exitcode.EnvFailure
		}
		return rc
	}
	_, _ = fmt.Fprint(stdout, text())
	return rc
}

func formatResult(r uninstall.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "計画 %s を実行した。片付けた %d / 止めた %d / 失敗 %d / 手で確かめる %d\n", r.PlanID, len(r.Done), len(r.Skipped), len(r.Failed), len(r.Manual))
	if r.Trash != "" {
		fmt.Fprintf(&b, "ゴミ箱の移し先: %s\n", r.Trash)
	}
	for _, d := range r.Done {
		fmt.Fprintf(&b, "  済 %s %s\n", d.Action, d.Path)
	}
	for _, s := range r.Skipped {
		fmt.Fprintf(&b, "  止めた %s %s: %s\n", s.Owner, s.Path, s.Reason)
	}
	for _, s := range r.Failed {
		fmt.Fprintf(&b, "  失敗 %s %s: %s\n", s.Owner, s.Path, s.Reason)
	}
	for _, m := range r.Manual {
		fmt.Fprintf(&b, "  手で %s: %s\n", m.Path, m.Note)
	}
	return b.String()
}
