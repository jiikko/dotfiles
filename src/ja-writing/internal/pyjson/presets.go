package pyjson

// 本家 yomiyasu の check が JSON を書く 3 通りの出し方 (どれも ensure_ascii=True)。
// 記録・結果・登録は Indent1、記録の record_digest と照合の JSON は Compact、状態の印 (state.json) は Plain。

// Indent1 は json.dumps(v, ensure_ascii=True, indent=1)。
func Indent1() Options { return Options{Indented: true, Indent: 1} }

// Compact は json.dumps(v, sort_keys=True, ensure_ascii=True, separators=(",", ":"))。
func Compact() Options {
	return Options{SortKeys: true, Separators: &Separators{Item: ",", Key: ":"}}
}

// Plain は json.dumps(v, ensure_ascii=True) (indent も sort_keys も指定しない)。
func Plain() Options { return Options{} }
