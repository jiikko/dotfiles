package visibility

// check が使う os.path・pathlib の写し。本家の check は markdown_visibility の absolute_path のほかに、同じ規則の os.path.realpath・os.getcwd・pathlib を
// 自分で呼ぶので、規則を 2 つ持たないよう、ここの実装を公開する (visibility の中の規則は変えない)。

// Getcwd は os.getcwd()。読めなければ ok が偽。
func Getcwd() (string, bool) { return getcwd() }

// Realpath は os.path.realpath(path) (strict でない)。作業ディレクトリを読めないときは ok が偽。
func Realpath(path string) (string, bool) { return realpath(path) }

// Normpath は os.path.normpath。
func Normpath(path string) string { return normpath(path) }

// PathString は str(Path(s)) (空の部品と . を落とし、何もなければ ".")。
func PathString(s string) string { return parsePure(s).String() }

// PathJoin は str(Path(a) / b)。b が絶対なら b だけ。
func PathJoin(a, b string) string { return pureJoin(a, b) }

// PathName は Path(s).name。
func PathName(s string) string { return pureName(s) }

// PathParent は str(Path(s).parent)。
func PathParent(s string) string { return pureParent(s) }

// PathExists は Path(path).exists() (読めない名前は存在しない扱い)。
func PathExists(path string) bool { return pathExists(path) }

// ExpandUser は os.path.expanduser("~")。取れなければ ok が偽。
func ExpandUser(lookup func(string) (string, bool)) (string, bool) { return expandUser(lookup) }
