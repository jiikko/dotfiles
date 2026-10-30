package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"mac-lan-manager/setup"
)

// runUpgradeAdvert は `mac-lan-manager upgrade-advert`: この Mac が前の形 (目印が setup.AdvertTag と違う) で有線の名前を広告していれば、
// 読み込まれている job の名前と IP のまま今の形で入れ直す (dotfiles の setup.sh が呼ぶ)。名前と IP を台帳や今の I/F から決め直さないのは、
// 入れ直しで広告する中身を変えないため (変えるのは初期設定の名前の手順の仕事)。広告していない・今の形・job が読み込まれていない
// (名前と IP を読めない) なら入れ直さない。plist だけがあって job が読み込まれていなければ (入れ直しの bootout の後に bootstrap が
// 落ちた跡。次のログインまで広告が無い) rc=1 で戻し方を出す: 黙ると setup.sh を回し直しても気づけない。
// rc: 0 = 入れ直した・何もしない / 1 = 読めない・入れ直せない・plist が読み込まれていない / 2 = 引数の誤り。
func runUpgradeAdvert(args []string, stdout, stderr io.Writer, read func(context.Context) (setup.NameState, error),
	plistExists func() bool, run func(context.Context, []string) error) int {
	if len(args) > 0 {
		fmt.Fprintln(stderr, "mac-lan-manager upgrade-advert: 引数は取らない")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := read(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "mac-lan-manager upgrade-advert: 広告の状態を読めない:", err)
		return 1
	}
	switch {
	case st.Tag == setup.AdvertTag:
		fmt.Fprintln(stdout, "有線の名前の広告は今の形 ("+st.Name+" → "+st.IP+")")
		return 0
	case st.Name == "" || st.IP == "": // 広告していない (plist も job も無い) と、plist だけで job が読み込まれていないは同じ値で来る
		if plistExists() {
			fmt.Fprintln(stderr, "mac-lan-manager upgrade-advert: 有線の名前の広告の plist はあるが job が読み込まれていない (次のログインまで広告しない)。"+
				"今戻すなら: launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/"+setup.WiredNameLabel+".plist")
			return 1
		}
		fmt.Fprintln(stdout, "有線の名前は広告していない (入れ直さない)")
		return 0
	}
	a := setup.NameAdvert{Name: st.Name, IP: st.IP}
	if err := a.Validate(); err != nil {
		fmt.Fprintln(stderr, "mac-lan-manager upgrade-advert: 前の形の広告の名前か IP が今の検査を通らない:", err)
		return 1
	}
	if err := run(ctx, []string{"sh", "-c", a.InstallScript()}); err != nil {
		fmt.Fprintln(stderr, "mac-lan-manager upgrade-advert: 入れ直せない:", err)
		return 1
	}
	fmt.Fprintln(stdout, "有線の名前の広告を今の形で入れ直した ("+a.Name+" → "+a.IP+"。前の目印: "+st.Tag+")")
	return 0
}
