# glogx: 依存ライブラリ / Go toolchain の更新余地 (2026-08-06 調査)

## 調査結果 (`go list -m -u` 実測 + module proxy / GitHub Releases API 照会)

直接依存は **fsnotify を除きすべて最新**:

| ライブラリ | 現行 | 最新 | 対応 |
|---|---|---|---|
| fsnotify/fsnotify | v1.9.0 | **v1.10.1** | 更新する (破壊的変更なし、Go 1.23+ 要求は満たす) |
| alecthomas/chroma/v2 | v2.27.0 | 同 | 不要 |
| charmbracelet/x/ansi | v0.11.7 | 同 | 不要 |
| rivo/uniseg | v0.4.7 | 同 | 不要 |
| golang.org/x/term | v0.45.0 | 同 | 不要 |
| charm.land/bubbletea/v2 | v2.0.8 | 同 | 不要 |

間接依存: `charmbracelet/ultraviolet` に新しい pseudo-version あり (タグなしパッケージのため `go get -u` で追随)。`xo/terminfo` は 2022 年から タグなしだが terminfo データは安定領域で実害薄。

## Go toolchain

- go.mod: `go 1.25.0` / ローカル実行: go1.25.4
- **Go 1.26 リリース済み** (2026-02, Green Tea GC 既定化)。最新 1.26.5 / 1.25 系最新は 1.25.12
- 最低でもパッチ追随は低リスク。1.26 への引き上げは bubbletea 系の互換を CI で確認してから

## GitHub Actions

- `actions/checkout@v7` / `actions/setup-go@v7` — 最新は v7.0.1 で、メジャータグ指定のため**自動追随済み。対応不要**
- checkout v7.0.0 で pwn request 対策のセキュリティ強化が入っている (取得済み)

## 対応手順 (着手時)

```sh
cd src/glogx
go get github.com/fsnotify/fsnotify@v1.10.1
go get -u github.com/charmbracelet/ultraviolet
go mod tidy && make lint test
```

## 注意

- codex レビューはユーザー方針 (codex 不使用) により省略
