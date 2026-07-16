package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Mode はラッパーの動作モード。git log 互換表示か、--cached (staged diff) 独自モードか。
type Mode int

const (
	ModeLog Mode = iota
	ModeCached
)

// defaultMaxCount は -n 未指定時の表示件数。git log の「全履歴」既定とは意図的に変える:
// pager を持たないインライン CLI で全履歴を流すのは実用性がなく、CI 状態の一括取得数も
// 表示件数に比例するため、既定は直近に絞る。全部見たいときは -n -1 (git と同じ負数=無制限)。
const defaultMaxCount = 20

// Options は allowlist 済みの CLI 引数。
type Options struct {
	Mode     Mode
	MaxCount int  // 負数 = 無制限 (git log と同じ)
	HasCount bool // -n / --max-count が明示されたか
	Stat     bool
	Patch    bool
	Oneline  bool // コンパクト 1 行表示 (既定は git log 標準形式)
	NoPager  bool // TTY でも対話ブラウズせず静的出力する
	Refresh  bool // キャッシュを読まず再取得する (取得結果は保存する)
	NoCache  bool // キャッシュを読みも書きもしない
	Help     bool
	Revs     []string // revision 指定 (例: main, HEAD~10..HEAD)
	Paths    []string // "--" 以降の pathspec
}

// UnsupportedArgError は allowlist 外の引数。黙って無視せず、代替コマンドを案内する。
type UnsupportedArgError struct {
	Arg string
}

func (e *UnsupportedArgError) Error() string {
	return fmt.Sprintf("glog: 未対応の引数です: %s\n\n%s\n代わりに git log %s をそのまま使ってください。", e.Arg, usageShort(), e.Arg)
}

// ParseArgs は argv (プログラム名を除く) を allowlist で解析する。
func ParseArgs(argv []string) (*Options, error) {
	opts := &Options{MaxCount: defaultMaxCount}
	i := 0
	for i < len(argv) {
		arg := argv[i]
		switch {
		case arg == "--":
			opts.Paths = append(opts.Paths, argv[i+1:]...)
			i = len(argv)
			continue
		case arg == "-n":
			if i+1 >= len(argv) {
				return nil, errors.New("glog: -n には件数が必要です")
			}
			n, err := parseCount(argv[i+1])
			if err != nil {
				return nil, fmt.Errorf("glog: -n の件数を解釈できません: %s", argv[i+1])
			}
			opts.MaxCount = n
			opts.HasCount = true
			i += 2
			continue
		case strings.HasPrefix(arg, "-n") && len(arg) > 2:
			n, err := parseCount(arg[2:])
			if err != nil {
				return nil, fmt.Errorf("glog: -n の件数を解釈できません: %s", arg[2:])
			}
			opts.MaxCount = n
			opts.HasCount = true
		case strings.HasPrefix(arg, "--max-count="):
			n, err := parseCount(strings.TrimPrefix(arg, "--max-count="))
			if err != nil {
				return nil, fmt.Errorf("glog: --max-count の件数を解釈できません: %s", arg)
			}
			opts.MaxCount = n
			opts.HasCount = true
		case arg == "--stat":
			opts.Stat = true
		case arg == "-p" || arg == "--patch":
			opts.Patch = true
		case arg == "--oneline":
			opts.Oneline = true
		case arg == "--no-pager":
			opts.NoPager = true
		case arg == "--cached":
			opts.Mode = ModeCached
		case arg == "--refresh":
			opts.Refresh = true
		case arg == "--no-cache":
			opts.NoCache = true
		case arg == "-h" || arg == "--help":
			opts.Help = true
		case strings.HasPrefix(arg, "-") && arg != "-":
			return nil, &UnsupportedArgError{Arg: arg}
		default:
			opts.Revs = append(opts.Revs, arg)
		}
		i++
	}

	if opts.Mode == ModeCached {
		if len(opts.Revs) > 0 || len(opts.Paths) > 0 {
			return nil, errors.New("glog: --cached は revision / pathspec と併用できません")
		}
		if opts.HasCount {
			return nil, errors.New("glog: --cached は -n / --max-count と併用できません (対象は HEAD のみ)")
		}
	}
	return opts, nil
}

func parseCount(s string) (int, error) {
	return strconv.Atoi(s)
}

func usageShort() string {
	return `対応している引数:
  -n <count> / -n<count> / --max-count=<count>   表示件数 (既定 20、負数で無制限)
  --stat                                          diffstat を表示
  -p / --patch                                    patch を表示
  --oneline                                       コンパクト 1 行表示 (既定は git log 標準形式)
  --no-pager                                      対話ブラウズせず静的出力する
  --cached                                        HEAD の CI 状態 + staged diff を表示
  --refresh                                       CI キャッシュを無視して再取得
  --no-cache                                      CI キャッシュを読み書きしない
  <revision> / -- <pathspec>                      git log へそのまま渡す`
}

// Usage はヘルプ全文。git log の全引数互換を目標にしない旨を明記する (issue の完了条件)。
func Usage() string {
	return `glog — GitHub Actions / Checks の結果を添える git log ラッパー

使い方:
  glog [-n <count>] [--stat] [-p] [<revision>] [-- <pathspec>]
  glog --cached [--stat | -p]

` + usageShort() + `

git log の全引数への互換は目標にしていません。
上記以外の引数が必要な場合は git log を直接使ってください。

TTY では less 風の対話ブラウズになります:
  j/k/↑/↓  コミット移動      Enter/Space  CI job 一覧の展開/折りたたみ
  Ctrl-D/U ページスクロール   q            終了 (最終表示は履歴に残る)

CI 状態の記号:
  ✓ 成功   ✗ 失敗   ● 実行中/待機   ⊘ cancelled/skipped   – Check なし   ? 取得不能`
}
