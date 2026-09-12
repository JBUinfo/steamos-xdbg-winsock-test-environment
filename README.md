# WinSock xdbg runner

This small repository contains the diagnostic DLL, its MinHook source, the x64 injector, and a localhost server/client pair; all project paths are relative to this directory.

`setup.sh` builds the DLL, starts the fixture server outside x64dbg, opens the fixture client in x64dbg, lists Windows PIDs from the shared Proton prefix, lets you choose the client, injects the hook, and opens a live log window. The server sends a message every three minutes by default; the client replies to every message.

## Requirements

- SteamOS Proton 10 (or another compatible Proton build).
- [SteamOS xdbg launcher](https://github.com/JBUinfo/steamos-xdbg-launcher), with x64dbg and the [x64dbg MCP server](https://github.com/SetsunaYukiOvO/x64dbg-mcp); override the launcher path with `WS2_LAUNCHER` if needed.
- The private prefix `$HOME/xdbg/proton-prefix` (override with `--compatdata`).
- MinGW-w64 only when rebuilding the DLLs.

## Run

```bash
./setup.sh
```

The script asks which Windows PID should receive the DLL. Use `--interval 5s` for a quick test, or `--target /path/to/program.exe --no-server` for another executable; add `--target-cmdline '...'` for its arguments.

The fixture sources and rebuild command are under `src/winsock-test/`; the generated server/client binaries are under `examples/`.

The x64 injector is `bin/win-dll-injector-windows-amd64.exe`; the included 32-bit DLL is built for PE32 targets, but an x86 injector is not included yet.

The hook logs `send` and `recv` calls to the target prefix's `pfx/drive_c/users/steamuser/AppData/Local/Temp/ws2-hook.log` and does not change packet contents.

Use this only with processes you own or are authorised to test.
