# restartable

`restartable` builds and runs a foreground command, then accepts synchronous restart and status requests over a private Unix socket. Run commands are passed as argv; build and stop commands are interpreted by `/bin/sh -c`.

```sh
go run . --build 'make build' -- ./bin/server --foreground
go run . status
go run . restart
```

The default control socket is derived from the canonical current directory. Use `--control PATH` on both the runner and client commands to select another socket. Status responses include `restartPending` while an accepted restart is waiting to commit or finish. `--id-env NAME` passes the generated instance ID to run and stop commands. `--stop-cmd` asks the child to exit and waits without sending it a signal; Ctrl-C and SIGTERM always force the child process group down.

This milestone provides the headless path and the model/presenter seam. TTY rendering and the confirmation dialog interaction are scheduled for the next milestone.
