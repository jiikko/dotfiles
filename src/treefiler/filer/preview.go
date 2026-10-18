package filer

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jiikko/dotfiles/src/termsafe"
)

// preview.go はタイルに出すテキストの読み込み。全部は読まず、先頭から chunk ずつ読み、ページ送りで続きを読む
// (spec §0.4。ユーザー回答 2026-10-07「巨大テキストは全部ロードしないで先頭だけにして」)。

const (
	sniffBytes = 8192    // 先頭のこの範囲に NUL があればバイナリ (treebeard の head と同じ。spec §5.5)
	chunkBytes = 1 << 16 // 1 回に読む量
)

// audioExt は開かずに断る音声 (spec §0.4。treebeard の対応形式)。
var audioExt = map[string]bool{".mp3": true, ".flac": true, ".wav": true, ".ogg": true, ".m4a": true, ".aac": true}

// imageExt は画像。プレビューは後回し (spec §0.1。2026-10-07 ユーザー回答) なので、今は開かずに断る。
var imageExt = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true, ".tif": true, ".tiff": true,
	".ico": true, ".svg": true, ".svgz": true, ".avif": true, ".heic": true, ".tga": true, ".psd": true,
}

// refuse は開けない理由を返す (開けるなら "")。toast で出す文 (spec §0.4)。
func refuse(n *node) string {
	ext := strings.ToLower(filepath.Ext(n.raw))
	switch {
	case audioExt[ext]:
		return "音声は表示できません: " + n.name
	case ext == ".pdf": // PDF は写さない (2026-10-09 ユーザー回答)。先頭に NUL が無い PDF もあるので、テキストとして開かせない
		return "PDF は表示できません: " + n.name
	case imageExt[ext]:
		return "画像はまだ表示できません: " + n.name
	}
	return ""
}

// errBinary は先頭に NUL を含むファイル。
var errBinary = errors.New("binary")

// errNotRegular は通常のファイルでないもの (FIFO・デバイス・ソケット)。
// 🚨 開く前に Stat で弾く: FIFO を os.Open すると書き手が来るまで返らず、画面が永久に固まる (レビューで再現 2026-10-08)
var errNotRegular = errors.New("not a regular file")

// maxLineBytes は 1 行の長さの上限。超えた分は次の行として続ける (改行の無い巨大なファイルで、1 行のために
// 全部を読んで複製するのを止める。spec §0.4 の「先頭だけ読む」をバイト数でも守る)。
const maxLineBytes = 16 << 10

// textSource は 1 つのファイルを先頭から読み進める。
//
// 🚨 ファイルを開いたままにしない (読んだ位置 off だけ覚え、読むたびに開き直す)。タイルの枚数に上限が無いので
// (spec §0.2)、開いたままだと macOS の fd の既定の上限 (256) に当たりうる。
type textSource struct {
	path    string
	off     int64
	lines   []string
	partial []byte // まだ改行が来ていない行の頭
	cont    []int  // maxLineBytes で切った続きの行の添字 (昇順)。行番号を数えない
	eof     bool
	size    int64
	limit   int64 // 0 でなければ、先頭からここまでで読むのを止める (Markdown は整形のため全体を持つので上限を付ける)
	readErr bool  // 末尾の前で読めなくなった (タイルに「途中で読めなくなりました」と出す。末尾と見分ける)
}

func openText(path string) (*textSource, error) {
	if st, err := os.Stat(path); err != nil {
		return nil, err
	} else if !st.Mode().IsRegular() {
		return nil, errNotRegular
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	head := make([]byte, sniffBytes)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	head = head[:n]
	if bytes.IndexByte(head, 0) >= 0 {
		return nil, errBinary
	}
	s := &textSource{path: path, size: st.Size(), off: int64(n)}
	s.feed(head)
	if n < sniffBytes {
		s.finish()
	}
	return s, nil
}

// feed は読んだバイト列を行に割る。表示前に termsafe を通し、タブを 4 桁に展開する
// (生のタブを端末へ出すとカーソルが飛んで行の右側が崩れる。モックで実測)。
func (s *textSource) feed(b []byte) {
	s.partial = append(s.partial, b...)
	for {
		i := bytes.IndexByte(s.partial, '\n')
		if i < 0 {
			if len(s.partial) < maxLineBytes {
				break
			}
			cut := utf8Boundary(s.partial, maxLineBytes)
			if cut == 0 {
				// 🚨 文字の途中にならない切れ目が無い (続きのバイト 0x80〜0xBF だけが続く不正な UTF-8)。切らずに戻すと
				// partial が減らず空の行を足し続け、タイルを開くと固まった (再現 2026-10-09)
				cut = maxLineBytes
			}
			s.lines = append(s.lines, cleanLine(string(s.partial[:cut])))
			s.partial = s.partial[cut:]
			s.cont = append(s.cont, len(s.lines)) // 次に足す行はこの行の続き
			continue
		}
		s.lines = append(s.lines, cleanLine(string(s.partial[:i])))
		s.partial = s.partial[i+1:]
	}
}

// srcLine は i 番目 (0 起点) の行のファイルでの行番号 (1 起点)。maxLineBytes で切った続きの行は 0 (番号を出さない)。
func (s *textSource) srcLine(i int) int {
	k := sort.SearchInts(s.cont, i)
	if k < len(s.cont) && s.cont[k] == i {
		return 0
	}
	return i + 1 - k
}

// estLines は行番号の溝の幅を決めるための行数の見積もり。末尾まで読んでいなければ、読んだバイトあたりの行数から
// 全体 (Markdown は読む上限まで) を見積もる (読み進めるたびに桁が増えて本文が横へずれるのを減らす。見積もりが外れれば増える)。
func (s *textSource) estLines() int {
	n := len(s.lines)
	end := s.size
	if s.limit > 0 {
		end = min(end, s.limit)
	}
	if !s.eof && s.off > 0 && end > s.off {
		n = max(n, int(int64(n)*end/s.off))
	}
	return n
}

func (s *textSource) finish() {
	if len(s.partial) > 0 {
		s.lines = append(s.lines, cleanLine(string(s.partial)))
		s.partial = nil
	}
	s.eof = true
}

// utf8Boundary は n 以下で、UTF-8 の文字の途中にならない切れ目。
func utf8Boundary(b []byte, n int) int {
	for n > 0 && n < len(b) && b[n]&0xC0 == 0x80 {
		n--
	}
	return n
}

func cleanLine(l string) string {
	l = strings.TrimSuffix(l, "\r")
	return strings.ReplaceAll(termsafe.PlainLineKeepTabs(l), "\t", "    ")
}

// ensure は少なくとも want 行が読めるまで (または末尾まで) 読み進める。読めなくなったら末尾として扱う。
func (s *textSource) ensure(want int) {
	if s.eof || len(s.lines) >= want {
		return
	}
	f, err := os.Open(s.path)
	if err != nil {
		s.readErr = true
		s.finish()
		return
	}
	defer f.Close()
	if _, err := f.Seek(s.off, io.SeekStart); err != nil {
		s.readErr = true
		s.finish()
		return
	}
	buf := make([]byte, chunkBytes)
	for !s.eof && len(s.lines) < want && (s.limit == 0 || s.off < s.limit) {
		n, err := f.Read(buf)
		s.off += int64(n)
		s.feed(buf[:n])
		if err != nil {
			s.readErr = !errors.Is(err, io.EOF)
			s.finish()
		}
	}
}
