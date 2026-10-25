---
name: dupscan
version: 1.0.0
description: Swift の repo で、新しいメソッド (ヘルパー・初期化・値を組み立てる関数) を書く前に同じものが既にないかを dupscan index で引き、実装が終わったセルフレビューで dupscan gate を回して増えた写しを寄せる。関連の実装 (定義・ファイルの中身・呼び出し元) を grep より短く探すときにも使う。「dupscan」「重複したメソッド」「既にあるヘルパーを探して」「セルフレビュー」「呼び出し元」「似た関数」で発火。Swift 以外の repo では使わない (対応する言語は Swift だけ)。
---

# dupscan (書く前に引く・セルフレビューで写しを寄せる)

`dupscan` (実体は dotfiles の `bin/dupscan`。PATH 上にあるので、どの repo でもそのまま呼べる) は Swift のメソッドを構文木で読む。調べる repo には何も書かない。
詳しい使い方と限界は `~/dotfiles/src/dupscan/README.md`、検出の方法は `~/dotfiles/docs/dupscan-detection.md`。

## 1. 書く前に引く (気軽に)

写しが生まれやすいもの (ヘルパー・初期化・値を組み立てる関数・テストの fixture) を書く前に、同じものが既にないかを 1 回引く。
全部の定義の前に引く必要はない。上位 20 件を 1 行ずつ返す (DualNoteApp の 1130 単位で 0.5 秒。初回だけ build に時間がかかる)。

```sh
dupscan index -returns StorageLocation        # 作ろうとしている値の型 (戻り値の型の一部)
dupscan index -name location                  # 名前の候補の一部
dupscan index -param-type ConnectionID        # 受け取る型
dupscan index -doc bucket                     # doc comment の語
```

使えるものがあれば使う (repo が `.dupscan.json` の `shared` に書いた共通の置き場所の定義は `[shared]` と出て先に並ぶ。
他のファイルから使えないものは `[private]` か `[fileprivate]` と出る。使うなら共通の置き場所へ寄せる)。無ければ書く。

## 2. 調べもの (grep の前に)

定義だけを 1 件 1 行で返すので、grep より返る量が少ない (呼び出し側の行・コメント・文字列が混ざらない)。
文字列・コメント・設定ファイルを探すなら grep のほうが向く。

```sh
dupscan index -file Sources/Foo.swift         # このファイルに何があるか (outline)
dupscan callers 'make(x:_:)'                  # 呼び出しの箇所と、それを囲む関数 (名前だけでも引ける)
```

型を解決しないので、同名の別のメソッドの呼び出しも当たる (近似)。flag は名前・パスの前に書く。

## 3. セルフレビューで gate を回す

実装が終わったら、commit の前に回す。repo が `make test` や CI に `gate` を組み込んでいれば同じ判定がそこでも走るので、先に回せば手戻りが減る (組み込んでいない repo では、ここで回すのが唯一の確かめ)。

```sh
dupscan gate                    # upstream との merge-base から作業ツリーまでに増えた写し。無ければ 1 行で rc=0
```

- **rc=1 (違反)**: 「新しい」に出た単位を、「同じ形」に出た既存の単位へ寄せる (呼び出しに置き換える・共通の置き場所へ移す)。寄せてからもう一度回す
- **誤検知・意図して分けている写し**: 寄せずに除外を書く。単位の直前に `// dupscan:allow <false-positive|intentional> <理由>`、
  組なら違反の `-v` に出る「除外の 1 行」に `kind` と `reason` を埋めて repo の root の `.dupscan.json` に貼る。
  **除外を書いたら、ユーザーへの報告に書く** (除外は gate を緩めるので、人の目に届けるのが唯一の歯止め。`gate` の 1 行目にも「新しく足された除外」と出る)
- **rc=2 (判定不能)**: base が決まらない (upstream が無い) なら `-base origin/master` のように渡す
- 1 つの関数について「同じ形のものがあるか・なぜ同じと判定したか」は `dupscan near <file>:<関数名>` / `dupscan explain <file>:<関数名>`
