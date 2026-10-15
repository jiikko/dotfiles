# mac-lan-manager

手元の Mac 同士を、ネットに繋がっていない有線の LAN (内部の網) で繋いで管理する道具。Bonjour で見えている Mac を一覧にし、
初期設定 (鍵を置く・内部の I/F を固定 IP にする・台帳に登録する) を済ませた Mac を「管理対象」として ssh / 画面共有 / smb で繋ぐ。
glogx の `L` の板からも開く予定 (要件・決定・設計の正本は issue 747)。

**今の状態**: 段 1 (読み取りと台帳) だけ。画面と `bin/mac-lan-manager` は段 2 で足す。

## package

| package | 何か |
|---|---|
| `netinfo` | この Mac のネットワークのサービスと I/F の IP を `networksetup` / `ifconfig` の出力から読む (読むだけ) |
| `discover` | `dns-sd` の `-B` / `-L` / `-G` の出力を 1 行ずつ読む |
| `ledger` | 管理対象の台帳 (`~/.config/glogx/mac-lan-manager/hosts.toml`)。この Mac の中だけに持つ。読み書きは `Store` の口だけを通す (共有を後から足す余地) |

## 触る前に読むもの

- 出力を読む処理の正本は各 package の `testdata/` (2026-10-15 にこの Mac で本物のコマンドを起こして取った出力。MAC アドレスなどは伏せた)。
  形を推測で書き換えない。`networksetup -getinfo` の `IPv6 Router:` の行や、`dns-sd -L` の `\032` (空白) のような罠はテストが押さえている
- 🚨 台帳のテストは本物のファイルを書く。`ledger/main_test.go` が置き場所 (`MAC_LAN_MANAGER_CONFIG_DIR`) を一時ディレクトリへ向け、その外への
  書き込みを書く前に拒否する (`writeGuard`)。テストを足すときは `sandbox` の中に書く (`t.TempDir()` は関門の外に作られる)
- 台帳は版 (`version`) を持ち、この版より新しい形式の台帳には書き込まない (古い版で上書きして中身を失わないため)。壊れた台帳も空として上書きしない
- 台帳の書き換えは隣の `hosts.toml.lock` に flock を取ってから読み直して書く (単体のコマンドと glogx の板が同時に書いても登録を消さない)

## ビルド・テスト

- `make -C src/mac-lan-manager lint` / `test`
