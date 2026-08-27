# PATH 先頭に置く shim は、実体を絶対パスで解決してから exec する

> **トリガー型ルール。** 計測・テスト・トレースのために既存コマンドと同名の wrapper を
> 作り、それを PATH の先頭 (あるいは `PATH="$shimdir:$PATH"`) に差し込もうとした瞬間に発動する。

## ルール

- **shim の中から実体を呼ぶときは、絶対パスで解決してから exec する**。`command -v` /
  `type -P` で解決した結果を使い、**相対名 (`tmux` のような素の名前や、既定値が素の名前の
  環境変数) で呼ばない**。PATH 先頭には自分がいるので、相対名は shim 自身に解決し直す
- **解決した実体が shim 自身 (= shim のディレクトリ配下) でないことを起動時に確認する**。
  一致したら即座に失敗させる。無限再帰は「壊れる」のではなく「静かに回り続ける」ため、
  自己参照の検出は後段のどのテストでも代替できない
- 実体の解決は **shim をインストールする側 (PATH を書き換える前)** で済ませ、絶対パスを
  環境変数で渡すのが最も安全 (shim 自身が解決すると、既に汚染された PATH の中で解決する)
- **長時間走るバックグラウンド計測の出力をファイルへリダイレクトするなら、行バッファに
  する** (`stdbuf -oL` / 進捗は stderr へ)。ブロックバッファのままだと暴走が無音になり、
  気づくのが遅れる

## なぜ

起源: dotfiles bench, 2026-08-21。根拠・起源・実例は `~/dotfiles/_claude/rules-rationale/path-shim-must-resolve-real-binary.md` に置く（起動時には読まれない。ルールを疑う・改訂するときに読む）。

## 形

```sh
# インストール側: PATH を汚す前に実体を確定させる
real_tmux="$(command -v tmux)" || { echo "tmux not found" >&2; exit 1; }
mkdir -p "$shimdir"
cat > "$shimdir/tmux" <<SHIM
#!/bin/sh
exec "$real_tmux" "\$@"     # 絶対パス。相対名にすると自分に戻る
SHIM
chmod +x "$shimdir/tmux"
PATH="$shimdir:$PATH"

# shim 側で解決する形を採るなら、自己参照を検出して落とす
case "$resolved" in
  "$shimdir"/*) echo "shim resolved to itself: $resolved" >&2; exit 70 ;;
esac
```

## やること / やらないこと

- ✓ 実体は `command -v` の結果 (絶対パス) で exec する
- ✓ 解決結果が shim 自身の配下なら即座に失敗させる
- ✓ 実体の解決は PATH を書き換える前に済ませ、絶対パスを渡す
- ✓ バックグラウンド計測は行バッファにして、暴走が音を出す状態にする
- ✗ shim の中で相対名 (`tmux`) や「既定値が相対名の変数」を exec する
- ✗ 同じ変更の中に shim / wrapper が複数あるとき、片方だけ絶対パス化して満足する

## 関連

- [`tmux-probe-requires-socket-isolation.md`](tmux-probe-requires-socket-isolation.md) —
  「自作の計測装置が本番を壊す」同族。あちらのトリガは socket 隔離、こちらは PATH 解決
- [`adversarial-review-own-safeguards.md`](adversarial-review-own-safeguards.md) —
  自作の計測・検査を自己レビューで閉じない (暴走・無音は正常系の試行では出ない)
