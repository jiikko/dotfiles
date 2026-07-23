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
	NoFrame  bool // 最外周フレーム (板 + ドロップシャドウ) を描かない (既定は描く。issue 025)
	Help     bool
	Revs     []string // revision 指定 (例: main, HEAD~10..HEAD)
	Paths    []string // "--" 以降の pathspec
}

// UnsupportedArgError は allowlist 外の引数。黙って無視せず、代替コマンドを案内する。
type UnsupportedArgError struct {
	Arg string
}

func (e *UnsupportedArgError) Error() string {
	return fmt.Sprintf("glogx: 未対応の引数です: %s\n\n%s\n代わりに git log %s をそのまま使ってください。", e.Arg, usageShort(), e.Arg)
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
				return nil, errors.New("glogx: -n には件数が必要です")
			}
			n, err := parseCount(argv[i+1])
			if err != nil {
				return nil, fmt.Errorf("glogx: -n の件数を解釈できません: %s", argv[i+1])
			}
			opts.MaxCount = n
			opts.HasCount = true
			i += 2
			continue
		case strings.HasPrefix(arg, "-n") && len(arg) > 2:
			n, err := parseCount(arg[2:])
			if err != nil {
				return nil, fmt.Errorf("glogx: -n の件数を解釈できません: %s", arg[2:])
			}
			opts.MaxCount = n
			opts.HasCount = true
		case strings.HasPrefix(arg, "--max-count="):
			n, err := parseCount(strings.TrimPrefix(arg, "--max-count="))
			if err != nil {
				return nil, fmt.Errorf("glogx: --max-count の件数を解釈できません: %s", arg)
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
		case arg == "--no-frame":
			opts.NoFrame = true
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
			return nil, errors.New("glogx: --cached は revision / pathspec と併用できません")
		}
		if opts.HasCount {
			return nil, errors.New("glogx: --cached は -n / --max-count と併用できません (対象は HEAD のみ)")
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
  --oneline                                       コンパクト 1 行表示
  --stat                                          diffstat を表示
  -p / --patch                                    patch を表示
  --cached                                        HEAD の CI 状態 + staged diff を表示
  --no-pager                                      対話ブラウズせず静的出力する
  --no-frame                                      最外周フレーム (板 + 影) を描かない
  --refresh                                       CI キャッシュを無視して再取得
  --no-cache                                      CI キャッシュを読み書きしない
  -h / --help                                     このヘルプを表示
  <revision> / -- <pathspec>                      git log へそのまま渡す`
}

// Usage はヘルプ全文。git log の全引数互換を目標にしない旨を明記する (issue の完了条件)。
func Usage() string {
	return `glogx — glog (read-only) に push 機能を足した派生版

コミット履歴を即時表示し、GitHub の CI 状態 (statusCheckRollup) を非同期で
埋める。TTY では less 風の対話ブラウズになり、コミットを選んで CI job の
一覧を展開できる。

使い方:
  glogx [オプション] [<revision>] [-- <pathspec>]
  glogx --cached [--stat | -p]

オプション:
  -n <count>, -n<count>, --max-count=<count>
        表示するコミット数。既定は 20 (git log と異なり全履歴を流さない。
        CI の一括取得数が表示件数に比例するため)。git log と同じく負数
        (例: -n -1) で無制限
  --oneline
        コンパクト 1 行形式で表示する。既定は git log 標準 (medium) 形式
        (commit 行 + Author + Date + メッセージ)
  --stat
        各コミットに diffstat を付ける。CI 記号は commit 行にだけ付く
  -p, --patch
        各コミットに patch を付ける。出力が大きくなるため -n の併用を推奨
  --cached
        git log には無いラッパー独自モード。HEAD の CI 状態と
        git diff --cached (staged 変更) を表示する。staged 変更自体には
        CI 結果が存在しないため、表示されるのは「HEAD の」CI 状態
        (--stat で diffstat、-p でフル patch。既定は diffstat)
  --no-pager
        TTY でも対話ブラウズを開かず、CI 取得完了後に静的出力する
  --no-frame
        対話ブラウズ画面を「余白 + 枠 + 右下ドロップシャドウ」で囲む最外周フレームを
        描かない (既定は描く)。極小端末では自動で無効化される。tmux popup が自前の枠を
        描く運用ではこれで二重枠を避けられる
  --refresh
        CI キャッシュを読まずに再取得する (取得結果はキャッシュへ保存する)
  --no-cache
        CI キャッシュを読みも書きもしない
  -h, --help
        このヘルプを表示する

対話ブラウズのキー操作 (TTY のみ。Ctrl-F は全ビューで → の別名。
⚠️ 本家 glog と異なり Ctrl-B の ← 別名は無い):
  b                         git push (y/N 確認つき。未 push が無ければ警告のみ。
                            diff 表示中の b はスクロールのまま)
  u                         git pull --rebase (y/N 確認つき。成功で一覧を再読込。
                            conflict なら自動で rebase --abort して元に戻す)
  (tmux prefix)             popup 内では tmux にキーが届かないため window 操作は
                            できない。押すと案内を表示し、続く 1 キーは無視する
  j / k / ↑ / ↓ / Ctrl-N / Ctrl-P
                            カーソル移動
  Enter / Space / l / → / Tab
                            CI job 一覧のポップアップを開く (コミット直下に表示)
  d                         コミットの diff (git show --stat --patch) を
                            ポップアップ表示 (もう一度 d で閉じる toggle)
  o                         コミットの GitHub ページをブラウザで開く
  p                         コミットに紐づく PR をブラウザで開く
                            (associatedPullRequests。複数あれば OPEN > MERGED 優先)
  U                         Claude Code の /usage 残量を右上モーダルで表示 (toggle)
  C                         claude update を実行する (確認なし即実行。結果は下部に表示)
  w                         直近の警告/エラーをクリップボードへコピー (トーストが消えた後も可。
                            tmux popup 内では copy-mode に入れないため pbcopy 直書きで取り出す)
  P                         コミットに紐づく PR の状態ポップアップ (state / draft /
                            レビュー / conflict / CI。o でブラウザ、y で URL コピー、
                            P/q/h で閉じる)
  Ctrl-D / Ctrl-U / PgDn / PgUp
                            ページスクロール
  g / G                     先頭 / 末尾のコミットへ
  q / Esc / Ctrl-C          終了 (git log の pager と同じく表示は消える)

CI job ポップアップ表示中 (開いた直後のフォーカスはタイトル行):
  j / k / ↑ / ↓ / Ctrl-N / Ctrl-P
                            フォーカス移動 (j で job へ降り、k でタイトル行へ戻る)
  Enter / Space             タイトル行: ポップアップを閉じる。job: 詳細ポップアップを
                            TUI 内で開く (Enter は一貫して「TUI 内の開閉 toggle」)
  l / → / Tab               job: 詳細ポップアップを開く (Enter と同じ)
  g / G                     先頭 / 末尾の job へ
  o                         選択中の job の詳細ページをブラウザで開く
  p                         コミットに紐づく PR をブラウザで開く (一覧と同じ)
  r                         選択中の失敗 job を再実行 (y/N 確認つき。gh run rerun --job。
                            GitHub Actions の失敗 job 限定)
  y                         URL をクリップボードへコピー (job 選択中はその job、
                            それ以外はコミットの URL。LLM に貼る用)
  Y                         選択中 job の詳細 (step 一覧 + annotations / ログ末尾) を
                            Markdown でクリップボードへコピー (LLM に貼る用。
                            未取得なら取得してからコピー)
  h / ← / Esc / q           ポップアップを閉じる (q はビューを 1 段戻る。
                            コミット一覧まで戻った q で終了。即終了は Ctrl-C)

job 詳細ポップアップ表示中 (上から step 一覧 (結論+所要時間) → annotations が
あれば file:line + エラーメッセージ、無ければログ末尾 50 行。失敗 job は
失敗ステップのみ。GitHub Actions の job 限定):
  j / k / Ctrl-D / Ctrl-U / g / G
                            スクロール (開いた直後は末尾 = 直近の出力)
  Enter / h / ← / Esc / q   閉じて job 一覧へ戻る (Enter は開閉 toggle)
  o                         ブラウザで開く
  y                         URL コピー
  Y                         表示中の詳細を Markdown でコピー (job 一覧と同じ)
  r                         失敗 job を再実行 (job 一覧と同じ)

diff ポップアップ表示中 (d で開く。ほぼ全画面のモーダル。less 流儀のスクロール):
  j / k / Enter / ↑ / ↓     1 行スクロール
  Space / f / b / Ctrl-D / Ctrl-U
                            半ページスクロール
  g / G                     先頭 / 末尾へ (末尾で止まり自動では閉じない)
  q / h / ← / Esc / d       閉じてコミット一覧へ戻る

  全件キャッシュ済みで 1 画面に収まる場合は、ブラウズを開かずそのまま
  出力して終了する (less -F 相当)。stdout がパイプ / リダイレクトの
  場合は常に静的出力で、ANSI カーソル制御は出さない。

CI 状態の記号:
  ✓  すべての対象 Check が成功 (skipped 混在は成功扱い)
  ✗  1 つ以上の Check が失敗
  ●  queued / in_progress / pending
  ⊘  cancelled / skipped / neutral のみ
  –  push 済みだが Check が存在しない
  ↑  未 push (GitHub 上にまだ存在しない。API には問い合わせない)
  ?  未取得・取得不能 (gh 未導入 / 未認証 / API 障害。30 秒だけ再取得しない)
  ⠋  取得中 (TTY のみ)

GitHub 連携と前提:
  - 認証は GitHub CLI (gh) へ委譲する。gh auth login 済みであること。
    gh が未導入・未認証でも Git 履歴の表示は成立する (CI 欄は ? / –)
  - remote (upstream → origin) から owner/repo を解決する。GitHub 以外の
    remote では CI 欄は – になる
  - CI 状態は ~/.cache/glog/ ($XDG_CACHE_HOME 対応) に状態別 TTL で
    キャッシュされる (success/failure 24h, pending 10s など。本家 glog と共有)

使用例:
  glogx                     直近 20 件をブラウズ
  glogx -n 5 --oneline      直近 5 件をコンパクト表示
  glogx --stat main..HEAD   main からの差分コミットを diffstat 付きで
  glogx -- src/glogx/        特定パスに触れたコミットだけ
  glogx --cached            commit 前に staged 変更と HEAD の CI を確認
  glogx --no-pager -n 50 | grep '✗'
                           失敗コミットだけ抜き出す (パイプでは記号は素の文字)

終了コード:
  0    Git 履歴の表示に成功 (CI 取得の失敗は警告 1 行に落として 0 を返す)
  2    引数エラー (未対応の引数を含む)
  それ以外  git 自体が失敗した場合、その終了コードをそのまま返す

git log の全引数への互換は目標にしていません。
上記以外の引数はエラーになります。その場合は git log を直接使ってください。
詳細: ~/dotfiles/src/glogx/README.md`
}
