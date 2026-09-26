package dispatcher

// 入れ替え (issue 505。dispatcher が新版のバイナリへ syscall.Exec する) のための dispatcher 側の口。
// 安全な区切り (Busy) と、新版が今の記録を読めるかの確かめ (Preflight)。入れ替えそのものは main (dispupgrade.go)。
// exec はプロセス像を置き換えるので、このプロセスのメモリにしか無いもの (裏で走る子・結果の分からない起動) が残っていれば待つ。
// PG・PM・取り込みの係は Claude Code の bg の session で、dispatcher の子ではない (入れ替えで止まらない)。

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"pro-con/live"
	"pro-con/store"
)

// Preflight は、このバイナリが dir の記録を読めるか (新版へ入れ替える前に、新版のバイナリに走らせる。書かない・lock を取らない)。
// 記録の形を変えた新版が今の記録を読めないなら、入れ替えずに旧版のまま続ける (読めない記録で回ると、Tick が失敗して dispatcher が抜ける)。
// 旧版も同じ確かめを自分で走らせ、旧版は読めて新版は読めないときだけ新版のせいにする (壊れた記録は旧版でも読めない)。
func Preflight(dir string) error {
	if _, err := store.Load(dir); err != nil {
		return err
	}
	if _, err := store.LoadArchive(dir); err != nil {
		return err
	}
	for _, r := range roles() {
		if _, err := store.LoadRole(dir, r.file, r.name); err != nil {
			return err
		}
	}
	if _, err := live.LoadRegistry(filepath.Join(dir, live.RegistryFile)); err != nil {
		return err
	}
	_, err := store.LoadSettings(dir)
	return err
}

// Notified は人の番を macOS に知らせ済みの鍵 (入れ替えで新版へ渡す。渡さないと、切り替えのたびに人の番を全件知らせ直す)。
func (d *Dispatcher) Notified() []string {
	out := make([]string, 0, len(d.notified))
	for k, ok := range d.notified {
		if ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// SeedNotified は入れ替えの前のプロセス像が知らせ済みだった鍵を戻す (最初の Tick の前に呼ぶ)。
func (d *Dispatcher) SeedNotified(keys []string) {
	if d.notified == nil {
		d.notified = map[string]bool{}
	}
	for _, k := range keys {
		if k != "" {
			d.notified[k] = true
		}
	}
}

// Busy は今入れ替えてはいけない理由 ("" なら安全な区切り)。Tick と Tick の間 (serve の中) で呼ぶ。
// 記録を読めなければエラー (区切りか分からないので、呼び出し側は待つ)。
func (d *Dispatcher) Busy() (string, error) {
	var why []string
	if d.active != nil { // 子 (make test 等) を exec で置き去りにし、結果も PG へ返せない
		why = append(why, "テストの係が "+d.active.cardID+" を実行中")
	}
	if d.btw != nil { // haiku の子を置き去りにする
		why = append(why, "btw の答えを "+d.btw.cardID+" に作っている")
	}
	if d.progressBusy.Load() { // git の子を置き去りにする
		why = append(why, "進捗を集めている")
	}
	if d.blocked != nil { // repo の lock 待ち: 諦めるまでの起点 (runLockGiveUp) と取り直しの時刻はメモリにある
		why = append(why, d.blocked.cardID+" のテストの係が repo の lock を待っている")
	}
	if len(d.stopFrom) > 0 { // 諦めるまでの時間の起点はメモリにある (入れ替えるたびに数え直すと諦めない)
		why = append(why, "閉じた・削除のカードの PG を止めている")
	}
	st, err := store.Load(d.Dir)
	if err != nil {
		return "", err
	}
	for _, c := range st.Cards { // 起動・再開の結果を確かめる前 (launchGrace の起点は記録にあるが、結果待ちの間は動かさない)
		if c.Launching != "" {
			why = append(why, fmt.Sprintf("%s の PG の%sを確かめている", c.ID, c.Launching))
		}
	}
	for _, r := range roles() {
		pm, err := store.LoadRole(d.Dir, r.file, r.name)
		if err != nil {
			return "", err
		}
		if pm.Launching != "" {
			why = append(why, fmt.Sprintf("%sの%sを確かめている", r.name, pm.Launching))
		}
	}
	return strings.Join(why, " / "), nil
}
