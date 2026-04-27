package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/skaledata/cli/internal/api"
	"github.com/spf13/cobra"
)

var clustersCmd = &cobra.Command{
	Use:     "clusters",
	Aliases: []string{"cluster"},
	Short:   "Manage clusters",
}

var clustersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all clusters",
	RunE:  runClustersList,
}

var clustersCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new cluster",
	RunE:  runClustersCreate,
}

var clustersStatusCmd = &cobra.Command{
	Use:   "status <cluster-id>",
	Short: "Show detailed cluster status",
	Args:  cobra.ExactArgs(1),
	RunE:  runClustersStatus,
}

var clustersDestroyCmd = &cobra.Command{
	Use:   "destroy <cluster-id>",
	Short: "Destroy a cluster",
	Args:  cobra.ExactArgs(1),
	RunE:  runClustersDestroy,
}

func init() {
	rootCmd.AddCommand(clustersCmd)
	clustersCmd.AddCommand(clustersListCmd)
	clustersCmd.AddCommand(clustersCreateCmd)
	clustersCmd.AddCommand(clustersStatusCmd)
	clustersCmd.AddCommand(clustersDestroyCmd)

	// create flags
	clustersCreateCmd.Flags().String("name", "", "Cluster name (lowercase, hyphens allowed)")
	clustersCreateCmd.Flags().String("cloud", "", "Cloud ID to deploy into")
	clustersCreateCmd.Flags().String("region", "us-central1", "Cloud region")
	clustersCreateCmd.Flags().StringSlice("apps", nil, "Apps to enable (airflow,airbyte,docs,slackbot,superset,datahub)")
}

func runClustersList(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	var clusters []api.Cluster
	if err := client.Get("/clusters", &clusters); err != nil {
		return err
	}

	if len(clusters) == 0 {
		fmt.Println("No clusters found. Create one with: skaledata clusters create")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tSTATUS\tREGION\tAPPS\tCREATED")
	for _, c := range clusters {
		apps := strings.Join(c.EnabledApps(), ",")
		if apps == "" {
			apps = "-"
		}
		age := timeAgo(c.CreatedAt)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			c.PublicID, c.Name, c.Status, c.Region, apps, age)
	}
	return w.Flush()
}

func runClustersCreate(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	name, _ := cmd.Flags().GetString("name")
	cloudID, _ := cmd.Flags().GetString("cloud")
	region, _ := cmd.Flags().GetString("region")
	apps, _ := cmd.Flags().GetStringSlice("apps")

	// Interactive prompts for missing required fields
	if cloudID == "" {
		cloudID, err = promptCloudSelection(client)
		if err != nil {
			return err
		}
	}

	if name == "" {
		name, err = promptInput("Cluster name")
		if err != nil {
			return err
		}
	}

	if len(apps) == 0 {
		apps, err = promptAppSelection()
		if err != nil {
			return err
		}
	}

	// Get default scaffold version
	var versions api.AllowedVersionsResponse
	if err := client.Get("/clusters/allowed-versions", &versions); err != nil {
		return fmt.Errorf("get scaffold versions: %w", err)
	}

	req := api.ClusterCreateRequest{
		CloudID:         cloudID,
		Name:            name,
		Region:          region,
		ScaffoldVersion: versions.DefaultVersion,
	}
	for _, app := range apps {
		switch strings.TrimSpace(strings.ToLower(app)) {
		case "airflow":
			req.EnableAirflow = true
		case "airbyte":
			req.EnableAirbyte = true
		case "docs":
			req.EnableDocs = true
		case "slackbot":
			req.EnableSlackbot = true
		case "superset":
			req.EnableSuperset = true
		case "datahub":
			req.EnableDatahub = true
		}
	}

	var cluster api.Cluster
	if err := client.Post("/clusters", req, &cluster); err != nil {
		return err
	}

	fmt.Printf("Cluster %q created (ID: %s, status: %s)\n", cluster.Name, cluster.PublicID, cluster.Status)
	fmt.Printf("Track progress: skaledata clusters status %s\n", cluster.PublicID)
	return nil
}

func runClustersStatus(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	clusterID := args[0]
	var cluster api.Cluster
	if err := client.Get("/clusters/"+clusterID, &cluster); err != nil {
		return err
	}

	fmt.Printf("Name:     %s\n", cluster.Name)
	fmt.Printf("ID:       %s\n", cluster.PublicID)
	fmt.Printf("Status:   %s\n", cluster.Status)
	fmt.Printf("Region:   %s\n", cluster.Region)
	fmt.Printf("Version:  %s\n", cluster.ScaffoldVersion)
	fmt.Printf("Apps:     %s\n", strings.Join(cluster.EnabledApps(), ", "))
	if cluster.ClusterEndpoint != nil {
		fmt.Printf("Endpoint: %s\n", *cluster.ClusterEndpoint)
	}
	if cluster.ArtifactRegistryURL != nil {
		fmt.Printf("Registry: %s\n", *cluster.ArtifactRegistryURL)
	}
	if cluster.ErrorMessage != nil {
		fmt.Printf("Error:    %s\n", *cluster.ErrorMessage)
	}
	fmt.Printf("Created:  %s\n", cluster.CreatedAt.Format(time.RFC3339))
	if cluster.LastAppliedAt != nil {
		fmt.Printf("Applied:  %s\n", cluster.LastAppliedAt.Format(time.RFC3339))
	}

	// Show latest job
	var job *api.Job
	if err := client.Get("/clusters/"+clusterID+"/latest-job", &job); err == nil && job != nil {
		fmt.Printf("\nLatest job:\n")
		fmt.Printf("  Type:   %s\n", job.Type)
		fmt.Printf("  Status: %s\n", job.Status)
		if job.ErrorMessage != nil {
			fmt.Printf("  Error:  %s\n", *job.ErrorMessage)
		}
	}

	return nil
}

func runClustersDestroy(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	clusterID := args[0]

	// Get cluster info for confirmation prompt
	var cluster api.Cluster
	if err := client.Get("/clusters/"+clusterID, &cluster); err != nil {
		return err
	}

	fmt.Printf("This will destroy cluster %q (%s) in region %s.\n", cluster.Name, cluster.PublicID, cluster.Region)
	fmt.Printf("Type the cluster name to confirm: ")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	confirmation := strings.TrimSpace(scanner.Text())

	if confirmation != cluster.Name {
		return fmt.Errorf("confirmation failed — expected %q, got %q", cluster.Name, confirmation)
	}

	var result map[string]string
	if err := client.Delete("/clusters/"+clusterID, &result); err != nil {
		return err
	}

	fmt.Printf("Destroy initiated for cluster %q. Track progress: skaledata clusters status %s\n",
		cluster.Name, cluster.PublicID)
	return nil
}

// --- Interactive prompts ---

func promptCloudSelection(client *api.Client) (string, error) {
	var clouds []api.Cloud
	if err := client.Get("/clouds", &clouds); err != nil {
		return "", fmt.Errorf("list clouds: %w", err)
	}
	if len(clouds) == 0 {
		return "", fmt.Errorf("no clouds connected — connect one at https://app.dev.skaledata.com")
	}
	if len(clouds) == 1 {
		fmt.Printf("Using cloud: %s (%s)\n", clouds[0].DisplayName, clouds[0].Provider)
		return clouds[0].ID, nil
	}

	fmt.Println("Select a cloud:")
	for i, c := range clouds {
		fmt.Printf("  [%d] %s (%s)\n", i+1, c.DisplayName, c.Provider)
	}
	fmt.Print("Choice: ")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	var idx int
	if _, err := fmt.Sscanf(scanner.Text(), "%d", &idx); err != nil || idx < 1 || idx > len(clouds) {
		return "", fmt.Errorf("invalid selection")
	}
	return clouds[idx-1].ID, nil
}

func promptInput(label string) (string, error) {
	fmt.Printf("%s: ", label)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	val := strings.TrimSpace(scanner.Text())
	if val == "" {
		return "", fmt.Errorf("%s cannot be empty", label)
	}
	return val, nil
}

func promptAppSelection() ([]string, error) {
	available := []string{"airflow", "airbyte", "docs", "superset", "datahub", "slackbot"}
	fmt.Println("Select apps to enable (comma-separated):")
	for i, a := range available {
		fmt.Printf("  [%d] %s\n", i+1, a)
	}
	fmt.Print("Apps (e.g. 1,2): ")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	input := strings.TrimSpace(scanner.Text())
	if input == "" {
		return []string{"airflow"}, nil // default to airflow
	}

	var selected []string
	for _, part := range strings.Split(input, ",") {
		part = strings.TrimSpace(part)
		var idx int
		if _, err := fmt.Sscanf(part, "%d", &idx); err == nil && idx >= 1 && idx <= len(available) {
			selected = append(selected, available[idx-1])
		} else {
			// Try as a name
			for _, a := range available {
				if strings.EqualFold(part, a) {
					selected = append(selected, a)
					break
				}
			}
		}
	}
	if len(selected) == 0 {
		selected = []string{"airflow"}
	}
	return selected, nil
}

// timeAgo returns a human-readable relative time string.
func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
