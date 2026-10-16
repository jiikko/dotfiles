# 797 (feat): 繋ぐ先の Mac を一覧から選ぶ

起票日: 2026-10-16

> epic 793 の子。要望の 2 つ目。

## 要望 (ユーザーの言)

「接続したホストを一覧で選択して」

## 調べたこと (2026-10-16)

- 画面共有を有効にした Mac は Bonjour で `_rfb._tcp` を出す。issue 747 の mac-lan-manager は、この一覧と台帳を合わせて出し、画面共有は
  `open vnc://<有線の IP>` で標準アプリを開いている (747 の本文)。台帳の置き場所は `src/mac-lan-manager/ledger/ledger.go` の `DefaultPath`
  (`~/.config/mac-lan-manager/hosts.toml`。`$XDG_CONFIG_HOME` があればその下。747 の本文は旧パスのまま)
- `_rfb._tcp` の TXT の中身を書いた資料は見つからなかった。Apple silicon かどうかは `_device-info._tcp` の `model=` で分かる見込みだが、
  DTS は「not documented as API … could change」と書いている ([forums 726295](https://developer.apple.com/forums/thread/726295))
- アプリが Bonjour で探すには、ローカルネットワークのプライバシーの許可と Info.plist の `NSBonjourServices` が要る (796 の TN3179)

## ユーザー決定 (2026-10-16)

「747 の台帳は流用できると嬉しいが、`open xxx` みたいな感じで引き継ぐ感じでも十分 ok」

## 作り方の案

- **主: 747 の一覧から `open` で受け渡す**。このアプリが独自の URL scheme (例: `<scheme>://<user>@<IP>?name=<別名>`。名前は未定) を受け、
  受けたらその Mac へ繋ぐ (常時接続のセッションがあればその画面へ切り替える。798)。747 の側は、今の `open vnc://…` をこの scheme に替える
  (747 の作業。このアプリが入っていなければ標準アプリへ戻す)。台帳のファイルは読まないので、sandbox (App Store 版) とぶつからない
- 従: このアプリ自身の一覧 (一度繋いだ Mac と Bonjour)。747 を通さずに使うとき用。後回し
- 台帳を直に読む形は採らない (sandbox の中では `~/.config` を読めない。temporary-exception の entitlement という道はあるが審査で問われうる)

## 確かめること

- [ ] URL scheme の形 (別名・ユーザー名・IP・HostKeyAlias のどれを渡すか)。747 の側の受け渡しと合わせて決める
- [ ] scheme を受けたときに、既に繋いでいるセッションと同じ Mac かを何で判定するか (IP は付け替わる。747 の台帳の鍵を渡すか)

## 受け入れ条件

- 747 の一覧で Mac を選ぶと、このアプリがその Mac の画面を出す (繋いでいなければ繋ぎ、繋いでいればその画面へ切り替える)
