// lockman — ディレクトリ単位の排他を取る CLI (SMB 越しの複数マシン + 公開ホストの
// ローカル経路が混在する前提)。仕様の正本は issues/091-feat-lockman-directory-lease-lock.md。
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
	defaultTTL       = 30 * time.Minute
	minTTL           = 30 * time.Second // SMB の属性キャッシュ遅延より十分大きくするための下限
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

仕様: issues/091-feat-lockman-directory-lease-lock.md
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
	// ⚠️ flag は最初の非フラグ引数で解析を止める。シェルからは
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
	b, err := os.ReadFile(o.tokenFile)
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
			res := l.Cleanup(cmd == "cleanup" || o.force, "")
			if o.verbose {
				warnf("cleanup: removed=%d skipped=%v errors=%v", res.Removed, res.Skipped, res.Errors)
			}
		}()
	}

	switch cmd {
	case "acquire":
		return cmdAcquire(l, o)
	case "release":
		return cmdTokenOp(l, o, func(token string) error { return l.Release(token, o.ttl) })
	case "renew":
		return cmdTokenOp(l, o, l.Renew)
	case "check":
		st, err := timed(l, func() (*State, error) { return l.Inspect(o.ttl) })
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
		st, err := timed(l, func() (*State, error) { return l.Inspect(o.ttl) })
		if err != nil {
			warnf("%v", err)
			return exitError
		}
		if o.jsonOut {
			printJSON(st)
		} else if st.Held {
			fmt.Printf("held by %s@%s (label=%s, age=%ds, expires_in=%ds)\n",
				st.User, st.Host, st.Label, st.AgeSec, st.ExpiresIn)
		} else {
			fmt.Println("free")
		}
		return exitOK
	case "with":
		return runWith(l, o.ttl, o.label, o.onLost != "warn", child)
	case "break":
		if _, err := timed(l, func() (*State, error) { return nil, l.Break() }); err != nil {
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
		meta, err := timed(l, func() (*Meta, error) { return l.Acquire(o.ttl, o.label) })
		switch {
		case err == nil:
			if o.tokenFile != "" {
				if werr := os.WriteFile(o.tokenFile, []byte(meta.Token+"\n"), 0o600); werr != nil {
					// トークンを渡せないと解放できなくなる。取ったロックを戻してから失敗する。
					_ = l.Release(meta.Token, o.ttl)
					warnf("トークンを書けない: %v", werr)
					return exitError
				}
			} else {
				fmt.Println(meta.Token)
			}
			return exitOK
		case errors.Is(err, errBusy):
			if o.wait <= 0 || time.Now().After(deadline) {
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
	if _, err := timed(l, func() (*State, error) { return nil, fn(token) }); err != nil {
		if errors.Is(err, errNotOwner) {
			warnf("%v", err)
			return exitNotOwner
		}
		warnf("%v", err)
		return exitError
	}
	return exitOK
}

// timed は I/O が固まったときに沈黙しないための包み。
func timed[T any](l *Locker, fn func() (T, error)) (T, error) {
	return withTimeout(l.timeout, fn)
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
