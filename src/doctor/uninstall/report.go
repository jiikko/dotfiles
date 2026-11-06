package uninstall

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jiikko/dotfiles/src/termsafe"
)

// SanitizeInventory は表示の前の関門。名前・版は Info.plist / brew の出力 = 外の値、パスはファイル名を含む。
func SanitizeInventory(inv Inventory) Inventory {
	out := inv
	out.Apps = make([]App, len(inv.Apps))
	for i, a := range inv.Apps {
		a.Name = termsafe.PlainLine(a.Name)
		a.BundleID = termsafe.PlainLine(a.BundleID)
		a.Version = termsafe.PlainLine(a.Version)
		a.Path = termsafe.PlainLine(a.Path)
		a.CaskToken = termsafe.PlainLine(a.CaskToken)
		a.PkgID = termsafe.PlainLine(a.PkgID)
		a.Manual = termsafe.PlainLine(a.Manual)
		out.Apps[i] = a
	}
	out.Formulae = make([]Formula, len(inv.Formulae))
	for i, f := range inv.Formulae {
		f.Name = termsafe.PlainLine(f.Name)
		f.Version = termsafe.PlainLine(f.Version)
		f.Path = termsafe.PlainLine(f.Path)
		f.Protected = termsafe.PlainLine(f.Protected)
		out.Formulae[i] = f
	}
	out.Notes = termsafe.PlainLines(inv.Notes)
	return out
}

// SanitizeResult は apply の結果を表示する前の関門。理由の文はアプリの名前・brew の出力を含む。
func SanitizeResult(r Result) Result {
	out := r
	out.Trash = termsafe.PlainLine(r.Trash)
	out.Done = make([]Done, len(r.Done))
	for i, d := range r.Done {
		d.Owner, d.Path, d.To = termsafe.PlainLine(d.Owner), termsafe.PlainLine(d.Path), termsafe.PlainLine(d.To)
		out.Done[i] = d
	}
	clean := func(ss []Skip) []Skip {
		out := make([]Skip, len(ss))
		for i, s := range ss {
			s.Owner, s.Path, s.Reason = termsafe.PlainLine(s.Owner), termsafe.PlainLine(s.Path), termsafe.PlainLine(s.Reason)
			out[i] = s
		}
		return out
	}
	out.Skipped, out.Failed = clean(r.Skipped), clean(r.Failed)
	out.Manual = SanitizePlan(Plan{Items: r.Manual}).Items
	return out
}

// SanitizePlan は計画を表示する前の関門。🚨 表示用の写しにだけ使い、ファイルに書く計画は生のまま (apply がパスで照合する)。
func SanitizePlan(p Plan) Plan {
	out := p
	out.Items = make([]Item, len(p.Items))
	for i, it := range p.Items {
		it.Owner = termsafe.PlainLine(it.Owner)
		it.Path = termsafe.PlainLine(it.Path)
		it.Token = termsafe.PlainLine(it.Token)
		it.Note = termsafe.PlainLine(it.Note)
		out.Items[i] = it
	}
	return out
}

// FormatInventory は候補の一覧 (最後に使った日が古い順。不明は最後)。
func FormatInventory(inv Inventory, now time.Time) string {
	var b strings.Builder
	apps := slices.Clone(inv.Apps)
	slices.SortStableFunc(apps, func(a, b App) int { // 古い順。日が不明なものは最後
		if a.LastUsed.IsZero() != b.LastUsed.IsZero() {
			if a.LastUsed.IsZero() {
				return 1
			}
			return -1
		}
		return a.LastUsed.Compare(b.LastUsed)
	})
	fmt.Fprintf(&b, "## アプリ (%d。最後に使った日が古い順)\n\n| 名前 | 入り方 | 大きさ | 最後に使った日 | 動いている | bundle ID | 片付けられない理由 |\n|---|---|---|---|---|---|---|\n", len(apps))
	for _, a := range apps {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n", cell(a.Name), sourceWord(a), kb(a.SizeKB), lastUsedWord(a.LastUsed, now), yes(a.Running), cell(a.BundleID), cell(a.Manual))
	}
	fmt.Fprintf(&b, "\n## formula (他から依存されていないもの、%d)\n\n| 名前 | 入れた日 | 自分で入れた | 大きさ | 動いている | 片付けられない理由 |\n|---|---|---|---|---|---|\n", len(inv.Formulae))
	for _, f := range inv.Formulae {
		day := "不明"
		if !f.InstalledAt.IsZero() {
			day = f.InstalledAt.Format("2006-01-02")
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n", cell(f.Name), day, yes(f.OnRequest), kb(f.SizeKB), yes(f.Running), cell(f.Protected))
	}
	if inv.RunningUnknown {
		b.WriteString("\n- 🚨 動いているかを確かめられないので、計画を作れない (下の判定不能を見る)")
	}
	for _, n := range inv.Notes {
		fmt.Fprintf(&b, "\n- 判定不能: %s", cell(n))
	}
	return b.String()
}

// FormatPlan は計画を、動かすもの・残すもの (手で確かめるもの) に分けて出す。
func FormatPlan(p Plan) string {
	var b strings.Builder
	var total int64
	fmt.Fprintf(&b, "計画 %s (%s に作成。1 時間以内に apply する)\n\n| 持ち主 | 段 | 動かし方 | 大きさ | パス | メモ |\n|---|---|---|---|---|---|\n", p.ID, p.CreatedAt.Local().Format("15:04"))
	for _, it := range p.Items {
		if it.SizeKB > 0 && it.Action != ActionNone {
			total += it.SizeKB
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n", cell(it.Owner), tierWord(it.Tier), actionWord(it.Action), kb(it.SizeKB), cell(it.Path), cell(it.Note))
	}
	fmt.Fprintf(&b, "\n動かす合計 %s。ゴミ箱へ移したものは Finder から戻せる (容量はゴミ箱を空にするまで減らない)。brew が消したものは brew install で入れ直す\n", kb(total))
	return b.String()
}

func cell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

func kb(n int64) string {
	switch {
	case n < 0:
		return "不明"
	case n >= 1024*1024:
		return fmt.Sprintf("%.1fGB", float64(n)/1024/1024)
	case n >= 1024:
		return fmt.Sprintf("%dMB", n/1024)
	}
	return fmt.Sprintf("%dKB", n)
}

func yes(b bool) string {
	if b {
		return "はい"
	}
	return ""
}

func sourceWord(a App) string {
	switch a.Source {
	case SourceCask:
		return "brew cask (" + cell(a.CaskToken) + ")"
	case SourceMAS:
		return "App Store"
	case SourceManual:
		if a.PkgID != "" {
			return "pkg のインストーラ"
		}
		return "手で入れた"
	}
	return string(a.Source)
}

func lastUsedWord(t, now time.Time) string {
	if t.IsZero() {
		return "不明"
	}
	return fmt.Sprintf("%s (%d 日前)", t.Local().Format("2006-01-02"), int(now.Sub(t).Hours()/24))
}

func tierWord(t Tier) string {
	switch t {
	case TierApp:
		return "本体"
	case TierCache:
		return "作り直されるもの"
	case TierData:
		return "設定とデータ"
	case TierManual:
		return "手で確かめる"
	}
	return string(t)
}

func actionWord(a Action) string {
	switch a {
	case ActionTrash:
		return "ゴミ箱へ"
	case ActionBrewCask:
		return "brew uninstall --cask"
	case ActionBrewFormula:
		return "brew uninstall"
	case ActionNone:
		return "動かさない"
	}
	return string(a)
}
