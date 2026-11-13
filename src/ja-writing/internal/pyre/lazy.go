package pyre

import "sync"

// Lazy は初めて使うときに組む正規表現。本家は 3.0.1 から、読み込みのときに組む正規表現を初めて使うときに組む形にして、
// 1 回の実行で使う一部だけの費用にした (hook の起動の時間のため)。Go も package の初期化では組まず、Get で組む (同じパターン・同じフラグで pyre を通す)。
// lint と diff が同じものを持つ (正規表現が数百ある本家の module を写すので、写しを分けない)。
type Lazy struct {
	source func() string
	flags  int
	once   sync.Once
	re     *Pattern
}

// NewLazy は pattern を flags で組む Lazy。
func NewLazy(pattern string, flags int) *Lazy {
	return &Lazy{source: func() string { return pattern }, flags: flags}
}

// NewLazyFrom はパターンの文字列を、使うときに組む正規表現 (表から文字の組を作る本家の正規表現。表を作る費用も初めて使うときまで払わない)。
func NewLazyFrom(source func() string, flags int) *Lazy {
	return &Lazy{source: source, flags: flags}
}

// Pattern は組まずに読めるパターンの文字列 (本家の _LazyPattern.pattern)。
func (l *Lazy) Pattern() string { return l.source() }

// Flags は組むときのフラグ (re.UNICODE を足す前の値)。
func (l *Lazy) Flags() int { return l.flags }

// Get は組んだ正規表現。組めなければ panic する (パターンは定数で、組めないのは書き間違い。記録との突き合わせが見つける)。
func (l *Lazy) Get() *Pattern {
	l.once.Do(func() { l.re = MustCompile(l.source(), l.flags) })
	return l.re
}

// MustSearch は p.search(text) が当たるか。パターンは定数なので、照合が失敗するのは書き間違いで、panic する。
func (l *Lazy) MustSearch(text string) bool {
	m, err := l.Get().Search(NewText(text), 0, NoEnd)
	if err != nil {
		panic(err)
	}
	return m != nil
}

// MustMatchAt0 は p.match(text) が当たるか (文字列の先頭からの照合)。
func (l *Lazy) MustMatchAt0(text string) bool {
	m, err := l.Get().Match(NewText(text), 0, NoEnd)
	if err != nil {
		panic(err)
	}
	return m != nil
}

// MustFindall は p.findall(text)。
func (l *Lazy) MustFindall(text string) [][]string {
	found, err := l.Get().Findall(NewText(text), 0, NoEnd)
	if err != nil {
		panic(err)
	}
	return found
}

// MustSub は p.sub(repl, text) (repl は Python のテンプレート)。
func (l *Lazy) MustSub(repl, text string) string {
	out, err := l.Get().Sub(repl, NewText(text), 0)
	if err != nil {
		panic(err)
	}
	return out
}

// MustSplit は p.split(text) (グループの無いパターンの断片。空の一致でも割る)。
func (l *Lazy) MustSplit(text string) []string {
	parts, err := l.Get().Split(NewText(text), 0)
	if err != nil {
		panic(err)
	}
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = p.Value
	}
	return out
}
