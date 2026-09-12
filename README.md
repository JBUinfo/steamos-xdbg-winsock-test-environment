# WinSock xdbg runner

This small repository contains the diagnostic DLL, its MinHook source, the x64 injector, and a localhost server/client pair; all project paths are relative to this directory.

`setup.sh` builds the DLL, starts the fixture server outside x64dbg, opens the fixture client in x64dbg, lists Windows PIDs from the shared Proton prefix, lets you choose the client, injects the hook, and opens live server and hook-log windows. The server sends a message every three minutes by default; the client replies to every message.

The fixture server is terminated automatically when the x64dbg launcher exits.

## Requirements

- SteamOS Proton 10 (or another compatible Proton build).
- [SteamOS xdbg launcher](https://github.com/JBUinfo/steamos-xdbg-launcher), with x64dbg and the [x64dbg MCP server](https://github.com/SetsunaYukiOvO/x64dbg-mcp); override the launcher path with `WS2_LAUNCHER` if needed.
- The private prefix `$HOME/xdbg/proton-prefix` (override with `--compatdata`).
- MinGW-w64 when rebuilding the DLLs; Go is used to rebuild the fixture server/client (the checked-in binaries work without Go).

## Run

```bash
./setup.sh
```

Run it from a terminal rather than double-clicking, so errors and log paths remain visible. The script asks which Windows PID should receive the DLL. Use `--interval 5s` for a quick test, or `--target /path/to/program.exe --no-server` for another executable; add `--target-cmdline '...'` for its arguments. If the default port is busy, choose another with `--port N`.

The fixture sources and rebuild command are under `src/winsock-test/`; `setup.sh` rebuilds the server/client automatically when Go is installed, and the generated binaries are under `examples/`.

The `--interval` option controls the running server. If omitted, `setup.sh` leaves the flag unset and uses the compiled default from `main.go` (three minutes in the checked-in source).

If the MCP server is unavailable, setup continues to PID selection; press F9 in x64dbg after injection if the target is paused.

The x64 injector is `bin/win-dll-injector-windows-amd64.exe`; the included 32-bit DLL is built for PE32 targets, but an x86 injector is not included yet.

The hook logs `send` and `recv` calls to the target prefix's `pfx/drive_c/users/steamuser/AppData/Local/Temp/ws2-hook.log` and does not change packet contents.

Use this only with processes you own or are authorised to test.
