# mac-lan-manager

手元の Mac 同士を、ネットに繋がっていない有線の LAN (内部の網) で繋いで管理する道具。Bonjour で見えている Mac を一覧にし、
初期設定 (鍵を置く・内部の I/F を固定 IP にする・台帳に登録する) を済ませた Mac を「管理対象」として ssh / 画面共有 / smb で繋ぐ。
glogx の `L` の板からも開く (要件・決定・設計の正本は issue 747)。

**今の状態**: 段 3 と段 5 まで (一覧・詳細カード・台帳への登録と削除・別名・ssh / 画面共有 / コピー・設定・glogx の `L`)。初期設定の実行は段 4。
起動は `bin/mac-lan-manager`。キーの一覧は中で `?`。

**デバッグ (画面を出さない)**: `GO_AUTOBUILD_SYNC=1 bin/mac-lan-manager dump` (`--json` で JSON、`--wait 8s` で Bonjour を待つ上限)。
この Mac のサービス・一覧 (画面と同じ判定。各行の内部 IP・メイン IP・その他の IP と繋ぐ先)・Bonjour の生の結果・知らせを出す。
読むだけで何も書かない。「見つからない」「違う IP に繋ぐ」を調べるときはまずこれを見る (LLM に渡すのも安い)。
`GO_AUTOBUILD_SYNC=1` を付けないと、bin は古いバイナリで起動して裏で作り直す (直した直後は古い版の結果になる)。

## package

| package | 何か |
|---|---|
| `netinfo` | この Mac のネットワークのサービスと I/F の IP を `networksetup` / `ifconfig` の出力から読む (読むだけ) |
| `discover` | `dns-sd` の `-B` / `-L` / `-G` の出力を 1 行ずつ読む |
| `ledger` | 管理対象の台帳 (`~/.config/glogx/mac-lan-manager/hosts.toml`)。この Mac の中だけに持つ。読み書きは `Store` の口だけを通す (共有を後から足す余地) |
| `prefs` | 設定 (一覧に出す IP・内部 IP の決め方・その他の IP の種類。`settings.toml`) |
| `lanmgr` | 画面 (一覧・詳細カード・設定・確認の板)。lipgloss v2.0.5 + tuikit。単体の `main.go` と glogx の板の両方から使う口 (`New` / `HandleKey` / `HandleInput` / `Advance` / `View` / `Busy` / `Changed` / `Reload` / `OwnsKeys` / `Binds` / `TakeExec` / `TakeNotices` / `Close` …) |
| `setup` | 初期設定 (段 4) の手順を組む: 観測から残りの手順を組み直す `Plan` / 相手の有線を固定 IP にする命令列 `StaticIP` (確認の印が来なければ元へ戻すデッドマンスイッチ) / 空き番号の提案 `SuggestIP`。命令を組むだけで起こさない。🚨 本物の networksetup / sudo を起こすのは、段 4 の前の人手の確認が済んでから。テストは偽物だけが見える PATH (`<sandbox>:/bin`) で走らせ、外へ解決したら走らせる前に落とす |
| `live` | `lanmgr` を本物 (ネットワーク・Bonjour・台帳・設定・クリップボード) につなぐ `Options()`。単体の `main.go` と glogx の板が共有する (glogx はクリップボードと時計だけ自分の口に替える) |

## 触る前に読むもの

- 出力を読む処理の正本は各 package の `testdata/` (2026-10-15 にこの Mac で本物のコマンドを起こして取った出力。MAC アドレスなどは伏せた)。
  形を推測で書き換えない。`networksetup -getinfo` の `IPv6 Router:` の行や、`dns-sd -L` の `\032` (空白) のような罠はテストが押さえている
- 🚨 台帳のテストは本物のファイルを書く。`ledger/main_test.go` が置き場所 (`MAC_LAN_MANAGER_CONFIG_DIR`) を一時ディレクトリへ向け、その外への
  書き込みを書く前に拒否する (`writeGuard`)。テストを足すときは `sandbox` の中に書く (`t.TempDir()` は関門の外に作られる)
- 台帳は版 (`version`) を持ち、この版より新しい形式の台帳には書き込まない (古い版で上書きして中身を失わないため)。壊れた台帳も空として上書きしない
- 台帳の書き換えは隣の `hosts.toml.lock` に flock を取ってから読み直して書く (単体のコマンドと glogx の板が同時に書いても登録を消さない)

- 🚨 lipgloss は **v2.0.5 に固定**する。v2.0.6 は x/ansi v0.11.8 と ultraviolet の新しい版を要求し、tuikit を使う全 module の版上げと描画のエンジンの変更を連れてくる (issue 747 の段取りの 2)
- 🚨 色は 24bit で書く。tmux の中では bubbletea の判定が tmux の端末の定義 (RGB が missing) しか見ずに 256 色へ落とし、面の色が紺になる。
  `main.go` (と glogx の `RunBrowse`) が `tuikit/termcolor` でクライアントの機能 (`#{client_termfeatures}` の RGB) を聞いて 24bit にする
- 外から来た名前 (Bonjour のインスタンス名・ホスト名・台帳の別名・ユーザー名) は `lanmgr` の `rows()` の入口で 1 回だけ termsafe を通す (描画の側では無害化しない)
- `dns-sd` は終わらない。`discover.Scanner` が起こしたプロセスだけを `Close` で止める (名前で探して止めない)

## ビルド・テスト

- `make -C src/mac-lan-manager lint` / `test`
