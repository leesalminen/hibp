package main

import (
	"fmt"
	"os"

	"github.com/ory/viper"
	"github.com/leesalminen/hibp/cmd/dataimport"
	"github.com/leesalminen/hibp/cmd/migrate"
	"github.com/leesalminen/hibp/cmd/serve"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "hibp",
	Short: "Self hosted hibp password checker",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
		os.Exit(1)
	},
}

func init() {
	rootCmd.AddCommand(dataimport.Command)
	rootCmd.AddCommand(migrate.Command)
	rootCmd.AddCommand(serve.Command)
	cobra.OnInitialize(func() {
		viper.AutomaticEnv()
	})
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprint(os.Stderr, "error executing program, reason", err.Error())
		os.Exit(1)
	}
}
