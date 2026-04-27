package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "skale",
	Short: "SkaleData CLI — manage clusters, apps, and deployments",
	Long:  `The SkaleData CLI wraps the SkaleData API to let you create clusters, manage applications, deploy code, and scaffold new projects from your terminal.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default $HOME/.config/skaledata/config.yaml)")
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		configDir := home + "/.config/skaledata"
		viper.AddConfigPath(configDir)
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
	}

	viper.SetEnvPrefix("SKALEDATA")
	viper.AutomaticEnv()

	viper.SetDefault("api_url", "https://api.skaledata.com")

	_ = viper.ReadInConfig()
}
