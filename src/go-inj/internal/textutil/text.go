// Package textutil reúne las operaciones de texto que sustituyen a
// StringUtil.h, StringWrap.h y parte de UniUtil.h del proyecto C++.
package textutil

import "strings"

// Lower devuelve s en minúsculas usando las reglas Unicode de Go.
//
// El C++ aplicaba std::tolower sobre la locale actual. Para comparar nombres
// de procesos no necesitamos reproducir una locale global mutable: EqualFold
// ofrece además una comparación Unicode sin construir una segunda cadena.
func Lower(s string) string {
	return strings.ToLower(s)
}

// EqualFold compara dos cadenas sin distinguir mayúsculas/minúsculas.
func EqualFold(a, b string) bool {
	return strings.EqualFold(a, b)
}
