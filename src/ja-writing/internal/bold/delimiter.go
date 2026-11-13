package bold

// delim は _delimiter_parse の区切りの印 1 つ (本家の dict の closer / opener)。
type delim struct {
	start, head, remaining, length int
	canOpen, canClose              bool
	stackIndex                     int
}

// bottomKey は bottoms の鍵 (closer_mod, None if gfm_bounds else can_open)。open は gfm のとき openNone。
type bottomKey struct {
	mod  int
	open int8
}

const (
	openNone int8 = iota
	openFalse
	openTrue
)

func bottomKeyOf(mod int, gfmBounds, canOpen bool) bottomKey {
	switch {
	case gfmBounds:
		return bottomKey{mod, openNone}
	case canOpen:
		return bottomKey{mod, openTrue}
	default:
		return bottomKey{mod, openFalse}
	}
}

// emph は組んだ強調 (left, right, width)。width が 2 のものが太字。
type emph struct{ left, right, width int }

// pair は太字の組 (左の ** の位置, 右の ** の位置)。
type pair struct{ i, j int }

// stack は _DelimiterStack (_delimiter_parse の途中の状態)。advance(stop) で runs[stop] の手前まで進める。
// buckets は (長さ mod 3, 閉じにもなれるか) の 6 つの組ごとの、まだ相手の決まらない開く側の印。
type stack struct {
	text      []rune
	punct     profile
	runs      [][2]int
	index     int
	openers   []*delim
	buckets   [3][2][]*delim
	gfmBounds bool
	bottoms   map[bottomKey]int
	pairs     []pair
	emphasis  []emph
	consumed  map[int]bool
}

func newStack(text []rune, p profile, runs [][2]int) *stack {
	// cmark-gfm は見つからなかった下限を mod 3 だけで分け、新しい cmark は開けるかでも分ける (本家のコメント 2173-2175 行)。
	return &stack{text: text, punct: p, runs: runs, gfmBounds: p == gfm, bottoms: map[bottomKey]int{}, consumed: map[int]bool{}}
}

func closesIndex(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *stack) bucket(d *delim) *[]*delim {
	return &s.buckets[d.length%3][closesIndex(d.canClose)]
}

// blocked は 3 の倍数の規則 (CommonMark の rule 9 と 10) で、閉じる側と組めない開く側の組か。
func blocked(canOpen bool, closes bool, mod, closerMod int) bool {
	return (canOpen || closes) && (mod+closerMod)%3 == 0 && (mod != 0 || closerMod != 0)
}

// advance は runs[s.index:stop] を処理する (_delimiter_parse の state を渡した形)。
func (s *stack) advance(stop int) {
	if stop <= s.index {
		return
	}
	runs := s.runs[s.index:stop]
	s.index = stop
	for _, run := range runs {
		start, end := run[0], run[1]
		canOpen, canClose := roles(s.text, start, end, s.punct)
		closer := &delim{start: start, remaining: end - start, length: end - start, canOpen: canOpen, canClose: canClose}
		for canClose && closer.remaining > 0 {
			var opener *delim
			closerMod := closer.length % 3
			key := bottomKeyOf(closerMod, s.gfmBounds, canOpen)
			bottom, ok := s.bottoms[key]
			if !ok {
				bottom = -1
			}
			for mod := range 3 {
				for closes := range 2 {
					b := s.buckets[mod][closes]
					if len(b) > 0 && !blocked(canOpen, closes == 1, mod, closerMod) && b[len(b)-1].start >= bottom {
						candidate := b[len(b)-1]
						if opener == nil || candidate.stackIndex > opener.stackIndex {
							opener = candidate
						}
					}
				}
			}
			if opener == nil {
				s.bottoms[key] = start
				break
			}
			used := 1
			if opener.remaining >= 2 && closer.remaining >= 2 {
				used = 2
			}
			left := opener.start + opener.head + opener.remaining - used
			right := closer.start + closer.head
			if used == 2 {
				s.pairs = append(s.pairs, pair{left, right})
			}
			s.emphasis = append(s.emphasis, emph{left, right, used})
			for k := range used {
				s.consumed[left+k] = true
				s.consumed[right+k] = true
			}
			// 並んだスタックから外した開く側の印は、組ごとのスタックからもちょうど 1 回外す。残る印の stackIndex は動かない。
			for s.openers[len(s.openers)-1] != opener {
				removed := s.openers[len(s.openers)-1]
				s.openers = s.openers[:len(s.openers)-1]
				b := s.bucket(removed)
				*b = (*b)[:len(*b)-1]
			}
			b := s.bucket(opener)
			*b = (*b)[:len(*b)-1]
			opener.remaining -= used
			closer.head += used
			closer.remaining -= used
			if opener.remaining == 0 {
				s.openers = s.openers[:len(s.openers)-1]
			} else {
				*b = append(*b, opener)
			}
		}
		if canOpen && closer.remaining > 0 {
			closer.stackIndex = len(s.openers)
			s.openers = append(s.openers, closer)
			b := s.bucket(closer)
			*b = append(*b, closer)
		}
	}
}

// findsOpener は finds_opener (今の状態で、長さ closerLength の閉じる側の印が、組む相手を 1 つでも見つけるか。状態は変えない)。
func (s *stack) findsOpener(closerLength int, canOpen bool) bool {
	closerMod := closerLength % 3
	bottom, ok := s.bottoms[bottomKeyOf(closerMod, s.gfmBounds, canOpen)]
	if !ok {
		bottom = -1
	}
	for mod := range 3 {
		for closes := range 2 {
			b := s.buckets[mod][closes]
			if len(b) > 0 && !blocked(canOpen, closes == 1, mod, closerMod) && b[len(b)-1].start >= bottom {
				return true
			}
		}
	}
	return false
}

// delimiterParse は _delimiter_parse(text, punct, runs) (state なし)。太字の組・印に使った位置・全部の強調を返す。
func delimiterParse(text []rune, p profile, runs [][2]int) ([]pair, map[int]bool, []emph) {
	s := newStack(text, p, runs)
	s.advance(len(runs))
	return s.pairs, s.consumed, s.emphasis
}
