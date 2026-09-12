//go:build windows && amd64

// Package diagnostics contiene la parte portable del antiguo Seh.cpp que sí
// tiene una traducción razonable en Go: instalar un filtro Win32 que escriba
// un minidump. El traductor SEH->excepción C++ no existe como equivalente
// directo porque Go ya posee su propio runtime de excepciones/panics.
package diagnostics

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// Valores de retorno de un EXCEPTION_FILTER, definidos por Win32.
	exceptionContinueSearch = 0
	exceptionExecuteHandler = 1
	miniDumpWithFullMemory  = 0x00000002
	miniDumpWithHandleData  = 0x00000004
)

var (
	// SetUnhandledExceptionFilter está en kernel32; MiniDumpWriteDump vive
	// en dbghelp y puede no estar presente en instalaciones muy reducidas.
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	dbghelp  = windows.NewLazySystemDLL("dbghelp.dll")

	procSetUnhandledExceptionFilter = kernel32.NewProc("SetUnhandledExceptionFilter")
	procMiniDumpWriteDump           = dbghelp.NewProc("MiniDumpWriteDump")

	installedMu sync.Mutex
	installed   *Handler
)

// Exception describe la información que Windows entrega al filtro. Pointers
// es un puntero opaco a EXCEPTION_POINTERS; no se convierte a un puntero Go
// porque solo debe consumirse durante la callback nativa.
type Exception struct {
	Code     uint32
	Pointers uintptr
}

// Handler representa un filtro instalado y conserva el callback anterior para
// poder restaurarlo cuando el llamador termine.
type Handler struct {
	callback  uintptr
	previous  uintptr
	directory string
}

// InstallUnhandledExceptionDump instala el equivalente práctico de
// MyGenericUnhandledExceptionFilter. Los dumps se crean en directory con
// nombres Crash-YYYYMMDDhhmmss-PID.dmp.
func InstallUnhandledExceptionDump(directory string) (*Handler, error) {
	if directory == "" {
		directory = "."
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("diagnostics: resolver directorio: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return nil, fmt.Errorf("diagnostics: crear directorio: %w", err)
	}

	// NewCallback crea un trampoline compatible con la firma
	// LONG WINAPI filter(EXCEPTION_POINTERS*). El parámetro se mantiene como
	// uintptr porque la estructura es propiedad del runtime de Windows.
	callback := windows.NewCallback(func(exceptionPointers uintptr) uintptr {
		return writeMiniDump(absolute, exceptionPointers)
	})
	previous, _, _ := procSetUnhandledExceptionFilter.Call(callback)

	handler := &Handler{
		callback:  callback,
		previous:  previous,
		directory: absolute,
	}

	// Mantener una referencia global evita que una futura implementación del
	// runtime pueda recolectar el closure mientras Windows conserva su puntero.
	installedMu.Lock()
	installed = handler
	installedMu.Unlock()
	return handler, nil
}

// Restore desinstala el filtro y vuelve a dejar el filtro anterior en vigor.
// Es idempotente para que pueda llamarse desde defer sin coordinación externa.
func (h *Handler) Restore() error {
	if h == nil {
		return nil
	}

	installedMu.Lock()
	if installed == h {
		installed = nil
	}
	installedMu.Unlock()

	// SetUnhandledExceptionFilter devuelve el valor anterior; no expone un
	// resultado BOOL fiable para validar el cambio, por lo que no inventamos un
	// error cuando la llamada nativa no proporciona uno.
	procSetUnhandledExceptionFilter.Call(h.previous)
	h.callback = 0
	return nil
}

// writeMiniDump se ejecuta dentro del callback de Windows. Devuelve
// EXCEPTION_EXECUTE_HANDLER si pudo escribir el archivo y CONTINUE_SEARCH si
// algo falló, permitiendo que otro filtro o el runtime continúe el tratamiento.
func writeMiniDump(directory string, exceptionPointers uintptr) uintptr {
	process, err := windows.GetCurrentProcess()
	if err != nil {
		return exceptionContinueSearch
	}

	// Incluir el PID evita colisiones si dos procesos fallan en el mismo segundo.
	name := fmt.Sprintf("Crash-%s-%d.dmp", time.Now().Format("20060102150405"), windows.GetCurrentProcessId())
	path := filepath.Join(directory, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return exceptionContinueSearch
	}
	defer file.Close()

	// Layout equivalente a MINIDUMP_EXCEPTION_INFORMATION:
	// DWORD ThreadId; EXCEPTION_POINTERS* ExceptionPointers; BOOL ClientPointers.
	info := struct {
		ThreadID          uint32
		ExceptionPointers uintptr
		ClientPointers    int32
	}{
		ThreadID:          windows.GetCurrentThreadId(),
		ExceptionPointers: exceptionPointers,
		ClientPointers:    1,
	}

	dumpType := uintptr(miniDumpWithFullMemory | miniDumpWithHandleData)
	r1, _, _ := procMiniDumpWriteDump.Call(
		uintptr(process),
		uintptr(windows.GetCurrentProcessId()),
		file.Fd(),
		dumpType,
		uintptr(unsafe.Pointer(&info)),
		0,
		0,
	)
	if r1 == 0 {
		return exceptionContinueSearch
	}
	return exceptionExecuteHandler
}
