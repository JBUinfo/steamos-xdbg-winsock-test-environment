//go:build windows && amd64

package main

import "github.com/spf13/cobra"

// newEjectCommand crea el subcomando Cobra que descarga DLLs del proceso.
func newEjectCommand(targets *targetFlags) *cobra.Command {
	command := &cobra.Command{
		Use:   "eject [modules...]",
		Short: "expulsa una o más DLLs",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, modules []string) error {
			return runAction(command, "eject", targets, modules)
		},
	}
	addTargetFlags(command, targets)
	return command
}
