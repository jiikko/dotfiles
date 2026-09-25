package dispatcher

import (
	"slices"
	"strings"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/store"
)

// asked は id のカードを、since に q を質問した質問待ちにする (無ければ足す)。PG の session は持たせない (PM の知らせだけを見る)。
func asked(t *testing.T, dir, id, q string, since time.Time) {
	t.Helper()
	err := store.Update(dir, func(st *store.State) error {
		c := card.Card{ID: id, Title: "質問する " + id, Repo: "dotfiles"}
		i := slices.IndexFunc(st.Cards, func(c card.Card) bool { return c.ID == id })
		if i >= 0 {
			c = st.Cards[i]
		}
		c.State, c.Since, c.Wait = card.Waiting, since, card.Wait{Kind: card.WaitQuestion, Question: q}
		if i >= 0 {
			st.Cards[i] = c
		} else {
			st.Cards = append(st.Cards, c)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// PG の質問 (質問待ちの列) も PM に知らせる (pm-guide の役目 4)。質問の文を添え、同じ質問は 2 度知らせない。
func TestPMToldOfQuestion(t *testing.T) {
	r := startedPM(t)
	asked(t, r.dir, "C-009", "赤か青か", t0.Add(time.Minute))
	r.tick(t)
	if len(r.l.resumes) != 1 || !strings.Contains(r.l.resumes[0], "PG の質問 C-009「質問する C-009」") || !strings.Contains(r.l.resumes[0], "赤か青か") {
		t.Fatalf("質問待ちのカードを質問の文ごと PM に知らせていない: %v", r.l.resumes)
	}
	r.tick(t)
	r.tick(t)
	if len(r.l.resumes) != 1 {
		t.Fatalf("知らせ済みの質問でまた PM を起こした: %v", r.l.resumes)
	}
}

// 回答して列を離れ、同じカードがまた質問したら (知らせの間に 1 度も列を離れたのを見ていなくても) 新しい質問として知らせる。
func TestPMToldOfSecondQuestion(t *testing.T) {
	r := startedPM(t)
	asked(t, r.dir, "C-009", "一つ目の質問", t0.Add(time.Minute))
	r.tick(t)
	r.tick(t)
	asked(t, r.dir, "C-009", "二つ目の質問", t0.Add(3*time.Minute)) // 回答 → 再開 → また質問 (列を離れた Tick は PM から見えなかった)
	r.tick(t)
	if len(r.l.resumes) != 2 || !strings.Contains(r.l.resumes[1], "二つ目の質問") {
		t.Fatalf("2 度目の質問を PM に知らせていない: %v", r.l.resumes)
	}
}

// 権限の確認と落ちて止めた PG は PM には答えられない (人の番。452) ので知らせない。
func TestPMNotToldOfPermissionOrCrash(t *testing.T) {
	r := startedPM(t)
	err := store.Update(r.dir, func(st *store.State) error {
		for i, k := range []card.WaitKind{card.WaitPermission, card.WaitCrashed} {
			st.Cards = append(st.Cards, card.Card{ID: []string{"C-008", "C-009"}[i], Title: "t", Repo: "dotfiles", State: card.Waiting, Since: t0,
				Wait: card.Wait{Kind: k, Question: "q"}})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	if len(r.l.resumes) != 0 {
		t.Fatalf("PM に答えられない待ちを知らせた: %v", r.l.resumes)
	}
}

// 知らせた後に PM が止まったら、まだ答えていない質問を知らせ直す (依頼の列が空でも)。
func TestPMRenotifiedOfQuestionAfterDeath(t *testing.T) {
	r := startedPM(t)
	if _, err := store.Submit(r.dir, store.Request{Kind: "plan", CardID: "C-001", Issues: []card.IssueRef{{Repo: "dotfiles", Number: 1}}}); err != nil {
		t.Fatal(err)
	}
	asked(t, r.dir, "C-009", "赤か青か", t0.Add(time.Minute))
	r.tick(t)
	r.ss = nil // 知らせた PM が一覧から消えた
	r.now = t0.Add(time.Hour)
	r.tick(t)
	r.now = r.now.Add(restartWait + time.Second)
	r.tick(t)
	if len(r.l.resumes) != 2 || !strings.Contains(r.l.resumes[1], "C-009") {
		t.Fatalf("消えた PM の後に、答えていない質問を知らせ直していない: %v", r.l.resumes)
	}
}

// 人に回した質問 (handoff) は PM には片付けられないので、知らせる物から外す: PM が落ちても、それだけのために起こさない。
func TestPMNotWokenForHandedOffQuestion(t *testing.T) {
	r := startedPM(t)
	if _, err := store.Submit(r.dir, store.Request{Kind: "plan", CardID: "C-001", Issues: []card.IssueRef{{Repo: "dotfiles", Number: 1}}}); err != nil {
		t.Fatal(err)
	}
	asked(t, r.dir, "C-009", "赤か青か", t0) // handoff を適用する時計 (r.now) より後に質問待ちに入らない
	r.tick(t)
	if _, err := store.Submit(r.dir, store.Request{Kind: "handoff", CardID: "C-009", Text: "好みは人が決める", From: "PM"}); err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	r.ss = nil // PM が一覧から消えた
	for i := 1; i <= 3; i++ {
		r.now = t0.Add(time.Duration(i) * time.Hour)
		r.tick(t)
		r.now = r.now.Add(restartWait + time.Second)
		r.tick(t)
	}
	pmStarts := slices.DeleteFunc(slices.Clone(r.l.starts), func(n string) bool { return !strings.HasPrefix(n, "pc-pm-") }) // PG の起動 (C-001) は数えない
	if len(r.l.resumes) != 1 || len(pmStarts) != 1 {
		t.Fatalf("人に回した質問のためだけに PM を起こした: resumes=%v PM の起動=%v", r.l.resumes, pmStarts)
	}
	asked(t, r.dir, "C-009", "次の質問", t0.Add(5*time.Hour)) // 人が答えて、また質問した: 新しい質問は知らせる
	r.now = t0.Add(5 * time.Hour)
	r.tick(t)
	if len(r.l.resumes) != 2 || !strings.Contains(r.l.resumes[1], "次の質問") {
		t.Fatalf("人に回した後の新しい質問を知らせていない: %v", r.l.resumes)
	}
}
