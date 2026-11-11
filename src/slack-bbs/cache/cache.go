// Package cache はチャンネルごとに 2 つを残す: 取得してまとめ直した発言 (Raw) と、LLM の分類 (Summary)。
//
// 起動のたびに取得と分類 (claude -p。1 チャンネルで数十秒) を待たせないため。読む画面は起動したら Raw と Summary から前回の板を
// すぐ出し、裏で取り直す。取り直した発言がすべて前回に分類済みなら、LLM を呼ばずに前回の分類を使い回す。
// 本文をディスクに残すのはユーザーの決定 (2026-11-10)。置き場は本人だけが読める (ディレクトリ 0o700・ファイル 0o600)。
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"atomicfile"
	"slack-bbs/board"
	"slack-bbs/render"
	"slack-bbs/source"
)

// Version はファイルの形の版。形を変えたら上げる (古い版のファイルは読まずに捨てたものとして扱う)。
const Version = 2

// Raw は 1 チャンネルの取得の結果。Page は板の枠 (期間・注記・名前) で、Topics は持たない (話題は Summary から組む)。
type Raw struct {
	Version int          `json:"version"`
	SavedAt time.Time    `json:"saved_at"`
	Base    string       `json:"base"` // permalink の根 (render.Page.Base は JSON に出さないので別に持つ)
	Me      string       `json:"me"`   // 取得したときの読み手 (自分の user ID)
	Page    render.Page  `json:"page"`
	Units   []board.Unit `json:"units"`
}

// Summary は 1 チャンネルの LLM の分類。Basis は分類したときの発言の鍵 (MsgKeys) で、使い回せるかの判定に使う。
// Me は分類したときの読み手 (自分の user ID)。LLM のラベル (要対応・必読) は読み手から見た判断なので、読み手が違えば使い回さない。
type Summary struct {
	Version int            `json:"version"`
	SavedAt time.Time      `json:"saved_at"`
	Model   string         `json:"model"` // 分類のしかた (llm.Classifier。モデルと指示文のハッシュ)
	Me      string         `json:"me"`
	Basis   []string       `json:"basis"`
	Resp    board.Response `json:"response"`
}

// Store はキャッシュの置き場。Dir が空ならキャッシュしない (読むと無し、書くと何もしない)。
type Store struct{ Dir string }

func (s Store) path(ch, kind string) (string, error) {
	if !source.IsBoardID(ch) {
		return "", fmt.Errorf("チャンネル ID %q はファイル名に使えない形", ch)
	}
	return filepath.Join(s.Dir, ch+"."+kind+".json"), nil
}

// LoadRaw は ch の Raw を読む。無い・版が違うときは ok が偽 (誤りではない)。
func (s Store) LoadRaw(ch string) (r Raw, ok bool, err error) {
	ok, err = s.load(ch, "raw", &r, func() bool { return r.Version == Version })
	if ok {
		if verr := board.ValidateUnits(r.Units); verr != nil {
			return Raw{}, false, fmt.Errorf("キャッシュの発言の形が壊れている: %w", verr)
		}
		r.Page.Base = r.Base
	}
	return r, ok, err
}

// LoadSummary は ch の Summary を読む。無い・版が違うときは ok が偽 (誤りではない)。
func (s Store) LoadSummary(ch string) (sum Summary, ok bool, err error) {
	ok, err = s.load(ch, "summary", &sum, func() bool { return sum.Version == Version })
	return sum, ok, err
}

func (s Store) load(ch, kind string, v any, current func() bool) (bool, error) {
	if s.Dir == "" {
		return false, nil
	}
	p, err := s.path(ch, kind)
	if err != nil {
		return false, err
	}
	return loadFile(p, v, current)
}

func loadFile(p string, v any, current func() bool) (bool, error) {
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return false, fmt.Errorf("%s を読めない: %w", p, err)
	}
	return current(), nil
}

// SaveRaw は ch の Raw を書く (Version は書くときに入れる)。
func (s Store) SaveRaw(ch string, r Raw) error {
	r.Version, r.Base = Version, r.Page.Base
	r.Page.Topics = nil
	return s.save(ch, "raw", r)
}

// SaveSummary は ch の Summary を書く (Version は書くときに入れる)。
func (s Store) SaveSummary(ch string, sum Summary) error {
	sum.Version = Version
	return s.save(ch, "summary", sum)
}

func (s Store) save(ch, kind string, v any) error {
	if s.Dir == "" {
		return nil
	}
	p, err := s.path(ch, kind)
	if err != nil {
		return err
	}
	return s.saveFile(p, v)
}

func (s Store) saveFile(p string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	return atomicfile.Write(p, b, 0o600)
}

// MsgKeys は単位の発言の鍵 (ts と本文のハッシュ) を並べたもの。本文が編集されると鍵が変わり、分類し直す対象になる。
// グループの単位は鍵の頭にチャンネルを付ける (ts はチャンネルの中でしか一意でない。単独のチャンネルの鍵は変えない)。
func MsgKeys(units []board.Unit) []string {
	var keys []string
	for _, u := range units {
		for _, m := range u.Msgs {
			h := sha256.Sum256([]byte(m.Text))
			key := m.Ts + ":" + hex.EncodeToString(h[:8])
			if u.Ch != "" {
				key = u.Ch + "/" + key
			}
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// Covers は sum が units をそのまま使える分類か: 同じモデル・同じ読み手 me で分類していて、units の発言がすべて分類したときにもある
// (新しい発言・編集された発言が無い)。期間が進んで古い発言が外れただけなら真。
//
// 単位がどれかの話題に入っているかは見ない。単位の ID は発言 (スレッドの親の ts) から決まるので、前回に無い単位が現れるときは
// 必ず鍵の無い発言を伴い、鍵の照合で足りる (返信が消える・スレッドが読めないで単位が縮むのは、前回の単位の部分集合)。見ると、LLM が単位を落とした分類 (落とした単位は未分類に出る) を一度でも残したチャンネルは、毎回分類し直し、
// 起動時に前回の板も出せなくなる。
//
// 使い回しで受け入れていること (直すなら実害の実例を添えて再提起する):
//   - 期間が進んで発言が文脈 (期間前) に回っても、LLM のラベル (要対応・必読) と並び順は前回のまま残る。機械のラベルは毎回作り直す
//   - 起動時の前回の板は、今の -since / -lookback やワークスペースと照合しない。すぐ後の読み直しで置き換わる
//   - 2 つの slack-bbs が同じチャンネルを同時に読み直すと、Raw と Summary が別の取得のものになりうる。そのときは偽になって前回の板を
//     出さないか、新しい分類を古い発言に当てる (Prune で絞る) だけで、古い分類を新しい発言に当てる形にはならない
func (sum Summary) Covers(units []board.Unit, model, me string) bool {
	if sum.Model != model || sum.Me != me {
		return false
	}
	for _, k := range MsgKeys(units) {
		if _, found := slices.BinarySearch(sum.Basis, k); !found {
			return false
		}
	}
	return true
}

// ReadVersion は既読のファイルの形の版。Raw・Summary の Version とは別に数える (板の形を変えても、既読を捨てない)。
const ReadVersion = 1

// ReadKeep は既読の印を残す期間。印の値 (読んだときの単位の最後の発言の ts) がこれより古い印は、書くときに捨てる
// (動きの無くなった単位は板に戻らないので、残すとファイルが増え続ける)。単位の ID (スレッドの親の ts) で判定しない:
// -lookback を延ばすと、古くに始まって今も返信の付くスレッドの印を書くたびに捨て、起動のたびに未読へ戻す
// 受け入れていること: -since を ReadKeep より長くすると、最後の発言が ReadKeep より古い単位の印は書くたびに捨てるので、読んでも
// 次の起動で未読に戻る。既定の期間 (2 日) では起きない。直すなら印に読んだ時刻を別に持つ (実害の実例を添えて再提起する)
const ReadKeep = 60 * 24 * time.Hour

// Read は 1 チャンネルの既読の印。Units は単位の ID → 読んだときの単位の最後の発言の ts。
type Read struct {
	Version int               `json:"version"`
	Units   map[string]string `json:"units"`
}

// LoadRead は ch の既読の印を読む。無い・版が違うときは空 (誤りではない)。
func (s Store) LoadRead(ch string) (map[string]string, error) { return s.loadMarks(ch, "read") }

// SaveRead は ch の既読の印を書く。印の値が now から ReadKeep より古い印は落とす。
func (s Store) SaveRead(ch string, units map[string]string, now time.Time) error {
	return s.saveMarks(ch, "read", units, now)
}

// LoadHidden は ch の隠した単位 (単位の ID → 隠したときの単位の最後の発言の ts) を読む。形と期限は既読の印と同じ。
func (s Store) LoadHidden(ch string) (map[string]string, error) { return s.loadMarks(ch, "hidden") }

// SaveHidden は ch の隠した単位を書く (印の値が ReadKeep より古いものは落とす: 動きの無くなった単位は板に戻らない)。
func (s Store) SaveHidden(ch string, units map[string]string, now time.Time) error {
	return s.saveMarks(ch, "hidden", units, now)
}

func (s Store) loadMarks(ch, kind string) (map[string]string, error) {
	var r Read
	ok, err := s.load(ch, kind, &r, func() bool { return r.Version == ReadVersion })
	if err != nil || !ok || r.Units == nil {
		return map[string]string{}, err
	}
	return r.Units, nil
}

func (s Store) saveMarks(ch, kind string, units map[string]string, now time.Time) error {
	keep := make(map[string]string, len(units))
	cut := board.TsOf(now.Add(-ReadKeep).Unix())
	for id, ts := range units {
		if !board.TsBefore(ts, cut) {
			keep[id] = ts
		}
	}
	return s.save(ch, kind, Read{Version: ReadVersion, Units: keep})
}

// CacheKeep は取得の結果 (Raw) と分類 (Summary) のファイルを残す期間 (ユーザーの決定 2026-11-11: 2 週間ほどで捨ててよい)。
// 書き換えの無いまま過ぎたファイル (購読をやめたチャンネル・開いていないチャンネル) を Expire が消す。既読の印のファイルは
// ReadKeep で数える: 印は -lookback (既定 30 日) の間返信の付きうるスレッドを覚えるので、14 日で消すと読み直しのたびに未読へ戻る。
const CacheKeep = 14 * 24 * time.Hour

// Expire は置き場の中で、書き換えの無いまま残す期間を過ぎたキャッシュのファイルを消し、消した数を返す。
//   - <ID>.raw.json と .summary.json は組で数え、新しい方の時刻から CacheKeep を過ぎたら両方消す。summary だけで数えない:
//     分類を使い回した読み直し (main.go の classify) は raw だけを書き直すので、毎日読んでいるチャンネルの summary も古い時刻のまま残る
//   - <ID>.read.json と .hidden.json は ReadKeep
//   - 書きかけで残った一時ファイル (atomicfile の <名前>.tmp.<乱数>。チャンネルの一覧 channels.json と最後のタブ view.json のものも) は CacheKeep
//
// この形でないファイル (slack-cli の lock など) には触らない。消す直前に時刻を取り直す (並んで起動した別の slack-bbs が
// 書き直したばかりのファイルを、先に読んだ古い時刻で消さない。組のどれかが新しくなっていたら組ごと残す。窓は残る)。1 つ消せなくても残りは続け、消せなかったものをまとめて返す。
// 時刻が未来のファイル (時計を戻した等) は、その時刻から数えるので期限が延びる (害は残るだけなので受け入れる)。
func (s Store) Expire(now time.Time) (int, error) {
	if s.Dir == "" {
		return 0, nil
	}
	ents, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var errs []error
	mtime := func(name string) (time.Time, bool) {
		info, err := os.Lstat(filepath.Join(s.Dir, name))
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
			return time.Time{}, false
		}
		return info.ModTime(), info.Mode().IsRegular()
	}
	groups := map[string]*expiring{}
	for _, e := range ents {
		if !e.Type().IsRegular() {
			continue
		}
		key, keep, ok := expireGroup(e.Name())
		if !ok {
			continue
		}
		at, ok := mtime(e.Name())
		if !ok {
			continue
		}
		g := groups[key]
		if g == nil {
			g = &expiring{keep: keep}
			groups[key] = g
		}
		g.names = append(g.names, e.Name())
		if at.After(g.newest) {
			g.newest = at
		}
	}
	n := 0
	for _, g := range groups {
		if now.Sub(g.newest) <= g.keep || g.touched(now, mtime) {
			continue
		}
		for _, name := range g.names {
			if err := os.Remove(filepath.Join(s.Dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
				continue
			}
			n++
		}
	}
	return n, errors.Join(errs...)
}

// expiring は期限をまとめて数えるファイルの組。
type expiring struct {
	names  []string
	newest time.Time
	keep   time.Duration
}

// touched は、組のどれかが期限の内に書き直されたか (消す直前に組の全部の時刻を取り直す)。1 つずつ取り直して古いものだけ消すと、
// 並んで起動した slack-bbs が分類を使い回して raw だけを書き直したとき、組が割れて分類だけが消える。
func (g *expiring) touched(now time.Time, mtime func(string) (time.Time, bool)) bool {
	for _, name := range g.names {
		if at, ok := mtime(name); ok && now.Sub(at) <= g.keep {
			return true
		}
	}
	return false
}

// expireGroup はキャッシュのファイル名を、まとめて期限を数える組の鍵と残す期間にする。キャッシュの形でなければ ok が偽。
func expireGroup(name string) (key string, keep time.Duration, ok bool) {
	if atomicfile.IsTemp(name) {
		if id, _, found := strings.Cut(name, "."); (found && source.IsBoardID(id)) || strings.HasPrefix(name, channelsFile+".") || strings.HasPrefix(name, tabFile+".") {
			return "tmp:" + name, CacheKeep, true
		}
		return "", 0, false
	}
	base, ok := strings.CutSuffix(name, ".json")
	if !ok {
		return "", 0, false
	}
	id, kind, found := strings.Cut(base, ".")
	if !found || !source.IsBoardID(id) {
		return "", 0, false
	}
	switch kind {
	case "raw", "summary":
		return "board:" + id, CacheKeep, true
	case "read":
		return "read:" + id, ReadKeep, true
	case "hidden":
		return "hidden:" + id, ReadKeep, true
	}
	return "", 0, false
}

// ChannelsVersion はチャンネルの一覧のファイルの形の版。
const ChannelsVersion = 1

// Channels は最後に取ったワークスペースのチャンネルの一覧 (購読の画面をすぐ開くため。開いたら裏で取り直す)。
// 本文は入らない (ID・名前・非公開か・参加しているか・アーカイブ済みか)。
type Channels struct {
	Version  int              `json:"version"`
	SavedAt  time.Time        `json:"saved_at"`
	Channels []source.Channel `json:"channels"`
}

// channelsFile はチャンネルの一覧の置き場。一覧そのものは Expire が消さない (購読の画面を開くたびに書き直す。書きかけの一時ファイルは消す)。
const channelsFile = "channels.json"

// LoadChannels は最後に取ったチャンネルの一覧を読む。無い・版が違うときは ok が偽 (誤りではない)。
func (s Store) LoadChannels() (c Channels, ok bool, err error) {
	if s.Dir == "" {
		return c, false, nil
	}
	ok, err = loadFile(filepath.Join(s.Dir, channelsFile), &c, func() bool { return c.Version == ChannelsVersion })
	return c, ok, err
}

// SaveChannels はチャンネルの一覧を書く。
func (s Store) SaveChannels(chs []source.Channel, now time.Time) error {
	if s.Dir == "" {
		return nil
	}
	return s.saveFile(filepath.Join(s.Dir, channelsFile), Channels{Version: ChannelsVersion, SavedAt: now, Channels: chs})
}

// TabVersion は最後に見ていたタブのファイルの形の版。
const TabVersion = 1

// tabFile は読む画面で最後に見ていたタブの置き場 (次の起動で最初に開く)。Expire はこのファイルを消さない (書きかけの一時ファイルだけを消す)。
const tabFile = "view.json"

type lastTab struct {
	Version int    `json:"version"`
	Tab     string `json:"tab"` // view.Model.Tab の値
}

// LoadTab は最後に見ていたタブを読む。無い・版が違うときは空 (誤りではない)。
func (s Store) LoadTab() (string, error) {
	if s.Dir == "" {
		return "", nil
	}
	var t lastTab
	ok, err := loadFile(filepath.Join(s.Dir, tabFile), &t, func() bool { return t.Version == TabVersion })
	if err != nil || !ok {
		return "", err
	}
	return t.Tab, nil
}

// SaveTab は最後に見ていたタブを書く。
func (s Store) SaveTab(tab string) error {
	if s.Dir == "" {
		return nil
	}
	return s.saveFile(filepath.Join(s.Dir, tabFile), lastTab{Version: TabVersion, Tab: tab})
}
