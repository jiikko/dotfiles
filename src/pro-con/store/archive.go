package store

// 終えたカードの書庫 (issue 478)。記録 (cards.json) は dispatcher と画面が毎回まるごと読むので、終えたカードを持ち続けると
// 読む費用が作ったカードの総数に比例して増える。終えたカードは書庫 (足していくだけの JSONL) へ移し、記録には動いているカードと
// 完了して間もないカードだけを置く。書庫は `card show` / `card list --all` だけが読む (438 の「記録からは消さない」を書庫で守る)。
//
// 失敗モード:
//   - 書庫に足した後、記録を書き直す前に落ちる → 次の Archive がもう一度足す。読む側は同じ ID を後の行で上書きし、記録にあれば記録を正とする
//   - 書庫の末尾が書きかけの行 → 足す前に改行を補う (次の行まで巻き込んで壊さない)。読む側は読めない行を飛ばし、その数をエラーで返す

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"pro-con/card"
)

// ArchiveFile は書庫のファイル名 (記録と同じ置き場)。
const ArchiveFile = "cards-archive.jsonl"

// AutoClearAfter は完了のカードを、片付け (x) を待たずに自動で片付けて書庫へ移すまでの長さ (完了にした時刻から)。
const AutoClearAfter = 24 * time.Hour

// AutoClearText は自動で片付けたカードの履歴の文。
const AutoClearText = "完了から 24 時間たったので自動で片付けた"

// Archive は書庫へ移せるカードを記録から書庫へ移し、移したカードを返す (dispatcher だけが呼ぶ。Apply と同じ書き手)。
// 移すのは完了のカードのうち、片付けた (x) か完了から AutoClearAfter たったもの。PG を止め終えていない・削除の途中・
// 答えていない btw がある・記録に残る子カードの親 (外すと子が親を失う) は残す。自動で片付けたカードには印と履歴を付けてから移す。
func Archive(dir string, now time.Time) ([]card.Card, error) {
	st, err := Load(dir)
	if err != nil {
		return nil, err
	}
	move := movable(st.Cards, now)
	if len(move) == 0 {
		return nil, nil
	}
	var moved []card.Card
	kept := make([]card.Card, 0, len(st.Cards)-len(move))
	for _, c := range st.Cards {
		if !move[c.ID] {
			kept = append(kept, c)
			continue
		}
		if !c.Archived {
			c.Archived = true
			c.History = append(slices.Clip(c.History), card.Event{At: now, Text: AutoClearText})
		}
		moved = append(moved, c)
	}
	if err := newViolation(st.Cards, kept); err != nil {
		return nil, err
	}
	if err := appendArchive(dir, moved); err != nil {
		return nil, err
	}
	st.Cards = kept
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeAtomic(filepath.Join(dir, StateFile), data); err != nil {
		return nil, err
	}
	return moved, nil
}

// movable は書庫へ移すカードの ID。親は、記録に残る子カードが 1 枚でもあれば残す (子を移すかを決めてから親を決め直す)。
func movable(cs []card.Card, now time.Time) map[string]bool {
	move := map[string]bool{}
	for _, c := range cs {
		if c.State == card.Done && (c.Archived || now.Sub(c.Since) >= AutoClearAfter) && !c.StopAfterClose && !c.Deleting() && !unansweredBtw(c) {
			move[c.ID] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, c := range cs {
			if c.ParentID != "" && !move[c.ID] && move[c.ParentID] {
				delete(move, c.ParentID)
				changed = true
			}
		}
	}
	return move
}

// unansweredBtw は答えていない btw があるか (btw はどの列でも受ける。移すと dispatcher の tickBtws が見なくなり、答えないまま書庫に残る)。
func unansweredBtw(c card.Card) bool {
	return slices.ContainsFunc(c.Btws, func(b card.Btw) bool { return b.Answered.IsZero() })
}

// appendArchive は書庫にカードを 1 行ずつ足し、ディスクへ書き切ってから返す (記録から外すのはその後)。
func appendArchive(dir string, cs []card.Card) error {
	var buf bytes.Buffer
	for _, c := range cs {
		b, err := json.Marshal(c)
		if err != nil {
			return err
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	f, err := os.OpenFile(filepath.Join(dir, ArchiveFile), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	data := buf.Bytes()
	if fi, err := f.Stat(); err != nil {
		return err
	} else if fi.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, fi.Size()-1); err != nil {
			return err
		}
		if last[0] != '\n' { // 書きかけで落ちた行の続きに書かない
			data = append([]byte{'\n'}, data...)
		}
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

// LoadArchive は書庫のカードを移した順に返す (同じ ID は後の行を正とする)。無ければ空。
// 読めない行は飛ばし、読めた分と一緒にその数をエラーで返す (1 行の壊れで書庫の全部を読めなくしない)。
func LoadArchive(dir string) ([]card.Card, error) {
	f, err := os.Open(filepath.Join(dir, ArchiveFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []card.Card
	at := map[string]int{}
	broken := 0
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var c card.Card
			if jerr := json.Unmarshal(line, &c); jerr != nil || c.ID == "" {
				broken++
			} else if i, ok := at[c.ID]; ok {
				out[i] = c
			} else {
				at[c.ID] = len(out)
				out = append(out, c)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return out, err
		}
	}
	if broken > 0 {
		return out, fmt.Errorf("書庫 (%s) に読めない行が %d 行ある (飛ばした)", filepath.Join(dir, ArchiveFile), broken)
	}
	return out, nil
}

// Find はカードを記録から、無ければ書庫から探す (記録にあれば記録を正とする)。
// 記録に無いときだけ書庫を読む (動いているカードを探すたびに書庫を読まない)。書庫の読めない行は、見つかれば無視する。
func Find(dir, id string) (card.Card, bool, error) {
	st, err := Load(dir)
	if err != nil {
		return card.Card{}, false, err
	}
	if i := indexOf(st.Cards, id); i >= 0 {
		return st.Cards[i], true, nil
	}
	arch, err := LoadArchive(dir)
	if i := indexOf(arch, id); i >= 0 {
		return arch[i], true, nil
	}
	return card.Card{}, false, err
}
