package tmpclean

import (
	"io/fs"
	"syscall"
)

// rename を拒む st_flags (<sys/stat.h>)。SIP が守る置き場は SF_NOUNLINK / SF_RESTRICTED を持ち、利用者の権限では移せない
// ($TMPDIR の AudioConverterService などのサービスの dir。名前の一覧では足りないので属性で見る)。
const (
	ufImmutable   = 0x00000002
	ufAppend      = 0x00000004
	sfImmutable   = 0x00020000
	sfAppend      = 0x00040000
	sfRestricted  = 0x00080000
	sfNoUnlink    = 0x00100000
	unmovableMask = ufImmutable | ufAppend | sfImmutable | sfAppend | sfRestricted | sfNoUnlink
)

// unmovable は fi に rename を拒む属性が付いているかを返す。
func unmovable(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Flags&unmovableMask != 0
}
