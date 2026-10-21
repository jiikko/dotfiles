package tmpclean

import (
	"io/fs"
	"syscall"
	"testing"
	"time"
)

type flagsInfo uint32

func (flagsInfo) Name() string       { return "x" }
func (flagsInfo) Size() int64        { return 0 }
func (flagsInfo) Mode() fs.FileMode  { return fs.ModeDir }
func (flagsInfo) ModTime() time.Time { return time.Time{} }
func (flagsInfo) IsDir() bool        { return true }
func (f flagsInfo) Sys() any         { return &syscall.Stat_t{Flags: uint32(f)} }

// SF_* は root でしか付けられず実物のテストでは通せないので、st_flags を偽の FileInfo で渡して bit ごとに確かめる。
func TestUnmovableEachFlag(t *testing.T) {
	for name, bit := range map[string]uint32{
		"UF_IMMUTABLE": ufImmutable, "UF_APPEND": ufAppend, "SF_IMMUTABLE": sfImmutable,
		"SF_APPEND": sfAppend, "SF_RESTRICTED": sfRestricted, "SF_NOUNLINK": sfNoUnlink,
	} {
		if !unmovable(flagsInfo(bit)) {
			t.Errorf("%s (%#x) を移せない属性と見ていない", name, bit)
		}
	}
	if unmovable(flagsInfo(0x1)) { // UF_NODUMP は rename を拒まない
		t.Error("rename を拒まないフラグを移せない属性と見た")
	}
}
