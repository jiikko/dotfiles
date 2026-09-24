package live

// pro-con が起動した session の記録。本物のモードは、ここにある session だけを扱う
// (Desktop や他の shell で立ち上げた session は見せず、触れる経路も作らない。2026-09-24 のユーザーの方針)。
//
// 🚨 これは「pro-con が外の session に触らない」ための仕組みで、「外から pro-con の session に触らせない」は作れない
// (claude --bg の session はどの shell からも claude attach / stop できる)。外からの操作の検出は issue 427。

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// RegistryFile は記録のファイル名 (本物のモードの状態の置き場の下。模擬とは別の場所)。
const RegistryFile = "sessions.json"

// Owned は pro-con が起動した session 1 本。照合は「記録にある欄が全部一致し、PID もある」(owns)。
// 🚨 PID は必須。PID が無いと、pro-con が起動した session を他の shell で同じ session id のまま再開 (--resume) したものまで
// 「自分のもの」と見なす (敵対的レビュー 2026-09-24 の P2 / 2 周目の P1)。PID は session ごとの worker (`claude bg-spare …`) で、
// 落ちて自動で再開されると変わる (425 の実測)。変わったら pro-con の session も一覧から外れる (失敗側に倒れる)。
// 自動の再開なら記録を書き直し、外での再開なら書き直さない、の区別は 427 が行う。
type Owned struct {
	ID        string    `json:"id,omitempty"`
	SessionID string    `json:"sessionId,omitempty"`
	PID       int       `json:"pid,omitempty"`    // pro-con が起動・再開したプロセス (外で再開されたものは pid が違うので外れる)
	CardID    string    `json:"cardId,omitempty"` // どのカードのために起動したか (427 が書く)
	StartedAt time.Time `json:"startedAt"`
}

// LoadRegistry は記録を読む。ファイルが無ければ空 (まだ 1 本も起動していない)。壊れていたらエラー (空と区別する)。
func LoadRegistry(path string) ([]Owned, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var reg []Owned
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, err
	}
	return reg, nil
}

// ErrNoPID / ErrNoSessionID は照合に要る欄の無い行を記録しようとしたとき (誰とも一致しない行を書く意味が無い)。
var (
	ErrNoPID       = errors.New("PID の無い session は記録しない (照合に PID が要る)")
	ErrNoSessionID = errors.New("session id の無い session は記録しない (照合と書き直しの鍵に session id が要る)")
)

// Register は起動・再開した session を記録する。同じ SessionID の行があれば書き直し、無ければ足す。
// SessionID と PID は必須 (短い id だけの行で書き直すと、SessionID の制約が消えて照合が緩む。敵対的レビュー 3 周目の P2)。
// 起動する側は、claude --bg が返す短い id で一覧を引き、SessionID と PID を得てから記録する (427)。
// 🚨 書き直しは行ごと置き換える。再開のときも CardID を渡すこと (渡し忘れると、どのカードのための session かが消える。照合には影響しない)。
// 書き込みは一時ファイルからの rename (途中で落ちても壊れた記録を残さない)。
// 🚨 書き手は daemon だけの前提 (issue 426 の決定 1)。複数から同時に書くと、後から書いた方が前の書き込みを消す。
func Register(path string, o Owned) error {
	if o.SessionID == "" {
		return ErrNoSessionID
	}
	if o.PID == 0 {
		return ErrNoPID
	}
	cur, err := LoadRegistry(path)
	if err != nil {
		return err
	}
	replaced := false
	for i, c := range cur {
		if c.SessionID == o.SessionID {
			cur[i], replaced = o, true
		}
	}
	if !replaced {
		cur = append(cur, o)
	}
	data, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), RegistryFile+".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// owns は記録に session があるか。記録の 1 行にある欄 (SessionID / ID) が**全部**一致し、PID も一致したときだけ持っていると見なす
// (どれか 1 つの一致で通すと、短い id の衝突や、外で同じ session id を再開したものを拾う)。
// id を 1 つも持たない行と、PID の無い行 (古い形式・書き忘れ) は誰とも一致しない (失敗側に倒す)。
func owns(reg []Owned, sessionID, id string, pid int) bool {
	for _, o := range reg {
		if (o.SessionID == "" && o.ID == "") || o.PID == 0 {
			continue
		}
		if (o.SessionID == "" || o.SessionID == sessionID) && (o.ID == "" || o.ID == id) && o.PID == pid {
			return true
		}
	}
	return false
}
