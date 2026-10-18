package subproc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// GitRepoArgs は GitArgs に加えて、dir の repo のローカルの設定 (`.git/config` と、そこから include したファイル・config.worktree) で
// 定義された filter (clean / smudge / process) を -c で止める引数を返す。dir が空なら -C を付けず、cwd の repo を見る。
//
// 🚨 表示のために作業ツリーを読むだけの呼び出し (status・作業ツリーとの diff) に使う。add / commit など内容を書き込む操作には使わない:
// repo のローカルに `git lfs install --local` した repo では、clean を止めると pointer の代わりに本体を書き込む。
//
// filter の名前は .gitattributes に書かれた任意の文字列で、git の設定には「全部の filter を止める」鍵が無い。attributes の側も
// `.git/info/attributes` は attr.tree / GIT_ATTR_SOURCE で差し替えられない (git 2.55.0 で実測)。そこで git 自身に設定を
// scope つきで列挙させ、ローカルで定義された鍵 (clean / smudge / process) ごとに上書きする。上書きの値は global / system / -c (command) に同じ鍵があればその値
// (git-lfs は `git lfs install` で global に置くので、そのまま動く)、無ければ空。空にした名前は required=false も付ける
// (required=true のまま clean を空にすると status が「clean filter failed」で落ちる)。
//
// 🚨 上書きが届かないもの (呼び出し側の責任):
//   - submodule: status / diff は submodule の中で子の git を起こし、子は submodule 自身の設定 (.git/modules/<名前>/config) の filter を読む。
//     呼び出し側で --ignore-submodules=dirty を付けて子を起こさない (-c diff.ignoreSubmodules は repo の submodule.<名前>.ignore に負ける。レビューで再現)
//
// 割り切り: ローカルで filter を設定した正当な repo (`git lfs install --local`・git-crypt) では、内容が変わっていないファイルが変更として出る
// (pointer / 暗号文と平文を比べる。--no-optional-locks で index の stat が直らないので、シェルで git status を打つまで出続ける)。
func GitRepoArgs(ctx context.Context, dir string, args ...string) ([]string, error) {
	listArgs := []string{"config", "--show-scope", "-z", "--get-regexp", `^filter\.`}
	if dir != "" {
		listArgs = append([]string{"-C", dir}, listArgs...)
	}
	cmd := CommandContext(ctx, "git", GitArgs(listArgs...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 && stderr.Len() == 0 {
		out, err = nil, nil // 一致する鍵が無い
	}
	if err != nil {
		return nil, fmt.Errorf("git config で filter を列挙できない: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	overrides, err := filterOverrides(out)
	if err != nil {
		return nil, err
	}
	head := make([]string, 0, len(overrides)*2+2)
	for _, o := range overrides {
		head = append(head, "-c", o)
	}
	if dir != "" {
		head = append(head, "-C", dir)
	}
	return GitArgs(append(head, args...)...), nil
}

// GitRepoCommand は GitRepoArgs の引数で、WaitDelay を張った git を返す。
func GitRepoCommand(ctx context.Context, dir string, args ...string) (*exec.Cmd, error) {
	a, err := GitRepoArgs(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	return CommandContext(ctx, "git", a...), nil
}

// filterCommandVars は filter.<名前>.<鍵> のうち、コマンドを走らせる鍵。
var filterCommandVars = []string{"clean", "smudge", "process"}

// filterOverrides は `git config --show-scope -z --get-regexp '^filter\.'` の出力 (scope\0key\nvalue\0 の並び) から、-c に渡す
// "key=value" を返す。ローカルの scope (local・worktree など、system / global / command 以外) で定義されたコマンドの鍵が対象。
func filterOverrides(out []byte) ([]string, error) {
	type entry struct{ scope, name, key, value string }
	var es []entry
	fields := strings.Split(string(out), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		scope, kv := fields[i], fields[i+1]
		key, value, _ := strings.Cut(kv, "\n")
		rest, ok := strings.CutPrefix(key, "filter.")
		dot := strings.LastIndexByte(rest, '.')
		if !ok || dot < 0 {
			continue // filter.<鍵> (subsection の無い形) は filter として読まれない。filter..clean (空の名前) は読まれる (`* filter=` と一致する)
		}
		es = append(es, entry{scope, rest[:dot], rest[dot+1:], value})
	}
	trusted := func(scope string) bool { return scope == "system" || scope == "global" || scope == "command" }
	var names []string
	seen := map[string]bool{}
	for _, e := range es {
		if !trusted(e.scope) && !seen[e.name] {
			seen[e.name] = true
			names = append(names, e.name)
		}
	}
	var res []string
	for _, name := range names {
		// -c の "key=value" は最初の = で切られるので、名前に = や改行があると別の鍵になって上書きが外れる。止められないので落とす
		if strings.ContainsAny(name, "=\n") {
			return nil, fmt.Errorf("repo の設定の filter の名前 %q は -c で上書きできない", name)
		}
		blanked := false
		for _, v := range filterCommandVars {
			// ローカルで定義された鍵だけを上書きする。🚨 定義されていない鍵まで空にしない: process は空でも「設定済み」と読まれ、
			// global の clean まで呼ばれなくなる (git 2.55.0 で実測)
			local := false
			val, found := "", false
			for _, e := range es { // 後に読まれた値が勝つので、信頼できる scope の最後の値を取る
				if e.name != name || e.key != v {
					continue
				}
				if trusted(e.scope) {
					val, found = e.value, true
				} else {
					local = true
				}
			}
			if !local {
				continue
			}
			if !found {
				blanked = true
			}
			res = append(res, "filter."+name+"."+v+"="+val)
		}
		if blanked {
			res = append(res, "filter."+name+".required=false")
		}
	}
	return res, nil
}
