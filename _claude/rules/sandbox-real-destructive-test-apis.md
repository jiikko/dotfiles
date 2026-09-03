# 本物の破壊的 API を呼ぶテストは、サンドボックス外を「実行前に拒否」する仕掛けを同じ commit で入れる

> **トリガー型ルール。** テストから **本物の削除・上書き・移動 API**
> (`os.RemoveAll` / `rm -rf` / `renameatx_np` / `SecItemDelete` / `FileManager.removeItem` 等) を
> 呼ぶコードを書こうとした瞬間に発動する。「fixture は一時ディレクトリだから安全」と
> 思った時点で発動している。

## ルール

- **fixture の正しさに依存しない**。「テストが渡すパスは一時ディレクトリだから安全」は
  *そのテストが正しく書かれている限り*成立する条件で、**次にテストを足す人**には効かない
- **サンドボックス外への破壊的操作を、実行前に拒否する仕掛けを同じ commit で入れる**。
  実装の commit と隔離の commit を分けない (分けると「動いたから完了」で閉じる)
- 拒否は **API の手前**に置く (呼び出しを受け取った時点で、対象がサンドボックス配下かを
  検査して**実行せずに失敗**させる)。事後の検証では、消えた後にしか気づけない
- **段数を宣言したら段ごとに変異を当てる**
  ([`adversarial-review-own-safeguards.md`](adversarial-review-own-safeguards.md) §1.5)。
  「3 段構え」と書いて守られていたのが 1 段だけ、が実際に起きている

## 検査と破壊的操作は隣接させる (TOCTOU の窓は「距離」そのもの)

- **「全部計画してから全部実行」の素朴な構造を、破壊的操作に使わない。**
  読みやすいが、**検査から実行までの距離が窓そのもの**になる
- 実例は rationale (dotfiles 148, 2026-09-03: 全ターゲットを plan → 1 件ずつ exec と書いたため、
  あいだに再走査 (最大 60 秒) が何度も挟まり、**最後のエントリは検査から実行まで分単位空いていた**。
  敵対レビューが親ディレクトリの差し替えで実測した)
- 直し方: **エントリ単位で plan → exec → verify** に組み替える。窓は「1 件の処理時間」に縮む
- 窓を 0 にはできないので、**実行の直前に取り直した値** (サイズ・(dev, ino)・mtime) で
  判定する。呼び出し元から渡された申告値を正当性の根拠にしない
  ([`adversarial-review-own-safeguards.md`](adversarial-review-own-safeguards.md) 0-B)

## なぜ

起源: dotfiles 148 段階 ④ (削除エンジン), 2026-09-03。根拠・起源・実例は
`~/dotfiles/_claude/rules-rationale/sandbox-real-destructive-test-apis.md` に置く
(起動時には読まれない。ルールを疑う・改訂するときに読む)。

## やること / やらないこと

- ✓ 破壊的 API を本物で呼ぶテストには、サンドボックス外を**実行前に拒否**する仕掛けを同じ commit で入れる
- ✓ 拒否は API の手前に置く (事後検証にしない)
- ✓ 段数を宣言したら段ごとに変異を当てて red を確認する
- ✓ 破壊的操作は「エントリ単位で plan → exec → verify」に組み、実行直前に取り直した値で判定する
- ✗ 「fixture が一時ディレクトリだから安全」で隔離を省く
- ✗ 実装の commit と隔離ハーネスの commit を分ける
- ✗ 「全部 plan → 全部 exec」の構造を破壊的操作に使う (窓が処理件数に比例して広がる)

## 関連

- [`adversarial-review-own-safeguards.md`](adversarial-review-own-safeguards.md) —
  **姉妹ルール。あちらは「機構そのものの検査」、こちらは「テスト環境の隔離」**が発動点。
  §1.5 (段ごとの変異) と 0-B (既にある答えを近似で作り直さない) は本ルールからも使う
- [`mutation-verify-new-tests.md`](mutation-verify-new-tests.md) — 隔離ハーネスも
  「変異を当てて red を見る」対象 (自己テストが何を証明しているかを確かめる)
- [`tmux-probe-requires-socket-isolation.md`](tmux-probe-requires-socket-isolation.md) —
  同じ「自作の道具が本番を壊す」ファミリー (あちらは socket、こちらはファイルシステム)
