# userdirs

道具ごとの設定の置き場を 1 か所で決める module (issue 777)。

- `ConfigDir(app)` — `$XDG_CONFIG_HOME/<app>`、未設定なら `~/.config/<app>`
- `MoveIfMissing(old, new)` — 置き場を変えたときの 1 回きりの引っ越し (新しい方に無く古い方にあるときだけ rename。上書きしない)

使う側: treefiler (`~/.config/treefiler`)・mac-lan-manager (`~/.config/mac-lan-manager`)・pro-con (`~/.config/pro-con`)。
道具ごとのテスト用の差し替え (`TREEFILER_CONFIG_DIR` / `MAC_LAN_MANAGER_CONFIG_DIR`) は各道具が持ち、差し替えているときは引っ越さない。
