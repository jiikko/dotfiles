module dupscan

go 1.26.0

require (
	github.com/alex-pinkus/tree-sitter-swift v0.0.0-20261004031734-82bb3a533e08
	github.com/tree-sitter/go-tree-sitter v0.25.0
)

require (
	github.com/mattn/go-pointer v0.0.1 // indirect
	github.com/tree-sitter/tree-sitter-swift v0.0.0-00010101000000-000000000000 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
)

require (
	golang.org/x/tools v0.52.0
	subproc v0.0.0
	testwait v0.0.0
)

replace subproc => ../subproc

// 文法の module の bindings/go のテストが github.com/tree-sitter/tree-sitter-swift/bindings/go (存在しない package) を import しており、
// go mod tidy (scripts/go_module_graph.sh の tidy -diff) が止まる。空の代わりに向ける。文法の版を上げてテストの import が直ったら外す。
replace github.com/tree-sitter/tree-sitter-swift => ./third_party/tree-sitter-swift-teststub

replace testwait => ../testwait
