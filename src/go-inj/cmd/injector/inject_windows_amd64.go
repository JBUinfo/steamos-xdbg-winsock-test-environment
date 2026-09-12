//go:build windows && amd64

package main

import "github.com/spf13/cobra"

// newInjectCommand crea el subcomando Cobra que carga DLLs en el proceso.
func newInjectCommand(targets *targetFlags) *cobra.Command {
	command := &cobra.Command{
		Use:   "inject [modules...]",
		Short: "inyecta una o más DLLs",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, modules []string) error {
			return runAction(command, "inject", targets, modules)
		},
	}
	addTargetFlags(command, targets)
	return command
}
