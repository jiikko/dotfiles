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
	if !source.IsChannelID(ch) {
		return "", fmt.Errorf("チャンネル ID %q はファイル名に使えない形", ch)
	}
	return filepath.Join(s.Dir, ch+"."+kind+".json"), nil
}

// LoadRaw は ch の Raw を読む。無い・版が違うときは ok が偽 (誤りではない)。
func (s Store) LoadRaw(ch string) (r Raw, ok bool, err error) {
	ok, err = s.load(ch, "raw", &r, func() int { return r.Version })
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
	ok, err = s.load(ch, "summary", &sum, func() int { return sum.Version })
	return sum, ok, err
}

func (s Store) load(ch, kind string, v any, version func() int) (bool, error) {
	if s.Dir == "" {
		return false, nil
	}
	p, err := s.path(ch, kind)
	if err != nil {
		return false, err
	}
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
	return version() == Version, nil
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
func MsgKeys(units []board.Unit) []string {
	var keys []string
	for _, u := range units {
		for _, m := range u.Msgs {
			h := sha256.Sum256([]byte(m.Text))
			keys = append(keys, m.Ts+":"+hex.EncodeToString(h[:8]))
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
