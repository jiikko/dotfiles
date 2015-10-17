#!/bin/sh

brew install karabiner

cli=/Applications/Karabiner.app/Contents/Library/bin/karabiner
open $cli

cd ~
git clone git@github.com:jiikko/dotfiles.git || git clone https://github.com/jiikko/dotfiles.git
cd dotfiles/mac
sh setup_karabinar.sh
sudo cp karabinar_private.xml ~/Library/Application\ Support/Karabiner/private.xml

$cli set option.jis_emacsmode_controlLeftbracket 1
/bin/echo -n .
$cli set repeat.initial_wait 150
/bin/echo -n .
$cli set hf_to_tag 1
/bin/echo -n .
$cli set option.emacsmode_controlI 1
/bin/echo -n .
$cli set private.comannd_r_to_space 1
/bin/echo -n .
$cli set private.space_to_command 1
/bin/echo -n .
$cli set private.control_to_shift 1
/bin/echo -n .
$cli set option.emacsmode_controlLeftbracket 1
/bin/echo -n .
$cli set private.app_to_option 1
/bin/echo -n .
$cli set repeat.wait 20
/bin/echo -n .
$cli set private.comannd_l_to_space 1
/bin/echo -n .
$cli set private.menu_to_control 1
/bin/echo -n .
$cli set remap.controlJ2enter 1
/bin/echo -n .
$cli set private.shift_r_to_command 1
/bin/echo -n .
$cli set option.emacsmode_controlPNBF 1
/bin/echo -n .
/bin/echo
