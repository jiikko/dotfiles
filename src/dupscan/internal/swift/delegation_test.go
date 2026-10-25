package swift

import "testing"

// 「1 つの呼び出しに原子だけを渡す」委譲の判定 (除外のプリセット l2-single-call が読む。issue 919)。
func TestDelegationIsOneCallWithAtomArguments(t *testing.T) {
	src := `
final class D: UserDefaults {
    init() { super.init(suiteName: "a.\(UUID().uuidString)")! }
}
func tryCall(contents: Data) throws -> URL { try TestScratchDirectory.makeFile(prefix: "x", contents: contents) }
func returned(_ p: String) -> K { return K(bucket: p, path: .root, count: -1, flag: true, none: nil) }
func awaited(_ x: Int) async -> Int { await f(x: x) }
func commented(_ x: Int) -> Int {
    // 説明
    g(x: x, y: self.limit)
}
func arithmetic(_ x: Int) -> Int { x * 3 + 1 }
func computedArg(_ x: Int) -> Int { f(x: x + 1) }
func nestedCallWithArgs(_ x: Int) -> Int { f(x: g(x)) }
func trailing() { run { print(1) } }
func twoStatements(_ x: Int) -> Int {
    let y = x
    return f(x: y)
}
func guarded(_ x: Int?) {
    guard let v = x else { audit("missing"); rollback(); return }
}
func thrown(_ x: Int) throws { throw E(code: x) }
func conditional(_ x: Int) -> Int {
#if DEBUG
    f(x: x)
#else
    g(x: x)
#endif
}
func empty() {}
func interpolatedCall(_ x: Int) -> String { h(s: "v=\(compute(x))") }
func negVariable(_ y: Int) { print("a", -y, separator: ":") }
func bitNot(_ x: Int) { print("a", ~x, separator: ":") }
func notFlag(_ z: Bool) { print("a", !z, separator: ":") }
func negLiteral() { print("a", -1, separator: ":") }
func negHex() { print("a", -0x1, -0o7, -0b1, separator: ":") }
func subscripted(_ s: Store, _ fallback: Int) -> Int { return s.cache.values["a", default: fallback] }
`
	r := parse(t, "Sources/X.swift", src)
	want := map[string]bool{
		"init": true, "tryCall": true, "returned": true, "awaited": true, "commented": true,
		"arithmetic": false, "computedArg": false, "nestedCallWithArgs": false, "trailing": false, "twoStatements": false,
		"guarded": false, "thrown": false, "conditional": false, "empty": false, "interpolatedCall": false,
		"negVariable": false, "bitNot": false, "notFlag": false, "negLiteral": true, "negHex": true, "subscripted": false,
	}
	for name, w := range want {
		if got := unitBy(t, r, name).Delegation; got != w {
			t.Errorf("%s: Delegation = %v, want %v", name, got, w)
		}
	}
}
