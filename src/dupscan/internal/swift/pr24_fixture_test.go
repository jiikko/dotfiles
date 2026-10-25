package swift

import (
	"testing"

	"dupscan/internal/norm"
)

// obaket の PR 24 (週次の Claude audit が寄せたテストのヘルパー) の前の木 (commit 45cc3b208^) から切り出した 6 つの写し。
// 本体は obaket のまま写し、まわりは型の宣言と、値を読むプロパティだけを残した (ユーザー 2026-10-25: dotfiles に置いてよい)。
// 出典のパスは shared/Tests/ObaketCoreTests/Move/ の下。dupscan がこの repo の外で一番最初に見つけるべきだった組なので、段ごとの期待値を固定する。
var pr24Files = map[string]string{
	"MoveReconcileExecutorTests.swift": `
private func location(path: String) -> StorageLocation {
    StorageLocation(
        connectionID: ConnectionID(rawValue: "conn"),
        namespace: .bucket("bucket"),
        path: ObjectPath(rawValue: path)
    )
}`,
	"MoveReconcileTests.swift": `
struct MoveReconcileTests {
    private func location(path: String) -> StorageLocation {
        StorageLocation(
            connectionID: ConnectionID(rawValue: "conn"),
            namespace: .bucket("bucket"),
            path: ObjectPath(rawValue: path)
        )
    }
}`,
	"ResumableMoveUseCaseTests.swift": `
private func location(path: String) -> StorageLocation {
    StorageLocation(
        connectionID: ConnectionID(rawValue: "conn"),
        namespace: .bucket("bucket"),
        path: ObjectPath(rawValue: path)
    )
}`,
	"MutationFinishingMoveReconcileStorageTests.swift": `
struct MutationFinishingMoveReconcileStorageTests {
    private func location(_ path: String) -> StorageLocation {
        StorageLocation(connectionID: ConnectionID(rawValue: "conn"), namespace: .bucket("bucket"), path: ObjectPath(rawValue: path))
    }
}`,
	"BuildMoveManifestUseCaseTests.swift": `
struct BuildMoveManifestUseCaseTests {
    private let connectionID = ConnectionID(rawValue: "conn")
    private let namespace = StorageNamespace.bucket("bucket")

    private func location(path: String) -> StorageLocation {
        StorageLocation(
            connectionID: connectionID,
            namespace: namespace,
            path: ObjectPath(rawValue: path)
        )
    }
}`,
	"MovePathMappingTests.swift": `
struct MovePathMappingTests {
    private let connectionID = ConnectionID(rawValue: "conn")
    private let namespace = StorageNamespace.bucket("bucket")

    private func location(path: String) -> StorageLocation {
        StorageLocation(
            connectionID: connectionID,
            namespace: namespace,
            path: ObjectPath(rawValue: path)
        )
    }
}`,
}

// 直書きの 4 つは名前の違う写し (L1 から同じ)。プロパティから値を読む 2 つは互いに L1 で同じで、直書きとは L3 (値の書き方を潰す段) で初めて同じになる。
func TestPR24CopiesMatchByLevel(t *testing.T) {
	literal := []string{"MoveReconcileExecutorTests.swift", "MoveReconcileTests.swift", "ResumableMoveUseCaseTests.swift", "MutationFinishingMoveReconcileStorageTests.swift"}
	property := []string{"BuildMoveManifestUseCaseTests.swift", "MovePathMappingTests.swift"}
	hash := map[string]norm.Result{}
	for name, src := range pr24Files {
		r := parse(t, "Tests/"+name, src)
		hash[name] = norm.Normalize(unitBy(t, r, "location"))
	}
	same := func(lv norm.Level, a, b string) bool { return hash[a].Hash[lv] == hash[b].Hash[lv] }
	for _, lv := range all3 {
		for _, f := range literal[1:] {
			if !same(lv, literal[0], f) {
				t.Errorf("%s: 直書きの %s と %s が違う\n%s\n%s", lv, literal[0], f, hash[literal[0]].Forms[lv], hash[f].Forms[lv])
			}
		}
		if !same(lv, property[0], property[1]) {
			t.Errorf("%s: プロパティを読む 2 つが違う", lv)
		}
	}
	for _, lv := range []norm.Level{norm.L1, norm.L2} {
		if same(lv, literal[0], property[0]) {
			t.Errorf("%s: 直書きとプロパティを読む版が同じになった (L3 より前の段で値の書き方を潰している)", lv)
		}
	}
	if !same(norm.L3, literal[0], property[0]) {
		t.Errorf("L3: 直書きとプロパティを読む版が違う\n%s\n%s", hash[literal[0]].Forms[norm.L3], hash[property[0]].Forms[norm.L3])
	}
}
