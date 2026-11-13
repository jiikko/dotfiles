package lint

import "ja-writing/internal/pyre"

// lazyPattern と下の道具は internal/pyre の Lazy (diff と共有) の別名。lint の群のコードの書き方を変えずに使う。
type lazyPattern = pyre.Lazy

var (
	newLazy     = pyre.NewLazy
	newLazyFrom = pyre.NewLazyFrom
)

func mustSearch(p *lazyPattern, text string) bool        { return p.MustSearch(text) }
func mustMatchAt0(p *lazyPattern, text string) bool      { return p.MustMatchAt0(text) }
func mustFindall(p *lazyPattern, text string) [][]string { return p.MustFindall(text) }
