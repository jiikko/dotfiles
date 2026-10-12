//go:build !darwin

package filer

// getattrlistbulk は macOS にしか無い。他の OS は今の形 (readEntriesSlow) で読む。
func readEntriesBulk(string) ([]dirEntry, error) { return nil, errNoBulk }
