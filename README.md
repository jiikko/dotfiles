dotfiles
========

# Installing

```
cd ~
git clone git@github.com:jiikko/dotfiles.git || git clone https://github.com/jiikko/dotfiles.git
cd dotfiles
./setup.sh
```

[for Mac](./mac "for Mac")

## Testing

Run the regression test suite (Neovim, tmux, setup.sh, plus existing zsh tests) with:

```
make test
```

You can run individual checks as well:

```
make test-syntax # zsh/zlogin/setup.sh syntax checks + tmux/nvim smoke
make test-nvim   # verifies Neovim config loads and lazy.nvim is reachable
make test-tmux   # ensures _tmux.conf can boot a tmux server (skips if tmux sockets are disallowed)
make test-setup  # exercises setup.sh in a temporary HOME
make test-shellcheck # runs shellcheck on shell-compatible scripts
make test-yaml   # yamllint on workflow/pre-commit config
make test-json   # jq validation for JSON configs
make test-lint   # aggregate lint target (shellcheck + YAML + JSON)
make test-runtime # aggregate runtime target (syntax + zshrc + nvim + tmux + setup)
tests/zshrc/test_zshrc.sh  # existing zsh tests (also run via make test)
```
