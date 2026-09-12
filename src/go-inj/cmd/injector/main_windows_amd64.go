//go:build windows && amd64

// Este es el punto de entrada del ejecutable. La definición de comandos vive
// en los ficheros vecinos; main solo ejecuta Cobra y traduce el error final a
// un código de salida de proceso.
package main

import (
	"fmt"
	"os"
)

// main crea el árbol Cobra y lo ejecuta con los argumentos del proceso.
func main() {
	if err := newRootCommand().Execute(); err != nil {
		// SilenceErrors se configura en root para evitar que Cobra imprima dos
		// veces el mismo texto. Aquí queda un único mensaje consistente.
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(2)
	}
}
