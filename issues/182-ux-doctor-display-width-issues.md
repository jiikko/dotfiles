# 182 ux: 狭い幅で意味が消える表示が 4 系統ある (blocked 理由 / 長いラベル / バイト幅 pad / 再利用注記)

起票日: 2026-09-02
重要度: **P3**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 6) / [`_claude/rules/no-mixed-width-columns-in-terminal-ui.md`](../_claude/rules/no-mixed-width-columns-in-terminal-ui.md) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (「レイアウトの決定」)

## 対象

`src/glogx/doctor_view.go` の `diskSection` (マーク列 / 助言行 / Enter の詳細順)、`src/doctor/disk/report.go` の `Format` (`%-48s`)、
`src/doctor/disk/catalog.go` の長いラベル

## 何が起きるか

いずれも実証済み (体 6 が幅 60 / 80 / 120 でサンプル出力を目視)。

### (a) blocked の理由をマーク列に置くので、狭い幅で理由が消える

- 幅 80: `🚨 Google Chrome Canary …` (「起動中のため対象外」が落ちる)
- 幅 60: 全マークが `✅ 安…` `🚨 注…` になる
- `NO_COLOR` では caution も blocked も同じ 🚨 なので、記号だけでは区別できない

対応: マーク列は固定語彙にする (例 `🚫 対象外`)、理由は下の dim 行へ移す。CLI の `Format` も同型。

### (b) ラベルが 40 桁を超えるエントリが 1 件ある

「アンインストール済み formula の状態 (/opt/homebrew/var)」= 55 桁 → UI で `(/o…` になる (実機サンプルでも同じ)。

対応: 括弧内の path を Enter の詳細へ移す。

### (c) `disk.Format` の `%-48s` はバイト幅で詰めるので、日本語ラベルで列が揃わない

CLI 出力のマーク列が行ごとにずれる。対応: 表示幅で pad するか、揃えを諦めて改行する。

### (d) 「N 分前の計測を再利用」が助言行の末尾なので幅 80 で切れる

値が stale であることが分からなくなる (issue 172 の「注記で分かる形にしてある」という前提が幅で崩れる)。

対応: 再利用の注記は行頭側 (または専用の記号) に置く。

### (e) Enter の詳細の挿入位置

詳細 (削除経路 / 補足 / 内訳) が親行の直下に入るので、助言行「再取得されます。最終更新…」と Failures 行が
その**後ろ**に来る。failed の行にも「削除経路 / 補足」が出る (CLI は理由だけ)。

対応: 詳細は助言行の後に置くか、failed の行では削除経路を出さない。

## 受け入れ条件

- [ ] 幅 60 / 80 / 120 のサンプル出力を端末に出して目で確認する (幅計算のテストだけでは判定しない)
- [ ] `NO_COLOR` で blocked と caution が区別できる
