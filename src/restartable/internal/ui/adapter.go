// Package ui contains the adapter boundary for the TTY milestone. The runner
// owns lifecycle state and keys; a Bubble Tea model can render that state and
// translate KeyPressMsg values into runner key events.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/jiikko/dotfiles/src/tuikit/confirm"
)

// TeaModel lets the future terminal presenter implement Bubble Tea's model
// contract without coupling the process actor to terminal rendering.
type TeaModel interface{ tea.Model }

// IsConfirmYes keeps confirmation key semantics aligned with tuikit.
func IsConfirmYes(key string) bool { return confirm.IsYes(key) }
