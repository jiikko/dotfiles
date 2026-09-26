package live

import (
	"path/filepath"

	"pro-con/card"
	"pro-con/diskuse"
	"pro-con/store"
)

// DiskInput は pro-con の使うディスクを測る対象を、起動の記録 (今の分と退いた分) とカードの記録から組む (issue 456。読むだけ)。
// root は状態の置き場の根 (~/.local/state/pro-con)、dir はその下の本物のモードの置き場 (root/live)。読めなかった記録は warns に出す。
func DiskInput(root, dir, projects, binary string) (in diskuse.Input, warns []string) {
	in = diskuse.Input{StateDir: root, Projects: projects, Binary: binary, Done: map[string]bool{}}
	for _, f := range []string{RegistryFile, RetiredFile} {
		reg, err := LoadRegistry(filepath.Join(dir, f))
		if err != nil {
			warns = append(warns, f+" を読めない: "+err.Error())
		}
		for _, o := range reg {
			in.Sessions = append(in.Sessions, diskuse.Session{SessionID: o.SessionID, CardID: o.CardID, Cwd: o.Cwd})
		}
	}
	st, err := store.Load(dir)
	if err != nil {
		warns = append(warns, "カードの記録を読めない: "+err.Error())
	}
	for _, c := range st.Cards {
		in.Done[c.ID] = c.State == card.Done
	}
	return in, warns
}
