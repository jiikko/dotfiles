#!/bin/sh
# @DotfilesSyncer:name Karabiner 設定を復元
# @DotfilesSyncer:description dotfiles の Karabiner 設定を復元してキーボード種別を自動設定

cp ~/dotfiles/mac/karabiner.json ~/.config/karabiner/karabiner.json

# キーボードタイプを自動判定してパッチする
kb_lang=$(ioreg -r -k KeyboardLanguage 2>/dev/null | grep '"KeyboardLanguage"' | head -1)

if echo "$kb_lang" | grep -qi "Japanese"; then
    kb_type="jis"
    country_code=45
elif echo "$kb_lang" | grep -qi "ISO"; then
    kb_type="iso"
    country_code=13
else
    # ANSI (デフォルト) — パッチ不要
    echo 'dotfilesにあるkarabinerの設定ファイルをkarabinerにコピーしました。(ANSI)'
    exit 0
fi

target=~/.config/karabiner/karabiner.json

jq --arg type "$kb_type" --argjson code "$country_code" \
    '.profiles[].virtual_hid_keyboard.keyboard_type = $type
     | .profiles[].virtual_hid_keyboard.keyboard_type_v2 = $type
     | .profiles[].virtual_hid_keyboard.country_code = $code' \
    "$target" > "${target}.tmp" && mv "${target}.tmp" "$target"

echo "dotfilesにあるkarabinerの設定ファイルをkarabinerにコピーしました。(${kb_type}に自動設定)"
