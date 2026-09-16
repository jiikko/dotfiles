// lockman — ディレクトリ単位の排他を取る CLI (SMB 越しの複数マシン + 公開ホストの
// ローカル経路が混在する前提)。仕様の正本は issue 091。
//
// 設計の要点 (崩すと排他が消える):
//   - 勝敗は「存在すれば失敗する 1 回の原子操作」だけで決める。事前に存在チェックをしない
//   - 期限切れの引き継ぎは unlink ではなく「存在しない名前への rename」で勝者を 1 人に絞る
//   - TTL 判定にローカル時計を使わない (probe ファイルの mtime = 公開ホストの時計)
//   - 判定不能は必ず busy / エラー側へ倒す。「空いている」に倒さない
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strings"
	"time"
)

// 終了コードは呼び出し側の shell が分岐に使う API。issue 091 の表と一致させること。
//
// acquire / release / renew / check / status / break / cleanup:
//
//	0 成功 / 1 エラー (判定不能を含む) / 3 他者が保持中 / 4 持ち主ではない
//
// with は子プロセスの終了コードをそのまま透過するため、ロック側の失敗は
// 子と衝突しない上位の番号へ逃がす (timeout(1) / env(1) と同じ流儀)。
const (
	exitOK          = 0
	exitError       = 1
	exitBusy        = 3
	exitNotOwner    = 4
	exitWithBusy    = 121
	exitWithLost    = 122
	exitWithInvalid = 125
)

const (
	defaultTTL = 30 * time.Minute
	minTTL     = 30 * time.Second // SMB の属性キャッシュ遅延より十分大きくするための下限
	// --io-timeout の下限 / 上限。下限は「詰まっていないマウントを詰まっていると
	// 誤判定しない」ため、上限は「固まったときに人が待てる」ため (既定は 10s)。
	minIOTimeout     = 100 * time.Millisecond
	maxIOTimeout     = 5 * time.Minute
	defaultIOTimeout = 10 * time.Second
)

// commands はサブコマンド名とその 1 行説明。usage の出典をここ 1 つに保つ。
var commands = map[string]string{
	"acquire": "ロックを取る (取れなければ exit 3)",
	"release": "自分が取ったロックを解放する",
	"renew":   "保持を更新する (失っていれば exit 4)",
	"check":   "空いているかを見る (参考値。排他の根拠にはならない)",
	"status":  "誰が・いつから・あと何秒かを表示する",
	"with":    "取得 → コマンド実行 → 確実に解放 (長い処理はこれを使う)",
	"break":   "人が今すぐ剥がす (graveyard へ退避する)",
	"cleanup": "残骸の掃除だけを明示的に走らせる",
}

func usage(w *os.File) {
	fmt.Fprintf(w, `usage: lockman <command> <dir> [options]

長い処理には with を使うこと。acquire を素で使い renew を呼ばないと、TTL を超えた
時点で他者に引き継がれ、二重に書き込むことになる。

commands:
`)
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(w, "  %-8s %s\n", name, commands[name])
	}
	fmt.Fprintf(w, `
exit codes:
  acquire/release/renew/check/status/break/cleanup:
    0 成功 / 1 エラー (判定不能を含む) / 3 他者が保持中 / 4 持ち主ではない
  with:
    子プロセスの終了コードを透過 / 121 取得できない / 122 走行中に lease を失った
    125 lockman 自体のエラー

注意:
  - check は参考値。排他の根拠にならない (読んだ次の瞬間に変わる)
  - 待ち行列は持たない。--wait を付けても公平性は保証しない
  - 再入不可。同じディレクトリを二重に acquire すると自分で自分を締め出す
  - 複数ディレクトリを取るときは絶対パスの辞書順で取る (デッドロック回避)

仕様: issue 091
`)
}

type opts struct {
	ttl       time.Duration
	wait      time.Duration
	ioTimeout time.Duration
	label     string
	token     string
	tokenFile string
	jsonOut   bool
	verbose   bool
	force     bool
	onLost    string
}

func parseFlags(cmd string, args []string) (*opts, []string, error) {
	o := &opts{}
	fs := flag.NewFlagSet("lockman "+cmd, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.DurationVar(&o.ttl, "ttl", defaultTTL, "保持期間 (下限 30s)")
	fs.DurationVar(&o.wait, "wait", 0, "取れるまで待つ上限 (既定は待たない)")
	fs.DurationVar(&o.ioTimeout, "io-timeout", defaultIOTimeout, "I/O が返らないと判断するまでの時間")
	fs.StringVar(&o.label, "label", "", "何のための保持かを記録する")
	fs.StringVar(&o.token, "token", "", "トークンを直接渡す")
	fs.StringVar(&o.tokenFile, "token-file", "", "トークンの読み書き先 (推奨)")
	fs.BoolVar(&o.jsonOut, "json", false, "機械可読な出力")
	fs.BoolVar(&o.verbose, "verbose", false, "掃除などの詳細を stderr に出す")
	fs.BoolVar(&o.force, "force", false, "確認せず実行する")
	fs.StringVar(&o.onLost, "on-lost", "kill", "with で lease を失ったとき: kill | warn")
	// 🚨 flag は最初の非フラグ引数で解析を止める。シェルからは
	// `lockman acquire "$dir" --ttl 5m` の順で書くのが自然なので、位置引数を
	// 取り除きながら解析し直して、フラグが後ろに来ても効くようにする。
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return nil, nil, err
		}
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}
	return o, positional, nil
}

func (o *opts) resolveToken() (string, error) {
	if o.token != "" {
		return o.token, nil
	}
	if o.tokenFile == "" {
		return "", errors.New("--token または --token-file が要る")
	}
	b, err := readFileTimed(o.tokenFile, o.ioTimeout)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// run は終了コードを返す。os.Exit をここに書かないのはテストから呼べるようにするため。
func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return exitError
	}
	cmd := args[0]
	if cmd == "-h" || cmd == "--help" || cmd == "help" {
		usage(os.Stdout)
		return exitOK
	}
	if _, ok := commands[cmd]; !ok {
		warnf("未知のサブコマンド: %s", cmd)
		usage(os.Stderr)
		return exitError
	}
	// with は "--" の後ろを子のコマンドとして扱うので、フラグ解析の対象から外す。
	rest := args[1:]
	var child []string
	if cmd == "with" {
		for i, a := range rest {
			if a == "--" {
				child, rest = rest[i+1:], rest[:i]
				break
			}
		}
	}
	o, positional, err := parseFlags(cmd, rest)
	if err != nil {
		return exitError
	}
	if len(positional) < 1 {
		warnf("対象ディレクトリを指定すること")
		return exitError
	}
	// 🚨 --io-timeout の値検証。0 や負値は**全操作を即「判定不能」**にする (0 を「無制限」と
	// 読む CLI が多いので、黙って受けると意図と逆に倒れる)。上限も要る: 大きすぎると
	// cleanup.go の minRetention が前提にしている「--io-timeout は十分小さい」が崩れる。
	// (issue 359 の残タスク / cleanup.go の注記が「356 / 357 と一緒に直す」と予告していた分)
	if o.ioTimeout < minIOTimeout || o.ioTimeout > maxIOTimeout {
		warnf("--io-timeout が範囲外 (%v)。%v 〜 %v で指定すること", o.ioTimeout, minIOTimeout, maxIOTimeout)
		return failCode(cmd, exitError)
	}
	if o.ttl < minTTL {
		// 短い TTL は SMB の属性キャッシュ遅延に埋もれ、生きている lock を stale と
		// 誤判定する。警告ではなくエラーで拒否する。
		warnf("--ttl が短すぎる (%v)。下限は %v", o.ttl, minTTL)
		return exitError
	}
	if cmd == "with" && len(child) == 0 {
		warnf("with は -- の後ろに実行するコマンドが要る")
		return exitWithInvalid
	}

	l, err := NewLocker(positional[0], o.ioTimeout)
	if err != nil {
		warnf("%v", err)
		return failCode(cmd, exitError)
	}
	return dispatch(cmd, l, o, child)
}

func dispatch(cmd string, l *Locker, o *opts, child []string) int {
	// 掃除は状態を変えるコマンドのときだけ。check / status はループから呼ばれるため走らせない。
	if cmd == "acquire" || cmd == "with" || cmd == "break" || cmd == "cleanup" {
		defer func() {
			res := l.CleanupTimed(cmd == "cleanup" || o.force)
			// 失敗は verbose に関係なく出す。verbose 任せにすると、掃除が止まって
			// いること自体が観測できない (掃除の失敗は打刻もしないので毎回出る)。
			// 件数も一緒に出す: 部分的に進んでいるのか何も進んでいないのかは、
			// 失敗している場面でこそ知りたい。
			if len(res.Errors) > 0 || o.verbose {
				warnf("cleanup: removed=%d skipped=%v errors=%v", res.Removed, res.Skipped, res.Errors)
			}
		}()
	}

	switch cmd {
	case "acquire":
		return cmdAcquire(l, o)
	case "release":
		return cmdTokenOp(l, o, l.ReleaseTimed)
	case "renew":
		return cmdTokenOp(l, o, l.RenewTimed)
	case "check":
		st, err := l.InspectTimed()
		if err != nil {
			warnf("%v", err)
			return exitBusy // 判定不能は busy 側へ倒す (空いているとは言わない)
		}
		if o.jsonOut {
			printJSON(st)
		}
		if st.Held {
			return exitBusy
		}
		return exitOK
	case "status":
		st, err := l.InspectTimed()
		if err != nil {
			warnf("%v", err)
			return exitError
		}
		if o.jsonOut {
			printJSON(st)
		} else if st.Unreadable {
			// 🚨 **一過性か恒久かは言わない** (1 回の観測では区別できない)。人が判断する材料を出す。
			fmt.Printf("unreadable lock (size=%dB, age=%ds) — 書き込み中の一瞬か、書きかけの残骸。"+
				"経過が伸び続けるなら残骸。`lockman break` で剥がせる (保持者が居ないことを確かめてから)\n",
				st.SizeBytes, st.AgeSec)
		} else if st.Held {
			fmt.Printf("held by %s@%s (label=%s, age=%ds, expires_in=%ds)\n",
				st.User, st.Host, st.Label, st.AgeSec, st.ExpiresIn)
		} else {
			fmt.Println("free")
		}
		// 🚨 中身を読めない lock は**空いているとは言わない** (`check` の「判定不能は busy 側へ
		// 倒す」と揃える)。旧版は rc=1 = 「道具が壊れた」で、監視からは dir が無いのと区別できなかった
		if st.Unreadable {
			return exitBusy
		}
		return exitOK
	case "with":
		return runWith(l, o.ttl, o.label, o.onLost != "warn", child)
	case "break":
		if err := l.BreakTimed(); err != nil {
			warnf("%v", err)
			return exitError
		}
		return exitOK
	case "cleanup":
		return exitOK // 掃除自体は defer で走る
	}
	return exitError
}

func cmdAcquire(l *Locker, o *opts) int {
	deadline := time.Now().Add(o.wait)
	backoff := time.Second
	for {
		meta, err := l.AcquireTimed(o.ttl, o.label)
		switch {
		case err == nil:
			if o.tokenFile != "" {
				if werr := writeFileTimed(o.tokenFile, []byte(meta.Token+"\n"), 0o600, o.ioTimeout); werr != nil {
					// トークンを渡せないと解放できなくなる。取ったロックを戻してから失敗する。
					_ = l.ReleaseTimed(meta.Token)
					warnf("トークンを書けない: %v", werr)
					return exitError
				}
			} else {
				fmt.Println(meta.Token)
			}
			return exitOK
		case errors.Is(err, errBusy):
			if o.wait <= 0 || time.Now().After(deadline) {
				// 🚨 **「人が動くまで解けない busy」のときだけ理由を出す** (issue 383)。
				// 正常な busy まで鳴らすと、`lockman acquire || exit 0` のような cron が
				// skip のたびにメールを飛ばすようになり、operator が `2>/dev/null` を足す →
				// **本当に伝えたい wedge の案内まで黙る** (敵対レビュー P2)。
				// 待ち直す枝でも出さない (毎周回 stderr を汚さないため)。
				if errors.Is(err, errUnreadableLock) {
					warnf("%v", err)
				}
				return exitBusy
			}
		default:
			warnf("%v", err)
			return exitError
		}
		// 複数マシンが同時に待つと再試行が同期して叩き合うため、ジッタを必ず入れる。
		sleep := backoff + time.Duration(rand.Int63n(int64(backoff*3/5))) - backoff*3/10
		if remain := time.Until(deadline); remain < sleep {
			sleep = remain
		}
		if sleep > 0 {
			time.Sleep(sleep)
		}
		if backoff < 15*time.Second {
			backoff *= 2
		}
	}
}

func cmdTokenOp(l *Locker, o *opts, fn func(string) error) int {
	token, err := o.resolveToken()
	if err != nil {
		warnf("%v", err)
		return exitError
	}
	if err := fn(token); err != nil {
		if errors.Is(err, errNotOwner) {
			warnf("%v", err)
			return exitNotOwner
		}
		warnf("%v", err)
		return exitError
	}
	return exitOK
}

// failCode は with のときだけ番号空間を上位へ寄せる。
func failCode(cmd string, code int) int {
	if cmd == "with" && code == exitError {
		return exitWithInvalid
	}
	return code
}

func printJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		warnf("JSON にできない: %v", err)
		return
	}
	fmt.Println(string(b))
}

func main() {
	os.Exit(run(os.Args[1:]))
}
