module github.com/jiikko/dotfiles/src/chromecookie

go 1.25.0

require modernc.org/sqlite v1.28.0

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/jiikko/dotfiles/src/testwait v0.0.0-20261101103228-d17e1488b625
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/mattn/go-isatty v0.0.16 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/mod v0.3.0 // indirect
	golang.org/x/sys v0.9.0 // indirect
	golang.org/x/tools v0.0.0-20201124115921-2c860bdd6e78 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	lukechampine.com/uint128 v1.2.0 // indirect
	modernc.org/cc/v3 v3.40.0 // indirect
	modernc.org/ccgo/v3 v3.16.13 // indirect
	modernc.org/libc v1.29.0 // indirect
	modernc.org/mathutil v1.6.0 // indirect
	modernc.org/memory v1.7.2 // indirect
	modernc.org/opt v0.1.3 // indirect
	modernc.org/strutil v1.1.3 // indirect
	modernc.org/token v1.0.1 // indirect
)

// chromecookie は dotfiles の外の repo が go get で取り込み、取り込む側はこの replace を見ない。testwait の require を
// 他の module と同じ v0.0.0 にすると、取り込む側の go mod tidy が実在しない版を取りに行って落ちる。取得できる
// pseudo-version (testwait を変えた commit 以降なら何でもよい) にしておく。テストの実行は replace の ../testwait を使う。
replace github.com/jiikko/dotfiles/src/testwait => ../testwait
