// Package tree_sitter_swift は、文法の module (alex-pinkus/tree-sitter-swift) の binding_test.go が誤った import path で参照する package の空の代わり。
// dupscan はこの package を使わない。go mod tidy が依存先のテストの import まで解くので、無いと tidy が止まる。
package tree_sitter_swift
