package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadJobs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "jobs.tsv")
	content := "a\t1000\tmain:3 claude\tmake test\n" +
		"broken-line\n" + // フィールド不足
		"c\tnot-a-number\tx\ty\n" + // epoch が数値でない
		"\t1000\tx\ty\n" + // id が空
		"d\t2000\tmain:1 zsh\techo  hi\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	jobs, err := readJobs(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("壊れた行を捨てて 2 件になるはず: %+v", jobs)
	}
	if jobs[0].id != "a" || !jobs[0].at.Equal(time.Unix(1000, 0)) || jobs[0].text != "make test" {
		t.Errorf("1 件目 = %+v", jobs[0])
	}
	if jobs[1].text != "echo  hi" {
		t.Errorf("空白 2 つが保たれない: %q", jobs[1].text)
	}
}

func TestReadJobsMissingFileIsEmpty(t *testing.T) {
	jobs, err := readJobs(filepath.Join(t.TempDir(), "nope.tsv"))
	if err != nil || len(jobs) != 0 {
		t.Errorf("不在ファイル = %v, %v; want 空", jobs, err)
	}
	if jobs, err := readJobs(""); err != nil || len(jobs) != 0 {
		t.Errorf("パス空 = %v, %v; want 空", jobs, err)
	}
}
