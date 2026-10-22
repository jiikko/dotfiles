//go:build darwin

package filer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// getattrlistbulk は x/sys/unix にラッパーが無いので、番号 (SYS_GETATTRLISTBULK) で直接呼ぶ (cgo を使わない。
// darwin の Syscall6 は libc の syscall(2) を通る)。定数と並びは <sys/attr.h> と getattrlistbulk(2) の man。
//
// 1 件の記録の並び (FSOPT_PACK_INVAL_ATTRS 付き。実測 2026-10-12、APFS):
//
//	uint32 長さ / attribute_set_t (返った属性。uint32 × 5) / uint32 ATTR_CMN_ERROR / attrreference_t ATTR_CMN_NAME (この欄からの位置 int32・NUL 込みの長さ uint32)
//	/ uint32 ATTR_CMN_OBJTYPE / timespec ATTR_CMN_MODTIME / uint64 の大きさ (フォルダは ATTR_DIR_DATALENGTH、それ以外は ATTR_FILE_DATALENGTH の片方だけ) / 名前
//
// 🚨 大きさの欄は種類で片方しか入らない (返った属性の dirattr / fileattr で見分ける)。どちらも返らない項目と、ERROR が 0 でない項目は
// その項目だけ lstat で読み直す (並びの位置を推測しない)。記録が壊れて見えたら全体を errNoBulk にして今の形へ戻る。
// 🚨 フォルダの項目は全部 lstat で読み直す: マウントポイント・ファームリンク (/ の Users・Applications 等)・autofs では、bulk は覆われた側
// (元の空のフォルダ) の mtime と大きさを返し、lstat (= 今の形) は mount した先の root の値を返す (実測。
// / で 9 件・/Volumes・自分で mount した HFS+ / ExFAT / FAT)。フォルダは普通は項目の一部なので、まとめて読む得はほぼ残る。
const (
	attrBitMapCount      = 5
	attrCmnName          = 0x00000001
	attrCmnObjType       = 0x00000008
	attrCmnModTime       = 0x00000400
	attrCmnError         = 0x20000000
	attrCmnReturnedAttrs = 0x80000000
	attrDirDataLength    = 0x00000020
	attrFileDataLength   = 0x00000200
	fsoptPackInvalAttrs  = 0x00000008
	vDir                 = 2 // fsobj_type_t の VDIR
)

type attrList struct {
	bitmapCount uint16
	reserved    uint16
	commonAttr  uint32
	volAttr     uint32
	dirAttr     uint32
	fileAttr    uint32
	forkAttr    uint32
}

// bulkBuf は呼び出しごとの受け皿 (小さいフォルダを読むたびに確保しない)。
var bulkBuf = sync.Pool{New: func() any { b := make([]byte, 64<<10); return &b }}

func readEntriesBulk(dir string) ([]dirEntry, error) {
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(fd) }()
	al := attrList{
		bitmapCount: attrBitMapCount,
		commonAttr:  attrCmnReturnedAttrs | attrCmnName | attrCmnError | attrCmnObjType | attrCmnModTime,
		dirAttr:     attrDirDataLength,
		fileAttr:    attrFileDataLength,
	}
	bp := bulkBuf.Get().(*[]byte)
	defer bulkBuf.Put(bp)
	buf := *bp
	var out []dirEntry
	for {
		// 🚨 番号で直接呼ぶのは x/sys が非推奨にしている (Apple は syscall の番号を約束しない。libSystem を通すなら cgo が要る)。
		// 失敗は errno で今の形へ戻り、崩れた記録は parseBulkRecord が errNoBulk にする。macOS で bulk が動かなくなったら
		// TestBulkMatchesReadDirLstat が落ちる (黙って遅い経路に戻らない)。そのときは cgo で libc の getattrlistbulk を呼ぶ形へ移す
		n, _, errno := unix.Syscall6(unix.SYS_GETATTRLISTBULK, uintptr(fd), uintptr(unsafe.Pointer(&al)), //nolint:staticcheck // 上の注記 (番号を直接呼ぶ)
			uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), fsoptPackInvalAttrs, 0)
		if errno != 0 {
			return nil, fmt.Errorf("%w: %w", errNoBulk, errno)
		}
		if n == 0 {
			return out, nil
		}
		p := buf
		for range int(n) {
			e, l, err := parseBulkRecord(dir, p)
			if err != nil {
				return nil, err
			}
			if e.name != "" {
				out = append(out, e)
			}
			p = p[l:]
		}
	}
}

// parseBulkRecord は p の先頭の 1 件を読み、項目とその記録の長さを返す。名前が空なら落とす項目 (読み直しで消えていた)。
func parseBulkRecord(dir string, p []byte) (dirEntry, int, error) {
	bad := errors.New("getattrlistbulk の記録を読めない")
	if len(p) < 4 {
		return dirEntry{}, 0, fmt.Errorf("%w: %w", errNoBulk, bad)
	}
	l := int(binary.LittleEndian.Uint32(p))
	const fixed = 4 + 20 + 4 + 8 + 4 + 16 // 長さ・返った属性・ERROR・名前の参照・種類・mtime
	if l < fixed || l > len(p) {
		return dirEntry{}, 0, fmt.Errorf("%w: %w", errNoBulk, bad)
	}
	rec := p[:l]
	le := binary.LittleEndian
	common, dirA, fileA := le.Uint32(rec[4:]), le.Uint32(rec[12:]), le.Uint32(rec[16:])
	errCode := le.Uint32(rec[24:])
	nameAt := 28
	off, ln := int(int32(le.Uint32(rec[nameAt:]))), int(le.Uint32(rec[nameAt+4:]))
	start := nameAt + off
	if common&attrCmnName == 0 || ln < 1 || start < fixed || start+ln > l {
		return dirEntry{}, 0, fmt.Errorf("%w: %w", errNoBulk, bad)
	}
	name := string(rec[start : start+ln-1]) // 長さは NUL 込み
	if name == "" || name == "." || name == ".." {
		return dirEntry{}, l, nil
	}
	typ := le.Uint32(rec[36:])
	hasSize := dirA&attrDirDataLength != 0 || fileA&attrFileDataLength != 0
	if typ == vDir || errCode != 0 || common&(attrCmnObjType|attrCmnModTime) != attrCmnObjType|attrCmnModTime || !hasSize || l < fixed+8 {
		// この項目だけ今の形で読む (読めなければ、読む間に消えたとして落とす。readEntriesSlow と同じ)
		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil {
			return dirEntry{}, l, nil //nolint:nilerr // 読む間に消えた項目は落とす (上のコメント)
		}
		return dirEntry{name: name, entryAttr: attrOf(info)}, l, nil
	}
	mtime := time.Unix(int64(le.Uint64(rec[40:])), int64(le.Uint64(rec[48:])))
	size := int64(le.Uint64(rec[56:]))
	return dirEntry{name: name, entryAttr: entryAttr{mtime: mtime, size: size}}, l, nil // フォルダは上で lstat に回した
}
