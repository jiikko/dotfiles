package main

// listState tracks cursor and viewport scroll for a scrollable list view.
// All operations are keyed by the total item count and page size passed at
// call time; the state itself stores no reference to the underlying data.
type listState struct {
	cursor int
	scroll int
}

// reset sets cursor and scroll back to the top.
func (s *listState) reset() {
	s.cursor = 0
	s.scroll = 0
}

// clampListIdx clips i into [0, total-1], or returns 0 when total is 0.
func clampListIdx(i, total int) int {
	if i < 0 {
		return 0
	}
	if total > 0 && i > total-1 {
		return total - 1
	}
	return i
}

// ensureVisible scrolls the viewport so the cursor is within it.
func (s *listState) ensureVisible(total, pageSize int) {
	if pageSize < 1 {
		pageSize = 1
	}
	if s.cursor < s.scroll {
		s.scroll = s.cursor
	}
	if s.cursor >= s.scroll+pageSize {
		s.scroll = s.cursor - pageSize + 1
	}
	s.scroll = clampListIdx(s.scroll, total)
}

// handleNavKey translates a navigation key into a cursor/scroll update and
// returns true if the key was recognised. Non-navigation keys return false so
// the caller can handle them (e.g. esc to close the view, enter to select).
func (s *listState) handleNavKey(key string, total, pageSize int) bool {
	if pageSize < 1 {
		pageSize = 1
	}
	switch key {
	case "up", "k":
		s.cursor = clampListIdx(s.cursor-1, total)
	case "down", "j":
		s.cursor = clampListIdx(s.cursor+1, total)
	case "pgup", "b":
		s.cursor = clampListIdx(s.cursor-pageSize, total)
	case "pgdown", " ", "f":
		s.cursor = clampListIdx(s.cursor+pageSize, total)
	case "home", "g":
		s.reset()
		return true
	case "end", "G":
		s.cursor = clampListIdx(total-1, total)
	default:
		return false
	}
	s.ensureVisible(total, pageSize)
	return true
}
