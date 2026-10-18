// Package crosskey は横断キー (どの全画面からも同じ板へ飛ぶキー。docs/glogx-ui-guide.md §2) の集合。
// 画面のパッケージはこれでキーを判定し、押されたキーを main へ返す。行き先 (どの全画面か) は main の CrossTarget が決める
// (画面が全画面の一覧を知らずに済み、画面を足しても他の画面は作り直されない)。
package crosskey

// Is は key が横断キーか。main の CrossTarget のキーと同じ集合 (TestCrossKeysMatchCrossTarget が見る)。
func Is(key string) bool {
	switch key {
	case "i", "s", "R", "D", "F", "L":
		return true
	}
	return false
}
