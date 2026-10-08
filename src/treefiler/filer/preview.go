package filer

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
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

// imageExt は画像。プレビューは後回し (spec §0.1。2026-10-07 ユーザー回答。issue 688) なので、今は開かずに断る。
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
	eof     bool
	size    int64
	limit   int64 // 0 でなければ、先頭からここまでで読むのを止める (Markdown は整形のため全体を持つので上限を付ける)
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
			s.lines = append(s.lines, cleanLine(string(s.partial[:cut])))
			s.partial = s.partial[cut:]
			continue
		}
		s.lines = append(s.lines, cleanLine(string(s.partial[:i])))
		s.partial = s.partial[i+1:]
	}
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
		s.finish()
		return
	}
	defer f.Close()
	if _, err := f.Seek(s.off, io.SeekStart); err != nil {
		s.finish()
		return
	}
	buf := make([]byte, chunkBytes)
	for !s.eof && len(s.lines) < want && (s.limit == 0 || s.off < s.limit) {
		n, err := f.Read(buf)
		s.off += int64(n)
		s.feed(buf[:n])
		if err != nil {
			s.finish()
		}
	}
}
