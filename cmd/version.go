package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Build metadata, overridden at release time via -ldflags by GoReleaser:
//
//	-X github.com/dbuzatto/strix/cmd.version=...
//	-X github.com/dbuzatto/strix/cmd.commit=...
//	-X github.com/dbuzatto/strix/cmd.date=...
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the strix version",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := fmt.Printf("strix %s (commit %s, built %s, %s)\n",
			version, commit, date, runtime.Version())
		return err
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.Version = version
}
