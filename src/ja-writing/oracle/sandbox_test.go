package oracle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本家の道具は cwd から上へ .git・.yomiyasu-words・.yomiyasu を探す。使い捨ての dir がそれらの下に作られるなら、記録を取らずに止まる。
func TestNewSandboxRefusesDirUnderUpstreamMarker(t *testing.T) {
	for _, marker := range upstreamMarkers {
		t.Run(marker, func(t *testing.T) {
			top := t.TempDir()
			if err := os.Mkdir(filepath.Join(top, marker), 0o755); err != nil {
				t.Fatal(err)
			}
			tmp := filepath.Join(top, "a", "b")
			if err := os.MkdirAll(tmp, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TMPDIR", tmp)
			sb, err := newSandbox()
			if err == nil {
				sb.remove()
				t.Fatalf("%s の下の TMPDIR で sandbox が作れた", marker)
			}
			if !strings.Contains(err.Error(), filepath.Join(top, marker)) {
				t.Errorf("エラーが拾われるパスを示さない: %v", err)
			}
			if left, _ := filepath.Glob(filepath.Join(tmp, "yomiyasu-oracle-*")); len(left) != 0 {
				t.Errorf("断った sandbox の dir が残った: %v", left)
			}
		})
	}
}

// .git は worktree では dir でなくファイル。ファイルでも拾われる。
func TestNewSandboxRefusesGitFile(t *testing.T) {
	top := t.TempDir()
	if err := os.WriteFile(filepath.Join(top, ".git"), []byte("gitdir: /x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", top)
	if sb, err := newSandbox(); err == nil {
		sb.remove()
		t.Fatal(".git のファイルの下の TMPDIR で sandbox が作れた")
	}
}

func TestMarkerAboveIsEmptyForCleanTree(t *testing.T) {
	clean := t.TempDir()
	// t.TempDir の上 (os.TempDir) に偶然 marker があると検査にならないので、その場合は飛ばす
	if above, err := markerAbove(clean); err != nil || above != "" {
		t.Skipf("この環境の一時領域の上に %q がある (%v)", above, err)
	}
	t.Setenv("TMPDIR", clean)
	sb, err := newSandbox()
	if err != nil {
		t.Fatal(err)
	}
	sb.remove()
}
