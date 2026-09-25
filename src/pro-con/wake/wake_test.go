package wake

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitFor は cond が真になるまで待つ (上限はハングの防止。時間は判定に使わない)。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 400 {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("%s (10 秒待っても成立しない)", what)
}

func listen(t *testing.T, dir string) *Server {
	t.Helper()
	s, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// shortDir は socket のパスが上限に収まる一時ディレクトリ (t.TempDir は macOS では長く、一時ディレクトリへ逃がす側に倒れる)。
func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "wk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

// Poke を受けると Wakes に値が入る。socket は自分だけが読み書きできる。
func TestPokeWakes(t *testing.T) {
	dir := shortDir(t)
	s := listen(t, dir)
	if Path(dir) != filepath.Join(canonical(dir), File) {
		t.Fatalf("短い置き場なのに置き場の外に置いた: %s", Path(dir))
	}
	if st, err := os.Stat(Path(dir)); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("socket の権限: %v %v", st.Mode(), err)
	}
	if err := Poke(dir); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Wakes():
	case <-time.After(10 * time.Second):
		t.Fatal("Poke で起きない")
	}
}

// dispatcher が居なければ Poke は誤りを返す (ハングしない)。
func TestPokeWithoutServer(t *testing.T) {
	if err := Poke(shortDir(t)); err == nil {
		t.Fatal("居ない dispatcher を起こせたことにした")
	}
}

// 購読している画面は、Broadcast のたびに知らされる。dispatcher が起動し直しても繋ぎ直す。
func TestSubscribeGetsBroadcastAcrossRestart(t *testing.T) {
	old := retryEvery
	retryEvery = 20 * time.Millisecond
	t.Cleanup(func() { retryEvery = old })
	dir := shortDir(t)
	s, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan struct{}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	sub := NewSubscriber(dir, func() { got <- struct{}{} })
	go func() { sub.Run(ctx); close(done) }()
	waitFor(t, "購読が繋がらない", func() bool { return s.Subscribers() == 1 })
	select { // 繋がった直後の 1 回
	case <-got:
	case <-time.After(10 * time.Second):
		t.Fatal("繋がっても読み直させない (繋がる前の知らせを取り逃す)")
	}
	s.Broadcast()
	select {
	case <-got:
	case <-time.After(10 * time.Second):
		t.Fatal("Broadcast が届かない")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2 := listen(t, dir) // dispatcher の起動し直し
	waitFor(t, "起動し直した dispatcher に繋ぎ直さない", func() bool { return s2.Subscribers() == 1 })
	recv(t, got, "繋ぎ直しても読み直させない")
	s2.Broadcast()
	select {
	case <-got:
	case <-time.After(10 * time.Second):
		t.Fatal("繋ぎ直した後の Broadcast が届かない")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("ctx が終わっても購読が戻らない")
	}
	waitFor(t, "画面が抜けたのに購読に残る", func() bool { return s2.Subscribers() == 0 })
}

// 置き場のパスが長ければ /tmp/pro-con-<uid>/ にハッシュで置く (sun_path の上限)。同じ置き場は同じパス、違う置き場は違うパス。
// プロセスごとの TMPDIR にも、置き場の書き方 (/tmp と /private/tmp・相対パス) にもよらない (dispatcher・画面・pro-con card は別のプロセス)。
func TestLongDirFallsBackToTempDir(t *testing.T) {
	useFallbackRoot(t)
	long := filepath.Join(t.TempDir(), strings.Repeat("d", 120))
	if err := os.MkdirAll(long, 0o700); err != nil {
		t.Fatal(err)
	}
	p := Path(long)
	if len(p) > maxPath || strings.HasPrefix(p, long) || filepath.Dir(p) != fallbackDir() || p != Path(long) || p == Path(long+"x") {
		t.Fatalf("長い置き場の socket のパス: %s", p)
	}
	t.Setenv("TMPDIR", "/tmp/other-tmpdir/")
	if Path(long) != p {
		t.Fatal("TMPDIR で socket のパスが変わった")
	}
	if !strings.HasPrefix(long, "/var/") {
		t.Skip("前提: t.TempDir が /var の下 (macOS) でないので、/private の書き方を試せない")
	}
	if Path("/private"+long) != p {
		t.Fatal("置き場の書き方 (/var と /private/var) で socket のパスが変わった")
	}
	t.Chdir(filepath.Dir(long)) // 抜けるときに元の cwd へ戻す
	if Path(filepath.Base(long)) != p {
		t.Fatal("相対パスで socket のパスが変わった")
	}
	s := listen(t, long)
	if err := Poke(long); err != nil {
		t.Fatal(err)
	}
	<-s.Wakes()
}

// 前の dispatcher が残した socket は消して開き直す。socket でないファイルは消さない。
func TestListenReplacesStaleSocketOnly(t *testing.T) {
	dir := shortDir(t)
	s := listen(t, dir)
	s.ln.(interface{ SetUnlinkOnClose(bool) }).SetUnlinkOnClose(false) // 落ちた dispatcher の形 (ファイルが残る)
	_ = s.ln.Close()
	if _, err := os.Lstat(Path(dir)); err != nil {
		t.Fatal("前提: socket のファイルが残っていない")
	}
	listen(t, dir)
	other := shortDir(t)
	if err := os.WriteFile(Path(other), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(other); err == nil {
		t.Fatal("socket でないファイルの上に開いた")
	}
	if b, err := os.ReadFile(Path(other)); err != nil || string(b) != "x" {
		t.Fatal("socket でないファイルを消した")
	}
}

// Close は socket のファイルを消す (残すと次の Poke が死んだ口に繋ぎに行く)。
func TestCloseRemovesSocket(t *testing.T) {
	dir := shortDir(t)
	s, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(Path(dir)); !os.IsNotExist(err) {
		t.Fatalf("socket のファイルが残った: %v", err)
	}
}

// useFallbackRoot は逃がす先の親を一時ディレクトリにする (本物の /tmp/pro-con-<uid> を触らない。並行する検証・本物の e2e を巻き込まない)。
func useFallbackRoot(t *testing.T) string {
	t.Helper()
	old := fallbackRoot
	fallbackRoot = shortDir(t)
	t.Cleanup(func() { fallbackRoot = old })
	return fallbackRoot
}

// 逃がす先が自分のディレクトリでなければ (symlink・他のユーザーが作った) socket を置かず、繋ぎにも行かない (成りすまされない)。
// 自分のもので権限だけ緩ければ、繋ぐ側はつながず、listen する側 (dispatcher) が 0700 に直して使う。
func TestFallbackDirMustBeOwn(t *testing.T) {
	useFallbackRoot(t)
	long := filepath.Join(t.TempDir(), strings.Repeat("d", 120))
	if err := os.MkdirAll(long, 0o700); err != nil {
		t.Fatal(err)
	}
	elsewhere := shortDir(t)
	if err := os.Symlink(elsewhere, fallbackDir()); err != nil {
		t.Fatal(err)
	}
	if s, err := Listen(long); err == nil {
		_ = s.Close()
		t.Fatal("symlink の逃がす先に socket を置いた")
	}
	if err := Poke(long); err == nil || !strings.Contains(err.Error(), "自分のディレクトリではない") {
		t.Fatalf("symlink の逃がす先へ繋ぎに行った: %v", err)
	}
	if err := os.Remove(fallbackDir()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(fallbackDir(), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fallbackDir(), 0o777); err != nil { // umask を越えて緩める
		t.Fatal(err)
	}
	// 繋ぐ側 (Poke・購読) は緩い権限を直さず、つながない (読む口が状態の置き場の外を書かない。issue 445)
	if err := Poke(long); !errors.Is(err, ErrUnsafeDir) {
		t.Fatalf("権限の緩い逃がし先へ繋ぎに行った: %v", err)
	}
	if st, err := os.Stat(fallbackDir()); err != nil || st.Mode().Perm() != 0o777 {
		t.Fatalf("繋ぐ側が権限を直した: %v %v", st.Mode(), err)
	}
	s := listen(t, long) // 直すのは listen する側 (dispatcher) だけ
	if st, err := os.Stat(fallbackDir()); err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("緩い権限を直さない: %v %v", st.Mode(), err)
	}
	if err := Poke(long); err != nil {
		t.Fatal(err)
	}
	<-s.Wakes()
}

// 購読は、逃がし先の権限が緩ければつながずに 1 度だけ知らせ (直さない)、dispatcher が直して listen したらつながる。
func TestSubscriberRefusesLooseFallbackDir(t *testing.T) {
	useFallbackRoot(t)
	long := filepath.Join(t.TempDir(), strings.Repeat("d", 120))
	if err := os.MkdirAll(long, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(fallbackDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fallbackDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	t.Cleanup(func() { cancel(); wg.Wait() })
	refused := make(chan error, 10)
	changed := make(chan struct{}, 10)
	sub := NewSubscriber(long, func() { changed <- struct{}{} }).OnRefused(func(err error) { refused <- err })
	wg.Add(1)
	go func() { defer wg.Done(); sub.Run(ctx) }()
	select {
	case err := <-refused:
		if !errors.Is(err, ErrUnsafeDir) {
			t.Fatalf("知らせの理由が違う: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("逃がし先を使えないことを知らせない")
	}
	time.Sleep(3 * retryEvery) // 繋ぎ直しを何度か回す (知らせは 1 度だけ・権限は直さない)
	if len(refused) != 0 {
		t.Fatalf("つながらない間に何度も知らせた: %d", len(refused)+1)
	}
	if st, err := os.Stat(fallbackDir()); err != nil || st.Mode().Perm() != 0o755 {
		t.Fatalf("購読が権限を直した: %v %v", st.Mode(), err)
	}
	listen(t, long)
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("dispatcher が直して listen した後もつながらない")
	}
}

// Notify は購読している画面すべてへ知らせる (画面を開いた・閉じた。dispatcher は Tick を回さずに中継する)。
func TestNotifyBroadcasts(t *testing.T) {
	dir := shortDir(t)
	s := listen(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	t.Cleanup(func() { cancel(); wg.Wait() })
	got := make(chan int, 10)
	for i := range 2 {
		sub := NewSubscriber(dir, func() { got <- i })
		wg.Add(1)
		go func() { defer wg.Done(); sub.Run(ctx) }()
	}
	waitFor(t, "2 つの画面が購読しない", func() bool { return s.Subscribers() == 2 })
	recv(t, got, "繋がっても読み直させない (1 つ目)")
	recv(t, got, "繋がっても読み直させない (2 つ目)")
	if err := Notify(dir); err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for len(seen) < 2 {
		select {
		case i := <-got:
			seen[i] = true
		case <-time.After(10 * time.Second):
			t.Fatalf("知らせが全員に届かない: %v", seen)
		}
	}
	if len(s.Wakes()) != 0 {
		t.Fatal("Notify で dispatcher を起こした (Tick を回す理由は無い)")
	}
}

// recv は ch から 1 つ受ける (届かなければ落とす。上限はハングの防止で、時間は判定に使わない)。
func recv[T any](t *testing.T, ch <-chan T, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal(what)
	}
}
