package lint

// uniqueInOrder は本家の list(dict.fromkeys(items)) (重なりを除き、初めて出た順を保つ)。
// 並びが message と内部の欄 (_words など) に出るので、map を反復して作らない。lint の重なりの除去は全部これを通す。
func uniqueInOrder[T comparable](items []T) []T {
	seen := make(map[T]struct{}, len(items))
	out := make([]T, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
