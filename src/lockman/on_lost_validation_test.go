package main

import "testing"

// --on-lost の値検証 (issue 409)。着手時点で **0 件**だった。
//
// 判定が `o.onLost != "warn"` の否定形なので、検証が無いと `warm` のような綴り間違いが
// 黙って kill 側に倒れる。warn のつもりの人に SIGTERM が撃たれる向きなので危険側。
func TestOnLostValueIsValidated(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		name string
		arg  string
		want int
	}{
		{"typo-warm", "warm", exitError}, // issue 409 の症状そのもの
		{"empty", "", exitError},
		{"uppercase-KILL", "KILL", exitError}, // 大文字は別値 (比較は完全一致)
		{"kill", "kill", exitOK},
		{"warn", "warn", exitOK},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := run([]string{"check", dir, "--on-lost", c.arg})
			if got != c.want {
				t.Fatalf("check --on-lost %q: exit %d (期待 %d)", c.arg, got, c.want)
			}
		})
	}
	// with は番号空間が違う (091:398-399)
	t.Run("with-uses-125", func(t *testing.T) {
		if got := run([]string{"with", dir, "--on-lost", "warm", "--", "true"}); got != exitWithInvalid {
			t.Fatalf("with --on-lost warm: exit %d (期待 %d)", got, exitWithInvalid)
		}
	})
	// 既定値 (フラグ未指定) が検証を通ること。既定を壊すと全 with が止まるので対照に置く
	t.Run("default-passes", func(t *testing.T) {
		if got := run([]string{"check", dir}); got != exitOK {
			t.Fatalf("check (既定 --on-lost): exit %d (期待 %d)", got, exitOK)
		}
	})
}

// 091:397「with の終了コードは子と衝突させない」— 引数が不正なときの rc は 125 で、
// 子が返しうる 1 と区別できること (issue 409 の「同じ commit で揃えたい」節)。
//
// 🚨 with 側だけを見ると、失敗の rc が 125 なのは failCode のおかげか、たまたま別経路に
// 落ちたからか区別できない。check 側 (1 のまま) と対にして初めて failCode が効いたと言える。
func TestWithInvalidArgsExitWithInvalid(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		name      string
		withArgs  []string
		checkArgs []string
	}{
		{"unknown-flag", []string{"with", "--bogus", dir, "--", "true"}, []string{"check", "--bogus", dir}},
		{"missing-dir", []string{"with", "--", "true"}, []string{"check"}},
		{"ttl-too-short", []string{"with", dir, "--ttl", "1s", "--", "true"}, []string{"check", dir, "--ttl", "1s"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := run(c.withArgs); got != exitWithInvalid {
				t.Errorf("with: exit %d (期待 %d)", got, exitWithInvalid)
			}
			if got := run(c.checkArgs); got != exitError {
				t.Errorf("check: exit %d (期待 %d — with 以外は 1 のまま)", got, exitError)
			}
		})
	}
}
