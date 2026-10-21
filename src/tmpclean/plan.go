package tmpclean

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const planMaxAge = time.Hour

// planFile は plan が書き、apply が読むもの。ID は中身 (作成時刻・選ぶ条件・候補) の hash で、apply は計算し直して照合する
// (一覧を書き換えると ID が合わなくなる)。暗号的な結び付けではない: 書き換えた上で ID も計算し直せば通る。
// apply はさらに今の候補と交わりを取るので、書き換えで移すものを増やすことはできない (絞れるだけ)。
type planFile struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Selection selection `json:"selection"`
	Entries   []entry   `json:"entries"`
}

func planID(createdAt time.Time, sel selection, entries []entry) string {
	body, _ := json.Marshal(struct {
		CreatedAt time.Time `json:"created_at"`
		Selection selection `json:"selection"`
		Entries   []entry   `json:"entries"`
	}{createdAt.UTC(), sel, entries})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:8])
}

func newPlan(now time.Time, sel selection, entries []entry) planFile {
	if entries == nil {
		entries = []entry{}
	}
	now = now.UTC().Truncate(time.Second)
	return planFile{ID: planID(now, sel, entries), CreatedAt: now, Selection: sel, Entries: entries}
}

// writePlan は outDir に plan-<ID>.json を書く。既にあれば上書きしない。
func writePlan(outDir string, p planFile) (string, error) {
	if !isRealDir(outDir) {
		return "", stopf("-out の dir が無い: %s", outDir)
	}
	path := filepath.Join(outDir, "plan-"+p.ID+".json")
	body, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(append(body, '\n')); err != nil {
		_ = f.Close()
		return "", err
	}
	return path, f.Close()
}

// readPlan は plan を読み、ID・承認・古さ・選ぶ条件を検査する。どれかが合わなければ stopError。
func readPlan(path, approve string, now time.Time) (planFile, error) {
	var p planFile
	body, err := os.ReadFile(path)
	if err != nil {
		return p, stopf("plan を読めない: %v", err)
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return p, stopf("plan が壊れている: %v", err)
	}
	if got := planID(p.CreatedAt, p.Selection, p.Entries); got != p.ID {
		return p, stopf("plan の中身が ID (%s) と合わない (書き換えられた)", p.ID)
	}
	if approve != p.ID {
		return p, stopf("承認した ID (%s) が plan の ID (%s) と違う", approve, p.ID)
	}
	if age := now.Sub(p.CreatedAt); age < -time.Minute || age > planMaxAge {
		return p, stopf("plan が古い・未来の時刻 (作成 %s)。plan から作り直す", p.CreatedAt.Format(time.RFC3339))
	}
	if err := p.Selection.validate(); err != nil {
		return p, err
	}
	for _, e := range p.Entries {
		if !p.Selection.has(e.Kind) {
			return p, stopf("plan の項目の種類 (%s) が選んだ種類に無い: %s", e.Kind, e.Path)
		}
	}
	return p, nil
}
