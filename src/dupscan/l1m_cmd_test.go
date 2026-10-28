package main

import (
	"strings"
	"testing"
)

// L1m (メンバー名だけが違う写し) は既定では組にならず、-levels で明示したときだけ組になる。
// どちらの言語も同じ規則 (Go は selector の右、Swift は `.` の右がメンバー名)。

const goTwoSinks = `package a

import (
	"io"
	"os"
)

func captureOut(fn func()) string {
	r, w, _ := os.Pipe()
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stdout = orig
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

func captureErr(fn func()) string {
	r, w, _ := os.Pipe()
	orig := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stderr = orig
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}
`

const swiftTwoStores = `final class Saver {
    func saveAlpha(_ value: Int) -> Bool {
        let key = makeKey(value)
        guard store.isReady else { return false }
        store.alphaTable.insert(key, value)
        log.record(key, value)
        return store.alphaTable.contains(key)
    }

    func saveBeta(_ value: Int) -> Bool {
        let key = makeKey(value)
        guard store.isReady else { return false }
        store.betaTable.insert(key, value)
        log.record(key, value)
        return store.betaTable.contains(key)
    }
}
`

func TestL1mFindsMemberOnlyDifferencesOnlyWhenAsked(t *testing.T) {
	for _, c := range []struct {
		lang, file, src string
		a, b            string
	}{
		{"go", "a.go", goTwoSinks, "captureOut", "captureErr"},
		{"swift", "Saver.swift", swiftTwoStores, "saveAlpha", "saveBeta"},
	} {
		t.Run(c.lang, func(t *testing.T) {
			root := repoWith(t, map[string]string{c.file: c.src})
			rc, out, _ := runReport(t, root)
			if rc != 0 || !strings.Contains(out, "重複なし") {
				t.Fatalf("既定の段では組にならない: rc=%d %q", rc, out)
			}
			rc, out, _ = runReport(t, "-levels", "L1,L2,L1m", root)
			if rc != 0 || !strings.Contains(out, "L1m ") || !strings.Contains(out, c.a) || !strings.Contains(out, c.b) {
				t.Fatalf("-levels L1,L2,L1m で組になる: rc=%d %q", rc, out)
			}
			if strings.Contains(out, "L1 ") && strings.Contains(out, "L1 prod") {
				t.Errorf("L1 の組は無いはず: %q", out)
			}
		})
	}
}

func TestUnknownLevelNamesL1mInTheError(t *testing.T) {
	root := repoWith(t, map[string]string{"a.go": "package a\n"})
	_, _, errs := runReport(t, "-levels", "L9", root)
	if !strings.Contains(errs, "L1m") {
		t.Errorf("知らない段のエラーに指定できる段 (L1m を含む) を出す: %q", errs)
	}
}
