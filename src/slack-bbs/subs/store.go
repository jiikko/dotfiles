// Package subs は購読するチャンネルの一覧 (~/.config/slack-bbs/subscriptions.json) と、それを選ぶ画面を持つ。
//
// 保存するのはチャンネルの ID と名前だけで、会話の本文は保存しない (本文は cache package が別の置き場に残す)。
package subs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"atomicfile"
	"userdirs"
)

// Channel は購読しているチャンネル。
type Channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type file struct {
	Channels []Channel `json:"channels"`
}

// Path は購読の一覧の置き場。
func Path() (string, error) {
	return userdirs.ConfigFile("slack-bbs", "subscriptions.json", "")
}

// Load は購読の一覧を読む。ファイルが無ければ空 (まだ選んでいない)。
func Load(path string) ([]Channel, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s を読めない: %w", path, err)
	}
	return f.Channels, nil
}

// Save は購読の一覧を書く (途中の状態を残さない。本人だけが読める権限)。
func Save(path string, chs []Channel) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if chs == nil {
		chs = []Channel{}
	}
	b, err := json.MarshalIndent(file{Channels: chs}, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(b, '\n'), 0o600)
}
