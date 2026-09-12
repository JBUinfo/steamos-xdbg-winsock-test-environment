#!/usr/bin/env bash

# Run the complete local xdbg/WinSock demonstration from this repository.
set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
target="${WS2_TARGET_EXE:-$ROOT/examples/winsock64-client.exe}"
server="${WS2_SERVER_EXE:-}"
compat="${WS2_COMPATDATA:-${HOME:-}/xdbg/proton-prefix}"
proton="${WS2_PROTON:-${HOME:-}/.local/share/Steam/steamapps/common/Proton 10.0/proton}"
target_cmdline="${WS2_TARGET_CMDLINE:-}"
port="${WS2_PORT:-27015}"
message="${WS2_MESSAGE:-ping from winsock server}"
reply="${WS2_REPLY:-reply from winsock client}"
interval="${WS2_INTERVAL:-}"
no_build=0
no_server=0
no_reuse=0
no_wait=0

usage() {
    printf '%s\n' \
      'Usage: ./setup.sh [--target EXE] [--server EXE] [--compatdata DIR]' \
      '       [--proton FILE] [--target-cmdline TEXT] [--no-server] [--no-build]' \
      '       [--port N] [--interval DURATION] [--message TEXT] [--reply TEXT]' \
      '       [--no-reuse] [--no-wait]'
}
die() { printf 'Error: %s\n' "$*">&2; exit 1; }
expand_path() {
    case "$1" in
        '~') printf '%s' "${HOME:-}" ;;
        '~/'*) printf '%s/%s' "${HOME:-}" "${1#~/}" ;;
        /*) printf '%s' "$1" ;;
        *) printf '%s/%s' "$ROOT" "$1" ;;
    esac
}

while (($#)); do
    case "$1" in
        --target|--exe) (($# >= 2)) || die "$1 needs a path"; target=$(expand_path "$2"); shift 2 ;;
        --target=*) target=$(expand_path "${1#*=}"); shift ;;
        --server) (($# >= 2)) || die '--server needs a path'; server=$(expand_path "$2"); shift 2 ;;
        --server=*) server=$(expand_path "${1#*=}"); shift ;;
        --compatdata) (($# >= 2)) || die '--compatdata needs a directory'; compat=$(expand_path "$2"); shift 2 ;;
        --compatdata=*) compat=$(expand_path "${1#*=}"); shift ;;
        --proton) (($# >= 2)) || die '--proton needs a file'; proton=$(expand_path "$2"); shift 2 ;;
        --proton=*) proton=$(expand_path "${1#*=}"); shift ;;
        --target-cmdline) (($# >= 2)) || die '--target-cmdline needs text'; target_cmdline=$2; shift 2 ;;
        --target-cmdline=*) target_cmdline="${1#*=}"; shift ;;
        --port) (($# >= 2)) || die '--port needs a number'; port=$2; shift 2 ;;
        --port=*) port="${1#*=}"; shift ;;
        --message) (($# >= 2)) || die '--message needs text'; message=$2; shift 2 ;;
        --message=*) message="${1#*=}"; shift ;;
        --reply) (($# >= 2)) || die '--reply needs text'; reply=$2; shift 2 ;;
        --reply=*) reply="${1#*=}"; shift ;;
        --interval) (($# >= 2)) || die '--interval needs a duration'; interval=$2; shift 2 ;;
        --interval=*) interval="${1#*=}"; shift ;;
        --no-server) no_server=1; shift ;;
        --no-build) no_build=1; shift ;;
        --no-reuse) no_reuse=1; shift ;;
        --no-wait) no_wait=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) die "Unknown option: $1" ;;
    esac
done

command -v python3 >/dev/null || die 'python3 is required'
command -v file >/dev/null || die 'file is required'
command -v timeout >/dev/null || die 'timeout is required'
command -v ss >/dev/null || die 'ss is required'
[[ -f "$target" ]] || die "Target not found: $target"
[[ -d "$compat" ]] || die "Compatdata not found: $compat"
[[ -f "$proton" ]] || die "Proton script not found: $proton"
[[ "$port" =~ ^[0-9]+$ ]] && ((port >= 1 && port <= 65535)) || die 'Invalid port'

if ((no_build == 0)); then
    "$ROOT/build.sh"
    if command -v go >/dev/null 2>&1; then
        "$ROOT/src/winsock-test/build.sh"
    else
        printf 'Go is not installed; using the checked-in fixture binaries.\n' >&2
    fi
fi

pe=$(file -L -b -- "$target" 2>/dev/null || true)
case "$pe" in
    *PE32+*) arch=x64 ;;
    *PE32*) arch=x32 ;;
    *) arch=x64; printf 'Warning: PE architecture was not detected; using x64.\n' >&2 ;;
esac
if [[ "$arch" == x64 ]]; then
    dll="$ROOT/lib/ws2-hook64.dll"
    launcher="${WS2_LAUNCHER:-${HOME:-}/.local/share/xdbg/release/x64/launch-xdbg.sh}"
    injector="${WS2_INJECTOR:-$ROOT/bin/win-dll-injector-windows-amd64.exe}"
else
    dll="$ROOT/lib/ws2-hook32.dll"
    launcher="${WS2_LAUNCHER:-${HOME:-}/.local/share/xdbg/release/x32/launch-xdbg.sh}"
    injector="${WS2_INJECTOR:-$ROOT/bin/win-dll-injector-windows-386.exe}"
fi
[[ -f "$dll" ]] || die "Hook DLL not found: $dll"
[[ -f "$launcher" ]] || die "xdbg launcher not found: $launcher (set WS2_LAUNCHER)"
[[ -f "$injector" ]] || die "Injector not found: $injector (x32 injection needs an x86 injector)"

if [[ -z "$server" && "$target" == "$ROOT/examples/winsock64-client.exe" ]]; then
    server="$ROOT/examples/winsock64-server.exe"
fi
if ((no_server == 0)) && [[ -n "$server" ]]; then
    [[ -f "$server" ]] || die "Server not found: $server"
fi
export STEAM_COMPAT_DATA_PATH="$compat"
export STEAM_COMPAT_CLIENT_INSTALL_PATH="${HOME:-}/.local/share/Steam"
export WINEDEBUG=-all
export WS2_FIXTURE_PORT="$port"
export WS2_FIXTURE_REPLY="$reply"
export WS2_FIXTURE_MESSAGE="$message"
export WS2_FIXTURE_INTERVAL="$interval"
log="$compat/pfx/drive_c/users/steamuser/AppData/Local/Temp/ws2-hook.log"
mcp_config="$(dirname -- "$launcher")/mcp_config.json"
xdbg_log="${TMPDIR:-/tmp}/ws2-hook-xdbg-$$.log"
launcher_pid=''
server_pid=''
server_win_pid=''
existing_state=''
server_log=''

stop_server() {
    if [[ -n "$server_win_pid" ]]; then
        "$proton" runinprefix taskkill.exe /PID "$server_win_pid" /F >/dev/null 2>&1 || true
        server_win_pid=''
    fi
    if [[ -n "$server_pid" ]]; then
        kill "$server_pid" 2>/dev/null || true
        wait "$server_pid" 2>/dev/null || true
        server_pid=''
    fi
}

cleanup() {
    if [[ "$no_wait" == 0 ]]; then stop_server; fi
}
trap cleanup EXIT

open_tail_window() {
    local label=$1 path=$2
    if command -v konsole >/dev/null 2>&1; then
        konsole --separate --hold -e bash -c \
            'printf "\\n%s\\n\\n" "$1"; exec tail -F -- "$2"' \
            ws2-log-window "$label" "$path" >/dev/null 2>&1 &
        return 0
    fi
    return 1
}

watch_server_until_xdbg_exits() {
    local watched_pid=$1 server_host_pid=$2 server_windows_pid=$3
    nohup bash -c '
        while kill -0 "$1" 2>/dev/null; do sleep 1; done
        export STEAM_COMPAT_DATA_PATH="$3"
        export STEAM_COMPAT_CLIENT_INSTALL_PATH="$4"
        export WINEDEBUG=-all
        "$2" runinprefix taskkill.exe /PID "$5" /F >/dev/null 2>&1 || true
        kill "$6" 2>/dev/null || true
    ' ws2-server-watcher "$watched_pid" "$proton" "$compat" \
        "$STEAM_COMPAT_CLIENT_INSTALL_PATH" "$server_windows_pid" "$server_host_pid" \
        >/dev/null 2>&1 &
}

mcp() {
    local method=$1 args=${2:-\{\}}
    [[ -f "$mcp_config" ]] || return 1
    python3 - "$mcp_config" "$method" "$args" <<'PY'
import json, sys, urllib.request
try:
    cfg = json.load(open(sys.argv[1], encoding='utf-8'))
    host = cfg.get('IpAddress', cfg.get('ipAddress', '127.0.0.1'))
    port = int(cfg.get('Port', cfg.get('port')))
    body = {'jsonrpc':'2.0','id':1,'method':'tools/call',
            'params':{'name':sys.argv[2], 'arguments':json.loads(sys.argv[3])}}
    req = urllib.request.Request(f'http://{host}:{port}/',
        data=json.dumps(body).encode(),
        headers={'Authorization':'Bearer '+cfg.get('AuthToken', cfg.get('token','')),
                 'Content-Type':'application/json','Accept':'application/json'})
    with urllib.request.urlopen(req, timeout=5) as response: result=json.load(response)
    if result.get('error') or result.get('result',{}).get('isError'): raise RuntimeError('MCP request failed')
    print('\n'.join(x.get('text','') for x in result.get('result',{}).get('content',[]) if x.get('type')=='text'))
except Exception as exc:
    print(exc, file=sys.stderr); sys.exit(1)
PY
}

wait_mcp() {
    local state
    for _ in $(seq 1 40); do
        if state=$(mcp GetDebugState '{}'); then printf '%s\n' "$state"; return 0; fi
        sleep .5
    done
    return 1
}

continue_xdbg() {
    local state response
    state=$(mcp GetDebugState '{}') || return 1
    [[ "$state" == *'isRunning: true'* ]] && return 0
    response=$(mcp run '{"timeoutMs":1000}') || return 1
    printf '%s\n' "$response"
    state=$(mcp GetDebugState '{}') || return 1
    if [[ "$state" != *'isRunning: true'* ]]; then
        response=$(mcp run '{"timeoutMs":1000}') || return 1
        printf '%s\n' "$response"
    fi
    state=$(mcp GetDebugState '{}') || return 1
    [[ "$state" == *'isRunning: true'* ]]
}

list_pids() {
    local listing
    listing=$(timeout 20s "$proton" runinprefix winedbg --command 'info proc' 2>&1 || true)
    python3 -c 'import re, sys
for line in sys.stdin:
    m = re.match(r"\s*([0-9A-Fa-f]+)\s+\d+\s+.*?[\x27\"]([^\x27\"]+)[\x27\"]\s*$", line)
    if m: print(f"{int(m.group(1),16)}\t{m.group(2)}")' <<<"$listing"
}

target_stem=$(basename -- "$target")
target_stem=${target_stem%.exe}
if ((no_reuse == 0)) && state=$(mcp GetDebugState '{}') && [[ "$state" == *'isDebugging: true'* ]]; then
    [[ "$state" == *"$target_stem"* ]] || die "Another xdbg target is already active; close it or use a matching target"
    existing_state=$state
fi

if ((no_server == 0)) && [[ -n "$server" && -z "$existing_state" ]]; then
    server_log="${TMPDIR:-/tmp}/ws2-hook-server-$$.log"
    if ss -ltn 2>/dev/null | grep -q "127.0.0.1:$port"; then
        die "Port $port is already in use; stop the previous fixture server or choose another port with --port"
    fi
    if [[ -n "$interval" ]]; then
        printf 'Starting server (%s interval) in the shared prefix...\n' "$interval"
    else
        printf 'Starting server (compiled default interval) in the shared prefix...\n'
    fi
    server_args=(-port "$port" -message "$message")
    [[ -z "$interval" ]] || server_args+=(-interval "$interval")
    "$proton" runinprefix "$server" "${server_args[@]}" >"$server_log" 2>&1 &
    server_pid=$!
    ready=0
    for _ in $(seq 1 40); do
        server_win_pid=$(sed -n 's/.*(pid \([0-9][0-9]*\)).*/\1/p' "$server_log" | head -n 1)
        if [[ -n "$server_win_pid" ]]; then ready=1; break; fi
        kill -0 "$server_pid" 2>/dev/null || die "Server exited; see $server_log"
        sleep .25
    done
    ((ready == 1)) || die "Server did not open port $port; see $server_log"
    if ! open_tail_window 'WinSock server output' "$server_log"; then
        printf 'Server output: %s\n' "$server_log"
    fi
fi

if [[ -n "$existing_state" ]]; then
    printf 'Existing xdbg session detected; reusing it.\n'
else
    printf 'Launching %s in xdbg...\n' "$target"
    args=(--launch "$target" --compatdata "$compat" --proton "$proton")
    [[ -z "$target_cmdline" ]] || args+=(--target-cmdline "$target_cmdline")
    XDBG_DISABLE_SCYLLAHIDE=1 "$launcher" "${args[@]}" >"$xdbg_log" 2>&1 &
    launcher_pid=$!
    if ! wait_mcp || ! continue_xdbg; then
        printf 'MCP could not continue xdbg automatically; continuing to PID selection. Press F9 in x64dbg after injection if the target is paused.\n'
    fi
    if [[ -n "$server_pid" && -n "$server_win_pid" ]]; then
        watch_server_until_xdbg_exits "$launcher_pid" "$server_pid" "$server_win_pid"
    fi
fi

declare -a pids=() names=()
while IFS=$'\t' read -r pid name; do
    [[ -n "$pid" ]] || continue
    pids+=("$pid")
    names+=("$name")
done < <(list_pids)
((${#pids[@]} > 0)) || die 'No Windows processes found in this Proton prefix'
printf '\nWindows processes in the selected prefix:\n'
for i in "${!pids[@]}"; do printf '  %2d) PID %-6s %s\n' "$((i+1))" "${pids[i]}" "${names[i]}"; done
while :; do
    read -r -p "Select a PID [1-${#pids[@]}]: " choice
    [[ "$choice" =~ ^[0-9]+$ ]] && ((choice >= 1 && choice <= ${#pids[@]})) && break
    printf 'Choose a number from 1 to %s.\n' "${#pids[@]}" >&2
done
selected="${pids[choice-1]}"
printf 'Selected PID %s (%s).\n' "$selected" "${names[choice-1]}"

printf 'Injecting %s...\n' "$dll"
hook_win=$("$proton" runinprefix winepath -w "$dll" | tr -d '\r\n')
"$proton" runinprefix "$injector" inject --process-id "$selected" "$hook_win"

if command -v konsole >/dev/null 2>&1; then
    open_tail_window 'Injected hook output (hex + ASCII)' "$log"
    printf 'Log window opened: %s\n' "$log"
else
    printf 'Follow the log manually: %s\n' "$log"
fi

if [[ -n "$server" && "$target" == "$ROOT/examples/winsock64-client.exe" ]]; then
    printf 'The xdbg client will reply with: %s\n' "$reply"
else
    printf 'Server skipped; generate traffic in the selected process.\n'
fi

printf '\nSetup complete.\n'
if [[ -n "$launcher_pid" && "$no_wait" == 0 ]]; then
    printf 'This terminal stays attached until xdbg closes; use --no-wait to return immediately.\n'
    wait "$launcher_pid" || true
fi
