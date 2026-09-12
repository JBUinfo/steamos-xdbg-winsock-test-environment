// Package cleanup contiene una pequeña abstracción para trasladar el patrón
// RAII de C++ a Go. En Go la herramienta habitual es defer; Guard permite
// agrupar esa liberación y mantenerla idempotente cuando una operación tiene
// varias salidas por error.
package cleanup

import "sync"

// Guard ejecuta release una sola vez.
//
// Es el equivalente práctico, para los recursos usados por el inyector, de
// CEnsureCleanup y sus clases EnsureReleaseRegion/EnsureReleaseRegionEx. El
// paquete no conoce el tipo de recurso: el llamador decide cómo cerrarlo.
type Guard struct {
	mu      sync.Mutex
	active  bool
	release func() error
}

// New crea un guard activo. Si release es nil, Close sigue siendo seguro y no
// hace nada; esto facilita construir guards de forma condicional.
func New(release func() error) *Guard {
	return &Guard{
		active:  release != nil,
		release: release,
	}
}

// Close libera el recurso asociado exactamente una vez.
//
// La función de liberación se extrae bajo el mutex, pero se ejecuta fuera de
// él. Así una API nativa que bloquee o llame indirectamente a otro método no
// mantiene bloqueado el estado del guard durante toda la operación.
func (g *Guard) Close() error {
	if g == nil {
		return nil
	}

	g.mu.Lock()
	if !g.active {
		g.mu.Unlock()
		return nil
	}
	g.active = false
	release := g.release
	g.release = nil
	g.mu.Unlock()

	if release == nil {
		return nil
	}
	return release()
}

// Dismiss desactiva la liberación automática. Se usa cuando la propiedad del
// recurso se transfiere a otra parte del programa.
func (g *Guard) Dismiss() {
	if g == nil {
		return
	}

	g.mu.Lock()
	g.active = false
	g.release = nil
	g.mu.Unlock()
}

// Active informa de si Close todavía tiene una función pendiente.
func (g *Guard) Active() bool {
	if g == nil {
		return false
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	return g.active
}
