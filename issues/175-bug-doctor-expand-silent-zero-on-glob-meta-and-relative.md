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

## 事前検証 (2026-09-03、read-only サブエージェント sonnet)

**主張は成立する。** 修正コミットは無く、`git log -- src/doctor/disk/{paths,scan}.go` の最新は
`cd80c37b` (issue 148 の機能追加) で、glob メタ文字・相対パスへの対処は入っていない。

- 実装: `expand` は `src/doctor/disk/paths.go:28-50`。`~` / `$TMPDIR` 展開後に `filepath.Glob` へ
  そのまま渡す。**関数のコメント自身が「glob の結果 0 件は空スライス (エラーではない)」と明記**
- 無音である経路: `sizePaths` (`scan.go:251-281`) は `Items==0 && Failures==0` のとき素の
  `StatusOK` を返し、人向けの `Format` (`report.go:51`) は**その Result を丸ごと `continue` で
  出力から除外する**。つまり「診断できず」と「本当に候補なし」が構造的に区別できない
- 再現 (一時テストで実測、実行後に削除して `git status` で無変更を確認):
  - HOME にメタ文字 `[1]` を含む → `paths=[] err=<nil>` / `status=ok items=0 failures=[]`
  - 相対 TMPDIR でマッチ無し → 同上 (無音の 0 件)
  - ⚠️ **相対 TMPDIR で cwd にたまたま一致した場合は `validateTarget` (`paths.go:79-81`) が
    非絶対パスを拒否して `status=failed` になる**。つまり相対パスは「一致したら fail-closed /
    一致しなければ無音」という非対称。対応案を書くときはこの差を潰す形にする
- 影響範囲: `expand` の呼び出し元は `scanEntry` (`scan.go:177`) の 1 箇所だけで、そこから
  `disk.Scan` → `cmd/diskdoctor/main.go:38` と `src/glogx/doctor_view.go:162` の 2 経路。
  **カタログの `Paths` を持つ全エントリが通る**ので、`expand` を直せば一括で解消する見込み
- 併せて確認できたこと: symlink 経由の TMPDIR は `validateTarget` の symlink 検査で正しく
  `failed` になる (`paths.go:99-107`)。issue の表の他の行 (空白・`*`・末尾スラッシュ・実 TMPDIR)
  は正常挙動で、**問題はメタ文字 HOME とマッチしない相対パスの 2 パターンに限定される**
