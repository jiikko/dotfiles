# 180 ux: 「一部走査できず」「❔ 診断できず」の行が選べず末尾も切れるので、確かめる手段が無い

起票日: 2026-09-02
重要度: **P2**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 6) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (2 章「検出の健全性」/ 3 章「表示」)

## 対象

`src/glogx/doctor_view.go` の `diskSection` の Failures 行 / `svcSection` の Undiagnosed 行

## 何が起きるか

「診断できず」は最も追加調査が必要な行なのに、**選べない (非 selectable)** ので Enter も `y` も `Y` も効かない。
そのうえ幅 80 で末尾が切れる (実証済み)。

| 行 | 幅 80 での見え方 |
|---|---|
| disk の Failures | パスが `/Users/koji/Library/Deve…` で切れる |
| svc の Undiagnosed | **理由が丸ごと消える** |

親行の `Y` には Failures が入るが、**svc の Undiagnosed はどこからも取り出せない** (入口ゼロ)。
「診断できなかったので人が確かめてほしい」と言っておいて、確かめるための情報 (完全なパス・理由) を渡していない。

## 再現手順

走査を一部失敗させた状態 (読めない権限のディレクトリを対象に含める / brew を PATH から外す) で doctor を開き、
幅 80 の端末で該当行を見る。カーソルを合わせようとしても止まらない。

## 対応案

- Failures 行と Undiagnosed 行を **selectable にする**
- Enter の詳細に完全なパスと理由を出す
- `y` でパス、`Y` で理由 + 裏取りコマンド (issue 183) をコピーできるようにする

## 受け入れ条件

- [x] 両行にカーソルが止まり、Enter / y / Y が効く
- [ ] 幅 80 で理由が読める (詳細行に折り返して出す) — **保留**。折り返しは幅の問題なので
      [issues/182](182-ux-doctor-display-width-issues.md) に寄せる。今回は「Enter の詳細に理由を独立した行で出す」
      までで、行が幅を超えれば依然切れる (完全な文字列は y / Y で取れる)
- [x] 変異検証: selectable を外すとテストが red になる

## 対応 (2026-09-02、軽い方だけ。型変更は保留)

**入口ゼロだった svc の「診断できず」に y / Y / Enter を付け、disk の「一部走査できず」も選べるようにした。**

- `svcSection` の Undiagnosed 行: `selectable` + `y` = plist の完全なパス / `Y` = `svcUndiagnosedCopyText`
  (パス・理由・裏取りコマンド `plutil -p` と `ls -l`) / Enter の詳細に理由と裏取りコマンド
- `diskSection` の Failures 行: `selectable` + `y` = 走査できなかった理由の文字列 / `Y` = 親エントリの解説
  (`diskCopyText`。Failures を含む) / Enter の詳細に理由

### 保留にしたもの (ユーザー判断 2026-09-02)

**`y` が「パス単体」を渡すのは disk 側だけ実現できていない。** `disk.Result.Failures` が `[]string` で、
パスがエラー文に埋め込まれているため (`"走査できず: " + err.Error()`)。パス単体を渡すには
`Failures` を構造体のスライスにする必要があり、波及先は disk の JSON スキーマ (snapshot の互換) /
CLI の `Format` / `report.go` / 各テスト。**今回は y に理由の文字列 (パスを含む) を渡す形で妥協した**
(コピーして確かめる用には足りる)。コードにも同じ理由をコメントで残した。

**再開の trigger**: `Failures` の内容をパスで機械処理したくなったとき (④ の削除で
「走査できなかったパスを除外する」等)。そのときに構造化する。

### 検証

`TestDoctorUndiagnosedRowsAreSelectable` を追加。行は**押した回数ではなく行の種類で探す**
(選べる行が増えたときに前提が崩れないようにする)。

変異検証 (使い捨て worktree、5 本すべて red):

| 変異 | 結果 |
|---|---|
| svc の Undiagnosed 行を非 selectable に戻す | red |
| disk の Failures 行を非 selectable に戻す | red |
| Undiagnosed の `Y` から裏取りコマンドを落とす | red |
| Undiagnosed の `y` を plist でなく理由にする | red |
| Undiagnosed の detail から理由の行を落とす | red |
