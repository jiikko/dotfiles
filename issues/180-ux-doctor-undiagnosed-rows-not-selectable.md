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

- [ ] 両行にカーソルが止まり、Enter / y / Y が効く
- [ ] 幅 80 で理由が読める (詳細行に折り返して出す)
- [ ] 変異検証: selectable を外すとテストが red になる
