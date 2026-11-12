package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"slack-bbs/board"
	"slack-bbs/cache"
	"slack-bbs/source"
	"slack-bbs/subs"
)

// 画面を開かずに購読・グループ・チャンネルを扱うサブコマンド (ユーザー要望 2026-11-12: CLI からも操作できるように)。
// 購読の一覧は画面の保存と同じ subs.SaveIf で書く (同じ検査を通り、読んでから別の操作が書き換えていれば書かない)。

// usageError は使い方の誤り (rc=2。誤りの文をそのまま出す)。
type usageError string

func (e usageError) Error() string { return string(e) }

// cliRC は CLI のサブコマンドの誤りを出して終了コードを返す (使い方の誤りは 2、それ以外は 1)。
func cliRC(stderr io.Writer, err error) int {
	var u usageError
	if errors.As(err, &u) {
		fmt.Fprintln(stderr, "slack-bbs:", string(u))
		return 2
	}
	return report(stderr, err)
}

// cmdSubOps は slack-bbs sub add|rm <チャンネル>...。何も変わらないときは書かない (足す: もう購読している / 外す: 購読していない
// は誤り。グループのメンバーにだけ入っているチャンネルは group rm で外す)。
func cmdSubOps(ctx context.Context, d deps, args []string, stdout io.Writer) error {
	if len(args) < 2 || (args[0] != "add" && args[0] != "rm") {
		return usageError("使い方: slack-bbs sub add|rm <チャンネル>... (チャンネルは ID・#名前・名前)")
	}
	path, cur, base, err := loadSubs(d)
	if err != nil {
		return err
	}
	if args[0] == "rm" {
		ids, err := resolveIDs(d, cur, args[1:])
		if err != nil {
			return err
		}
		next, n := subs.RemoveChannels(cur, ids...)
		if n == 0 {
			return errors.New("どれも購読していない (グループのメンバーなら slack-bbs group rm <グループ> <チャンネル> で外す)")
		}
		return saveSubs(path, next, base, stdout, fmt.Sprintf("購読を %d 件外した", n))
	}
	chs, err := resolveAll(ctx, d, args[1:])
	if err != nil {
		return err
	}
	next, n := subs.AddChannels(cur, chs...)
	if n == 0 {
		fmt.Fprintln(stdout, "どれももう購読している (変えていない)")
		return nil
	}
	return saveSubs(path, next, base, stdout, fmt.Sprintf("購読を %d 件足した", n))
}

// cmdGroup は slack-bbs group list|create|add|rm|delete|rename。
func cmdGroup(ctx context.Context, d deps, args []string, stdout io.Writer) error {
	const use = "使い方: slack-bbs group list | create <名前> [<チャンネル>...] | add <名前> <チャンネル>... | rm <名前> <チャンネル>... | delete <名前> | rename <名前> <新しい名前>"
	need := map[string]int{"list": 1, "create": 2, "add": 3, "rm": 3, "delete": 2, "rename": 3}
	if len(args) == 0 {
		return usageError(use)
	}
	if n, ok := need[args[0]]; !ok || len(args) < n || (args[0] == "delete" || args[0] == "rename" || args[0] == "list") && len(args) != n {
		return usageError(use)
	}
	path, cur, base, err := loadSubs(d)
	if err != nil {
		return err
	}
	name := clean(args[1%len(args)]) // 出力の行に出すグループの名前 (端末の制御文字を落とす)
	switch args[0] {
	case "list":
		for _, c := range cur {
			if c.IsGroup() {
				names := make([]string, 0, len(c.Members))
				for _, m := range c.Members {
					names = append(names, m.Label())
				}
				fmt.Fprintf(stdout, "%s\t%s\t%d チャンネル\t%s\n", c.Label(), c.ID, len(c.Members), strings.Join(names, " "))
			}
		}
		return nil
	case "create":
		chs, err := resolveAll(ctx, d, args[2:])
		if err != nil {
			return err
		}
		next, err := subs.CreateGroup(cur, args[1], chs...)
		if err != nil {
			return err
		}
		return saveSubs(path, next, base, stdout, fmt.Sprintf("グループ「%s」を作った (%d チャンネル)", name, len(chs)))
	case "add":
		chs, err := resolveAll(ctx, d, args[2:])
		if err != nil {
			return err
		}
		next, n, err := subs.GroupAdd(cur, args[1], chs...)
		if err != nil {
			return err
		}
		if n == 0 {
			fmt.Fprintf(stdout, "どれももうグループ「%s」に入っている (変えていない)\n", name)
			return nil
		}
		return saveSubs(path, next, base, stdout, fmt.Sprintf("グループ「%s」に %d 件入れた", name, n))
	case "rm":
		ids, err := resolveIDs(d, cur, args[2:])
		if err != nil {
			return err
		}
		next, n, err := subs.GroupRemove(cur, args[1], ids...)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("どれもグループ「%s」に入っていない", name)
		}
		return saveSubs(path, next, base, stdout, fmt.Sprintf("グループ「%s」から %d 件外した", name, n))
	case "delete":
		next, err := subs.DeleteGroup(cur, args[1])
		if err != nil {
			return err
		}
		return saveSubs(path, next, base, stdout, fmt.Sprintf("グループ「%s」を消した (チャンネルの購読は変わらない)", name))
	default: // rename
		next, err := subs.RenameGroup(cur, args[1], args[2])
		if err != nil {
			return err
		}
		return saveSubs(path, next, base, stdout, fmt.Sprintf("グループ「%s」を「%s」にした", name, clean(args[2])))
	}
}

// cmdChannels は slack-bbs channels [-all] [-json] [-refresh]: チャンネルの一覧を、購読しているか・人数・最終投稿 (前に取った値) と出す。
// 既定は参加しているチャンネルだけ。一覧は前に取ったもの (無ければ取る。-refresh で取り直す)。
func cmdChannels(ctx context.Context, d deps, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("slack-bbs channels")
	all := fs.Bool("all", false, "")
	asJSON := fs.Bool("json", false, "")
	refresh := fs.Bool("refresh", false, "")
	if err := fs.Parse(args); err != nil {
		return flagRC(stderr, err)
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "slack-bbs: 使い方: slack-bbs channels [-all] [-json] [-refresh]")
		return 2
	}
	return report(stderr, listChannels(ctx, d, *all, *asJSON, *refresh, stdout, stderr))
}

func listChannels(ctx context.Context, d deps, all, asJSON, refresh bool, stdout, stderr io.Writer) error {
	st := openStore(d, stderr)
	list, err := channelList(ctx, d, st, refresh, stderr)
	if err != nil {
		return err
	}
	_, cur, _, err := loadSubs(d)
	if err != nil {
		return err
	}
	on := map[string]bool{}
	for _, c := range cur {
		if !c.IsGroup() {
			on[c.ID] = true
		}
	}
	posts, err := st.LoadLastPosts()
	if err != nil {
		fmt.Fprintf(stderr, "slack-bbs: 前に取った最終投稿を読めなかった (最終投稿の欄は空): %s\n", clean(err.Error()))
	}
	type row struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Subscribed bool   `json:"subscribed"`
		Member     bool   `json:"member"`
		Private    bool   `json:"private"`
		Members    int    `json:"members"`
		LastPost   string `json:"last_post,omitempty"` // 最新の発言の ts (前に取った値。購読の画面か slack-bbs sub が取る)
	}
	var rows []row
	for _, c := range list {
		if c.IsArchived || !all && !c.IsMember {
			continue
		}
		rows = append(rows, row{ID: c.ID, Name: c.Name, Subscribed: on[c.ID], Member: c.IsMember, Private: c.IsPrivate, Members: c.NumMembers, LastPost: posts[c.ID].Ts})
	}
	if asJSON {
		if rows == nil {
			rows = []row{}
		}
		return writeJSON(stdout, rows)
	}
	tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "購読\tチャンネル\tID\t参加\t人数\t最終投稿")
	for _, r := range rows {
		mark, member, last := "", "", "-"
		if r.Subscribed {
			mark = "x"
		}
		if r.Member {
			member = "x"
		}
		if r.LastPost != "" {
			last = board.TsTime(r.LastPost).Format("2006-01-02")
		}
		fmt.Fprintf(tw, "%s\t#%s\t%s\t%s\t%s\t%s\n", mark, clean(r.Name), r.ID, member, strconv.Itoa(r.Members), last)
	}
	return tw.Flush()
}

// channelList はチャンネルの一覧 (前に取ったもの。無いか refresh なら slack-cli で取ってキャッシュに書く)。
func channelList(ctx context.Context, d deps, st cache.Store, refresh bool, stderr io.Writer) ([]source.Channel, error) {
	if !refresh {
		if l, ok, err := st.LoadChannels(); err == nil && ok {
			return l.Channels, nil
		}
	}
	all, err := source.Client{R: d.slack}.Channels(ctx)
	if err != nil {
		return nil, err
	}
	if err := st.SaveChannels(all, d.now()); err != nil { // 書けなくても一覧は使える (次も取り直すだけ)
		fmt.Fprintf(stderr, "slack-bbs: 🚨 チャンネルの一覧をキャッシュに書けなかった: %s\n", clean(err.Error()))
	}
	return all, nil
}

// resolveAll は引数のチャンネルを、チャンネルの一覧で引く (ID でも一覧にあれば名前を付ける)。前に取った一覧を使い、
// 無いときは、名前で指したものがあるときだけ slack-cli で取る (ID だけなら Slack を呼ばずに ID を名前にする)。
func resolveAll(ctx context.Context, d deps, args []string) ([]subs.Channel, error) {
	st := openStore(d, io.Discard)
	var list []source.Channel
	refreshed := false // 一覧を取り直したら真 (取り直すのは 1 回だけ。1 回約 1 分かかる)
	if l, ok, err := st.LoadChannels(); err == nil && ok {
		list = l.Channels
	} else if slices.ContainsFunc(args, func(a string) bool { return !source.IsChannelID(strings.TrimPrefix(a, "#")) }) {
		var err error
		if list, err = channelList(ctx, d, st, true, io.Discard); err != nil {
			return nil, err
		}
		refreshed = true
	}
	out := make([]subs.Channel, 0, len(args))
	for _, a := range args {
		c, err := subs.Resolve(list, a)
		if errors.Is(err, subs.ErrNoChannel) && !refreshed && len(list) > 0 {
			// 前に取った一覧が古い (新しく参加した・改名した) ことがあるので、1 回だけ取り直して引き直す
			refreshed = true
			fresh, ferr := channelList(ctx, d, st, true, io.Discard)
			if ferr != nil {
				return nil, fmt.Errorf("%w (一覧の取り直しにも失敗した: %w)", err, ferr)
			}
			list = fresh
			c, err = subs.Resolve(list, a)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// resolveIDs は外すチャンネルの ID を引く。名前は、今の購読 (グループのメンバーを含む) にある名前から先に引き、無ければ前に取った
// チャンネルの一覧で引く (一覧から消えたチャンネルも外せるように)。外す操作では Slack を呼ばない (購読に無いものは外せないので、
// 一覧を取り直しても結果は変わらない)。
func resolveIDs(d deps, cur []subs.Channel, args []string) ([]string, error) {
	var known []source.Channel
	for _, c := range cur {
		if !c.IsGroup() {
			known = append(known, source.Channel{ID: c.ID, Name: c.Name})
		}
		for _, m := range c.Members {
			known = append(known, source.Channel{ID: m.ID, Name: m.Name})
		}
	}
	if l, ok, err := openStore(d, io.Discard).LoadChannels(); err == nil && ok {
		known = append(known, l.Channels...)
	}
	ids := make([]string, 0, len(args))
	for _, a := range args {
		c, err := subs.Resolve(known, a)
		if err != nil {
			return nil, err
		}
		ids = append(ids, c.ID)
	}
	return ids, nil
}

// loadSubs は購読の置き場・一覧・中身の印 (saveSubs で、読んでから書き換えられていないかを照合する) を読む。
func loadSubs(d deps) (string, []subs.Channel, string, error) {
	path, err := d.subsPath()
	if err != nil {
		return "", nil, "", err
	}
	cur, base, err := subs.LoadVersion(path)
	return path, cur, base, err
}

func saveSubs(path string, next []subs.Channel, base string, stdout io.Writer, done string) error {
	if _, err := subs.SaveIf(path, next, base); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s (%s)\n", done, path)
	return nil
}
