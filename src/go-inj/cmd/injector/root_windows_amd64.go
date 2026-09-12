//go:build windows && amd64

package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"go-inj/internal/diagnostics"
	"go-inj/internal/injector"
)

const (
	// version conserva la versión que mostraba Main.cpp. Cobra la expone con
	// --version y la incluye en la ayuda generada.
	version = "20260228"
)

// targetFlags agrupa las cuatro formas de seleccionar el proceso objetivo.
// Se comparte entre inject y eject para que ambos comandos tengan exactamente
// la misma interfaz.
type targetFlags struct {
	processName   string
	caseSensitive bool
	windowName    string
	processID     uint32
	processIDSet  bool
}

// newRootCommand construye el comando raíz y sus dos subcomandos de acción.
// Toda la lógica de Win32 queda fuera de aquí, en internal/injector.
func newRootCommand() *cobra.Command {
	var targets targetFlags
	var crashDumpDir string

	root := &cobra.Command{
		Use:           "injector",
		Short:         "inyecta o expulsa DLLs de procesos Windows x64",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
		Long: `Injector x64: carga o descarga una DLL en un proceso existente.

Selecciona el proceso con --process-name, --window-name o --process-id y usa
uno de los subcomandos inject o eject para elegir la acción.`,
	}

	// El filtro es opcional porque la traducción SEH->excepción C++ no existe
	// como tal en Go. Cuando se solicita, se instala el minidump filter Win32.
	root.PersistentFlags().StringVar(&crashDumpDir, "crash-dump-dir", "", "directorio opcional para minidumps de excepciones")
	root.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		if crashDumpDir == "" {
			return nil
		}
		absolute, err := filepath.Abs(crashDumpDir)
		if err != nil {
			return fmt.Errorf("resolver --crash-dump-dir: %w", err)
		}
		if _, err := diagnostics.InstallUnhandledExceptionDump(absolute); err != nil {
			return err
		}
		return nil
	}

	root.AddCommand(
		newInjectCommand(&targets),
		newEjectCommand(&targets),
	)
	return root
}

// addTargetFlags añade a un comando los flags que identifican el proceso.
func addTargetFlags(command *cobra.Command, targets *targetFlags) {
	flags := command.Flags()
	flags.StringVarP(&targets.processName, "process-name", "n", "", "nombre del ejecutable del proceso")
	flags.BoolVarP(&targets.caseSensitive, "case-sensitive", "c", false, "distinguir mayúsculas en --process-name")
	flags.StringVarP(&targets.windowName, "window-name", "w", "", "título exacto de la ventana")
	flags.Uint32VarP(&targets.processID, "process-id", "p", 0, "PID numérico del proceso")
}

// resolve selecciona un único método de búsqueda y devuelve el PID resultante.
// Rechazar selectores simultáneos evita la ambigüedad que en el C++ original se
// resolvía accidentalmente sobrescribiendo ProcID en el orden de los if.
func (targets *targetFlags) resolve(service *injector.Injector) (uint32, error) {
	selected := 0
	if targets.processName != "" {
		selected++
	}
	if targets.windowName != "" {
		selected++
	}
	if targets.processIDSet {
		selected++
	}
	if selected == 0 {
		return 0, fmt.Errorf("debes especificar --process-name, --window-name o --process-id")
	}
	if selected > 1 {
		return 0, fmt.Errorf("solo se puede especificar un identificador de proceso")
	}
	if targets.processIDSet && targets.processID == 0 {
		return 0, injector.ErrInvalidProcessID
	}

	switch {
	case targets.processName != "":
		return service.GetProcessIdByName(targets.processName, targets.caseSensitive)
	case targets.windowName != "":
		return service.GetProcessIdByWindow(targets.windowName)
	default:
		return targets.processID, nil
	}
}

// runAction contiene el flujo común de inject/eject: resolver el PID, activar
// el privilegio y procesar todos los módulos en el orden recibido.
func runAction(command *cobra.Command, action string, targets *targetFlags, modules []string) error {
	// Changed distingue --process-id 0 de la ausencia total del flag.
	targets.processIDSet = command.Flags().Changed("process-id")
	service := injector.New()

	pid, err := targets.resolve(service)
	if err != nil {
		return err
	}
	if err := service.GetSeDebugPrivilege(); err != nil {
		return err
	}

	for _, module := range modules {
		switch action {
		case "inject":
			// Para inyectar necesitamos una ruta existente y absoluta. Eject no
			// hace esta resolución porque el archivo puede haber desaparecido.
			path, err := service.GetPath(module)
			if err != nil {
				return err
			}
			if err := service.InjectLib(pid, path); err != nil {
				return fmt.Errorf("inyectar %q en PID %d: %w", path, pid, err)
			}
			fmt.Fprintf(command.OutOrStdout(), "Successfully injected module: %s\n", path)
		case "eject":
			if err := service.EjectLib(pid, module); err != nil {
				return fmt.Errorf("expulsar %q de PID %d: %w", module, pid, err)
			}
			fmt.Fprintf(command.OutOrStdout(), "Successfully ejected module: %s\n", module)
		default:
			return fmt.Errorf("acción desconocida: %s", action)
		}
	}
	return nil
}
