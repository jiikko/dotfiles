package main

import (
	"os"
	"strings"
)

// readTail returns the last `maxLines` non-header lines of the given log file.
// Header lines start with "# " (meta) or are exactly "---" (separator). On any
// error (missing file, IO) it returns nil.
//
// Only the last 8KB of the file are read, so this is safe to call on large
// log files during long-running jobs.
func readTail(path string, maxLines int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return nil
	}

	const window = 8192
	readSize := int64(window)
	if info.Size() < readSize {
		readSize = info.Size()
	}
	buf := make([]byte, readSize)
	offset := info.Size() - readSize
	if _, err := f.ReadAt(buf, offset); err != nil {
		return nil
	}
	s := string(buf)
	// If we started mid-file, drop the first (partial) line.
	if offset > 0 {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
	}

	raw := strings.Split(strings.TrimRight(s, "\n"), "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		if strings.HasPrefix(line, "# ") || line == "---" {
			continue
		}
		out = append(out, line)
	}
	if maxLines > 0 && len(out) > maxLines {
		out = out[len(out)-maxLines:]
	}
	return out
}
