package cmd

import (
	"fmt"
	"os"

	"github.com/dbuzatto/strix/internal/config"
	"github.com/spf13/cobra"
)

var flagConfigForce bool

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage Strix configuration",
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the config file path",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(config.Path())
		return nil
	},
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Write a sample config file",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := config.WriteSample(flagConfigForce)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "strix: wrote %s\n", path)
		return nil
	},
}

func init() {
	configInitCmd.Flags().BoolVar(&flagConfigForce, "force", false, "overwrite an existing config file")
	configCmd.AddCommand(configPathCmd, configInitCmd)
	rootCmd.AddCommand(configCmd)
}
