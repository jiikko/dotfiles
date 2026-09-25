package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"pro-con/card"
)

// DoingFile は dispatcher が集めた「PG が今走らせているもの」(issue 473)。書くのは dispatcher だけ、読むのは画面と `pro-con card show`
// (どちらも読むだけ。画面が ps や transcript を数秒ごとに重く読まない = 441)。
const DoingFile = "doing.json"

// Doing は DoingFile の中身。
type Doing struct {
	At    time.Time               `json:"at"`              // 集めた時刻
	Cards map[string][]card.Doing `json:"cards,omitempty"` // カード ID → 今走らせているもの (無いカードは載せない)
	Err   string                  `json:"err,omitempty"`   // プロセスの一覧を読めなかった理由 (道具の呼び出しとサブエージェントだけで出している)
}

// SaveDoing は様子を書く (書きかけを読ませない)。
func SaveDoing(dir string, d Doing) error {
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, DoingFile), data)
}

// LoadDoing は様子を読む。無ければ zero (dispatcher がまだ集めていない)。
func LoadDoing(dir string) (Doing, error) {
	data, err := os.ReadFile(filepath.Join(dir, DoingFile))
	if os.IsNotExist(err) {
		return Doing{}, nil
	}
	if err != nil {
		return Doing{}, err
	}
	var d Doing
	if err := json.Unmarshal(data, &d); err != nil {
		return Doing{}, err
	}
	return d, nil
}

// Attach は c に集めた様子を足す (DoingFile の中身。無ければ何もしない)。画面と card show が同じ足し方をする。
func (d Doing) Attach(c *card.Card) {
	if ds := d.Cards[c.ID]; len(ds) > 0 {
		c.Doing, c.DoingAt = ds, d.At
	}
}
