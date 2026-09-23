package main

import (
	"fmt"
	"strconv"
	"testing"
)

// 移動の語彙 (tuikit の listnav.MotionOf) が各画面へ配線されていることを、ユーザーが触る入口
// (browseModel.handleKey。doctor だけは行を直接持たせた doctorView.handleKey) から固定する。
//
// 見るのは「別名のキーを押した結果が、一次語彙のキーを押した結果と一致する」こと。画面ごとに
// case を手書きしていたころは、画面によって効く別名が違っていた (doctor だけ ctrl+n/p が効かない等)。
// 一次語彙のキーで状態が実際に動くことも確かめる (動かない位置で比べると、一致が空振りになる)。

// motionAlias は (別名, 一次語彙, 起点を末尾にするか)。上方向は末尾から押さないと動かない。
type motionAlias struct {
	alias, canonical string
	fromBottom       bool
}

var motionAliases = []motionAlias{
	{"down", "j", false}, {"ctrl+n", "j", false},
	{"up", "k", true}, {"ctrl+p", "k", true},
	{"pgdown", "ctrl+d", false}, {"f", "ctrl+d", false}, {" ", "ctrl+d", false},
	{"pgup", "ctrl+u", true}, {"b", "ctrl+u", true}, {"shift+space", "ctrl+u", true},
	{"home", "g", true}, {"end", "G", false},
}

// motionScreen は画面ごとの組み立て方。skip はその画面が別の意味を与えているキー
// (動作層が移動層より先に捌く)。
type motionScreen struct {
	name  string
	skip  map[string]string // キー → 別の意味
	build func(t *testing.T) (press func(string), observe func() string)
}

var motionScreens = []motionScreen{
	{
		name: "コミット一覧",
		skip: map[string]string{"b": "push", " ": "パネルを開く"},
		build: func(t *testing.T) (func(string), func() string) {
			m := newTestBrowse(t, 40, map[string]CIState{}, nil)
			m.width, m.height = 100, 20
			return func(k string) { m.handleKey(k) },
				func() string { return fmt.Sprintf("cursor=%d offset=%d", m.cursor, m.offset) }
		},
	},
	{
		name: "issues 一覧",
		build: func(t *testing.T) (func(string), func() string) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			m := newTestBrowse(t, 3, map[string]CIState{}, nil)
			m.width, m.height = 100, 20
			m.issuesOv = *loadedView(manyIssues(40)...)
			return func(k string) { m.handleKey(k) },
				func() string { return fmt.Sprintf("cursor=%d", m.issuesOv.cursor) }
		},
	},
	{
		name: "status viewer",
		skip: map[string]string{" ": "stage", "b": "push (routeKeyToStatus が先に捌く)"},
		build: func(t *testing.T) (func(string), func() string) {
			m := newTestBrowse(t, 3, map[string]CIState{}, nil)
			m.width, m.height = 100, 20
			m.statusOv = *statusRowsFixture(t, 40)
			return func(k string) { m.handleKey(k) },
				func() string { return fmt.Sprintf("cursor=%d", m.statusOv.cursor) }
		},
	},
	{
		name: "doctor",
		skip: map[string]string{" ": "選択"},
		build: func(t *testing.T) (func(string), func() string) {
			v := &doctorView{}
			for i := range 40 {
				v.rows = append(v.rows, doctorRow{text: strconv.Itoa(i), selectable: true, key: rowKey(strconv.Itoa(i))})
			}
			return func(k string) { v.handleKey(k, 20) },
				func() string { return fmt.Sprintf("index=%d", v.cur.index) }
		},
	},
}

func TestScreensShareMotionVocabulary(t *testing.T) {
	for _, sc := range motionScreens {
		for _, a := range motionAliases {
			if _, ok := sc.skip[a.alias]; ok {
				continue // この画面では別の意味 (動作層が先に捌く)
			}
			t.Run(sc.name+"/"+a.alias, func(t *testing.T) {
				run := func(key string) (start, end string) {
					press, observe := sc.build(t)
					if a.fromBottom {
						press("G")
					}
					start = observe()
					press(key)
					return start, observe()
				}
				start, want := run(a.canonical)
				if start == want {
					t.Fatalf("前提: 一次語彙 %q で状態が動かない (%s)。比べる位置が悪い", a.canonical, start)
				}
				if _, got := run(a.alias); got != want {
					t.Errorf("%q の結果 %s が %q の結果 %s と違う", a.alias, got, a.canonical, want)
				}
			})
		}
	}
}
