package subs

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jiikko/dotfiles/src/termsafe"
	"slack-bbs/source"
)

// 購読の一覧 (Load / Save の並び: チャンネル、その後ろにグループ) を、画面を開かずに変える操作 (slack-bbs sub add などの CLI)。
// どれも新しい一覧を返し、渡された一覧は書き換えない。保存は呼び出し側が Save で行う (画面の保存と同じ検査を通る)。

// ErrNoChannel は、引数がチャンネルの一覧のどれにも当たらないこと (古い一覧なら取り直して引き直す)。
var ErrNoChannel = errors.New("チャンネルが無い")

// Resolve は引数 (チャンネル ID・「#名前」・「名前」) を、チャンネルの一覧 all から引く。名前を先に引く (数字だけの名前 #2024 を
// ID と取り違えない)。ID は一覧にあるものだけを受ける (打ち間違えた ID を購読に入れない)。一覧が空 (まだ取っていない) なら、
// ID の形のものは名前を ID のまま受ける。見つからなければ、名前に含む候補を 5 件まで添えて ErrNoChannel を包んで返す。
func Resolve(all []source.Channel, arg string) (Channel, error) {
	name := strings.TrimPrefix(strings.TrimSpace(arg), "#")
	if name == "" {
		return Channel{}, errors.New("チャンネルが空")
	}
	for _, c := range all {
		if c.Name == name {
			return Channel{ID: c.ID, Name: c.Name}, nil
		}
	}
	for _, c := range all {
		if c.ID == name {
			return Channel{ID: c.ID, Name: c.Name}, nil
		}
	}
	if len(all) == 0 && source.IsChannelID(name) {
		return Channel{ID: name, Name: name}, nil
	}
	var near []string
	for _, c := range all {
		if strings.Contains(c.Name, name) && len(near) < 5 {
			near = append(near, "#"+c.Name)
		}
	}
	if len(near) > 0 {
		return Channel{}, fmt.Errorf("%w: %s (近い名前: %s)", ErrNoChannel, termsafe.PlainLine(arg), strings.Join(near, " "))
	}
	return Channel{}, fmt.Errorf("%w: %s (slack-bbs channels -all で一覧を見る)", ErrNoChannel, termsafe.PlainLine(arg))
}

// AddChannels は chs を購読に足す (購読済みのものは飛ばす)。足した数を返す。
func AddChannels(cur []Channel, chs ...Channel) ([]Channel, int) {
	chans, groups := split(cur)
	chans, n := addMembers(chans, chs)
	return append(chans, groups...), n
}

// RemoveChannels は ids の購読を外す (グループのメンバーは変えない)。外した数を返す。
func RemoveChannels(cur []Channel, ids ...string) ([]Channel, int) {
	chans, groups := split(cur)
	before := len(chans)
	chans = slices.DeleteFunc(chans, func(c Channel) bool { return slices.Contains(ids, c.ID) })
	return append(chans, groups...), before - len(chans)
}

// CreateGroup は名前 name のグループを members で作る (同じ名前のグループがあれば誤り)。
func CreateGroup(cur []Channel, name string, members ...Channel) ([]Channel, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("グループの名前が空")
	}
	if findGroup(cur, name) != -1 {
		return nil, fmt.Errorf("グループ「%s」はもうある (slack-bbs group add で入れる)", termsafe.PlainLine(name))
	}
	g := Channel{ID: NewGroupID(), Name: name}
	g.Members, _ = addMembers(nil, members)
	return append(slices.Clone(cur), g), nil
}

// GroupAdd はグループ name に members を入れる (入っているものは飛ばす)。入れた数を返す。
func GroupAdd(cur []Channel, name string, members ...Channel) ([]Channel, int, error) {
	return editGroup(cur, name, func(g *Channel) int {
		var n int
		g.Members, n = addMembers(g.Members, members)
		return n
	})
}

// GroupRemove はグループ name から ids を外す。外した数を返す。
func GroupRemove(cur []Channel, name string, ids ...string) ([]Channel, int, error) {
	return editGroup(cur, name, func(g *Channel) int {
		before := len(g.Members)
		g.Members = slices.DeleteFunc(g.Members, func(c Channel) bool { return slices.Contains(ids, c.ID) })
		return before - len(g.Members)
	})
}

// DeleteGroup はグループ name を消す (メンバーのチャンネルの購読は変わらない)。
func DeleteGroup(cur []Channel, name string) ([]Channel, error) {
	i := findGroup(cur, name)
	if i < 0 {
		return nil, noGroup(cur, name, i)
	}
	return slices.Delete(slices.Clone(cur), i, i+1), nil
}

// RenameGroup はグループ old の名前を name に変える (ID は変えないので、既読とキャッシュはそのまま)。
func RenameGroup(cur []Channel, old, name string) ([]Channel, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("グループの名前が空")
	}
	if findGroup(cur, name) != -1 {
		return nil, fmt.Errorf("グループ「%s」はもうある", termsafe.PlainLine(name))
	}
	out, _, err := editGroup(cur, old, func(g *Channel) int { g.Name = name; return 1 })
	return out, err
}

func split(cur []Channel) (chans, groups []Channel) {
	for _, c := range cur {
		if c.IsGroup() {
			groups = append(groups, c)
		} else {
			chans = append(chans, c)
		}
	}
	return chans, groups
}

// findGroup はグループを名前か ID で引く (無ければ -1)。同じ名前が 2 つあれば -2 (どちらか決められない。ID で指してもらう)。
func findGroup(cur []Channel, name string) int {
	at := -1
	for i, c := range cur {
		switch {
		case !c.IsGroup():
		case c.ID == name:
			return i
		case c.Name == name && at >= 0:
			return -2
		case c.Name == name:
			at = i
		}
	}
	return at
}

func noGroup(cur []Channel, name string, at int) error {
	name = termsafe.PlainLine(name)
	if at == -2 {
		return fmt.Errorf("グループ「%s」が 2 つ以上ある (slack-bbs group list の ID で指す)", name)
	}
	var names []string
	for _, c := range cur {
		if c.IsGroup() {
			names = append(names, c.Name)
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("グループ「%s」は無い (グループはまだ 1 つも無い)", name)
	}
	return fmt.Errorf("グループ「%s」は無い (あるのは: %s)", name, strings.Join(names, " "))
}

// editGroup はグループ name の写しを f で変えた一覧を返す (渡された一覧のメンバーの配列は書き換えない)。
func editGroup(cur []Channel, name string, f func(*Channel) int) ([]Channel, int, error) {
	i := findGroup(cur, name)
	if i < 0 {
		return nil, 0, noGroup(cur, name, i)
	}
	out := slices.Clone(cur)
	g := out[i]
	g.Members = slices.Clone(g.Members)
	n := f(&g)
	out[i] = g
	return out, n, nil
}

func addMembers(members, add []Channel) ([]Channel, int) {
	n := 0
	for _, c := range add {
		if !slices.ContainsFunc(members, func(x Channel) bool { return x.ID == c.ID }) {
			members = append(members, Channel{ID: c.ID, Name: c.Name})
			n++
		}
	}
	return members, n
}
