package cmd

import (
	"github.com/spf13/cobra"

	"github.com/openzro/openzro/version"
)

var (
	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "prints Openzro version",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.SetOut(cmd.OutOrStdout())
			cmd.Println(version.OpenzroVersion())
		},
	}
)
