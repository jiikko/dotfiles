package build

import (
	"fmt"
	"strconv"
	"strings"

	"zundamon-kaisetsu/internal/app"
	"zundamon-kaisetsu/internal/paths"
	"zundamon-kaisetsu/internal/pyjson"
	"zundamon-kaisetsu/internal/script"
	"zundamon-kaisetsu/internal/show"
)

// CmdView は台本をユーザーに見せる一覧を出す (SKILL.md の手順 3-4)。チャプターの頭の見出しと、行ごとの
// 行番号 / 話者 (表情) / 字幕、図解が変わる行 (出す・段を進める・消す) にはその中身を次の行に字下げして出す。
// 図解の範囲と段は build と同じ show.LineShows で決めるので、合成 (wav) は要らない。
func CmdView(env *app.Env, scriptArg string) error {
	s, err := script.LoadScript(paths.ResolvePath(scriptArg), env)
	if err != nil {
		return err
	}
	idx, steps, table, err := show.LineShows(s.Path, s.Lines)
	if err != nil {
		return err
	}
	for i, line := range s.Lines {
		if c, ok := line["chapter"]; ok && pyjson.Truthy(c) {
			fmt.Fprintf(env.Stdout, "== %s ==\n", tsvField(pyjson.Str(c)))
		}
		who := pyjson.Str(line["who"])
		if def, ok := script.CastByKey(who); ok {
			who = def.Name
		}
		face := script.DefaultFace
		if f, ok := line["face"]; ok && pyjson.Truthy(f) {
			face = pyjson.Str(f)
		}
		fmt.Fprintf(env.Stdout, "%d\t%s (%s)\t%s\n", i, who, face, tsvField(pyjson.Str(line["text"])))
		v, ok := line["show"]
		switch {
		case !ok:
			continue
		case v == nil:
			fmt.Fprintln(env.Stdout, "\t\t［図解を消す］")
		case v == show.Next:
			sd := &table[*idx[i]]
			fmt.Fprintf(env.Stdout, "\t\t［図解の段を進める］%s\n", stepLabel(sd, *steps[i]))
		default:
			sd := &table[*idx[i]]
			label := showSummary(sd)
			if steps[i] != nil {
				label += "  " + stepLabel(sd, *steps[i])
			}
			fmt.Fprintf(env.Stdout, "\t\t［図解］%s\n", tsvField(label))
		}
	}
	return nil
}

// showSummary は図解の中身を 1 行にまとめる (種類ごとに、台本に書いた欄を並べる)。
func showSummary(sd *show.Data) string {
	switch sd.Type {
	case "keyword":
		if sd.Sub != "" {
			return fmt.Sprintf("重要語「%s」 補足: %s", sd.Text, sd.Sub)
		}
		return fmt.Sprintf("重要語「%s」", sd.Text)
	case "compare":
		return fmt.Sprintf("比較 %s: %s ｜ %s: %s", sd.Left.Title, strings.Join(sd.Left.Items, " / "), sd.Right.Title, strings.Join(sd.Right.Items, " / "))
	case "list":
		head := "箇条書き"
		if sd.Title != "" {
			head += "「" + sd.Title + "」"
		}
		return head + ": " + strings.Join(sd.Items, " / ")
	case "code":
		head := "コード"
		if sd.Lang != "" {
			head += " (" + sd.Lang + ")"
		}
		head += fmt.Sprintf(" %d 行", len(sd.Lines))
		if len(sd.Marks) > 0 {
			head += " 強調: " + intList(sd.Marks)
		}
		return head
	case "image":
		return fmt.Sprintf("図「%s」 (%s)", sd.Alt, sd.SrcWritten)
	case "mermaid":
		return fmt.Sprintf("mermaid の図「%s」 (%d 行)", sd.Alt, len(sd.Code))
	}
	return sd.Type
}

// stepLabel は段のある図解の、今出している段 (0 始まりの step) で見えるもの。
func stepLabel(sd *show.Data, step int) string {
	n := sd.StepCount()
	head := fmt.Sprintf("段 %d/%d: ", step+1, n)
	switch sd.Type {
	case "list":
		return head + sd.Items[step] + " を強調"
	case "compare":
		return head + sd.Left.Items[step] + " ｜ " + sd.Right.Items[step] + " を出す"
	default:
		return head + "強調 " + intList(sd.Steps[step])
	}
}

func intList(v []int) string {
	if len(v) == 0 {
		return "なし"
	}
	s := make([]string, len(v))
	for i, n := range v {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, ",")
}
