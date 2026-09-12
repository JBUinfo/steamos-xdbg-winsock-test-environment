/*
 * ws2-hook.dll is a deliberately small diagnostic payload for the local
 * xdbg/Proton fixture.  It is not a game cheat: it logs localhost WinSock
 * traffic and then calls the original APIs through MinHook trampolines.
 */

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <winsock2.h>
#include <stdint.h>
#include <stdio.h>
#include <stdarg.h>
#include <string.h>

#include "MinHook.h"

typedef int (WSAAPI *send_fn)(SOCKET, const char *, int, int);
typedef int (WSAAPI *recv_fn)(SOCKET, char *, int, int);

static send_fn g_original_send;
static recv_fn g_original_recv;
static HANDLE g_log = INVALID_HANDLE_VALUE;
static CRITICAL_SECTION g_log_lock;
static volatile LONG g_log_ready;
static DWORD g_tls_index = TLS_OUT_OF_INDEXES;

static int in_hook(void)
{
    return g_tls_index != TLS_OUT_OF_INDEXES &&
           TlsGetValue(g_tls_index) != NULL;
}

static void set_in_hook(int value)
{
    if (g_tls_index != TLS_OUT_OF_INDEXES) {
        (void)TlsSetValue(g_tls_index, value ? (LPVOID)1 : NULL);
    }
}

static const char *status_name(MH_STATUS status)
{
    const char *name = MH_StatusToString(status);
    return name != NULL ? name : "unknown";
}

static void write_log(const char *format, ...)
{
    char line[4096];
    va_list args;
    int length;
    DWORD written;

    if (InterlockedCompareExchange(&g_log_ready, 1, 1) == 0 ||
        g_log == INVALID_HANDLE_VALUE || in_hook()) {
        return;
    }

    set_in_hook(1);
    va_start(args, format);
    length = vsnprintf(line, sizeof(line), format, args);
    va_end(args);
    if (length < 0) {
        set_in_hook(0);
        return;
    }
    if ((size_t)length >= sizeof(line) - 2) {
        length = (int)sizeof(line) - 3;
    }
    line[length++] = '\r';
    line[length++] = '\n';
    line[length] = '\0';

    EnterCriticalSection(&g_log_lock);
    WriteFile(g_log, line, (DWORD)length, &written, NULL);
    FlushFileBuffers(g_log);
    LeaveCriticalSection(&g_log_lock);
    OutputDebugStringA(line);
    set_in_hook(0);
}

static void log_packet(const char *operation, SOCKET socket,
                       const char *buffer, int length, int flags,
                       int result, int error_code)
{
    char line[4096];
    size_t used;
    int shown;
    int i;

    if (length < 0) {
        length = 0;
    }
    shown = buffer == NULL ? 0 : (length > 512 ? 512 : length);
    used = (size_t)snprintf(line, sizeof(line),
        "%s socket=%p len=%d flags=0x%x result=%d error=%d bytes=",
        operation, (void *)(uintptr_t)socket, length, (unsigned)flags,
        result, error_code);
    if (used >= sizeof(line)) {
        return;
    }
    if (buffer == NULL && used + 7 < sizeof(line)) {
        (void)snprintf(line + used, sizeof(line) - used, "<null>");
        used = strlen(line);
    }
    for (i = 0; i < shown && used + 4 < sizeof(line); ++i) {
        used += (size_t)snprintf(line + used, sizeof(line) - used,
                                 "%02X", (unsigned char)buffer[i]);
        if (i + 1 < shown && used + 1 < sizeof(line)) {
            line[used++] = ' ';
            line[used] = '\0';
        }
    }
    if (shown < length && used + 14 < sizeof(line)) {
        (void)snprintf(line + used, sizeof(line) - used,
                       " ... (truncated)");
        used = strlen(line);
    }
    if (used + 9 < sizeof(line)) {
        used += (size_t)snprintf(line + used, sizeof(line) - used,
                                 " ascii=\"");
        for (i = 0; i < shown && used + 7 < sizeof(line); ++i) {
            unsigned char value = (unsigned char)buffer[i];
            if (value >= 0x20 && value <= 0x7e) {
                if (value == '\\' || value == '\"') {
                    line[used++] = '\\';
                }
                line[used++] = (char)value;
                line[used] = '\0';
            } else {
                used += (size_t)snprintf(line + used, sizeof(line) - used,
                                         "\\x%02X", (unsigned)value);
            }
        }
        if (used + 2 < sizeof(line)) {
            line[used++] = '\"';
            line[used] = '\0';
        }
    }
    write_log("%s", line);
}

/*
 * Custom packet-processing point.
 *
 * For send, this runs before the original WinSock call.  For recv, it runs
 * after the original call and receives only the bytes that were read.  Keep
 * the buffer unchanged to inspect traffic without changing or consuming it.
 * The hook guard is active while this function runs, so WinSock calls made by
 * custom code bypass these hooks and do not recurse.
 */
static void user_process_packet(const char *operation,
                                const char *buffer, int length)
{
    (void)operation;
    (void)buffer;
    (void)length;
    /* Add custom inspection code here. */
}

static int WSAAPI hooked_send(SOCKET socket, const char *buffer,
                              int length, int flags)
{
    int result;
    int error_code = 0;

    if (in_hook() || g_original_send == NULL) {
        return g_original_send != NULL
            ? g_original_send(socket, buffer, length, flags)
            : SOCKET_ERROR;
    }
    set_in_hook(1);
    user_process_packet("send", buffer, length);
    result = g_original_send(socket, buffer, length, flags);
    if (result == SOCKET_ERROR) {
        error_code = WSAGetLastError();
    }
    set_in_hook(0);
    log_packet("send", socket, buffer, length, flags, result, error_code);
    return result;
}

static int WSAAPI hooked_recv(SOCKET socket, char *buffer,
                              int length, int flags)
{
    int result;
    int error_code = 0;

    if (in_hook() || g_original_recv == NULL) {
        return g_original_recv != NULL
            ? g_original_recv(socket, buffer, length, flags)
            : SOCKET_ERROR;
    }
    set_in_hook(1);
    result = g_original_recv(socket, buffer, length, flags);
    if (result == SOCKET_ERROR) {
        error_code = WSAGetLastError();
    }
    if (result > 0) {
        user_process_packet("recv", buffer, result);
    }
    set_in_hook(0);
    log_packet("recv", socket, buffer, result > 0 ? result : 0,
               flags, result, error_code);
    return result;
}

static void init_log(void)
{
    char path[MAX_PATH];
    DWORD length;

    InitializeCriticalSection(&g_log_lock);
    length = GetEnvironmentVariableA("WS2_HOOK_LOG", path, sizeof(path));
    if (length == 0 || length >= sizeof(path)) {
        length = GetTempPathA(sizeof(path), path);
        if (length == 0 || length >= sizeof(path) - 32) {
            lstrcpyA(path, "C:\\ws2-hook.log");
        } else {
            lstrcatA(path, "ws2-hook.log");
        }
    }
    g_log = CreateFileA(path, FILE_APPEND_DATA,
                        FILE_SHARE_READ | FILE_SHARE_WRITE, NULL,
                        OPEN_ALWAYS, FILE_ATTRIBUTE_NORMAL, NULL);
    if (g_log != INVALID_HANDLE_VALUE) {
        InterlockedExchange(&g_log_ready, 1);
        write_log("ws2-hook loaded; log=%s pid=%lu", path,
                  (unsigned long)GetCurrentProcessId());
    }
}

static DWORD WINAPI install_hooks(void *unused)
{
    MH_STATUS status;

    (void)unused;
    Sleep(100);
    init_log();

    status = MH_Initialize();
    if (status != MH_OK && status != MH_ERROR_ALREADY_INITIALIZED) {
        write_log("MinHook initialize failed: %s", status_name(status));
        return 0;
    }

    if (LoadLibraryA("Ws2_32.dll") == NULL) {
        write_log("LoadLibraryA(Ws2_32.dll) failed: %lu",
                  (unsigned long)GetLastError());
        return 0;
    }

    status = MH_CreateHookApi(L"Ws2_32.dll", "send",
                               (LPVOID)hooked_send,
                               (LPVOID *)&g_original_send);
    if (status != MH_OK) {
        write_log("send hook failed: %s", status_name(status));
    }
    status = MH_CreateHookApi(L"Ws2_32.dll", "recv",
                              (LPVOID)hooked_recv,
                              (LPVOID *)&g_original_recv);
    if (status != MH_OK) {
        write_log("recv hook failed: %s", status_name(status));
    }
    status = MH_EnableHook(MH_ALL_HOOKS);
    write_log("hooks enabled: %s send=%p recv=%p", status_name(status),
              (void *)g_original_send, (void *)g_original_recv);
    return 0;
}

BOOL WINAPI DllMain(HINSTANCE instance, DWORD reason, LPVOID reserved)
{
    HANDLE thread;

    (void)reserved;
    if (reason == DLL_PROCESS_ATTACH) {
        DisableThreadLibraryCalls(instance);
        g_tls_index = TlsAlloc();
        thread = CreateThread(NULL, 0, install_hooks, NULL, 0, NULL);
        if (thread != NULL) {
            CloseHandle(thread);
        }
    } else if (reason == DLL_PROCESS_DETACH &&
               g_tls_index != TLS_OUT_OF_INDEXES) {
        TlsFree(g_tls_index);
        g_tls_index = TLS_OUT_OF_INDEXES;
    }
    return TRUE;
}
