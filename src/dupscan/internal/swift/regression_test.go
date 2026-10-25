package swift

import (
	"strings"
	"testing"

	"dupscan/internal/norm"
	"dupscan/internal/tree"
)

// 誤検知 (違う処理を同じと判定する) と見逃しになりやすい形の固定。

func unitBy(t *testing.T, r FileResult, sig string) *tree.Unit {
	t.Helper()
	for _, u := range r.Units {
		if u.Signature == sig || u.Name == sig {
			return u
		}
	}
	var have []string
	for _, u := range r.Units {
		have = append(have, u.Signature)
	}
	t.Fatalf("単位 %q が無い: %v", sig, have)
	return nil
}

// differ は a と b が、levels の全部の段で違うハッシュになることを確かめる。
func differ(t *testing.T, src, a, b string, levels ...norm.Level) {
	t.Helper()
	r := parse(t, "Sources/X.swift", src)
	ra, rb := norm.NormalizeWithForms(unitBy(t, r, a)), norm.NormalizeWithForms(unitBy(t, r, b))
	for _, l := range levels {
		if ra.Hash[l] == rb.Hash[l] {
			t.Errorf("%s と %s が %s で一致してしまう\n%s\n%s", a, b, l, ra.Forms[l], rb.Forms[l])
		}
	}
}

var all3 = []norm.Level{norm.L1, norm.L2, norm.L3}

func TestEnumCasesAndPatternValuesAreNotLocals(t *testing.T) {
	src := `
enum Mode { case light, dark, system }
let minValue = 0, maxValue = 100
enum Err: Error { case timeout, offline }
func titleA(_ m: Mode) -> String { switch m { case .light: return "On"; case .dark, .system: return "Off" } }
func titleB(_ m: Mode) -> String { switch m { case .dark: return "On"; case .light, .system: return "Off" } }
func atMin(_ v: Int) -> String { switch v { case minValue: return "edge"; default: return "inside" } }
func atMax(_ v: Int) -> String { switch v { case maxValue: return "edge"; default: return "inside" } }
func onTimeout(_ f: () throws -> Void) -> Int { do { try f() } catch Err.timeout { return 1 } catch { return 2 }; return 0 }
func onOffline(_ f: () throws -> Void) -> Int { do { try f() } catch Err.offline { return 1 } catch { return 2 }; return 0 }
func codeA(_ r: Result<Int, Error>) -> Int { switch r { case .success(let v): return v + 1; case .failure: return 0 } }
func codeB(_ r: Result<Int, Error>) -> Int { switch r { case .success(let w): return w + 1; case .failure: return 0 } }`
	differ(t, src, "titleA", "titleB", all3...)
	differ(t, src, "atMin", "atMax", norm.L1, norm.L2)
	differ(t, src, "onTimeout", "onOffline", norm.L1, norm.L2)
	// `case .success(let v)` の v は束縛 (局所の名前) なので、名前だけが違えば L1 で同じ。
	r := parse(t, "Sources/X.swift", src)
	if a, b := norm.NormalizeWithForms(unitBy(t, r, "codeA")), norm.NormalizeWithForms(unitBy(t, r, "codeB")); a.Hash[norm.L1] != b.Hash[norm.L1] {
		t.Errorf("束縛の名前だけが違うのに L1 で一致しない\n%s\n%s", a.Forms[norm.L1], b.Forms[norm.L1])
	}
}

func TestPositionsAreKept(t *testing.T) {
	src := `
func asc(_ items: [Item]) -> [Item] { items.sorted { $0.date < $1.date } }
func desc(_ items: [Item]) -> [Item] { items.sorted { $1.date < $0.date } }
func sub1(a: Int, b: Int) -> Int { let d = a - b; return d * 2 + a }
func sub2(a: Int, b: Int) -> Int { let d = b - a; return d * 2 + b }
func t1(isOn: Bool) -> some View { Toggle("Label", isOn: $isOn).padding(4).opacity(0.5) }
func t2(isOn: Bool) -> some View { Toggle("Label", isOn: $isMuted).padding(4).opacity(0.5) }`
	differ(t, src, "asc", "desc", norm.L1, norm.L2)
	differ(t, src, "sub1", "sub2", all3...)
	differ(t, src, "t1", "t2", norm.L1, norm.L2) // projected value ($isOn) は局所の名前ではない
}

func TestReturnTypeAndSyntaxTextAreHashed(t *testing.T) {
	src := `
func parseInt(_ s: String) -> Int? { let t = s.trimmingCharacters(in: .whitespaces); return .init(t) }
func parseDouble(_ s: String) -> Double? { let t = s.trimmingCharacters(in: .whitespaces); return .init(t) }
func opA(_ x: Int, _ y: Int) -> Int { let z = x <+> y; return z <+> x <+> y }
func opB(_ x: Int, _ y: Int) -> Int { let z = x <-> y; return z <-> x <-> y }
func condA(_ x: Int) -> Int {
    #if os(iOS)
    let y = x + 1
    #endif
    return y * 3
}
func condB(_ x: Int) -> Int {
    #if DEBUG
    let y = x + 1
    #endif
    return y * 3
}
func lblA(_ xs: [Int]) { outer: for a in xs { for b in xs { if a == b { break outer } } } }
func lblB(_ xs: [Int]) { inner: for a in xs { for b in xs { if a == b { break inner } } } }`
	differ(t, src, "parseInt", "parseDouble", all3...)
	differ(t, src, "opA", "opB", all3...)
	differ(t, src, "condA", "condB", all3...)
	differ(t, src, "lblA", "lblB", all3...)
}

func TestL3KeepsReceiversAssignmentTargetsAndStatements(t *testing.T) {
	src := `
func encJSON(_ v: [String: Int]) throws -> Data { let d = try JSONEncoder().encode(v); return d + Data() }
func encPlist(_ v: [String: Int]) throws -> Data { let d = try PropertyListEncoder().encode(v); return d + Data() }
func mkA(_ id: Int) -> AnyObject { let o = UserStore.make(id); return o }
func mkB(_ id: Int) -> AnyObject { let o = PaymentStore.make(id); return o }
func startLoad(_ id: Int) { isLoading = true; print(id, id + 1) }
func stopEdit(_ id: Int) { isEditing = false; print(id, id + 1) }
func one(_ xs: [Int]) -> Int { deleteEverything(); return xs.count }
func two(_ xs: [Int]) -> Int { showWelcome(); return xs.count }`
	differ(t, src, "encJSON", "encPlist", all3...)
	differ(t, src, "mkA", "mkB", all3...)
	differ(t, src, "startLoad", "stopEdit", all3...)
	differ(t, src, "one", "two", all3...)
}

func TestPropertiesSubscriptsAndAccessorsAreUnits(t *testing.T) {
	r := parse(t, "Sources/M.swift", `
final class M {
    var fullName: String { let parts = [first, last].filter { !$0.isEmpty }; return parts.joined(separator: " ") + "!" }
    var displayName: String { let parts = [first, last].filter { !$0.isEmpty }; return parts.joined(separator: " ") + "!" }
    subscript(i: Int) -> Int { let v = items[i] * 2 + 3; return v - i }
    subscript(j j: Int) -> Int { let v = items[j] * 2 + 3; return v - j }
    deinit { items.removeAll() }
    var watched: Int = 0 { didSet { print(watched) } }
}`)
	a, b := norm.NormalizeWithForms(unitBy(t, r, "var fullName: String")), norm.NormalizeWithForms(unitBy(t, r, "var displayName: String"))
	if a.Hash[norm.L1] != b.Hash[norm.L1] {
		t.Errorf("同じ本体の computed property が L1 で一致しない\n%s\n%s", a.Forms[norm.L1], b.Forms[norm.L1])
	}
	s1, s2 := norm.NormalizeWithForms(unitBy(t, r, "subscript(i: Int) -> Int")), norm.NormalizeWithForms(unitBy(t, r, "subscript(j j: Int) -> Int"))
	if s1.Hash[norm.L1] != s2.Hash[norm.L1] {
		t.Errorf("同じ本体の subscript が L1 で一致しない\n%s\n%s", s1.Forms[norm.L1], s2.Forms[norm.L1])
	}
	unitBy(t, r, "deinit")
	unitBy(t, r, "watched.didSet")
}

func TestKeysDoNotCollide(t *testing.T) {
	r := parse(t, "Sources/K.swift", `
final class K {
    #if os(iOS)
    func color(_ x: Int) -> Int { let v = x * 2 + 1; return v - x + 3 }
    #else
    func color(_ x: Int) -> Int { let v = x * 5 - 7; return v + x * 9 }
    #endif
    static func make(_ x: Int) -> Int { x }
    func make(_ x: Int) -> Int { x }
    func a() { func helper(_ x: Int) -> Int { x }; _ = helper(1) }
    func b() { func helper(_ x: Int) -> Int { x + 1 }; _ = helper(1) }
}
extension Array where Element == Int { func total(_ k: Int) -> Int { k } }
extension Array where Element == String { func total(_ k: Int) -> Int { k } }`)
	seen := map[string]bool{}
	for _, u := range r.Units {
		if seen[u.Key] {
			t.Errorf("鍵が重なった: %s", u.Key)
		}
		seen[u.Key] = true
	}
}

func TestTestDetectionVariants(t *testing.T) {
	r := parse(t, "Specs/BarSpec.swift", `
import XCTest
final class BarSpec: XCTestCase {
    func testA() { XCTAssertEqual(1, 1) }
    func makeInput(n: Int) -> Int { n }
}
struct S {
    @Testing.Test func qualified() { #expect(true) }
}`)
	for _, u := range r.Units {
		if u.Name == "testA" || u.Name == "qualified" {
			t.Errorf("テスト関数が単位になった: %s", u.Name)
		}
		if u.Name == "makeInput" && !u.Test {
			t.Error("XCTestCase を継承した型の中のヘルパーが test にならない")
		}
	}
}

func TestUnitsInsideErrorAreUnreadable(t *testing.T) {
	r := parse(t, "Sources/B.swift", "struct Y {\n    let x = [1, 2,\n    func g(_ q: Int) -> Int { let a = q * 2 + 3; return a - q }\n}\n")
	if !r.HasError {
		t.Fatal("HasError が立たない")
	}
	for _, u := range r.Units {
		if !u.Unreadable {
			t.Errorf("読めない構文の中の単位が比べる対象に残った: %s %s", u.Key, strings.TrimSpace(u.Signature))
		}
	}
}

func same(t *testing.T, src, a, b string, l norm.Level) {
	t.Helper()
	r := parse(t, "Sources/X.swift", src)
	ra, rb := norm.NormalizeWithForms(unitBy(t, r, a)), norm.NormalizeWithForms(unitBy(t, r, b))
	if ra.Hash[l] != rb.Hash[l] {
		t.Errorf("%s と %s が %s で一致しない\n%s\n%s", a, b, l, ra.Forms[l], rb.Forms[l])
	}
}

func TestBindingsOutsideThePatternNode(t *testing.T) {
	src := `
func tupA(_ t: (Int, Int)) -> Int { let (a, b) = t; return a - b }
func tupB(_ t: (Int, Int)) -> Int { let (a, b) = t; return b - a }
func tupC(_ t: (Int, Int)) -> Int { let (x, y) = t; return x - y }
func icA(_ v: P) -> Int { if case let .pair(x, y) = v { return x - y }; return 0 }
func icB(_ v: P) -> Int { if case let .pair(x, y) = v { return y - x }; return 0 }
func icC(_ v: P) -> Int { if case let .pair(p, q) = v { return p - q }; return 0 }`
	differ(t, src, "tupA", "tupB", all3...)
	same(t, src, "tupA", "tupC", norm.L1)
	differ(t, src, "icA", "icB", all3...)
	same(t, src, "icA", "icC", norm.L1)
}

func TestActionCallsAreNotFoldedAtL3(t *testing.T) {
	src := `
func tryA(_ id: Int) throws { try deleteEverything(); print(id) }
func tryB(_ id: Int) throws { try showWelcome(); print(id) }
func awA(_ id: Int) async throws { try await store.deleteAll(); print(id) }
func awB(_ id: Int) async throws { try await store.save(); print(id) }
func retA(_ id: Int) -> Int { print(id); return fetchA() }
func retB(_ id: Int) -> Int { print(id); return fetchB() }
func stA(_ id: Int) { status = fetchA(); print(id) }
func stB(_ id: Int) { status = fetchB(); print(id) }
func isTimeout(_ e: Err) -> Bool { if case .timeout = e { return true }; return e == e }
func isOffline(_ e: Err) -> Bool { if case .offline = e { return true }; return e == e }`
	for _, p := range [][2]string{{"tryA", "tryB"}, {"awA", "awB"}, {"retA", "retB"}, {"stA", "stB"}, {"isTimeout", "isOffline"}} {
		differ(t, src, p[0], p[1], all3...)
	}
}

func TestDidSetParameterIsLocal(t *testing.T) {
	src := `
final class W {
    var a: Int = 0 { didSet(old) { print(a - old, old) } }
    var b: Int = 0 { didSet(prev) { print(a - prev, prev) } }
}`
	same(t, src, "a.didSet", "b.didSet", norm.L1)
}

func TestBindingDoesNotLeakIntoBodies(t *testing.T) {
	src := `
let minValue = 0, maxValue = 100
func inIfA(_ o: Int?, _ e: Int) -> Int { if let x = o { switch e { case minValue: return x; default: return 0 } }; return 1 }
func inIfB(_ o: Int?, _ e: Int) -> Int { if let x = o { switch e { case maxValue: return x; default: return 0 } }; return 1 }
func swExA(_ e: Int) -> Int { let r = switch e { case minValue: 1 default: 2 }; return r + e }
func swExB(_ e: Int) -> Int { let r = switch e { case maxValue: 1 default: 2 }; return r + e }`
	differ(t, src, "inIfA", "inIfB", norm.L1, norm.L2)
	differ(t, src, "swExA", "swExB", norm.L1, norm.L2)
}

func TestActionCalleeVariants(t *testing.T) {
	src := `
func uA(_ id: Int) throws { try _deleteAll(); print(id) }
func uB(_ id: Int) throws { try _save(); print(id) }
func bA(_ id: Int) throws { try ` + "`delete`" + `(); print(id) }
func bB(_ id: Int) throws { try ` + "`import`" + `(); print(id) }
func jA(_ id: Int) throws { try 削除(); print(id) }
func jB(_ id: Int) throws { try 保存(); print(id) }
func fA(_ id: Int) throws { try onDelete!(); print(id) }
func fB(_ id: Int) throws { try onSave!(); print(id) }
func pA(_ id: Int) throws { try (deleteAll)(); print(id) }
func pB(_ id: Int) throws { try (save)(); print(id) }
func wA(_ it: inout I) -> Int { var n = 0; while case .timeout = it.next() { n += 1 }; return n }
func wB(_ it: inout I) -> Int { var n = 0; while case .offline = it.next() { n += 1 }; return n }`
	for _, p := range [][2]string{{"uA", "uB"}, {"bA", "bB"}, {"jA", "jB"}, {"fA", "fB"}, {"pA", "pB"}, {"wA", "wB"}} {
		differ(t, src, p[0], p[1], all3...)
	}
}

func TestBindingsPerClauseAndForItems(t *testing.T) {
	src := `
let minValue = 0, maxValue = 100
func forA(_ xs: [(Int, Int)]) -> Int { var s = 0; for (a, b) in xs { s += a - b }; return s }
func forB(_ xs: [(Int, Int)]) -> Int { var t = 0; for (x, y) in xs { t += x - y }; return t }
func forC(_ xs: [(Int, Int)]) -> Int { var s = 0; for (a, b) in xs { s += a * b }; return s }
func icA(_ z: Int) -> Int { if case minValue = z { return 1 }; return z }
func icB(_ z: Int) -> Int { if case maxValue = z { return 1 }; return z }
func clA(_ o: Int?, _ z: E) -> Int { if let a = o, case .y(minValue) = z { return a }; return 0 }
func clB(_ o: Int?, _ z: E) -> Int { if let a = o, case .y(maxValue) = z { return a }; return 0 }
func capA(_ xs: [Int]) -> [Int] { xs.map { [k = xs.count] a in a * k + 1 } }
func capB(_ ys: [Int]) -> [Int] { ys.map { [m = ys.count] b in b * m + 1 } }`
	same(t, src, "forA", "forB", norm.L1)
	differ(t, src, "forA", "forC", all3...)
	differ(t, src, "icA", "icB", norm.L1, norm.L2)
	differ(t, src, "clA", "clB", norm.L1, norm.L2)
	same(t, src, "capA", "capB", norm.L1)
}

func TestValueConstructionsFoldAtL3(t *testing.T) {
	src := `
func a1(_ p: String) -> L { L(c: ConnectionID.init(rawValue: "c"), path: P(rawValue: p)) }
func a2(_ p: String) -> L { L(c: [Int](repeating: 0, count: 3), path: P(rawValue: p)) }
func a3(_ p: String) -> L { L(c: Ärger(rawValue: "c"), path: P(rawValue: p)) }
func b0(_ p: String) -> L { L(c: connectionID, path: P(rawValue: p)) }
func repA(_ it: inout I) -> Int { var n = 0; repeat { n += 1 } while case .timeout = it.next(); return n }
func repB(_ it: inout I) -> Int { var n = 0; repeat { n += 1 } while case .offline = it.next(); return n }`
	for _, a := range []string{"a1", "a2", "a3"} {
		same(t, src, a, "b0", norm.L3)
	}
	differ(t, src, "repA", "repB", all3...)
}

func TestCaptureListsForCaseAndDelegatingInit(t *testing.T) {
	src := `
let lo = 0, hi = 9
func capA(_ x: Int) -> () -> Int { { [store] in store.count + x } }
func capB(_ x: Int) -> () -> Int { { [cache] in cache.count + x } }
func valA(_ x: Int) -> () -> Int { { [k = store] in k.count + x } }
func valB(_ x: Int) -> () -> Int { { [k = cache] in k.count + x } }
func valC(_ x: Int) -> () -> Int { { [m = store] in m.count + x } }
func weakA(_ x: Int) { run { [weak delegate] in delegate?.go(x) } }
func weakB(_ x: Int) { run { [weak observer] in observer?.go(x) } }
func h1(_ xs: [E]) -> Int { var n = 0; for case .p(lo) in xs { n += 1 }; return n }
func h2(_ xs: [E]) -> Int { var n = 0; for case .p(hi) in xs { n += 1 }; return n }
final class C {
    var a = 0
    convenience init(a: Int) throws { try self.init(name: "x", flag: true); self.a = a + 1 }
    convenience init(b: Int) throws { try self.init(id: 0); self.a = b + 1 }
}`
	differ(t, src, "capA", "capB", norm.L1, norm.L2) // L3 では外側の値の読み (store.count) を畳むので同じでよい
	differ(t, src, "valA", "valB", norm.L1, norm.L2)
	same(t, src, "valA", "valC", norm.L1)
	differ(t, src, "weakA", "weakB", norm.L1, norm.L2)
	differ(t, src, "h1", "h2", norm.L1, norm.L2)
	differ(t, src, "init(a: Int)", "init(b: Int)", all3...)
}

func TestCapturedNamesAndStaticSelfInit(t *testing.T) {
	src := `
func t1() { run { [weak store] in store?.save(Config.alpha); store?.flush(Limits.max) } }
func t2() { run { [weak store] in store?.save(Config.beta); store?.flush(Limits.min) } }
final class C {
    var v = 0
    static func a(_ x: Int) -> C { let c = self.init(id: Defaults.alpha); c.v = x; return c }
    static func e(_ x: Int) -> C { let c = Self.init(id: Defaults.alpha); c.v = x; return c }
}`
	r := parse(t, "Sources/X.swift", src)
	for _, n := range []string{"t1", "t2"} {
		if norm.NormalizeWithForms(unitBy(t, r, n)).L3Trivial {
			t.Errorf("%s: 捕まえた名前を使う単位が L3Trivial になった", n)
		}
	}
	same(t, src, "t1", "t2", norm.L3)
	same(t, src, "static a(_ x: Int) -> C", "static e(_ x: Int) -> C", norm.L3)
}
