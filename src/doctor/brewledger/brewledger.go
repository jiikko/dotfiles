// Package brewledger は Homebrew の台帳 (installed formula の名前・旧名・別名)。disk の brew-orphan-state と
// svc の C 判定 (homebrew.mxcl.<formula> が台帳に無い) が同じ集合を引く。
//
// `brew list --formula` ではなく `brew info --json=v2 --installed` を使う (実測 1.5 秒): rename 済み formula
// (postgresql → postgresql@14 等) の状態 dir / plist は旧名で残るので、名前だけの台帳は現役を孤児にする
// (敵対レビュー 2026-09-02)。
package brewledger

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"doctor/runner"
)

const timeout = 30 * time.Second

// Installed は台帳。brew 不在 / 失敗は error (呼び出し側は「診断できず」に倒す。空集合にしない)。
func Installed(ctx context.Context, run runner.Runner) (map[string]bool, error) {
	out, stderr, rc, err := runner.WithTimeout(ctx, run, timeout, "brew", "info", "--json=v2", "--installed")
	if err != nil {
		return nil, err
	}
	if rc != 0 {
		return nil, fmt.Errorf("brew info --installed: exit %d: %s", rc, strings.TrimSpace(stderr))
	}
	return Parse([]byte(out))
}

// Parse は brew info --json=v2 の formulae から name / full_name / oldnames / aliases を集める。
// @version を落とした名前 (var/mysql は mysql@8.4 の状態 dir) と tap を落とした名前も入れる。
func Parse(data []byte) (map[string]bool, error) {
	var doc struct {
		Formulae []struct {
			Name     string   `json:"name"`
			FullName string   `json:"full_name"`
			Oldnames []string `json:"oldnames"`
			Aliases  []string `json:"aliases"`
		} `json:"formulae"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("brew info の JSON を解釈できない: %w", err)
	}
	set := map[string]bool{}
	add := func(n string) {
		if n == "" {
			return
		}
		set[n] = true
		if i := strings.IndexByte(n, '@'); i > 0 {
			set[n[:i]] = true
		}
		if i := strings.LastIndexByte(n, '/'); i >= 0 {
			set[n[i+1:]] = true
		}
	}
	for _, f := range doc.Formulae {
		add(f.Name)
		add(f.FullName)
		for _, o := range f.Oldnames {
			add(o)
		}
		for _, a := range f.Aliases {
			add(a)
		}
	}
	return set, nil
}
