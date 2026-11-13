package lint

import (
	"sync"

	"ja-writing/internal/pyre"
)

// lazyPattern は初めて使うときに組む正規表現。本家は 3.0.1 から、読み込みのときに組む正規表現を初めて使うときに組む形にして、
// 1 回の実行で使う一部だけの費用にした (hook の起動の時間のため)。Go も package の初期化では組まず、get で組む (同じパターン・同じフラグで pyre を通す)。
type lazyPattern struct {
	source func() string
	flags  int
	once   sync.Once
	re     *pyre.Pattern
}

func newLazy(pattern string, flags int) *lazyPattern {
	return &lazyPattern{source: func() string { return pattern }, flags: flags}
}

// newLazyFrom はパターンの文字列を、使うときに組む正規表現 (表から文字の組を作る本家の正規表現。表を作る費用も初めて使うときまで払わない)。
func newLazyFrom(source func() string, flags int) *lazyPattern {
	return &lazyPattern{source: source, flags: flags}
}

// pattern は組まずに読めるパターンの文字列 (本家の _LazyPattern.pattern)。
func (l *lazyPattern) pattern() string { return l.source() }

// get は組んだ正規表現。組めなければ panic する (パターンは定数で、組めないのは書き間違い。記録との突き合わせが見つける)。
func (l *lazyPattern) get() *pyre.Pattern {
	l.once.Do(func() { l.re = pyre.MustCompile(l.source(), l.flags) })
	return l.re
}

// mustSearch は p.search(text) が当たるか。パターンは定数なので、組めない・照合が失敗するのは書き間違いで、panic する。
func mustSearch(p *lazyPattern, text string) bool {
	m, err := p.get().Search(pyre.NewText(text), 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	return m != nil
}

// mustMatchAt0 は p.match(text) が当たるか (文字列の先頭からの照合)。
func mustMatchAt0(p *lazyPattern, text string) bool {
	m, err := p.get().Match(pyre.NewText(text), 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	return m != nil
}

// mustFindall は p.findall(text)。
func mustFindall(p *lazyPattern, text string) [][]string {
	found, err := p.get().Findall(pyre.NewText(text), 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	return found
}
