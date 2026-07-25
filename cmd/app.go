package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/skaledata/cli/internal/api"
	"github.com/skaledata/cli/internal/app"
	"github.com/spf13/cobra"
)

var appCmd = &cobra.Command{
	Use:   "app",
	Short: "Mint short-lived credentials for application APIs",
	Long: `Exchange your SkaleData session for a short-lived, app-scoped token that
authenticates directly to an application's own REST API (Airflow,
Airbyte, ...).

The token is scoped to a single app instance, carries your existing
permissions, and expires quickly. Prefer 'skale app exec' so the token
is injected into a subprocess environment and never touches disk or your
shell history.`,
}

var appTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Print a short-lived, app-scoped token",
	Long: `Mint a short-lived token for an application's REST API and print it to
stdout. Use --json to also get the base URL and expiry.

Example:
  URL=$(skale app token --app airflow --cluster <id> --json | jq -r .base_url)
  TOKEN=$(skale app token --app airflow --cluster <id>)
  curl "$URL/api/v1/dags" -H "Authorization: Bearer $TOKEN"

Prefer 'skale app exec' when possible — it keeps the token out of your
shell history and scrollback.`,
	RunE: runAppToken,
}

var appExecCmd = &cobra.Command{
	Use:   "exec --app <type> [--cluster <id>] -- <command> [args...]",
	Short: "Run a command with app credentials injected into its environment",
	Long: `Mint a short-lived, app-scoped token and run <command> with it injected
into the child process environment only — never written to disk or your
shell history. Sets SKALE_APP_URL / SKALE_APP_TOKEN plus app-specific
aliases (e.g. AIRFLOW_URL / AIRFLOW_TOKEN).

Put '--' before the command so its own flags aren't parsed by skale.

Example:
  skale app exec --app airflow --cluster <id> -- \
    curl "$AIRFLOW_URL/api/v1/dags" -H "Authorization: Bearer $AIRFLOW_TOKEN"`,
	Args: cobra.MinimumNArgs(1),
	RunE: runAppExec,
}

func init() {
	rootCmd.AddCommand(appCmd)
	appCmd.AddCommand(appTokenCmd)
	appCmd.AddCommand(appExecCmd)

	for _, c := range []*cobra.Command{appTokenCmd, appExecCmd} {
		c.Flags().String("app", "", "Application type: airflow, airbyte, ... (required)")
		c.Flags().String("cluster", "", "Cluster ID (prompts if omitted)")
		c.Flags().String("name", "", "App instance name (for clusters with multiple instances of a type)")
		c.Flags().String("ttl", "15m", "Requested token lifetime; server clamps to a max")
		_ = c.MarkFlagRequired("app")
	}
	appTokenCmd.Flags().Bool("json", false, "Print full JSON (token, base_url, expires_at)")
}

// mintAppTokenFromFlags resolves the target app from --app/--cluster/--name
// and mints a short-lived token for it.
func mintAppTokenFromFlags(cmd *cobra.Command) (*api.AppTokenResponse, error) {
	appType, _ := cmd.Flags().GetString("app")
	clusterID, _ := cmd.Flags().GetString("cluster")
	name, _ := cmd.Flags().GetString("name")
	ttlStr, _ := cmd.Flags().GetString("ttl")

	ttl, err := time.ParseDuration(ttlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid --ttl %q: %w", ttlStr, err)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("--ttl must be positive, got %q", ttlStr)
	}

	client, err := api.NewClient()
	if err != nil {
		return nil, err
	}

	if clusterID == "" {
		clusterID, err = selectCluster(client)
		if err != nil {
			return nil, err
		}
	}

	appID, err := app.ResolveAppID(client, clusterID, appType, name)
	if err != nil {
		return nil, err
	}

	return client.MintAppToken(appID, int(ttl.Seconds()))
}

func runAppToken(cmd *cobra.Command, args []string) error {
	tok, err := mintAppTokenFromFlags(cmd)
	if err != nil {
		return err
	}

	if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
		out, err := json.MarshalIndent(tok, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}

	fmt.Println(tok.Token)
	return nil
}

func runAppExec(cmd *cobra.Command, args []string) error {
	tok, err := mintAppTokenFromFlags(cmd)
	if err != nil {
		return err
	}

	code, err := app.Exec(tok, args)
	if err != nil {
		return err
	}
	if code != 0 {
		os.Exit(code)
	}
	return nil
}
