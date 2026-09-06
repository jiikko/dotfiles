package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"glogx/issues"
)

// TestEpicChildStatusReachedByAllThreeConsumers は epic group 内の状態ディレクトリ名について、
// 走査 (issues.Scan)・発見 (issues.FindDirs)・監視 (issuesWatchDirs) の 3 者が
// issues.EpicChildStatus と同じ集合を見ていることを固定する。
//
// 🚨 判定の出典は EpicChildStatus 1 つで、テスト側に第 2 の列挙を作らない (作ると
// 「実装とテストが同じ間違いをする」形になる)。候補名を投げて EpicChildStatus に答えさせ、
// 3 経路がその答えと一致するかだけを見る。片方だけ名前を増やすと、監視されないディレクトリや
// 発見されない issue が黙って生まれる (parse.go の EpicChildStatus のコメントが正本)。
func TestEpicChildStatusReachedByAllThreeConsumers(t *testing.T) {
	// 受ける名前・受けない名前の両方を混ぜる (全部受ける候補だと、素通しの実装でも緑になる)
	candidates := []string{issues.NextDirName, "done", "pending", "DONE", "closed", "completed", "hold", "archive"}
	for _, name := range candidates {
		t.Run(name, func(t *testing.T) {
			wantStatus, known := issues.EpicChildStatus(name)
			root := t.TempDir()
			dir := filepath.Join(root, "issues")
			sub := filepath.Join(dir, issues.EpicDirName, "cloud", name)
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}

			// (1) 空でも、受ける名前のディレクトリは最初の md が入る前から見張る
			// (ここだけが EpicChildStatus の答えで分岐する。受けない名前は下の (2) の
			// 「issue が居るディレクトリは見張る」経路で拾われる)
			v := newTestIssuesView()
			v.dirs = []string{dir}
			watchedEmpty, _ := v.watchTargets()
			if slices.Contains(watchedEmpty, sub) != known {
				t.Errorf("空の状態ディレクトリの監視が EpicChildStatus と食い違う: watched=%v want=%v (%v)",
					slices.Contains(watchedEmpty, sub), known, watchedEmpty)
			}

			// この md が repo にある唯一の issue。3 経路のどれかが name を知らなければ、
			// 走査で 0 件 / 発見されない / 見張られない のいずれかで落ちる
			if err := os.WriteFile(filepath.Join(sub, "700-feat-only-issue.md"), []byte("# 700\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			// (2) 走査: 受ける名前は group の子として状態つきで、受けない名前は迷子として読む。
			// どちらでも「消えない」ことがこの assert の本体
			got, _ := issues.Scan([]string{dir})
			if len(got) != 1 {
				t.Fatalf("走査が 1 件を返さない (issue が消えた): %d 件", len(got))
			}
			if known {
				if got[0].Status != wantStatus || got[0].GroupKind != issues.GroupEpic {
					t.Errorf("走査の状態が EpicChildStatus と違う: status=%v kind=%v want status=%v kind=%v",
						got[0].Status, got[0].GroupKind, wantStatus, issues.GroupEpic)
				}
			} else if got[0].Status != issues.StatusUnknown || got[0].GroupKind != issues.GroupUnknown {
				t.Errorf("受けない名前 %q の配下が迷子になっていない: status=%v kind=%v",
					name, got[0].Status, got[0].GroupKind)
			}

			// (3) 発見: この md しか無いので、見つけられなければ issue dir 自体が候補に出ない
			if found := issues.FindDirs(root); !slices.Contains(found, dir) {
				t.Errorf("issue dir が発見されない: %v", found)
			}

			// (4) 監視: issue が居るディレクトリは必ず見張る (外部編集を取りこぼさない)。
			// 走査結果を持たせた状態で見る (production も scan 後の all で見張る先を決める)
			v = newTestIssuesView()
			v.dirs, v.all = []string{dir}, got
			watched, _ := v.watchTargets()
			if !slices.Contains(watched, sub) {
				t.Errorf("issue が居るディレクトリが監視対象にない: %v", watched)
			}
		})
	}
}
