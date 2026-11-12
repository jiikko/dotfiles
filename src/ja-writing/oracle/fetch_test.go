package oracle

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// 本物のネットワークを使わない Getter。呼ばれた URL を数える。
type fakeServer struct {
	bodies map[string]string
	calls  int
}

func (f *fakeServer) get(_ context.Context, url string) ([]byte, error) {
	f.calls++
	rel := strings.TrimPrefix(url, "https://raw.githubusercontent.com/"+Repo+"/"+Commit+"/")
	body, ok := f.bodies[rel]
	if !ok {
		return nil, errors.New("404 " + url)
	}
	return []byte(body), nil
}

func fakeFiles() ([]string, map[string]string, map[string]string) {
	bodies := map[string]string{"a/one.py": "print(1)\n", "b/two.txt": "二つ目\n"}
	files := []string{"a/one.py", "b/two.txt"}
	sums := map[string]string{}
	for p, b := range bodies {
		sums[p] = SHA256Hex([]byte(b))
	}
	return files, sums, bodies
}

func TestFetchWritesVerifiedFilesAndReusesCache(t *testing.T) {
	files, sums, bodies := fakeFiles()
	cache := t.TempDir()
	srv := &fakeServer{bodies: bodies}
	tree, err := fetchFiles(t.Context(), cache, files, sums, srv.get)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range files {
		got, rerr := os.ReadFile(tree.Path(rel))
		if rerr != nil || string(got) != bodies[rel] {
			t.Fatalf("%s: got %q err=%v", rel, got, rerr)
		}
	}
	if srv.calls != len(files) {
		t.Fatalf("1 回目の取得は %d 回のはず: %d", len(files), srv.calls)
	}
	if _, err := fetchFiles(t.Context(), cache, files, sums, srv.get); err != nil {
		t.Fatal(err)
	}
	if srv.calls != len(files) {
		t.Fatalf("SHA-256 の合うキャッシュは取り直さない: 取得 %d 回", srv.calls)
	}
}

func TestFetchRefetchesCorruptedCacheFile(t *testing.T) {
	files, sums, bodies := fakeFiles()
	cache := t.TempDir()
	srv := &fakeServer{bodies: bodies}
	tree, err := fetchFiles(t.Context(), cache, files, sums, srv.get)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tree.Path("a/one.py"), []byte("改ざん"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fetchFiles(t.Context(), cache, files, sums, srv.get); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(tree.Path("a/one.py"))
	if string(got) != bodies["a/one.py"] {
		t.Fatalf("キャッシュの改ざんが残っています: %q", got)
	}
}

func TestFetchStopsOnChecksumMismatchWithoutWriting(t *testing.T) {
	files, sums, bodies := fakeFiles()
	bodies["b/two.txt"] = "別の中身\n"
	cache := t.TempDir()
	srv := &fakeServer{bodies: bodies}
	tree := Tree{Root: cache + "/" + Commit}
	_, err := fetchFiles(t.Context(), cache, files, sums, srv.get)
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("SHA-256 の不一致で止まるはず: %v", err)
	}
	for _, rel := range files {
		if _, serr := os.Stat(tree.Path(rel)); !errors.Is(serr, os.ErrNotExist) {
			t.Fatalf("1 つでも合わなければ、合ったファイル %s も書いてはいけない: %v", rel, serr)
		}
	}
}

func TestFetchPropagatesGetterError(t *testing.T) {
	files, sums, _ := fakeFiles()
	srv := &fakeServer{bodies: map[string]string{}}
	if _, err := fetchFiles(t.Context(), t.TempDir(), files, sums, srv.get); err == nil {
		t.Fatal("取得の失敗を握り潰してはいけない")
	}
}

func TestManifestCoversExactlyUpstreamFiles(t *testing.T) {
	sums, err := Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != len(UpstreamFiles) {
		t.Fatalf("manifest %d 件 / UpstreamFiles %d 件", len(sums), len(UpstreamFiles))
	}
}

func TestParseManifestRejectsMalformedAndMissing(t *testing.T) {
	good, err := Manifest()
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"形が違う":     "abc  skills/yomiyasu/LICENSE\n",
		"足りない":     manifestLines(map[string]string{UpstreamFiles[0]: good[UpstreamFiles[0]]}),
		"余計なパスがある": manifestLines(withExtra(good)),
		"重複":       manifestLines(good) + UpstreamFiles[0] + "\n",
	} {
		if _, err := parseManifest(text); err == nil {
			t.Errorf("%s: エラーになるはず", name)
		}
	}
}

func withExtra(m map[string]string) map[string]string {
	out := map[string]string{"extra/file": strings.Repeat("0", 64)}
	for k, v := range m {
		out[k] = v
	}
	return out
}
