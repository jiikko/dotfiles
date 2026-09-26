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

// RetiredFile は、再開で入れ替わって記録から外した前の session (ReplaceCard が外した行) を足していくファイル (RegistryFile と同じ置き場)。
// 読むのは終了のときの確かめだけ (dispatcher の ensureStopped。入れ替わった前の session が生きていても止める)。カードとの対応には使わない
const RetiredFile = "sessions-retired.json"

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
	// Cwd は session の作業ディレクトリ (PG の worktree)。再開をそこで走らせる (他の cwd では transcript が見つからない / 別の tree を書く)。
	// 所有の照合 (owns) には使わない
	Cwd string `json:"cwd,omitempty"`
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
// 🚨 書き手は dispatcher だけの前提 (issue 426 の決定 1)。複数から同時に書くと、後から書いた方が前の書き込みを消す。
func Register(path string, o Owned) error { return write(path, o, false) }

// ReplaceCard は o.CardID の行を o 1 本に置き換える (再開で session が新しくなったとき。
// claude --bg --resume は元の session を続けず、別の session id の session を立てる = 427 の 3f で実測)。
func ReplaceCard(path string, o Owned) error { return write(path, o, true) }

func write(path string, o Owned, dropCard bool) error {
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
	next := cur[:0:0]
	var retired []Owned
	for _, c := range cur {
		if c.SessionID == o.SessionID {
			continue // 同じ session の書き直し
		}
		if dropCard && o.CardID != "" && c.CardID == o.CardID {
			retired = append(retired, c) // 再開で入れ替わった前の session
			continue
		}
		next = append(next, c)
	}
	if len(retired) > 0 { // 先に退いた側へ足す (記録から外した後で落ちても、前の session を見失わない)
		retiredPath := filepath.Join(filepath.Dir(path), RetiredFile)
		old, err := LoadRegistry(retiredPath)
		if err != nil {
			return err
		}
		if err := writeRows(retiredPath, append(old, retired...)); err != nil {
			return err
		}
	}
	return writeRows(path, append(next, o))
}

// Forget は cardID のカードの行のうち、session id が sessionIDs にあるものを記録 (今の分と退いた分) から消し、消した本数を返す
// (片付けが transcript を消した session。issue 497)。🚨 書き手は dispatcher だけ (Register と同じ)。
// 挙げていない session の行 (片付けの後に再開した session) と、ほかのカードの行は残す。
func Forget(path, cardID string, sessionIDs []string) (int, error) {
	drop := map[string]bool{}
	for _, s := range sessionIDs {
		drop[s] = s != ""
	}
	n := 0
	for _, p := range []string{filepath.Join(filepath.Dir(path), RetiredFile), path} {
		cur, err := LoadRegistry(p)
		if err != nil {
			return n, err
		}
		next := cur[:0:0]
		for _, o := range cur {
			if cardID != "" && o.CardID == cardID && drop[o.SessionID] {
				n++
				continue
			}
			next = append(next, o)
		}
		if len(next) == len(cur) {
			continue
		}
		if err := writeRows(p, next); err != nil {
			return n, err
		}
	}
	return n, nil
}

// LoadRetired は、再開で入れ替わって記録から外した前の session を読む (RetiredFile)。
func LoadRetired(registryPath string) ([]Owned, error) {
	return LoadRegistry(filepath.Join(filepath.Dir(registryPath), RetiredFile))
}

// writeRows は行を path へ書く (一時ファイルからの rename)。
func writeRows(path string, cur []Owned) error {
	data, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
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
