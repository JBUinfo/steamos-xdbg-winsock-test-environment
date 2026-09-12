//go:build windows && amd64

package textutil

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	// Estas dos funciones son las equivalentes de ConvertWideToANSI y
	// ConvertAnsiToWide. CP_ACP (0) significa la página ANSI activa del sistema.
	kernel32    = windows.NewLazySystemDLL("kernel32.dll")
	wideToMulti = kernel32.NewProc("WideCharToMultiByte")
	multiToWide = kernel32.NewProc("MultiByteToWideChar")
)

// UTF8ToUTF16 convierte una cadena Go UTF-8 en una cadena UTF-16 terminada en
// cero, lista para pasarse a una API W de Windows.
func UTF8ToUTF16(s string) ([]uint16, error) {
	return windows.UTF16FromString(s)
}

// UTF16ToString elimina el terminador y convierte una secuencia UTF-16 a una
// cadena Go. Es útil para buffers rellenados por Windows.
func UTF16ToString(s []uint16) string {
	return windows.UTF16ToString(s)
}

// ConvertWideToANSI reproduce la conversión de una cadena wide a la página
// ANSI del sistema (CP_ACP), que era el comportamiento del helper C++.
func ConvertWideToANSI(wide []uint16) (string, error) {
	if len(wide) == 0 {
		return "", nil
	}

	// Las llamadas con longitud -1 esperan un terminador. No modificamos el
	// slice del llamador; añadimos una copia solo si hace falta.
	input := wide
	if input[len(input)-1] != 0 {
		input = append(append([]uint16(nil), input...), 0)
	}

	count, _, callErr := wideToMulti.Call(
		0, // CP_ACP.
		0, // dwFlags.
		uintptr(unsafe.Pointer(&input[0])),
		^uintptr(0), // -1 como cchWideChar: la entrada está terminada en cero.
		0,
		0,
		0,
		0,
	)
	if count == 0 {
		if callErr != nil {
			return "", callErr
		}
		return "", fmt.Errorf("WideCharToMultiByte no produjo salida")
	}

	output := make([]byte, count)
	written, _, callErr := wideToMulti.Call(
		0,
		0,
		uintptr(unsafe.Pointer(&input[0])),
		^uintptr(0),
		uintptr(unsafe.Pointer(&output[0])),
		count,
		0,
		0,
	)
	runtime.KeepAlive(input)
	if written == 0 {
		if callErr != nil {
			return "", callErr
		}
		return "", fmt.Errorf("WideCharToMultiByte no pudo convertir la salida")
	}

	// Con longitud -1 Windows incluye el byte NUL; no lo exponemos como parte
	// de la cadena Go.
	if output[written-1] == 0 {
		written--
	}
	return string(output[:written]), nil
}

// ConvertANSIToWide convierte bytes de la página ANSI activa a UTF-16. Se
// conserva para cubrir la utilidad inversa del código original, aunque el CLI
// moderno trabaja internamente con UTF-8 y usa UTF8ToUTF16 para las APIs W.
func ConvertANSIToWide(ansi string) ([]uint16, error) {
	input, err := windows.ByteSliceFromString(ansi)
	if err != nil {
		return nil, err
	}

	count, _, callErr := multiToWide.Call(
		0, // CP_ACP.
		0, // dwFlags.
		uintptr(unsafe.Pointer(&input[0])),
		^uintptr(0), // -1 como cbMultiByte.
		0,
		0,
	)
	if count == 0 {
		if callErr != nil {
			return nil, callErr
		}
		return nil, fmt.Errorf("MultiByteToWideChar no produjo salida")
	}

	output := make([]uint16, count)
	written, _, callErr := multiToWide.Call(
		0,
		0,
		uintptr(unsafe.Pointer(&input[0])),
		^uintptr(0),
		uintptr(unsafe.Pointer(&output[0])),
		count,
	)
	runtime.KeepAlive(input)
	if written == 0 {
		if callErr != nil {
			return nil, callErr
		}
		return nil, fmt.Errorf("MultiByteToWideChar no pudo convertir la salida")
	}

	return output[:written], nil
}
