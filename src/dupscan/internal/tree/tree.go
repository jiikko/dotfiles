// Package tree は、言語に依らない「比べる単位」と、その本体の木を表す。
//
// adapter (言語ごと) が構文木をこの形に写し、コア (norm・group) はここに書かれた役割だけを見て正規化する。
package tree

// Role は、木の葉がコアにとって何であるかを表す。
type Role uint8

const (
	// RoleStruct は構造だけを表す節 (種類だけをハッシュに入れる)。
	RoleStruct Role = iota
	// RoleToken は匿名トークン (`+`・`==`・`try` など)。字面をハッシュに入れる。
	RoleToken
	// RoleName は名前の参照。局所の名前なら番号に、それ以外は字面のまま残す。
	RoleName
	// RoleDecl は局所の名前の宣言 (ローカル変数の束縛・クロージャの引数)。番号にする。
	RoleDecl
	// RoleMember はメンバーの名前 (`a.b` の `b`)。局所の名前と同じ字面でも番号にしない。
	RoleMember
	// RoleLabel は呼び出しの引数ラベル。字面のまま残す (潰すとラベルの違う呼び出しがどれも同じになる)。
	RoleLabel
	// RoleType は型の位置の名前。字面のまま残す。
	RoleType
	// RoleLiteral はリテラル。L1 では字面を、L2 からは LitKind を入れる。
	RoleLiteral
)

// Node は単位の本体の木の節。
type Node struct {
	Kind     string
	Text     string
	Role     Role
	LitKind  string
	Expr     bool // L3 で畳める式の境界
	Callee   bool // 呼び出しの callee。単独では畳まない (畳むと呼ぶ相手の違う呼び出しが同じになる)
	Children []*Node
}

// UnitKind は単位の種類。
type UnitKind string

// 単位の種類。
const (
	KindFunc    UnitKind = "func"
	KindInit    UnitKind = "init"
	KindClosure UnitKind = "closure"
)

// Unit は比べる単位 (関数・初期化子・クロージャ) 1 つ。
type Unit struct {
	// Key は単位の同一性の鍵 (ファイル + 囲む型 + 名前・署名。クロージャは囲む単位の鍵 + 出現の順番)。
	Key       string
	File      string // repo の root からの相対パス
	Line      int
	Col       int
	Name      string
	Signature string
	Kind      UnitKind
	// Owner は囲む単位の鍵 (クロージャのとき)。囲む単位が無ければ空。
	Owner      string
	Enclosing  string
	Test       bool // test のファイルにある
	Visibility string
	Params     []string // 局所の名前として扱う引数の名前
	Body       *Node
	Unreadable bool // 読めない構文 (ERROR / MISSING) を含む
}
