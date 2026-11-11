// Package subs は購読するチャンネルの一覧 (~/.config/slack-bbs/subscriptions.json) と、それを選ぶ画面を持つ。
//
// 保存するのはチャンネルの ID と名前だけで、会話の本文は保存しない (本文は cache package が別の置き場に残す)。
package subs

import (
	"crypto/rand"
	"crypto/sha256"
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

// ErrChanged は、購読の一覧を読んでから保存するまでの間に、ファイルが別の操作 (slack-bbs sub add・別の画面) で書き換えられたこと。
// そのまま書くと、その変更を知らせずに消す。
var ErrChanged = errors.New("購読の一覧が、開いてから別の操作 (slack-bbs sub / group・別の画面) で書き換えられた。開き直して選び直す")

// Load は購読の一覧を読む。ファイルが無ければ空 (まだ選んでいない)。
func Load(path string) ([]Channel, error) {
	chs, _, err := LoadVersion(path)
	return chs, err
}

// LoadVersion は購読の一覧と、ファイルの中身の印 (SaveIf で、読んでから書き換えられていないかを照合する。ファイルが無ければ空) を読む。
func LoadVersion(path string) ([]Channel, string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, "", fmt.Errorf("%s を読めない: %w", path, err)
	}
	if err := validate(path, f); err != nil {
		return nil, "", err
	}
	return append(f.Channels, f.Groups...), version(b), nil
}

// dupscan:allow intentional job.digest と同じ 3 行だが役割が違う (購読のファイルの照合の印と、2 段の実行の依頼の改ざん検出)。寄せると片方の都合 (長さ・形) を変えにくくなる
func version(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// validate は購読の一覧の形を確かめる (読むときと書くときの両方)。チャンネルの ID はファイル名にも使うので形を確かめ、グループは
// ID の形と、メンバーの形・重複を確かめる (同じメンバーが 2 回あると同じ単位の ID が 2 つでき、グループの板ごと読めなくなる)。
func validate(path string, f file) error {
	for _, c := range f.Channels {
		if !source.IsChannelID(c.ID) {
			return fmt.Errorf("%s のチャンネル %q の ID %q が読めない形 (slack-bbs sub で直す)", path, c.Name, c.ID)
		}
	}
	for _, g := range f.Groups {
		if !source.IsGroupID(g.ID) {
			return fmt.Errorf("%s のグループ %q の ID %q が読めない形", path, g.Name, g.ID)
		}
		seen := map[string]bool{}
		for _, m := range g.Members {
			if !source.IsChannelID(m.ID) || seen[m.ID] {
				return fmt.Errorf("%s のグループ %q のメンバー %q が読めない形か重複している (slack-bbs sub で直す)", path, g.Name, m.ID)
			}
			seen[m.ID] = true
		}
	}
	return nil
}

// Save は購読の一覧 (チャンネルとグループ。Load と同じ並び) を書く (読むときと同じ形の検査を通してから。途中の状態を残さない。本人だけが読める権限)。
// 読んでから書き換えられていないかは照合しない (照合するなら SaveIf)。
func Save(path string, chs []Channel) error {
	_, err := save(path, chs)
	return err
}

// SaveIf は、ファイルの中身が base (LoadVersion が返した印) のままなら書き、新しい印を返す。書き換えられていたら ErrChanged を返して書かない。
// 照合と書き込みの間は lock していない (その間に別の操作が書いた変更は消えうる。人が打つ操作どうしでは起きにくい窓として受け入れる)。
func SaveIf(path string, chs []Channel, base string) (string, error) {
	b, err := os.ReadFile(path)
	cur := ""
	switch {
	case err == nil:
		cur = version(b)
	case !errors.Is(err, fs.ErrNotExist):
		return "", err
	}
	if cur != base {
		return "", ErrChanged
	}
	return save(path, chs)
}

func save(path string, chs []Channel) (string, error) {
	f := file{Channels: []Channel{}}
	for _, c := range chs {
		if c.IsGroup() {
			f.Groups = append(f.Groups, c)
		} else {
			f.Channels = append(f.Channels, c)
		}
	}
	if err := validate(path, f); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return "", err
	}
	b = append(b, '\n')
	if err := atomicfile.Write(path, b, 0o600); err != nil {
		return "", err
	}
	return version(b), nil
}
