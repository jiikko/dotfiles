# Claude Integration

## Wrapped CLI commands

- `_zshrc` defines `claude()` which renames the current tmux window to `"🤖 claude"`, runs the `claude` CLI, and restores the name afterward.
- Similar wrappers exist for `gemini()` and `codex()`.

## How to use

```zsh
claude chat <args>
```

When invoked inside tmux, the window is renamed so it’s easy to spot the AI session. Outside tmux the command simply forwards to the real binary.

## Troubleshooting

- Ensure the `claude` CLI is installed and on `PATH`.
- If the tmux window name doesn’t revert, check for errors inside the wrapper or long-running processes that bypass the cleanup.
