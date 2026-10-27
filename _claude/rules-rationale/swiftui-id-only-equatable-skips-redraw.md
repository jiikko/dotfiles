# swiftui-id-only-equatable-skips-redraw の根拠

## 起源 (vlc-multi-video-player issue 422 / retro 426, 2026-10-27)

swift-standard-app-components (SAC) のトーストに、同じ `uniqueKey` の通知を表示中のものと置き換える機能を足した。置き換えでは吹き出しが
入り直さないように元の id を引き継いだ。`Toast` の `==` は id だけを比べる (アニメーションを「トーストの入れ替わり」の粒度で付けるため)。

- 置き換えた後、manager の `visibleToasts` は新しい中身を持っていたが、画面の吹き出しの高さ (frame) は変わらなかった
- print で観測すると、container の body は新しい中身で評価されていたが、吹き出しの `ToastView` の body は走っていなかった
- `ForEach` に等しさを持たない包みを渡し、`ToastView` が中身を値で持つ形にして直った。どちらか片方を外すと、frame のテストが落ちる
  (SAC commit `feat(Toast): 複数のトーストを縦に並べて同時に出せるようにし、uniqueKey で重複を置き換える`)
- 観測を入れる前に推測の修正を 2 回打った (View を値で持つ / Observation の版の番号)。単独ではどちらも効かず、見当を外していた

## なぜ新しいルールか

発動点 (id だけの等しさを UI の差分の入力に流す瞬間) は、既存のどのルールとも違う。SwiftUI に固有なので、Swift のファイルを読んだときだけ
読み込む (`paths:`)。
