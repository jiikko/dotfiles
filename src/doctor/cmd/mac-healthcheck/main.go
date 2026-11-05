package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"doctor/exitcode"
	"doctor/health"
	"doctor/runner"
)

func main() { os.Exit(run()) }

const usage = `usage: mac-healthcheck [-categories hw,proc,net,disk,tmux,nvim,chrome,apps] [-interval 5s] [-explicit] [-json] [-report <path>]

macOS 全体を読み取りだけで診断し、Markdown (既定) か JSON で出す。止める・消す・設定を変えることはしない。
観点・判定・直し方の提示の手順は skill mac-healthcheck、項目の正本は docs/macos-health-check.md。

終了コード (diskdoctor / svcdoctor と共通の語彙):
  0  診断できた + 注意・異常なし
  1  診断できた + 注意か異常がある
  2  引数が不正、または判定不能の項目がある (2 が 1 より優先)
  3  実行環境・出力の失敗 (JSON のエンコード / -report の書き込み)

`

// run は main の本体 (os.Exit が defer を走らせないので、main に閉じ込める)。
func run() int {
	cats := flag.String("categories", "", "調べる観点 (カンマ区切り。空なら全部)")
	interval := flag.Duration("interval", 5*time.Second, "CPU とメモリを 2 回読む間隔")
	explicit := flag.Bool("explicit", false, "重い検査 (networkQuality: 約 20 秒・数百 MB の通信) も回す")
	jsonOut := flag.Bool("json", false, "JSON で出力する")
	reportPath := flag.String("report", "", "Markdown のレポートをこのファイルにも書く (-json と併用できる。ps を 2 回走らせ直さずに両方を得る)")
	flag.Usage = func() {
		_, _ = fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "mac-healthcheck: 引数は取りません (%q)\n", flag.Arg(0))
		return exitcode.Undiagnosed
	}
	cs, err := health.ParseCategories(*cats)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "mac-healthcheck:", err)
		return exitcode.Undiagnosed
	}
	if *interval <= 0 {
		_, _ = fmt.Fprintln(os.Stderr, "mac-healthcheck: -interval は 0 より大きくする")
		return exitcode.Undiagnosed
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	home, _ := os.UserHomeDir()
	rep := health.Scan(ctx, health.Options{
		Run: runner.Exec, Categories: cs, Interval: *interval, Explicit: *explicit,
		DiagDirs: health.DefaultDiagDirs(home), Home: home,
	})
	if *reportPath != "" {
		if err := os.WriteFile(*reportPath, []byte(health.Format(health.SanitizeForDisplay(rep))), 0o600); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "mac-healthcheck:", err)
			return exitcode.EnvFailure
		}
	}
	return emit(rep, *jsonOut, os.Stdout, os.Stderr)
}

// emit は出力して終了コードを返す。終了コードは出力の分岐の外で、人が見た内容 (無害化の後) から決める。
func emit(rep health.Report, jsonOut bool, stdout, stderr io.Writer) int {
	disp := health.SanitizeForDisplay(rep)
	if jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(disp); err != nil {
			_, _ = fmt.Fprintln(stderr, "mac-healthcheck:", err)
			return exitcode.EnvFailure
		}
	} else {
		_, _ = fmt.Fprint(stdout, health.Format(disp))
	}
	return healthExitCode(disp)
}
