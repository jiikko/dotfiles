package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// fill は v のすべての公開の欄を零でない値で埋める (slice は 1 要素、pointer は先まで)。記録の型に欄が増えても追従する。
func fill(v reflect.Value, depth int) {
	if depth > 8 {
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString("埋めた")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fill(s.Index(0), depth+1)
		v.Set(s)
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		fill(p.Elem(), depth+1)
		v.Set(p)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		k, e := reflect.New(v.Type().Key()).Elem(), reflect.New(v.Type().Elem()).Elem()
		fill(k, depth+1)
		fill(e, depth+1)
		m.SetMapIndex(k, e)
		v.Set(m)
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			v.Set(reflect.ValueOf(time.Date(2026, 9, 27, 1, 2, 3, 4, time.UTC)))
			return
		}
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				fill(v.Field(i), depth+1)
			}
		}
	default: // interface・chan・func は記録に無い (JSON で往復しない)
	}
}

// scribble は v の届くかぎりの値を書き換える (呼び手が読んだ State をいじる形を、欄を漏らさず真似る)。
func scribble(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("書き換えた")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(v.Int() + 1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(v.Uint() + 1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(v.Float() + 1)
	case reflect.Bool:
		v.SetBool(!v.Bool())
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			scribble(v.Index(i))
		}
	case reflect.Pointer:
		if !v.IsNil() {
			scribble(v.Elem())
		}
	case reflect.Map:
		for it := v.MapRange(); it.Next(); {
			e := reflect.New(v.Type().Elem()).Elem()
			e.Set(it.Value())
			scribble(e)
			v.SetMapIndex(it.Key(), e)
		}
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			v.Set(reflect.ValueOf(time.Unix(1, 0)))
			return
		}
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				scribble(v.Field(i))
			}
		}
	default: // interface・chan・func は記録に無い (JSON で往復しない)
	}
}

// 読むだけの呼び手が後で読んだ記録は、前に (キャッシュから) 読んだ State を呼び手がどう書き換えても、ファイルの中身のまま (issue 528)。
// キャッシュの State をそのまま返す・浅く写して返すと、書き換えが次の呼び手に漏れて落ちる。
func TestLoadResultIsNotShared(t *testing.T) {
	dir := t.TempDir()
	var full State
	fill(reflect.ValueOf(&full).Elem(), 0)
	data, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, StateFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	var want State
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err != nil { // 解析してキャッシュに置く
		t.Fatal(err)
	}
	decodes := loadDecodes
	a, err := Load(dir) // キャッシュに当たった読み (書き換えるのはこちら)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, want) {
		t.Fatalf("ファイルの中身と違う:\n%+v\n%+v", a, want)
	}
	scribble(reflect.ValueOf(&a).Elem())
	if reflect.DeepEqual(a, want) {
		t.Fatal("書き換えが効いていない (テストの組み立ての誤り)")
	}
	b, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loadDecodes != decodes {
		t.Fatalf("変わっていない記録を解析し直した (%d 回 → %d 回。キャッシュに当たっていない = このテストは共有を確かめられない)", decodes, loadDecodes)
	}
	if !reflect.DeepEqual(b, want) {
		t.Fatalf("前に読んだ State の書き換えが後の読みに漏れた:\n%+v", b)
	}
}

// 記録が書き換わったら Load は読み直す: 書き手の Update (rename で置く) と、同じ大きさでの上書き (テストや人の手) の両方。
func TestLoadSeesRewrite(t *testing.T) {
	dir := t.TempDir()
	if err := Update(dir, func(s *State) error { s.NextID = 5; return nil }); err != nil {
		t.Fatal(err)
	}
	if st, _ := Load(dir); st.NextID != 5 {
		t.Fatalf("NextID %d", st.NextID)
	}
	if err := Update(dir, func(s *State) error { s.NextID = 6; return nil }); err != nil {
		t.Fatal(err)
	}
	if st, _ := Load(dir); st.NextID != 6 {
		t.Fatalf("Update の後に古い記録を返した: NextID %d", st.NextID)
	}
	p := filepath.Join(dir, StateFile)
	if err := os.WriteFile(p, []byte(`{"nextId":7,"cards":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, _ := Load(dir); st.NextID != 7 {
		t.Fatalf("上書きの後に古い記録を返した: NextID %d", st.NextID)
	}
	if err := os.WriteFile(p, []byte(`{"nextId":8,"cards":[]}`), 0o600); err != nil { // 同じ大きさ・同じ inode
		t.Fatal(err)
	}
	if st, _ := Load(dir); st.NextID != 8 {
		t.Fatalf("同じ大きさの上書きの後に古い記録を返した: NextID %d", st.NextID)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if st, err := Load(dir); err != nil || st.NextID != 1 || st.Cards != nil {
		t.Fatalf("消した後に前の記録を返した: %+v %v", st, err)
	}
}

// 記録の型の非公開の欄は cloneState が浅く写すので、参照 (slice・pointer・map・interface) を持たせない (time.Time の *Location は変わらないので除く)。
func TestLoadedStateHasNoUnexportedRefs(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type, string)
	walk = func(ty reflect.Type, path string) {
		if seen[ty] || ty == reflect.TypeFor[time.Time]() {
			return
		}
		seen[ty] = true
		switch ty.Kind() {
		case reflect.Slice, reflect.Pointer, reflect.Array:
			walk(ty.Elem(), path+"[]")
		case reflect.Map:
			walk(ty.Key(), path+"{key}")
			walk(ty.Elem(), path+"{}")
		case reflect.Struct:
			for i := range ty.NumField() {
				f := ty.Field(i)
				if !f.IsExported() {
					switch f.Type.Kind() {
					case reflect.Slice, reflect.Pointer, reflect.Map, reflect.Interface, reflect.Struct, reflect.Array:
						t.Errorf("%s.%s は参照を持ちうる非公開の欄 (cloneState が複製しない)", path, f.Name)
					default: // 代入で写せる値
					}
					continue
				}
				walk(f.Type, path+"."+f.Name)
			}
		default: // 先に型の無いもの (interface は deepCopy が中の値を辿る)
		}
	}
	walk(reflect.TypeFor[State](), "State")
}
