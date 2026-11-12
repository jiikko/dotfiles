package oracle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"atomicfile"
)

// Getter は URL の中身を返す。テストが本物のネットワークを使わず差し替えられるようにする。
type Getter func(ctx context.Context, url string) ([]byte, error)

const (
	fetchTimeout = 60 * time.Second
	// 本家の最大のファイル (yomiyasu_diff.py) は約 770 KB。
	maxFileBytes = 8 << 20
)

// HTTPGetter は net/http で取る Getter。200 以外・大きすぎる応答はエラー。
func HTTPGetter(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("要求を作れません (%s): %w", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("取得に失敗しました (%s): %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("取得に失敗しました (%s): %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("本文を読めません (%s): %w", url, err)
	}
	if len(body) > maxFileBytes {
		return nil, fmt.Errorf("%s が %d バイトを超えています", url, maxFileBytes)
	}
	return body, nil
}

func rawURL(rel string) string {
	return "https://raw.githubusercontent.com/" + Repo + "/" + Commit + "/" + rel
}

// SHA256Hex は中身の SHA-256 を 16 進で返す (本家との照合と入力の記録に使う)。
func SHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Fetch は本家のファイルを cacheRoot/<Commit>/ に揃える。すでにあって SHA-256 が合うものは取り直さない。
// 取ったものが upstream.sha256 と合わなければ、何も書かずに止まる。
func Fetch(ctx context.Context, cacheRoot string, get Getter) (Tree, error) {
	sums, err := Manifest()
	if err != nil {
		return Tree{}, err
	}
	return fetchFiles(ctx, cacheRoot, UpstreamFiles, sums, get)
}

// fetchFiles は Fetch の本体。files と sums を引数にして、テストが本物の本家のファイルなしで検証の経路を通せるようにする。
func fetchFiles(ctx context.Context, cacheRoot string, files []string, sums map[string]string, get Getter) (Tree, error) {
	tree := Tree{Root: filepath.Join(cacheRoot, Commit)}
	for _, rel := range files {
		dst := tree.Path(rel)
		if have, rerr := os.ReadFile(dst); rerr == nil && SHA256Hex(have) == sums[rel] {
			continue
		} else if rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			return Tree{}, fmt.Errorf("%s を読めません: %w", dst, rerr)
		}
		body, gerr := get(ctx, rawURL(rel))
		if gerr != nil {
			return Tree{}, gerr
		}
		if got := SHA256Hex(body); got != sums[rel] {
			return Tree{}, fmt.Errorf("%s の SHA-256 が upstream.sha256 と合いません (取得 %s、期待 %s)。何も書かずに止めます", rel, got, sums[rel])
		}
		if err := writeFileAtomic(dst, body); err != nil {
			return Tree{}, err
		}
	}
	return tree, nil
}

// Pin は本家のファイルを検証なしで取り、upstream.sha256 の中身を返す。固定する commit を決めた人が 1 回だけ使う。
func Pin(ctx context.Context, get Getter) (string, error) {
	sums := map[string]string{}
	for _, rel := range UpstreamFiles {
		body, err := get(ctx, rawURL(rel))
		if err != nil {
			return "", err
		}
		sums[rel] = SHA256Hex(body)
	}
	return manifestLines(sums), nil
}

// writeFileAtomic は dir を作ってから atomicfile で置き、途中の状態を残さない。
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("%s の dir を作れません: %w", path, err)
	}
	if err := atomicfile.Write(path, data, 0o644); err != nil {
		return fmt.Errorf("%s へ書けません: %w", path, err)
	}
	return nil
}
