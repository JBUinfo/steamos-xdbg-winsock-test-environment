package injector

import "errors"

// Los errores sentinela permiten que la CLI distinga un fallo de selección de
// proceso, de ruta o de una operación Win32 sin tener que comparar textos.
var (
	ErrProcessNotFound      = errors.New("injector: no se encontró el proceso")
	ErrWindowNotFound       = errors.New("injector: no se encontró la ventana")
	ErrInvalidProcessID     = errors.New("injector: el process id no es válido")
	ErrModulePathNotFound   = errors.New("injector: no se encontró el módulo")
	ErrModuleNotLoaded      = errors.New("injector: el módulo no está cargado en el proceso")
	ErrInvalidRemoteProcess = errors.New("injector: no se pudo abrir el proceso objetivo")
	ErrRemoteAllocation     = errors.New("injector: no se pudo reservar memoria remota")
	ErrRemoteWrite          = errors.New("injector: no se pudo escribir en memoria remota")
	ErrRemoteThread         = errors.New("injector: no se pudo crear el hilo remoto")
	ErrRemoteCallFailed     = errors.New("injector: la llamada remota falló")
	ErrPrivilege            = errors.New("injector: no se pudo activar SeDebugPrivilege")
	ErrModuleEnumeration    = errors.New("injector: no se pudieron enumerar los módulos")
	ErrProcessEnumeration   = errors.New("injector: no se pudo crear o recorrer el snapshot de procesos")
	ErrWindowProcessID      = errors.New("injector: no se pudo obtener el process id de la ventana")
	ErrPathResolution       = errors.New("injector: no se pudo resolver la ruta del módulo")
)
