# 698 (bug): glogx の issues viewer の不具合 (2026-10-09 の監査。品質とその他の観点)

起票日: 2026-10-09

## 概要

10/07 の監査 (668。設計とテストだけ) で回していなかった品質の観点で見つけたもの。すべて複製の上のテストで再現した (9 は読解)。

## 詳細

1. **走査の警告がヘッダーに 1 本しか出ず、同名の二重化の警告が隠れる** (P2) — `src/glogx/issues_view.go` の listHeadLines が `v.warnings[0]` だけを描く。
   順序は `issues/parse.go` の Scan が決め、「目印の警告 → 最後に conflicts (同名の二重化)」。壊れた next の目印が 1 本でもあると、spec が
   「静かな内容喪失に気づける唯一の手段」とする二重化の警告が出ない。直し方: 重さ順に並べて先頭を出す・「🚨 N 件」を併記する
2. **読めない状態のフォルダの issue が、警告なしで一覧から消える** (P2) — `issues/parse.go` の scanDir / scanEpicDir が `os.ReadDir` の err で
   `return out, nil` / `continue`。`done/` を chmod 000 にすると 1 件になり warnings は空。「空 = 全部 done」と見分けがつかず、前回の良い一覧を置き換える。
   直し方: 読めなかったことを warnings に積む (パスは termsafe を通す)
3. **URL の一覧が文字の向きを変える制御文字を無害化せずに描く** (P2) — `issues/body.go` の urlRe (除外は C0 / DEL / C1 だけ) と
   `src/glogx/url_picker.go` の lines。本文の `https://example.com/a‮b` (U+202E) が生のまま出た。表示は並べ替えられ、開く先は実際の URL。
   他の経路は termsafe が BiDi を落としている。直し方: 描くときに無害化する・urlRe の除外に BiDi と U+2028 / 2029 を足す。開く引数は `https?://` 始まりなので注入は無い
4. **FIFO の `NNN-x.md` で viewer が固まる** (P3) — `issues/discover.go` の isIssueFile が通常のファイルかを見ない。mkfifo した `.md` で LoadMeta が
   戻らない (一覧が loading のまま)。git は FIFO を持てないのでローカル由来だけ。直し方: `e.Type().IsRegular()` を要求する (treefiler の 662 と同じ形)
5. **claim のバナーが front matter の前に入る** (P3) — `issues/banner.go` の addBanner は先頭が `# ` のときだけ H1 の後ろに入れる。
   `---\nstatus: pending\n---\n\n# T` に `n` を押すと、バナーが `---` の前に入り、LoadMeta が front matter と見なくなって status 不一致の 🚨 が消える
   (外すと元に戻る)。直し方: front matter の閉じの後ろへ入れる
6. **バナーが CRLF の本文に LF を混ぜる** (P3) — `issues/banner.go` が "\n" 前提
7. **N キー (次の番号) が日付の名前で狂う** (P3) — `issues/parse.go` の NextNumber。`20260101-notes.md` があると次が 20260102、`9223372036854775807-x.md`
   でオーバーフロー。直し方: 桁数の上限 (6 桁を超えたら番号と見なさない)
8. 本文を読む経路が走査後の symlink への差し替えを見ない (`ReadBody` の `os.ReadFile`。再読込の前の狭い窓。P3・記録)
9. `n` の解除が他のホストの claim でも確認なしで外す / 他人のバナーが付いたまま claim が成り立つ (`removeBanner`・`MoveToSubdir`。ClaimBanner は
   ホスト名を持つが照合しない。意図かは未確認)

## 関連ファイル

- `src/glogx/issues_view.go`・`src/glogx/url_picker.go`・`src/glogx/issues/{parse,body,discover,banner,move}.go`
- 監査の記録: 702。前回の監査: 668

## 進捗

- [ ] 未着手
- [x] 1 `issues.Scan` が同名の二重化の警告を先頭に置き、viewer のヘッダーは先頭の 1 本に「(ほか N 件)」を併記する
- [x] 2 `readDirWarn`: 状態ディレクトリ・epic の中が読めないとき (無い場合を除く) 「読めないフォルダがあります」を警告に積む (パスは termsafe)
- [x] 3 `urlRe` が BiDi の制御文字 (U+202A-202E / U+2066-2069) と U+2028 / 2029 で URL を切る。一覧に出る URL は `Body.URLs` からしか来ないので、
  描くときの無害化は足さなかった (差が出ない)
- [x] 4 `isIssueFile` は通常のファイルだけを通す (FIFO・デバイスも弾く)
- [x] 5・6 `addBanner` は front matter (`---` で閉じるもの) と H1 の後ろに入れ、CRLF の本文には CRLF で足す。CI の
  `tests/issues/test_next_claims_have_banner.sh` が front matter の後ろのバナーを認めることはレビューで実測した
- [x] 7 `NextNumber` は 7 桁以上の番号 (日付の名前・桁あふれ) を無視する
- 8 記録のまま。9 は仕様の判断が要る (他のホストの claim を確認なしで外す / 他人のバナーが付いたまま claim できる。止めるか確認を出すかを決めてから直す)
- 変異 (8 本すべて red): 警告の順序を戻す / 読めない警告を消す / 件数の併記を消す / urlRe の BiDi の範囲を消す / isIssueFile を戻す /
  front matter の判定を消す / CRLF の判定を消す / 桁の上限を消す
- 敵対レビュー (sonnet、1 周): P1 / P2 なし。記録のみの P3: 幅 20 桁ほど以下では件数の接頭辞で警告の本文が切れる・二重化があると読めないフォルダの
  警告は件数にしか出ない (重い順の意図どおり)・issues/ の直下の権限の無い hidden フォルダで警告が鳴り続ける・7 桁以上の番号だけを使う repo は 001 から
  採番し直す (実在しにくい)
- `make test` / `make lint` (src/glogx) rc=0
