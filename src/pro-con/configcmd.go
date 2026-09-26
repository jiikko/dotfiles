package main

// pro-con config — 動いている dispatcher を止めずに PG の枠と PM の数を変える口 (issue 456)。受付の箱に「設定を変える」依頼を置くだけで、
// 状態の置き場 (settings.json) へ書くのは dispatcher (store.Apply)。show は読むだけ。優先の順は store/settings.go の doc。

import (
	"cmp"
	"fmt"
	"io"
	"strconv"
	"strings"

	"pro-con/store"
)

const configUsage = `usage: pro-con config <操作> ...   (受付の箱に依頼を置く。適用は dispatcher の次の Tick。止めずに効く)
  set limit <n>      同時に動かす PG の上限 (1 以上。dispatcher の --limit より優先。利用枠の絞り 80% で 1 本 / 95% で 0 本はこれより優先)
  set usage on|off   利用枠 (5 時間・週) を見て PG を絞るか (既定 on。off にすると枠が 80% を超えても上限まで起動する)
  set pm <n>         PM の数 (今は 1 だけ。2 以上は 415 の論点 6 が決まるまで受けない)
  set review claude|codex
                     敵対的レビューの担い手 (既定 claude。~/.config/pro-con/config.toml の review より優先。起動済みの PG の指示は変わらない)
  unset <名前>       設定を消す (limit なら --limit / 既定 2 に戻る)
  show               今の値と出どころ (読むだけ)`

func runConfig(args []string, dir string, stdout, stderr io.Writer) int {
	usage := func(err error) int {
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "pro-con config:", err)
		}
		_, _ = fmt.Fprintln(stderr, configUsage)
		return 2
	}
	if len(args) == 0 {
		return usage(nil)
	}
	var key, value string
	switch args[0] {
	case "show":
		if len(args) != 1 {
			return usage(nil)
		}
		return showConfig(dir, stdout, stderr)
	case "set":
		if len(args) != 3 {
			return usage(nil)
		}
		key, value = args[1], args[2]
		if strings.TrimSpace(value) == "" {
			return usage(fmt.Errorf("値が空 (消すなら pro-con config unset %s)", key))
		}
	case "unset":
		if len(args) != 2 {
			return usage(nil)
		}
		key = args[1]
	default:
		return usage(fmt.Errorf("知らない操作 %q", args[0]))
	}
	if _, err := store.CheckSetting(key, value); err != nil { // 置く前に弾く (dispatcher でも同じ検査で除ける)
		return usage(err)
	}
	id, err := store.Submit(dir, store.Request{Kind: store.KindConfig, Key: key, Value: value})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con config: 受付の箱に置けない:", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, id)
	return 0
}

// showConfig は設定・dispatcher が最後に使った値・適用待ちを出す。
func showConfig(dir string, stdout, stderr io.Writer) int {
	s, serr := store.LoadSettings(dir)
	ds, ok, derr := store.LoadDispatcherState(dir)
	rc := 0
	if serr != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con config:", serr)
		rc = 1
	}
	if derr != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con config: dispatcher の様子を読めない:", derr)
		rc = 1
	}
	set := func(n int) string {
		if n == 0 {
			return "(設定なし)"
		}
		return strconv.Itoa(n)
	}
	_, _ = fmt.Fprintf(stdout, "limit  設定 %s", set(s.Limit))
	if ok {
		_, _ = fmt.Fprintf(stdout, " / dispatcher が使っている上限 %d (%s) / 今の枠 %d", ds.Limit, orDashCLI(ds.LimitFrom), ds.Cap)
		if ds.Why != "" {
			_, _ = fmt.Fprintf(stdout, " (%s)", ds.Why)
		}
		_, _ = fmt.Fprintf(stdout, " / 最後の Tick %s", ds.Tick.Local().Format("01-02 15:04:05"))
	} else {
		_, _ = fmt.Fprint(stdout, " / dispatcher はまだ 1 度も回っていない")
	}
	_, _ = fmt.Fprintln(stdout)
	usage := "on (枠で絞る。既定)"
	if s.UsageOff {
		usage = "off (枠で絞らない)"
	}
	_, _ = fmt.Fprintf(stdout, "usage  %s\n", usage)
	_, _ = fmt.Fprintf(stdout, "pm     設定 %s / PM は 1 つで動く (2 以上は未対応なので、今は dispatcher が読まない)\n", set(s.PMs))
	_, _ = fmt.Fprintf(stdout, "review 設定 %s", cmp.Or(s.Review, "(設定なし)"))
	if !ok || ds.Review == "" { // dispatcher が回る前・前の版の様子: config.toml の review は dispatcher が起動のときに読むので、ここでは分からない
		_, _ = fmt.Fprint(stdout, " / dispatcher の担い手はまだ分からない (設定が無ければ config.toml の review か既定 claude)")
	} else {
		_, _ = fmt.Fprintf(stdout, " / dispatcher が使っている担い手 %s (%s)", ds.Review, ds.ReviewFrom)
		switch {
		case ds.Codex != "":
			_, _ = fmt.Fprintf(stdout, " / codex %s", ds.Codex)
		case ds.CodexErr != "":
			_, _ = fmt.Fprintf(stdout, " / codex を解けない (codex の設定でも Claude で代わりに回す): %s", ds.CodexErr)
		}
	}
	_, _ = fmt.Fprintln(stdout)
	if _, n := store.PendingCounts(dir); n > 0 {
		_, _ = fmt.Fprintf(stdout, "適用待ちの設定の依頼 %d 件 (dispatcher の次の Tick で使う。pro-con dispatcher が動いているか: pro-con ps)\n", n)
	}
	return rc
}
