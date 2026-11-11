// Package subs は購読するチャンネルの一覧 (~/.config/slack-bbs/subscriptions.json) と、それを選ぶ画面を持つ。
//
// 保存するのはチャンネルの ID と名前だけで、会話の本文は保存しない (本文は cache package が別の置き場に残す)。
package subs

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"atomicfile"
	"github.com/jiikko/dotfiles/src/termsafe"
	"slack-bbs/source"
	"userdirs"
)

// Channel は購読しているチャンネル、またはグループ (複数のチャンネルをまとめて 1 つの板として読む購読)。
// グループは Members を持ち、ID は NewGroupID が振る (source.IsGroupID)。キャッシュと既読の印はグループの ID で持つので、
// メンバーを出し入れしても引き継ぐ (ユーザーの決定 2026-11-11)。同じチャンネルを複数のグループや単独の購読に入れてよい。
type Channel struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Members []Channel `json:"members,omitempty"`
}

// IsGroup はグループか。
func (c Channel) IsGroup() bool { return source.IsGroupID(c.ID) }

// Label は画面に出す名前: チャンネルは「#名前」、グループは「📂名前」(端末の制御文字は落とす)。
func (c Channel) Label() string {
	if c.IsGroup() {
		return "📂" + termsafe.PlainLine(c.Name)
	}
	return "#" + termsafe.PlainLine(c.Name)
}

// NewGroupID はグループの ID を振る。
func NewGroupID() string {
	var b [4]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read は失敗しない (Go 1.24 から。失敗したら panic する)
	return "grp" + hex.EncodeToString(b[:])
}

// file は購読の一覧のファイルの形。グループは channels と分けて書く (チャンネルだけを読む古い slack-bbs が、グループを
// チャンネルとして読まない)。
type file struct {
	Channels []Channel `json:"channels"`
	Groups   []Channel `json:"groups,omitempty"`
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
	for _, g := range f.Groups {
		if !source.IsGroupID(g.ID) {
			return nil, fmt.Errorf("%s のグループ %q の ID %q が読めない形", path, g.Name, g.ID)
		}
	}
	return append(f.Channels, f.Groups...), nil
}

// Save は購読の一覧 (チャンネルとグループ。Load と同じ並び) を書く (途中の状態を残さない。本人だけが読める権限)。
func Save(path string, chs []Channel) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f := file{Channels: []Channel{}}
	for _, c := range chs {
		if c.IsGroup() {
			f.Groups = append(f.Groups, c)
		} else {
			f.Channels = append(f.Channels, c)
		}
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(b, '\n'), 0o600)
}
