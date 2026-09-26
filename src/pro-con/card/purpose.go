package card

import (
	"fmt"
	"strings"
)

// Purpose はカードの種類 (issue 531)。作業のカード (PG が付く) か、人に確かめるだけのカード (確認。PG は付かない) か。
// 🚨 Wait.Kind (WaitKind。何を待っているか) とは別の軸。名前を Kind にしない
type Purpose int

const (
	ForWork     Purpose = iota // 作業 (既定。PM が issue に分けて PG に回す)
	ForQuestion                // 確認: 人の答えを受けて PM が issue を書くか閉じる。plan (PG を付ける) は受け付けない
)

func (p Purpose) Label() string {
	switch p {
	case ForWork:
		return "作業"
	case ForQuestion:
		return "確認"
	}
	return fmt.Sprintf("Purpose(%d)", int(p))
}

// Name は CLI の `--purpose` と `card list --json` で使う名前。
func (p Purpose) Name() string {
	switch p {
	case ForWork:
		return "work"
	case ForQuestion:
		return "question"
	}
	return fmt.Sprintf("purpose-%d", int(p))
}

// QuestionPurposeText は詳細 (画面と card show) の「種類:」の行に出す、確認のカードの説明。
var QuestionPurposeText = ForQuestion.Label() + " (人に確かめるだけ。PG は付かず、答えを受けた PM が閉じる)"

var purposes = []Purpose{ForWork, ForQuestion}

// PurposeNames は ParsePurpose が受ける名前を「 / 」で繋いだもの (使い方の文に出す)。
func PurposeNames() string {
	var names []string
	for _, p := range purposes {
		names = append(names, p.Name())
	}
	return strings.Join(names, " / ")
}

// ParsePurpose は Name の逆。
func ParsePurpose(s string) (Purpose, error) {
	for _, p := range purposes {
		if s == p.Name() {
			return p, nil
		}
	}
	return ForWork, fmt.Errorf("--purpose は %s のどれか: %q", PurposeNames(), s)
}

// CheckPurpose は記録に書ける値か (箱に手で置かれた依頼もここで止める。cardcmd の検査を通らない)。
func CheckPurpose(p Purpose) error {
	if p != ForWork && p != ForQuestion {
		return fmt.Errorf("カードの種類 %d は無い", int(p))
	}
	return nil
}
