package store

// 完了から 1 週間たったカードを書庫から消す (issue 497)。ユーザーの決定 (2026-09-26): カードの記録は自動で消し、
// session (起動の記録の行と transcript) と worktree は人が明示したとき (pro-con worktree clean --yes) だけ消す。
//
// 🚨 カードを消すと、片付け (wtclean) が「pro-con が作った・完了した」と示す材料が無くなり、消したカードの worktree と session を
// 二度と片付けられなくなる。消す前に、片付けに要る最小限の印 (カード ID・完了の時刻・session の id・worktree・ブランチ) を
// 印のファイル (PurgeFile) に残す。印は片付けが済んだら dispatcher が消す (DropPurged。worktree clean が受付の箱に置く forget)。
//
// 書き手は dispatcher だけ (書庫も印も。426 の決定 1)。失敗モード:
//   - 印を書いた後、書庫を書き直す前に落ちる → 次の Purge がもう一度印を足す。読む側は同じカードを後の行で上書きする
//   - 書庫の書き直しは一時ファイルからの rename (途中で落ちても書庫を壊さない)。読めない行は消さずにそのまま残す
//     (読めないので消してよいか示せない。LoadArchive がその数を知らせ続ける)
//   - 印の末尾が書きかけの行 → 足す前に改行を補う (appendLines)。読む側は読めない行を飛ばす
// 消したカードの ID は使い回さない (記録の nextId は触らない)。

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"pro-con/card"
)

// PurgeFile は片付けの印のファイル名 (記録と同じ置き場)。
const PurgeFile = "cards-purged.jsonl"

// PurgeAfter は完了にした時刻から、書庫のカードを消すまでの長さ。設定では変えない (片付けの印があれば、消した後も片付けられる)。
const PurgeAfter = 7 * 24 * time.Hour

// PurgeText は消したカードの出来事の文。
const PurgeText = "完了から 1 週間たったので記録から消した (worktree と session は pro-con worktree clean --yes で消す)"

// Purged はカードを消した後に残す片付けの印 1 件。
type Purged struct {
	CardID   string    `json:"cardId"`
	Repo     string    `json:"repo,omitempty"` // 設定の repo の名前
	DoneAt   time.Time `json:"doneAt"`         // 完了にした時刻
	PurgedAt time.Time `json:"purgedAt"`
	Worktree string    `json:"worktree,omitempty"` // PG の worktree の絶対パス (repo の場所が分からなければ空)
	Branch   string    `json:"branch,omitempty"`
	// Sessions はそのカードのために pro-con が起動した session (起動の記録の今の分と退いた分)
	Sessions []PurgedSession `json:"sessions,omitempty"`
}

// PurgedSession は印に残す session 1 本 (claude の短い id と session id)。
type PurgedSession struct {
	ID        string `json:"id,omitempty"`
	SessionID string `json:"sessionId"`
}

// Purge は書庫のカードのうち、完了から PurgeAfter たったものを消し、消したカードを返す (dispatcher だけが呼ぶ)。
// mark は消すカードの印の worktree・ブランチ・session を埋める (dispatcher が設定の repo と起動の記録から)。
// mark が 1 件でも失敗したら何も消さない (印の無いカードを消さない)。
func Purge(dir string, now time.Time, mark func(card.Card) (Purged, error)) ([]card.Card, error) {
	st, err := Load(dir)
	if err != nil {
		return nil, err
	}
	arch, err := LoadArchive(dir)
	if err != nil && arch == nil {
		return nil, err
	} // 読めない行があっても、読めた行は消してよいか判定できる (読めない行は書き直しでそのまま残す)
	gone := purgeable(st.Cards, arch, now)
	if len(gone) == 0 {
		return nil, nil
	}
	var purged []card.Card
	var marks []Purged
	for _, c := range arch {
		if !gone[c.ID] {
			continue
		}
		m, err := mark(c)
		if err != nil {
			return nil, fmt.Errorf("%s の片付けの印を作れないので消さない: %w", c.ID, err)
		}
		m.CardID, m.Repo, m.DoneAt, m.PurgedAt = c.ID, c.Repo, c.Since, now
		marks = append(marks, m)
		purged = append(purged, c)
	}
	if err := appendLines(filepath.Join(dir, PurgeFile), marks); err != nil {
		return nil, err
	}
	if err := dropLines(filepath.Join(dir, ArchiveFile), func(line []byte) bool {
		var c struct {
			ID string `json:"id"`
		}
		return json.Unmarshal(line, &c) == nil && gone[c.ID]
	}); err != nil {
		return nil, err
	}
	return purged, nil
}

// purgeable は書庫から消すカードの ID。記録にもあるカードは消さない (記録を正とする。書き戻しの途中の二重)。
// 完了で、PG を止め終え、削除の途中でないものだけ (書庫へ移す条件と同じ。書庫の行は移した時点の姿なので、ここでも見直す)。
// 親は、残るカード (記録か書庫) に子が 1 枚でもあれば残す (子が親を失わない。子を決めてから親を決め直す)。
func purgeable(records, arch []card.Card, now time.Time) map[string]bool {
	inRecord := map[string]bool{}
	for _, c := range records {
		inRecord[c.ID] = true
	}
	gone := map[string]bool{}
	for _, c := range arch {
		if !inRecord[c.ID] && c.State == card.Done && !c.Since.IsZero() && now.Sub(c.Since) >= PurgeAfter && !c.StopAfterClose && !c.Deleting() {
			gone[c.ID] = true
		}
	}
	all := append(append([]card.Card(nil), records...), arch...)
	for changed := true; changed; {
		changed = false
		for _, c := range all {
			if c.ParentID != "" && gone[c.ParentID] && !gone[c.ID] {
				delete(gone, c.ParentID)
				changed = true
			}
		}
	}
	return gone
}

// LoadPurged は片付けの印を読む (同じカードは後の行を正とする)。無ければ空。
// 読めない行は飛ばし、読めた分と一緒にその数をエラーで返す (LoadArchive と同じ)。
func LoadPurged(dir string) (map[string]Purged, error) {
	out := map[string]Purged{}
	broken, err := eachLine(filepath.Join(dir, PurgeFile), func(line []byte) bool {
		var p Purged
		if json.Unmarshal(line, &p) != nil || p.CardID == "" {
			return false
		}
		out[p.CardID] = p
		return true
	})
	if err != nil {
		return out, err
	}
	if broken > 0 {
		return out, fmt.Errorf("片付けの印 (%s) に読めない行が %d 行ある (飛ばした)", filepath.Join(dir, PurgeFile), broken)
	}
	return out, nil
}

// DropPurged は id のカードの印を消す (片付けが済んだ。dispatcher だけが呼ぶ)。印が無ければ何もしない。
func DropPurged(dir, id string) error {
	return dropLines(filepath.Join(dir, PurgeFile), func(line []byte) bool {
		var p Purged
		return json.Unmarshal(line, &p) == nil && p.CardID == id
	})
}

// eachLine は JSONL の空でない行ごとに f を呼び、f が false を返した (読めない) 行の数を返す。ファイルが無ければ 0。
func eachLine(path string, f func(line []byte) bool) (int, error) {
	fh, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer func() { _ = fh.Close() }()
	broken := 0
	r := bufio.NewReader(fh)
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 && !f(line) {
			broken++
		}
		if errors.Is(err, io.EOF) {
			return broken, nil
		}
		if err != nil {
			return broken, err
		}
	}
}

// dropLines は JSONL から drop が true を返す行を除いて書き直す (一時ファイルからの rename)。それ以外の行は、読めない行も含めて
// 元のまま残す。除く行が無ければ書かない。ファイルが無ければ何もしない。
func dropLines(path string, drop func(line []byte) bool) error {
	var kept bytes.Buffer
	dropped := 0
	if _, err := eachLine(path, func(line []byte) bool {
		if drop(bytes.TrimSpace(line)) {
			dropped++
			return true
		}
		kept.Write(line) // 書きかけの行 (改行の無い行) は末尾にしか無いので、そのまま残す (次に足すときに appendLines が改行を補う)
		return true
	}); err != nil {
		return err
	}
	if dropped == 0 {
		return nil
	}
	return writeAtomic(path, kept.Bytes())
}

// appendLines は v を 1 行ずつ JSONL に足し、ディスクへ書き切ってから返す。末尾が書きかけの行なら改行を補ってから足す。
func appendLines[T any](path string, vs []T) error {
	var buf bytes.Buffer
	for _, v := range vs {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
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
