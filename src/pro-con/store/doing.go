package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pro-con/card"
)

// 集めた様子のファイル (記録 cards.json とは別)。書くのは dispatcher (DoingFile / ProgressFile) と見張り (ConflictsFile) だけ、
// 読むのは画面と `pro-con card show` (どちらも読むだけ。画面が ps・git・transcript を数秒ごとに重く読まない = 441)。
const (
	DoingFile     = "doing.json"     // PG が今走らせているもの (issue 473)
	ProgressFile  = "progress.json"  // 作業の進捗 (commit・未 commit・issue の進捗節。issue 469)
	ConflictsFile = "conflicts.json" // 見張りが見た取り込みの衝突 (issue 469 / 475)
	DiffDir       = "diffs"          // 取り込む先との差分の本文 (カードごとに <ID>.diff。書くのは dispatcher、読むのは画面の差分の板。issue 508)
)

// DiffPath はカード id の差分の本文の置き場。
func DiffPath(dir, id string) string { return filepath.Join(dir, DiffDir, id+".diff") }

// SaveDiff は差分の本文を書く。中身が前と同じなら書かない (30 秒ごとに数千行を書き直さない)。
func SaveDiff(dir, id string, body []byte) error {
	path := DiffPath(dir, id)
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, body) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, body)
}

// PruneDiffs は keep に無いカードの差分の本文を消す (完了・削除・片付けたカード。置き場に溜めない)。
func PruneDiffs(dir string, keep map[string]bool) error {
	ents, err := os.ReadDir(filepath.Join(dir, DiffDir))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".diff")
		if !ok || keep[id] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, DiffDir, e.Name())); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Doing は DoingFile の中身。
type Doing struct {
	At    time.Time               `json:"at"`              // 集めた時刻
	Cards map[string][]card.Doing `json:"cards,omitempty"` // カード ID → 今走らせているもの (無いカードは載せない)
	Err   string                  `json:"err,omitempty"`   // プロセスの一覧を読めなかった理由 (道具の呼び出しとサブエージェントだけで出している)
}

// Progress は ProgressFile の中身。
type Progress struct {
	At    time.Time                `json:"at"`
	Cards map[string]card.Progress `json:"cards,omitempty"`
}

// Conflicts は ConflictsFile の中身。見張りが今見えている衝突 (見られなかった組は前の結果のまま)。
type Conflicts struct {
	At    time.Time           `json:"at"`
	Cards map[string][]string `json:"cards,omitempty"` // カード ID → 衝突の文 (PG どうしの衝突は両方のカードに載せる)
}

func SaveDoing(dir string, d Doing) error         { return saveJSON(dir, DoingFile, d) }
func SaveProgress(dir string, p Progress) error   { return saveJSON(dir, ProgressFile, p) }
func SaveConflicts(dir string, c Conflicts) error { return saveJSON(dir, ConflictsFile, c) }

// LoadDoing は様子を読む。無ければ zero (dispatcher がまだ集めていない)。
func LoadDoing(dir string) (Doing, error) { return loadJSON[Doing](dir, DoingFile) }

// LoadConflicts は見張りが前に書いた衝突を読む (見張りが見られなかった回に前の結果を残す)。
func LoadConflicts(dir string) (Conflicts, error) { return loadJSON[Conflicts](dir, ConflictsFile) }

// Attach は c に集めた様子を足す (DoingFile の中身。無ければ何もしない)。
// 完了したカードには足さない (dispatcher は完了のカードを集めないが、完了してから次に集めるまで最大 10 秒は前の様子が残る)。
func (d Doing) Attach(c *card.Card) {
	if c.State == card.Done {
		return
	}
	if ds := d.Cards[c.ID]; len(ds) > 0 {
		c.Doing, c.DoingAt = ds, d.At
	}
}

// Derived は集めた様子のファイル 3 つ。画面と card show は LoadDerived で読み、Attach で同じ足し方をする。
type Derived struct {
	Doing     Doing
	Progress  Progress
	Conflicts Conflicts
}

// LoadDerived は 3 つを読む。読めなかったものは zero のまま、理由を errs に返す (読めた分は足す)。
func LoadDerived(dir string) (Derived, []error) {
	var d Derived
	var errs []error
	var err error
	if d.Doing, err = LoadDoing(dir); err != nil {
		errs = append(errs, fmt.Errorf("PG が今走らせているもの: %w", err))
	}
	if d.Progress, err = loadJSON[Progress](dir, ProgressFile); err != nil {
		errs = append(errs, fmt.Errorf("進捗: %w", err))
	}
	if d.Conflicts, err = LoadConflicts(dir); err != nil {
		errs = append(errs, fmt.Errorf("取り込みの衝突: %w", err))
	}
	return d, errs
}

// Attach は 3 つをカードに足す。完了したカードには worktree の場所だけ足す (取り込み済みの branch の進捗・衝突は古い。
// 完了してから次に集めるまでの間も、前の回の commit を出さない)。
func (d Derived) Attach(c *card.Card) {
	if c.State == card.Done {
		if p, ok := d.Progress.Cards[c.ID]; ok && p.Worktree != "" {
			c.Progress, c.ProgressAt = &card.Progress{Worktree: p.Worktree, NoWorktree: p.NoWorktree}, d.Progress.At
		}
		return
	}
	d.Doing.Attach(c)
	if p, ok := d.Progress.Cards[c.ID]; ok {
		c.Progress, c.ProgressAt = &p, d.Progress.At
	}
	if !d.Conflicts.At.IsZero() {
		c.Conflicts, c.ConflictsAt = d.Conflicts.Cards[c.ID], d.Conflicts.At
	}
}

// saveJSON は書きかけを読ませずに書く。
func saveJSON(dir, name string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, name), data)
}

// loadJSON は読む。無ければ zero (まだ書かれていない)。
func loadJSON[T any](dir, name string) (T, error) {
	var zero T
	data, err := os.ReadFile(filepath.Join(dir, name))
	if os.IsNotExist(err) {
		return zero, nil
	}
	if err != nil {
		return zero, err
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return zero, err
	}
	return v, nil
}
