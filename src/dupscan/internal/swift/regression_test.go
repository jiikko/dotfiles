package swift

import (
	"strings"
	"testing"

	"dupscan/internal/norm"
	"dupscan/internal/tree"
)

// 敵対的レビュー (2026-10-24) が再現した誤検知・見逃しの固定。入力はレビューの再現の入力を縮めたもの。

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
	ra, rb := norm.Normalize(unitBy(t, r, a)), norm.Normalize(unitBy(t, r, b))
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
	if a, b := norm.Normalize(unitBy(t, r, "codeA")), norm.Normalize(unitBy(t, r, "codeB")); a.Hash[norm.L1] != b.Hash[norm.L1] {
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
	a, b := norm.Normalize(unitBy(t, r, "var fullName: String")), norm.Normalize(unitBy(t, r, "var displayName: String"))
	if a.Hash[norm.L1] != b.Hash[norm.L1] {
		t.Errorf("同じ本体の computed property が L1 で一致しない\n%s\n%s", a.Forms[norm.L1], b.Forms[norm.L1])
	}
	s1, s2 := norm.Normalize(unitBy(t, r, "subscript(i: Int) -> Int")), norm.Normalize(unitBy(t, r, "subscript(j j: Int) -> Int"))
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
