# One-line setup

1. Run `./setup.sh --interval 5s` from this directory.
2. Select the `winsock64-client.exe` PID when the list appears.
3. Watch x64dbg receive server messages and send client replies.
4. Close x64dbg when finished; the fixture server is stopped automatically.

For a standalone target: `./setup.sh --target /path/to/program.exe --no-server`.
