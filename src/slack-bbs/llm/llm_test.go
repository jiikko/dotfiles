package llm

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"slack-bbs/board"
)

// claude -p に渡す応答の形 (Schema) の欄と、応答を読む型 (board.RespTopic) の欄が一致する
// (片方だけ変えると、LLM の返した欄を黙って読み落とすか、要る欄を返させない)。
func TestSchemaMatchesRespTopic(t *testing.T) {
	var s struct {
		Properties struct {
			Topics struct {
				Items struct {
					Required []string `json:"required"`
				} `json:"items"`
			} `json:"topics"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(Schema), &s); err != nil {
		t.Fatalf("Schema が JSON でない: %v", err)
	}
	var fields []string
	rt := reflect.TypeOf(board.RespTopic{})
	for i := range rt.NumField() {
		fields = append(fields, strings.Split(rt.Field(i).Tag.Get("json"), ",")[0])
	}
	got := slices.Sorted(slices.Values(s.Properties.Topics.Items.Required))
	slices.Sort(fields)
	if !slices.Equal(got, fields) {
		t.Errorf("Schema の required %v と RespTopic の欄 %v が違う", got, fields)
	}
}

// 指示文か応答の形を変えたら、分類のしかたの名前 (キャッシュの使い回しの鍵) も変わる。
func TestClassifierNamesModelAndPrompt(t *testing.T) {
	a, b := Classifier("sonnet"), Classifier("opus")
	if a == b || !strings.HasPrefix(a, "sonnet@") || len(a) <= len("sonnet@") {
		t.Errorf("Classifier: %q %q", a, b)
	}
	if classifierOf("sonnet", "指示 1") == classifierOf("sonnet", "指示 2") {
		t.Errorf("指示文が違っても同じ名前になる")
	}
	if Classifier("sonnet") != classifierOf("sonnet", instructions+Schema) {
		t.Errorf("Classifier が指示文と応答の形の両方からハッシュを取っていない")
	}
}
