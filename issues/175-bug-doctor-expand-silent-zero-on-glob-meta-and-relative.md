# 175 bug: `expand` が glob メタ文字入り HOME と相対パスで無音の 0 件になる (false green)

起票日: 2026-09-02
重要度: **P2**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 5) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (2 章「パス安全性」/「検出の健全性」)

## 対象

`src/doctor/disk/paths.go` の `expand` (と、その戻り値を「候補 0 件」と解釈する `scanEntry`)

## 何が起きるか

`expand` は展開結果が空のときだけを弾く設計になっていない。展開後の文字列が glob として**解釈されてしまう**ため、
パスに glob メタ文字が含まれると 0 件になり、`validateTarget` にも届かず、**「候補はありません」に化ける**。
「診断できず」ではなく「安全です」と出るので、issue 148 2 章の「検出の健全性 (sinking silently の禁止)」に反する。

実測 (体 5 の probe。偽 HOME で `~/.npm/_cacache` を実在させた。実証済み):

| HOME | 結果 |
|---|---|
| `…/h[1]` (glob メタ文字 `[` `]`) | **ok / items=0** (false green) |
| `…/h 1` (空白) | ok / items=1 (正常) |
| `…/h*` | ok / items=1 (正常) |
| 末尾スラッシュ | ok / items=1 (正常) |

同型で TMPDIR にも出る:

| TMPDIR | 結果 |
|---|---|
| 実 `/var/folders/...` | 正常 |
| `~/tmp` (symlink 経由) | failed「経路の途中に symlink がある」(fail-closed。良い) |
| `tmp` (相対パス) | **ok / items=0** (相対 glob が 0 件。`validateTarget` に届かない) |

`[` を含む HOME は珍しいが、**相対 TMPDIR は環境変数の設定ミスで普通に起こる**。

## 再現手順

```
d=$(mktemp -d); mkdir -p "$d/h[1]/.npm/_cacache" && : > "$d/h[1]/.npm/_cacache/x"
env -i HOME="$d/h[1]" PATH=/usr/bin:/bin TMPDIR="$d" <diskdoctor> -json
```

npm キャッシュのエントリが `status=ok, items=0` になる (実在するのに)。

## 対応案

- 展開前のパスに glob メタ文字が含まれるかを見て、**含まれるなら `filepath.Glob` に渡さずリテラルとして扱う**
  (`Glob` は quote できないので、メタ文字を含む prefix は `os.Stat` で確認する経路に回す)
- **相対パスは fail-closed** にする (絶対パスでないパスは「診断できず」。`validateTarget` の手前で弾く)
- glob が 0 件でも、展開元のディレクトリが存在しない場合と、存在するのに 0 件の場合を区別して報告する

## 受け入れ条件

- [ ] メタ文字入り HOME で実在するキャッシュが検出される (probe テスト)
- [ ] 相対 TMPDIR が「診断できず」に倒れる
- [ ] 変異検証: fail-closed を外すと items=0 の false green が再現することを確認する
