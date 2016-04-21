#!/bin/sh

cli=/Applications/Karabiner.app/Contents/Library/bin/karabiner

ls ~ | grep -q dotfiles
if [ $? = 1 ]; then
  cd ~
  git clone git@github.com:jiikko/dotfiles.git || git clone https://github.com/jiikko/dotfiles.git
fi
cp ~/dotfiles/mac/karabinar_private.xml ~/Library/Application\ Support/Karabiner/private.xml

$cli set repeat.initial_wait 216
/bin/echo -n .
$cli set private.space_to_command 1
/bin/echo -n .
$cli set repeat.wait 16
/bin/echo -n .
$cli set private.comannd_l_to_space 1
/bin/echo -n .
$cli set hf_to_tag 1
/bin/echo -n .
$cli set private.menu_to_control 1
/bin/echo -n .
$cli set private.comannd_r_to_space 1
/bin/echo -n .
$cli set remap.mouse_keys_mode_2 1
/bin/echo -n .
$cli set general.disable_internal_keyboard_if_external_keyboard_exsits 1
/bin/echo -n .
$cli set private.control_to_shift 1
/bin/echo -n .
$cli set option.emacsmode_controlLeftbracket 1
/bin/echo -n .
$cli set remap.jis_command2eisuukana_prefer_command 1
/bin/echo -n .
$cli set remap.commandLcommandR2enter 1
/bin/echo -n .
$cli set private.shift_r_to_command 1
/bin/echo -n .
$cli set option.emacsmode_controlPNBF 1
/bin/echo -n .
$cli set private.app_to_option 1
/bin/echo -n .
$cli set remap.controlJ2enter 1
/bin/echo -n .
$cli set option.emacsmode_controlI 1
/bin/echo -n .
$cli set option.jis_emacsmode_controlLeftbracket 1
/bin/echo -n .
/bin/echo
