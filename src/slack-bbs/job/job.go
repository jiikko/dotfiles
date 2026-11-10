// Package job は 2 段の実行 (prepare → 分類 → show) の間で受け渡すファイルを読み書きする。
//
// prepare が 1 つのディレクトリにチャンネルごとの依頼 (<ID>.request.json と、分類する側が読む <ID>.prompt.txt) を書き、
// 分類する側 (GUI の Claude のセッション等) が <ID>.response.json を書き、show がそれを読む。
// 本文を含むので、show は読み終えたら自分の名前のファイルだけを消す (ディレクトリごと消す書き方はしない。
// 渡されたディレクトリに別のものがあっても巻き込まない)。ディレクトリ自体は prepare が作ったときだけ消す。
package job

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"atomicfile"
	"slack-bbs/board"
	"slack-bbs/source"
)

// Version は依頼の形の版。形を変えたら上げる (古い prepare の依頼を新しい show が読み違えない)。
const Version = 4

// Request は 1 チャンネルの依頼。Units が分類の対象で、ページの見出しに要る値も持つ。
type Request struct {
	Version     int          `json:"version"`
	Channel     string       `json:"channel"`
	ChannelName string       `json:"channel_name"`
	Since       time.Time    `json:"since"`
	Until       time.Time    `json:"until"`
	Lookback    time.Time    `json:"lookback"`
	Base        string       `json:"base"`
	Notes       []string     `json:"notes,omitempty"`
	Units       []board.Unit `json:"units"`
}

// Manifest は prepare が書いたチャンネルの一覧。show はここに載ったファイルだけを読み、消す。
type Manifest struct {
	Version int `json:"version"`
	// CreatedDir は prepare がディレクトリを作ったか。作っていない (利用者が渡した既存のディレクトリ) なら show は消さない
	CreatedDir bool     `json:"created_dir"`
	Channels   []string `json:"channels"`
	// Hashes はチャンネルごとの依頼 (request.json) の SHA-256。分類する側はファイルを書ける道具を持ち、本文の中の指示に
	// 従って依頼を書き換えうるので、show は prepare が書いたままかを確かめてから組む
	Hashes map[string]string `json:"hashes"`
	// MAC は一覧 (MAC 自身を除く) の HMAC-SHA256。鍵はディレクトリに置かず prepare の出力にだけ出すので、ディレクトリを
	// 書ける分類役が依頼を書き換えてハッシュと一覧を作り直しても (created_dir を書き換えても)、show は鍵で見破る
	MAC string `json:"mac"`

	key []byte
}

// Key は show に渡す鍵 (16 進)。
func (m Manifest) Key() string { return hex.EncodeToString(m.key) }

func (m Manifest) mac(key []byte) (string, error) {
	m.MAC, m.key = "", nil
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	h := hmac.New(sha256.New, key)
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil)), nil
}

const manifestName = "manifest.json"

// RequestPath / PromptPath / ResponsePath はチャンネルのファイルの置き場。
func RequestPath(dir, ch string) string  { return filepath.Join(dir, ch+".request.json") }
func PromptPath(dir, ch string) string   { return filepath.Join(dir, ch+".prompt.txt") }
func ResponsePath(dir, ch string) string { return filepath.Join(dir, ch+".response.json") }

// Open は prepare の書き先を用意する。dir が空なら一時ディレクトリを作る。前の依頼 (manifest.json) が残っている
// ディレクトリは断る (古い応答が新しい単位に当たる・前のチャンネルの本文が誰にも消されずに残る)。
func Open(dir string) (Manifest, string, error) {
	m := Manifest{Version: Version, Hashes: map[string]string{}, key: make([]byte, 32)}
	if _, err := rand.Read(m.key); err != nil {
		return m, "", err
	}
	if dir == "" {
		d, err := os.MkdirTemp("", "slack-bbs-")
		if err != nil {
			return m, "", err
		}
		m.CreatedDir = true
		return m, d, nil
	}
	switch _, err := os.Stat(dir); {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return m, "", err
		}
		m.CreatedDir = true
	case err != nil:
		return m, "", err
	}
	if _, err := os.Stat(filepath.Join(dir, manifestName)); err == nil {
		return m, "", fmt.Errorf("%s に前の依頼が残っている。slack-bbs show で出し切るか、別のディレクトリを渡す", dir)
	}
	return m, dir, nil
}

// Add はチャンネルの依頼と分類する側へ渡す文面を書き、一覧に足して書き直す (途中で止まっても、書いた依頼は一覧に載っていて
// show や後始末から見える)。前の応答が残っていれば消す。
func Add(dir string, m *Manifest, r Request, prompt []byte) error {
	if !source.IsChannelID(r.Channel) {
		return fmt.Errorf("チャンネル ID %q はファイル名に使えない形", r.Channel)
	}
	r.Version = Version
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.Remove(ResponsePath(dir, r.Channel)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := atomicfile.Write(RequestPath(dir, r.Channel), b, 0o600); err != nil {
		return err
	}
	if err := atomicfile.Write(PromptPath(dir, r.Channel), prompt, 0o600); err != nil {
		return err
	}
	m.Channels = append(m.Channels, r.Channel)
	m.Hashes[r.Channel] = digest(b)
	return writeManifest(dir, *m)
}

func digest(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func writeManifest(dir string, m Manifest) error {
	mac, err := m.mac(m.key)
	if err != nil {
		return err
	}
	m.MAC = mac
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(dir, manifestName), b, 0o600)
}

// ReadManifest は一覧を読み、prepare が出した鍵 (16 進) で署名を確かめる。
func ReadManifest(dir, key string) (Manifest, error) {
	var m Manifest
	b, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return m, fmt.Errorf("%s は slack-bbs prepare の出力ではない: %w", dir, err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("%s を読めない: %w", manifestName, err)
	}
	if m.Version != Version {
		return m, fmt.Errorf("依頼の版 %d を読めない (この slack-bbs は %d)。prepare からやり直す", m.Version, Version)
	}
	for _, ch := range m.Channels {
		if !source.IsChannelID(ch) {
			return m, fmt.Errorf("%s のチャンネル ID %q がファイル名に使えない形", manifestName, ch)
		}
	}
	k, err := hex.DecodeString(key)
	if err != nil || len(k) == 0 {
		return m, errors.New("鍵 (-key) が無いか読めない。prepare の出力の key を渡す")
	}
	want, err := m.mac(k)
	if err != nil {
		return m, err
	}
	if !hmac.Equal([]byte(want), []byte(m.MAC)) {
		return m, fmt.Errorf("%s の署名が鍵と合わない (prepare の後に書き換えられたか、別の prepare の鍵)", manifestName)
	}
	return m, nil
}

// ErrNoResponse は分類の応答がまだ無いとき。
var ErrNoResponse = errors.New("分類の応答 (response.json) が無い")

// Read はチャンネルの依頼と応答を読む。依頼は prepare が書いたまま (一覧のハッシュと一致) で、単位の形が正しいものだけを受ける。
// 応答は board.Response の形の JSON 1 つだけを受ける (知らない欄・後ろに続く文字があれば誤り)。
func Read(dir string, m Manifest, ch string) (Request, board.Response, error) {
	var r Request
	b, err := os.ReadFile(RequestPath(dir, ch))
	if err != nil {
		return r, board.Response{}, err
	}
	if want := m.Hashes[ch]; want == "" || digest(b) != want {
		return r, board.Response{}, fmt.Errorf("依頼 %s が prepare の書いたものから変わっている。prepare からやり直す", RequestPath(dir, ch))
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, board.Response{}, fmt.Errorf("依頼を読めない: %w", err)
	}
	if r.Version != Version || r.Channel != ch {
		return r, board.Response{}, fmt.Errorf("依頼 %s の版・チャンネルが一覧と合わない", RequestPath(dir, ch))
	}
	if err := board.ValidateUnits(r.Units); err != nil {
		return r, board.Response{}, fmt.Errorf("依頼 %s: %w", RequestPath(dir, ch), err)
	}
	rb, err := os.ReadFile(ResponsePath(dir, ch))
	if errors.Is(err, fs.ErrNotExist) {
		return r, board.Response{}, ErrNoResponse
	}
	if err != nil {
		return r, board.Response{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(rb))
	dec.DisallowUnknownFields()
	var resp board.Response
	err = dec.Decode(&resp)
	if err == nil {
		if _, more := dec.Token(); !errors.Is(more, io.EOF) {
			err = errors.New("JSON の後ろに続きがある")
		}
	}
	if err != nil {
		return r, board.Response{}, fmt.Errorf("応答 %s を読めない (board.Response の形の JSON 1 つだけを書く): %w", ResponsePath(dir, ch), err)
	}
	return r, resp, nil
}

// Clean は prepare と分類する側が書いたファイル (一覧に載ったチャンネルの 3 つと一覧) だけを消す。ディレクトリは prepare が作ったもので、
// 空になったときだけ消す (他のファイルがあれば残し、left で知らせる。誤りにはしない)。
func Clean(dir string, m Manifest) (left bool, err error) {
	var errs []error
	for _, ch := range m.Channels {
		for _, p := range []string{RequestPath(dir, ch), PromptPath(dir, ch), ResponsePath(dir, ch)} {
			if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
		}
	}
	if err := os.Remove(filepath.Join(dir, manifestName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return false, errors.Join(errs...)
	}
	if !m.CreatedDir {
		return false, nil
	}
	if err := os.Remove(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return true, nil // 空でない: 他のファイルを巻き込まない
	}
	return false, nil
}

// Discard は依頼を 1 つも書けなかった prepare の後始末: prepare が作った空のディレクトリだけを消す。
func Discard(dir string, m Manifest) {
	if m.CreatedDir && len(m.Channels) == 0 {
		_ = os.Remove(dir) // 空でなければ消えない (他のものを巻き込まない)。消せなくても依頼は入っていない
	}
}
