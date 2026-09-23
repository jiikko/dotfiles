package main

import (
	"fmt"
	"strconv"
	"testing"
	"tuikit/listnav"
)

// 移動の語彙 (tuikit の listnav.MotionOf) が各画面へ配線されていることを、ユーザーが触る入口
// (browseModel.handleKey) から固定する。対象は一覧を持つ 4 画面 (コミット一覧 / issues / status /
// doctor)。job パネル・job 詳細・pager の配線は既存のテスト (TestBrowseJobDetailPopup 等) が持つ。
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
	{"pgdown", "ctrl+d", false}, {"f", "ctrl+d", false}, {" ", "ctrl+d", false}, {"space", "ctrl+d", false},
	{"pgup", "ctrl+u", true}, {"b", "ctrl+u", true}, {"shift+space", "ctrl+u", true},
	{"home", "g", true}, {"end", "G", false},
}

// motionProbe は組み立てた画面の操作と観測。pos は半ページで動く量 (カーソル行か offset)、
// half はその画面の半ページの期待値 (listnav.Half(その画面の表示行数))。
type motionProbe struct {
	press   func(string)
	observe func() string
	pos     func() int
	half    int
}

// motionScreen は画面ごとの組み立て方。skip はその画面が別の意味を与えているキー
// (動作層が移動層より先に捌く)。
type motionScreen struct {
	name  string
	skip  map[string]string // キー → 別の意味
	build func(t *testing.T) motionProbe
}

var motionScreens = []motionScreen{
	{
		name: "コミット一覧",
		skip: map[string]string{"b": "push", " ": "パネルを開く", "space": "パネルを開く"},
		build: func(t *testing.T) motionProbe {
			m := newTestBrowse(t, 40, map[string]CIState{}, nil)
			m.width, m.height = 100, 20
			return motionProbe{
				press:   func(k string) { m.handleKey(k) },
				observe: func() string { return fmt.Sprintf("cursor=%d offset=%d", m.cursor, m.offset) },
				pos:     func() int { return m.offset }, // この面の半ページはビューポートを送る
				half:    listnav.Half(m.pageSize()),
			}
		},
	},
	{
		name: "issues 一覧",
		build: func(t *testing.T) motionProbe {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			m := newTestBrowse(t, 3, map[string]CIState{}, nil)
			m.width, m.height = 100, 20
			m.issuesOv = *loadedView(manyIssues(40)...)
			return motionProbe{
				press:   func(k string) { m.handleKey(k) },
				observe: func() string { return fmt.Sprintf("cursor=%d", m.issuesOv.cursor) },
				pos:     func() int { return m.issuesOv.cursor },
				half:    listnav.Half(m.issuesOv.visibleRows(m.issuesOpts().viewport())),
			}
		},
	},
	{
		name: "status viewer",
		skip: map[string]string{" ": "stage", "space": "stage", "b": "push (routeKeyToStatus が先に捌く)"},
		build: func(t *testing.T) motionProbe {
			m := newTestBrowse(t, 3, map[string]CIState{}, nil)
			m.width, m.height = 100, 20
			m.statusOv = *statusRowsFixture(t, 40)
			return motionProbe{
				press:   func(k string) { m.handleKey(k) },
				observe: func() string { return fmt.Sprintf("cursor=%d", m.statusOv.cursor) },
				pos:     func() int { return m.statusOv.cursor },
				half:    listnav.Half(max(m.statusOpts().viewport().page-1, 1)), // listKey と同じ行数
			}
		},
	},
	{
		name: "doctor",
		skip: map[string]string{" ": "選択", "space": "選択"},
		build: func(t *testing.T) motionProbe {
			m := newTestBrowse(t, 3, map[string]CIState{}, nil)
			m.width, m.height = 100, 20
			m.doctorOv = doctorView{shown: true}
			for i := range 40 {
				m.doctorOv.rows = append(m.doctorOv.rows, doctorRow{text: strconv.Itoa(i), selectable: true, key: rowKey(strconv.Itoa(i))})
			}
			return motionProbe{
				press:   func(k string) { m.handleKey(k) },
				observe: func() string { return fmt.Sprintf("index=%d", m.doctorOv.cur.index) },
				pos:     func() int { return m.doctorOv.cur.index },
				half:    listnav.Half(m.pageSize()), // routeKeyToDoctor が渡す page
			}
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
					p := sc.build(t)
					if a.fromBottom {
						p.press("G")
					}
					start = p.observe()
					p.press(key)
					return start, p.observe()
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

// 半ページの移動量は、どの画面でも listnav.Half(その画面の表示行数)。「別名 = 一次語彙」だけでは
// 両方が同じ適用を通るので、移動量そのものを変えても緑のまま通る (敵対レビューで実測)。
func TestScreensHalfPageMovesHalfRows(t *testing.T) {
	for _, sc := range motionScreens {
		t.Run(sc.name, func(t *testing.T) {
			p := sc.build(t)
			if p.half < 2 {
				t.Fatalf("前提: 半ページが %d 行しかない (移動量の違いを見分けられない)", p.half)
			}
			before := p.pos()
			p.press("ctrl+d")
			if got := p.pos() - before; got != p.half {
				t.Errorf("ctrl+d で %d 動いた, want %d (listnav.Half)", got, p.half)
			}
			p.press("ctrl+u")
			if got := p.pos(); got != before {
				t.Errorf("ctrl+u で起点へ戻らない: %d, want %d", got, before)
			}
		})
	}
}
