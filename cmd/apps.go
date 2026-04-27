package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/pkg/browser"
	"github.com/skaledata/cli/internal/api"
	"github.com/spf13/cobra"
)

var appsCmd = &cobra.Command{
	Use:     "apps",
	Aliases: []string{"app"},
	Short:   "Manage applications",
}

var appsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all applications",
	RunE:  runAppsList,
}

var appsAddCmd = &cobra.Command{
	Use:   "add <type>",
	Short: "Add an app to a cluster",
	Long:  `Add an application (airflow, airbyte, docs, superset, datahub, slackbot) to an existing cluster.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runAppsAdd,
}

var appsOpenCmd = &cobra.Command{
	Use:   "open <cluster-id>",
	Short: "Open an app in the browser",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppsOpen,
}

func init() {
	rootCmd.AddCommand(appsCmd)
	appsCmd.AddCommand(appsListCmd)
	appsCmd.AddCommand(appsAddCmd)
	appsCmd.AddCommand(appsOpenCmd)

	appsListCmd.Flags().String("cluster", "", "Filter by cluster ID")
	appsAddCmd.Flags().String("cluster", "", "Cluster ID (required)")
	appsAddCmd.Flags().String("name", "", "Instance name (defaults to app type)")
	appsOpenCmd.Flags().String("app-type", "airflow", "App type to open")
}

func runAppsList(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	var apps []api.Application
	if err := client.Get("/applications", &apps); err != nil {
		return err
	}

	clusterFilter, _ := cmd.Flags().GetString("cluster")

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tTYPE\tSTATUS\tCLUSTER\tCREATED")
	for _, a := range apps {
		if clusterFilter != "" && a.ClusterID != clusterFilter {
			continue
		}
		age := timeAgo(a.CreatedAt)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			a.ID, a.Name, a.AppType, a.Status, a.ClusterID[:8], age)
	}
	return w.Flush()
}

func runAppsAdd(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	appType := strings.ToLower(args[0])
	clusterID, _ := cmd.Flags().GetString("cluster")
	instanceName, _ := cmd.Flags().GetString("name")

	if clusterID == "" {
		clusterID, err = promptClusterSelection(client)
		if err != nil {
			return err
		}
	}

	body := &api.AddAppRequest{
		Name: instanceName,
	}

	var cluster api.Cluster
	if err := client.Post(fmt.Sprintf("/clusters/%s/add-app?app_type=%s", clusterID, appType), body, &cluster); err != nil {
		return err
	}

	fmt.Printf("Adding %s to cluster %q. An upgrade is in progress.\n", appType, cluster.Name)
	fmt.Printf("Track progress: skaledata clusters status %s\n", cluster.PublicID)
	return nil
}

func runAppsOpen(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	clusterID := args[0]
	appType, _ := cmd.Flags().GetString("app-type")

	var tokenResp api.DataPlaneTokenResponse
	if err := client.Post(
		fmt.Sprintf("/clusters/%s/data-plane-token?app_type=%s", clusterID, appType),
		nil, &tokenResp,
	); err != nil {
		return err
	}

	fmt.Printf("Opening %s...\n", appType)
	if err := browser.OpenURL(tokenResp.URL); err != nil {
		fmt.Fprintf(os.Stderr, "Could not open browser: %v\n", err)
		fmt.Printf("Open manually: %s\n", tokenResp.URL)
	}
	return nil
}

func promptClusterSelection(client *api.Client) (string, error) {
	var clusters []api.Cluster
	if err := client.Get("/clusters", &clusters); err != nil {
		return "", fmt.Errorf("list clusters: %w", err)
	}

	// Filter to ready clusters only
	var ready []api.Cluster
	for _, c := range clusters {
		if c.Status == "ready" {
			ready = append(ready, c)
		}
	}

	if len(ready) == 0 {
		return "", fmt.Errorf("no ready clusters found")
	}
	if len(ready) == 1 {
		fmt.Printf("Using cluster: %s\n", ready[0].Name)
		return ready[0].PublicID, nil
	}

	fmt.Println("Select a cluster:")
	for i, c := range ready {
		fmt.Printf("  [%d] %s (%s, %s)\n", i+1, c.Name, c.Region, c.Status)
	}
	fmt.Print("Choice: ")

	var idx int
	fmt.Scan(&idx)
	if idx < 1 || idx > len(ready) {
		return "", fmt.Errorf("invalid selection")
	}
	return ready[idx-1].PublicID, nil
}
