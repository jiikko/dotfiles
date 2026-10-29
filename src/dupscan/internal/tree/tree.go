// Package tree は、言語に依らない「比べる単位」と、その本体の木を表す。
//
// adapter (言語ごと) が構文木をこの形に写し、コア (norm・group) はここに書かれた役割だけを見て正規化する。
package tree

import "strings"

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
	Kind    string
	Text    string
	Role    Role
	LitKind string
	Expr    bool // L3 で畳める式の境界
	// NoFold はこの節そのものを畳まない (子は畳んでよい)。文として置かれた式に付ける
	// (`deleteEverything()` と `showWelcome()` を 1 記号にすると、副作用の違う文が同じになる)。
	NoFold bool
	// Keep はこの節の下を何も畳まない。呼び出しの callee (`JSONEncoder().encode` の受け手を畳むと呼ぶ相手が消える) と
	// 代入の左辺 (`isLoading = …` と `isEditing = …` が同じになる) に付ける。
	Keep bool
	// Action は処理の呼び出し (小文字で始まる関数・メソッドの呼び出し)。これを含む式は L3 でも畳まない
	// (`try deleteEverything()` と `try showWelcome()`・`status = fetchA()` と `status = fetchB()` を同じにしない)。
	// 大文字の型の初期化 (`ConnectionID(rawValue:)`) と暗黙のメンバー (`.bucket("b")`) は値を作るだけなので畳む。
	Action bool
	// Capture は `[store]` のように捕まえた外側の名前。番号にはせず字面を残すが、畳むかの判定では局所の参照として数える
	// (数えないと、捕まえた名前だけを使うクロージャが L3 で本体ごと畳まれて比べられなくなる)。
	Capture bool
	// Outer は、局所の名前と同じ綴りでも単位の外の名前 (package・package 水準の宣言・プロパティ) を指す参照。番号にせず字面を残す
	// (綴りだけで局所と決めると `bytes := bytes.TrimSpace(b)` と `strings := strings.TrimSpace(s)` が同じ形になる)。
	// 解決できる adapter (Go の名前解決) だけが付ける。付かない参照は従来どおり綴りで決める。
	Outer    bool
	Children []*Node
}

// UnitKind は単位の種類。
type UnitKind string

// 単位の種類。
const (
	KindFunc    UnitKind = "func"
	KindInit    UnitKind = "init"
	KindClosure UnitKind = "closure"
	// KindProperty は computed property・subscript・accessor (didSet など)・deinit。
	KindProperty UnitKind = "property"
)

// Call は呼び出しの箇所 1 つ (callers で引く)。型を解決しないので、名前と引数ラベルだけの近似。
type Call struct {
	// Selector is a normalized Go identifier/selector chain; it does not resolve an import or receiver.
	Selector string `json:"-"`
	File     string
	Line     int
	Col      int
	Name     string   // 呼ばれる名前 (`a.b.make(…)` なら make。`Foo(…)` なら Foo)
	Labels   []string // 括弧の中の引数ラベル (ラベルの無い引数は `_`)
	// Trailing は後置クロージャがあること (その分の引数ラベルは Labels に入らない)。
	Trailing bool
	// Owner is nil when the adapter uses Unit-based ownership. A nonnil empty
	// owner explicitly means top-level or unknown, so callers must not fall back.
	Owner *CallOwner
	Text  string // その行 (前後の空白を除く)
}

// CallOwner describes a named call container independently of clone eligibility.
// File comes from Call; positions refer to the physical source, with an exclusive end.
type CallOwner struct {
	Signature       string
	Enclosing       string
	Line, Col       int
	EndLine, EndCol int
	Test            bool
	Unreadable      bool
}

// Unit は比べる単位 (関数・初期化子・クロージャ) 1 つ。
type Unit struct {
	// Key は単位の同一性の鍵 (ファイル + 囲む型 + 名前・署名。クロージャは囲む単位の鍵 + 出現の順番)。
	Key       string
	File      string // repo の root からの相対パス
	Line      int
	EndLine   int
	Col       int
	Name      string
	Signature string
	Kind      UnitKind
	// Owner は囲む単位の鍵 (クロージャのとき)。囲む単位が無ければ空。
	Owner string
	// OwnerName は囲む単位 (無ければ囲む property) の表示名 (クロージャのとき。入れ子なら `f.{closure}`)。表示だけに使う:
	// 鍵と Name に入れると、既にある除外の釘 (key@hash) が外れ、gate の出どころの当て方 (同じ名前を先に当てる) も変わる。
	OwnerName  string
	Enclosing  string
	Test       bool // test のファイルにある
	Visibility string
	Params     []string // 局所の名前として扱う引数の名前 (宣言の順。番号はこの順に先に振る)
	ReturnType string   // 戻り値の型 (ハッシュに入れる)
	ParamTypes []string // 引数の型 (宣言の順。索引で引く)
	Labels     []string // 呼び出しで書く引数ラベル (宣言の順。ラベルの無い引数は `_`)
	Doc        string   // doc comment (`///` か `/** */`) の最初の行 (索引で引く)
	// DocStart / DocEnd は doc comment の行の範囲 (無ければ 0)。evidence が doc の後半に書いた語から単位へ辿る。ハッシュには入れない
	DocStart, DocEnd int
	Body             *Node
	// Delegation は本体が「1 つの呼び出しに定数や名前を渡すだけ」の委譲か (本体が 1 文の呼び出しで、引数が全部リテラル・名前・メンバーの参照など)。
	// 除外のプリセット l2-single-call が読む。コアは言語の節の種類を知らないので adapter が決める。
	Delegation bool
	// SingleCall は本体が「共通の道具を 1 回呼ぶだけ」の薄い委譲か (本体が 1 文の呼び出しで、呼ぶ先が名前・メンバーの参照、引数が 2 つまでで、どれも原子か文が 1 つのクロージャ)。
	// 共通の道具へ寄せた後に残る薄い委譲の組に印を付けるのに使う (隠さない。寄せられる 1 行の写しも同じ形になるため)。
	SingleCall bool
	Unreadable bool // 読めない構文 (ERROR / MISSING) を含む
	// Allow は単位の直前のコメントに書いた `dupscan:allow` の後ろ (種別と理由)。HasAllow はその印があること (後ろが空でも)。
	Allow     string
	HasAllow  bool
	AllowLine int // 印を書いたコメントの行
	// AllowExtraLine は同じ単位の 2 つ目の印の行 (無ければ 0)。読むのは 1 つ目だけなので、CommentRules が誤りにする
	// (段の違う 2 つの組を 1 つの単位の 2 行の allow-group で消そうとすると、2 行目が黙って効かなかった)。
	AllowExtraLine int
}

// MatchName は名前を照合するための形にする。Swift の識別子は、予約語と同じ綴りのときバッククォートで囲んで宣言できる
// (`func `repeat`()`) が、使う側は囲まずに書く (`self.repeat(1)`)。宣言名・呼び出し名・参照名・query が同じ対象を指せるよう、
// 全体を囲む 1 組のバッククォートだけを外す。
//
// 🚨 照合にだけ使う。表示用の署名・単位の鍵 (除外の釘に入る)・重複検出の hash が使う名前は変えない。
// 外すのは「先頭と末尾が 1 つずつのバッククォートで、間に別のバッククォートが無い」ものだけ。
// Go の名前や演算子、途中に `を含む文字列は、そのまま返す。
func MatchName(s string) string {
	if len(s) >= 3 && s[0] == '`' && s[len(s)-1] == '`' && !strings.Contains(s[1:len(s)-1], "`") {
		return s[1 : len(s)-1]
	}
	return s
}
