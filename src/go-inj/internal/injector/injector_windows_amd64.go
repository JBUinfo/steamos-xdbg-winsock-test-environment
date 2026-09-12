//go:build windows && amd64

// Package injector contiene la lógica interna del inyector de DLL. La CLI
// situada en cmd/injector solo convierte flags en llamadas a este paquete.
package injector

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"go-inj/internal/cleanup"
	"go-inj/internal/textutil"
)

const (
	// Los dos modos de acceso reproducen los permisos que pedía el C++ al
	// abrir el proceso para inyectar o expulsar una DLL.
	injectionProcessAccess = windows.PROCESS_QUERY_INFORMATION |
		windows.PROCESS_VM_READ |
		windows.PROCESS_CREATE_THREAD |
		windows.PROCESS_VM_OPERATION |
		windows.PROCESS_VM_WRITE
	ejectionProcessAccess = windows.PROCESS_QUERY_INFORMATION |
		windows.PROCESS_VM_READ |
		windows.PROCESS_CREATE_THREAD |
		windows.PROCESS_VM_OPERATION

	// No permitimos que una lista corrupta de módulos pueda hacer que el
	// enumerador reserve cantidades desproporcionadas de memoria.
	initialModuleCapacity = 64
	maxModuleCapacity     = 1 << 20
)

var (
	// kernel32 contiene las funciones que no tienen un wrapper suficiente en
	// x/sys/windows para esta operación concreta.
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procCreateRemoteThread = kernel32.NewProc("CreateRemoteThread")
	procVirtualAllocEx     = kernel32.NewProc("VirtualAllocEx")
	procVirtualFreeEx      = kernel32.NewProc("VirtualFreeEx")
	procGetExitCodeThread  = kernel32.NewProc("GetExitCodeThread")
	procGetModuleHandleW   = kernel32.NewProc("GetModuleHandleW")

	// FindWindowW pertenece a user32.dll y recibe una cadena UTF-16.
	user32         = windows.NewLazySystemDLL("user32.dll")
	procFindWindow = user32.NewProc("FindWindowW")
)

// Injector es un servicio sin estado que agrupa las operaciones de proceso.
//
// El C++ usaba un singleton. En Go no es necesario ocultar una instancia
// global: la estructura no contiene estado mutable de la operación y una
// instancia explícita facilita las pruebas y el uso desde varias CLI.
type Injector struct {
}

// New crea un servicio de inyección nuevo.
func New() *Injector {
	return &Injector{}
}

// GetModuleBaseAddress busca una DLL cargada en process y devuelve su base de
// memoria. La comparación acepta tanto el nombre base como la ruta completa,
// y no distingue mayúsculas/minúsculas como el icompare del C++.
func (i *Injector) GetModuleBaseAddress(process windows.Handle, path string) (uintptr, error) {
	if process == 0 || process == windows.InvalidHandle {
		return 0, fmt.Errorf("%w: handle %#x", ErrInvalidRemoteProcess, process)
	}
	if path == "" {
		return 0, ErrModulePathNotFound
	}

	// filepath.Abs y Clean sustituyen a GetFullPathNameW para esta validación.
	// A diferencia de MAX_PATH, Go puede conservar rutas largas si Windows las
	// tiene habilitadas.
	searchPath, err := filepath.Abs(path)
	if err != nil {
		searchPath = filepath.Clean(path)
	} else {
		searchPath = filepath.Clean(searchPath)
	}
	fileName := filepath.Base(searchPath)

	modules, err := enumerateModules(process)
	if err != nil {
		return 0, err
	}

	for _, module := range modules {
		// Las APIs GetModule*W escriben directamente en estos buffers. Cada
		// buffer contiene espacio para el terminador UTF-16.
		moduleName := make([]uint16, windows.MAX_PATH)
		if err := windows.GetModuleBaseName(process, module, &moduleName[0], uint32(len(moduleName))); err != nil {
			return 0, fmt.Errorf("%w: GetModuleBaseNameW: %v", ErrModuleEnumeration, err)
		}

		exePath := make([]uint16, windows.MAX_PATH)
		if err := windows.GetModuleFileNameEx(process, module, &exePath[0], uint32(len(exePath))); err != nil {
			return 0, fmt.Errorf("%w: GetModuleFileNameExW: %v", ErrModuleEnumeration, err)
		}

		if textutil.EqualFold(windows.UTF16ToString(moduleName), fileName) ||
			textutil.EqualFold(windows.UTF16ToString(exePath), searchPath) {
			// HMODULE coincide con la dirección base de la imagen en el proceso
			// remoto, por eso devolvemos su representación uintptr.
			return uintptr(module), nil
		}
	}

	return 0, fmt.Errorf("%w: %s", ErrModuleNotLoaded, path)
}

// InjectLib carga path en el proceso identificado por pid utilizando el
// patrón clásico LoadLibraryW + CreateRemoteThread.
func (i *Injector) InjectLib(pid uint32, path string) (err error) {
	if pid == 0 {
		return ErrInvalidProcessID
	}
	if path == "" {
		return ErrModulePathNotFound
	}

	// OpenProcess obtiene un handle con todos los permisos necesarios para
	// reservar/escribir memoria y crear el hilo remoto.
	process, openErr := windows.OpenProcess(injectionProcessAccess, false, pid)
	if openErr != nil {
		return fmt.Errorf("%w (pid %d): %v", ErrInvalidRemoteProcess, pid, openErr)
	}
	processGuard := cleanup.New(func() error {
		return windows.CloseHandle(process)
	})
	defer func() {
		// El cierre del handle es una limpieza secundaria: el error principal de
		// la operación debe conservar prioridad si ya existe.
		_ = processGuard.Close()
	}()

	// UTF16FromString añade el NUL final que LoadLibraryW espera. El tamaño
	// remoto incluye exactamente ese terminador.
	widePath, err := windows.UTF16FromString(path)
	if err != nil {
		return fmt.Errorf("%w: ruta UTF-16: %v", ErrModulePathNotFound, err)
	}
	byteCount := uintptr(len(widePath)) * 2

	// VirtualAllocEx reserva memoria RW en el proceso objetivo. Usamos el
	// comportamiento del código original (MEM_COMMIT); Windows asigna la región
	// comprometida para escribir el nombre.
	remoteAddress, _, callErr := procVirtualAllocEx.Call(
		uintptr(process),
		0,
		byteCount,
		windows.MEM_COMMIT,
		windows.PAGE_READWRITE,
	)
	if remoteAddress == 0 {
		return nativeError(ErrRemoteAllocation, "VirtualAllocEx", callErr)
	}

	// La región se libera en cualquier salida, incluso si WriteProcessMemory
	// o CreateRemoteThread fallan a mitad de la operación.
	regionGuard := cleanup.New(func() error {
		return releaseRemoteRegion(process, remoteAddress)
	})
	defer func() {
		if cleanupErr := regionGuard.Close(); err == nil && cleanupErr != nil {
			err = fmt.Errorf("%w: liberar memoria remota: %v", ErrRemoteAllocation, cleanupErr)
		}
	}()

	// WriteProcessMemory copia la cadena al espacio remoto. Se pasa solo el
	// puntero temporal que necesita la llamada y se mantiene viva la slice hasta
	// después de volver de la API nativa.
	var written uintptr
	writeErr := windows.WriteProcessMemory(
		process,
		remoteAddress,
		(*byte)(unsafe.Pointer(&widePath[0])),
		byteCount,
		&written,
	)
	runtime.KeepAlive(widePath)
	if writeErr != nil {
		return fmt.Errorf("%w: %v", ErrRemoteWrite, writeErr)
	}
	if written != byteCount {
		return fmt.Errorf("%w: escritos %d de %d bytes", ErrRemoteWrite, written, byteCount)
	}

	// LoadLibraryW se busca en el kernel32 ya cargado en este proceso. En
	// Windows x64 la dirección del export del sistema es válida para el hilo
	// remoto del mismo sistema de procesos.
	kernelHandle, err := getModuleHandle("kernel32.dll")
	if err != nil {
		return fmt.Errorf("%w: GetModuleHandleW(kernel32.dll): %v", ErrRemoteThread, err)
	}
	loadLibraryW, err := windows.GetProcAddress(kernelHandle, "LoadLibraryW")
	if err != nil {
		return fmt.Errorf("%w: GetProcAddress(LoadLibraryW): %v", ErrRemoteThread, err)
	}

	// CreateRemoteThread arranca LoadLibraryW(remoteAddress) dentro del
	// proceso objetivo. El último parámetro reserva el espacio para el thread id,
	// que no necesitamos, por eso es cero.
	thread, _, callErr := procCreateRemoteThread.Call(
		uintptr(process),
		0,
		0,
		loadLibraryW,
		remoteAddress,
		0,
		0,
	)
	if thread == 0 {
		return nativeError(ErrRemoteThread, "CreateRemoteThread", callErr)
	}
	threadHandle := windows.Handle(thread)
	threadGuard := cleanup.New(func() error {
		return windows.CloseHandle(threadHandle)
	})
	defer func() {
		_ = threadGuard.Close()
	}()

	// Esperar evita liberar la cadena mientras LoadLibraryW todavía la está
	// leyendo.
	waitResult, waitErr := windows.WaitForSingleObject(threadHandle, windows.INFINITE)
	if waitErr != nil {
		return fmt.Errorf("%w: WaitForSingleObject: %v", ErrRemoteCallFailed, waitErr)
	}
	if waitResult != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("%w: WaitForSingleObject devolvió %#x", ErrRemoteCallFailed, waitResult)
	}

	// El exit code es DWORD y puede truncar un HMODULE x64. Por eso, igual que
	// el C++ actualizado, la comprobación definitiva enumera los módulos.
	if _, err := i.GetModuleBaseAddress(process, path); err != nil {
		return fmt.Errorf("%w: %v", ErrRemoteCallFailed, err)
	}
	return nil
}

// EjectLib descarga path del proceso pid llamando a FreeLibrary en un hilo
// remoto. La DLL ya puede no existir en el disco, por lo que se busca solo en
// los módulos cargados del proceso.
func (i *Injector) EjectLib(pid uint32, path string) (err error) {
	if pid == 0 {
		return ErrInvalidProcessID
	}
	if path == "" {
		return ErrModulePathNotFound
	}

	process, openErr := windows.OpenProcess(ejectionProcessAccess, false, pid)
	if openErr != nil {
		return fmt.Errorf("%w (pid %d): %v", ErrInvalidRemoteProcess, pid, openErr)
	}
	processGuard := cleanup.New(func() error {
		return windows.CloseHandle(process)
	})
	defer func() {
		_ = processGuard.Close()
	}()

	baseAddress, err := i.GetModuleBaseAddress(process, path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrModuleNotLoaded, err)
	}

	kernelHandle, err := getModuleHandle("kernel32.dll")
	if err != nil {
		return fmt.Errorf("%w: GetModuleHandleW(kernel32.dll): %v", ErrRemoteThread, err)
	}
	freeLibrary, err := windows.GetProcAddress(kernelHandle, "FreeLibrary")
	if err != nil {
		return fmt.Errorf("%w: GetProcAddress(FreeLibrary): %v", ErrRemoteThread, err)
	}

	// El primer parámetro de FreeLibrary es el HMODULE/base remoto.
	thread, _, callErr := procCreateRemoteThread.Call(
		uintptr(process),
		0,
		0,
		freeLibrary,
		baseAddress,
		0,
		0,
	)
	if thread == 0 {
		return nativeError(ErrRemoteThread, "CreateRemoteThread", callErr)
	}
	threadHandle := windows.Handle(thread)
	threadGuard := cleanup.New(func() error {
		return windows.CloseHandle(threadHandle)
	})
	defer func() {
		_ = threadGuard.Close()
	}()

	waitResult, waitErr := windows.WaitForSingleObject(threadHandle, windows.INFINITE)
	if waitErr != nil {
		return fmt.Errorf("%w: WaitForSingleObject: %v", ErrRemoteCallFailed, waitErr)
	}
	if waitResult != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("%w: WaitForSingleObject devolvió %#x", ErrRemoteCallFailed, waitResult)
	}

	exitCode, err := remoteThreadExitCode(threadHandle)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRemoteCallFailed, err)
	}
	if exitCode == 0 {
		return fmt.Errorf("%w: FreeLibrary devolvió cero", ErrRemoteCallFailed)
	}
	return nil
}

// GetSeDebugPrivilege habilita SeDebugPrivilege en el token del proceso
// actual. Normalmente requiere ejecutar con permisos administrativos.
func (i *Injector) GetSeDebugPrivilege() error {
	process, err := windows.GetCurrentProcess()
	if err != nil {
		return fmt.Errorf("%w: GetCurrentProcess: %v", ErrPrivilege, err)
	}

	var token windows.Token
	access := uint32(windows.TOKEN_ADJUST_PRIVILEGES | windows.TOKEN_QUERY)
	if err := windows.OpenProcessToken(process, access, &token); err != nil {
		return fmt.Errorf("%w: OpenProcessToken: %v", ErrPrivilege, err)
	}
	tokenGuard := cleanup.New(func() error {
		return windows.CloseHandle(windows.Handle(token))
	})
	defer func() {
		_ = tokenGuard.Close()
	}()

	privilegeName, err := windows.UTF16PtrFromString("SeDebugPrivilege")
	if err != nil {
		return fmt.Errorf("%w: nombre UTF-16: %v", ErrPrivilege, err)
	}
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, privilegeName, &luid); err != nil {
		return fmt.Errorf("%w: LookupPrivilegeValue: %v", ErrPrivilege, err)
	}
	if luid.LowPart == 0 && luid.HighPart == 0 {
		return fmt.Errorf("%w: LUID vacío", ErrPrivilege)
	}

	// Tokenprivileges contiene una entrada inline, exactamente el layout que
	// necesita AdjustTokenPrivileges para activar un único privilegio.
	privileges := windows.Tokenprivileges{
		PrivilegeCount: 1,
	}
	privileges.Privileges[0] = windows.LUIDAndAttributes{
		Luid:       luid,
		Attributes: windows.SE_PRIVILEGE_ENABLED,
	}
	if err := windows.AdjustTokenPrivileges(
		token,
		false,
		&privileges,
		uint32(unsafe.Sizeof(privileges)),
		nil,
		nil,
	); err != nil {
		return fmt.Errorf("%w: AdjustTokenPrivileges: %v", ErrPrivilege, err)
	}
	// Windows puede devolver TRUE y dejar ERROR_NOT_ALL_ASSIGNED cuando el
	// token no posee el privilegio. x/sys expone ese último error por separado.
	if lastErr := windows.GetLastError(); lastErr != nil {
		return fmt.Errorf("%w: SeDebugPrivilege no asignado: %v", ErrPrivilege, lastErr)
	}
	return nil
}

// GetPath resuelve un nombre de módulo a una ruta absoluta existente. Primero
// intenta el directorio de trabajo y después el directorio del ejecutable,
// igual que el fallback de GetPath en el C++ original.
func (i *Injector) GetPath(moduleName string) (string, error) {
	if moduleName == "" {
		return "", ErrModulePathNotFound
	}

	// filepath.Abs normaliza separadores y componentes . / .. para la primera
	// búsqueda. os.Stat reemplaza a GetFileAttributes.
	workingPath, err := filepath.Abs(moduleName)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrPathResolution, err)
	}
	workingPath = filepath.Clean(workingPath)
	if exists, statErr := os.Stat(workingPath); statErr == nil && !exists.IsDir() {
		return workingPath, nil
	}

	// Si el nombre era relativo, probar junto al ejecutable del inyector. Esto
	// permite invocar el comando desde otra carpeta sin copiar el DLL.
	if !filepath.IsAbs(moduleName) {
		executable, exeErr := os.Executable()
		if exeErr != nil {
			return "", fmt.Errorf("%w: obtener ruta del ejecutable: %v", ErrPathResolution, exeErr)
		}
		executablePath := filepath.Join(filepath.Dir(executable), moduleName)
		executablePath, err = filepath.Abs(executablePath)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrPathResolution, err)
		}
		if exists, statErr := os.Stat(executablePath); statErr == nil && !exists.IsDir() {
			return filepath.Clean(executablePath), nil
		}
	}

	return "", fmt.Errorf("%w: %q", ErrModulePathNotFound, moduleName)
}

// GetProcessIdByName devuelve el PID del primer proceso cuyo ejecutable
// coincide con name. CompareCaseSensitive conserva el flag -c del C++.
func (i *Injector) GetProcessIdByName(name string, compareCaseSensitive bool) (uint32, error) {
	if name == "" {
		return 0, ErrProcessNotFound
	}

	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, fmt.Errorf("%w: CreateToolhelp32Snapshot: %v", ErrProcessEnumeration, err)
	}
	snapshotGuard := cleanup.New(func() error {
		return windows.CloseHandle(snapshot)
	})
	defer func() {
		_ = snapshotGuard.Close()
	}()

	entry := windows.ProcessEntry32{}
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return 0, fmt.Errorf("%w: Process32First: %v", ErrProcessEnumeration, err)
	}

	for {
		currentName := windows.UTF16ToString(entry.ExeFile[:])
		matches := currentName == name
		if !compareCaseSensitive {
			matches = textutil.EqualFold(currentName, name)
		}
		if matches {
			return entry.ProcessID, nil
		}

		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if err == syscall.ERROR_NO_MORE_FILES {
				break
			}
			return 0, fmt.Errorf("%w: Process32Next: %v", ErrProcessEnumeration, err)
		}
	}

	return 0, fmt.Errorf("%w: %s", ErrProcessNotFound, name)
}

// GetProcessIdByWindow encuentra una ventana por su título exacto y devuelve
// el PID del proceso que la posee.
func (i *Injector) GetProcessIdByWindow(name string) (uint32, error) {
	if name == "" {
		return 0, ErrWindowNotFound
	}
	title, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, fmt.Errorf("%w: título UTF-16: %v", ErrWindowNotFound, err)
	}

	// NULL como nombre de clase significa que solo se filtra por título.
	hwnd, _, callErr := procFindWindow.Call(0, uintptr(unsafe.Pointer(title)))
	runtime.KeepAlive(title)
	if hwnd == 0 {
		return 0, nativeError(ErrWindowNotFound, "FindWindowW", callErr)
	}

	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrWindowProcessID, err)
	}
	if pid == 0 {
		return 0, ErrWindowProcessID
	}
	return pid, nil
}

// enumerateModules obtiene todos los HMODULE de process. EnumProcessModules
// informa cuántos bytes necesitaba y puede obligar a repetir la llamada con un
// buffer mayor.
func enumerateModules(process windows.Handle) ([]windows.Handle, error) {
	capacity := initialModuleCapacity
	for capacity <= maxModuleCapacity {
		modules := make([]windows.Handle, capacity)
		var bytesNeeded uint32
		byteCapacity := uint32(uint64(len(modules)) * uint64(unsafe.Sizeof(modules[0])))
		if err := windows.EnumProcessModules(process, &modules[0], byteCapacity, &bytesNeeded); err != nil {
			return nil, fmt.Errorf("%w: EnumProcessModules: %v", ErrModuleEnumeration, err)
		}

		moduleSize := uint32(unsafe.Sizeof(modules[0]))
		count := int(bytesNeeded / moduleSize)
		if count <= len(modules) {
			return modules[:count], nil
		}

		// Redondear un poco hacia arriba evita repetir la llamada si el proceso
		// carga un módulo durante la enumeración.
		capacity = count + 16
	}

	return nil, fmt.Errorf("%w: el proceso tiene demasiados módulos", ErrModuleEnumeration)
}

// releaseRemoteRegion libera una reserva remota con MEM_RELEASE. Se mantiene
// separado para que cleanup.Guard pueda usarlo como cierre diferido.
func releaseRemoteRegion(process windows.Handle, address uintptr) error {
	r1, _, callErr := procVirtualFreeEx.Call(
		uintptr(process),
		address,
		0,
		windows.MEM_RELEASE,
	)
	if r1 == 0 {
		return nativeError(ErrRemoteAllocation, "VirtualFreeEx", callErr)
	}
	return nil
}

// remoteThreadExitCode lee el DWORD que dejó la función ejecutada por el
// hilo remoto.
func remoteThreadExitCode(thread windows.Handle) (uint32, error) {
	var exitCode uint32
	r1, _, callErr := procGetExitCodeThread.Call(
		uintptr(thread),
		uintptr(unsafe.Pointer(&exitCode)),
	)
	if r1 == 0 {
		return 0, nativeError(ErrRemoteCallFailed, "GetExitCodeThread", callErr)
	}
	return exitCode, nil
}

// getModuleHandle resuelve GetModuleHandleW mediante LazyProc porque el
// paquete x/sys/windows solo expone en esta versión la variante Ex.
func getModuleHandle(name string) (windows.Handle, error) {
	moduleName, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	r1, _, callErr := procGetModuleHandleW.Call(uintptr(unsafe.Pointer(moduleName)))
	runtime.KeepAlive(moduleName)
	if r1 == 0 {
		return 0, nativeError(ErrModuleEnumeration, "GetModuleHandleW", callErr)
	}
	return windows.Handle(r1), nil
}

// nativeError homogeneiza los fallos de LazyProc.Call, que pueden no traer un
// error útil aunque el valor de retorno indique FALSE/NULL.
func nativeError(kind error, operation string, callErr error) error {
	if callErr != nil {
		return fmt.Errorf("%w: %s: %v", kind, operation, callErr)
	}
	return fmt.Errorf("%w: %s", kind, operation)
}
