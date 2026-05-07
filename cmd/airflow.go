package cmd

import (
	"fmt"
	"os"

	"github.com/skaledata/cli/internal/airflow"
	"github.com/skaledata/cli/internal/api"
	"github.com/skaledata/cli/internal/prompt"
	"github.com/spf13/cobra"
)

var airflowCmd = &cobra.Command{
	Use:     "airflow",
	Aliases: []string{"af"},
	Short:   "Develop Airflow locally",
	Long:    `Create, run, and manage a local Airflow development environment using Docker.`,
}

var airflowInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a new Airflow project in the current directory",
	Long: `Scaffolds a new Airflow 3 project with:
  - Dockerfile (apache/airflow:3.1.7)
  - dags/ with an example DAG
  - requirements.txt, .gitignore, .dockerignore

Docker Compose is managed by the CLI — no compose file in your project.`,
	RunE: runAirflowInit,
}

var airflowStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Build and start Airflow locally",
	Long:  `Builds the Docker image from your Dockerfile, starts all services, and waits for the webserver to be healthy.`,
	RunE:  runAirflowStart,
}

var airflowStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop all Airflow containers",
	Long:  `Gracefully stops all running containers. Preserves volumes and data — use 'skale airflow start' to resume.`,
	RunE:  runAirflowStop,
}

var airflowRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart all Airflow containers",
	Long:  `Stops and restarts all containers without rebuilding. Preserves volumes and data. Useful after config changes.`,
	RunE:  runAirflowRestart,
}

var airflowKillCmd = &cobra.Command{
	Use:   "kill",
	Short: "Kill all Airflow containers and delete volumes",
	Long:  `Stops and removes all containers, networks, and volumes. This deletes your local Postgres data. Use 'skale airflow init' + 'start' to start fresh.`,
	RunE:  runAirflowKill,
}

var airflowBashCmd = &cobra.Command{
	Use:   "bash [container]",
	Short: "Open a shell in an Airflow container",
	Long:  `Opens an interactive bash shell. Defaults to the scheduler container. Valid containers: scheduler, webserver, triggerer, postgres.`,
	Args:  cobra.MaximumNArgs(1),
	RunE:  runAirflowBash,
}

var airflowDeployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Build and deploy to a SkaleData cluster",
	Long: `Builds the Docker image from your project's Dockerfile, pushes it to
the cluster's container registry, and triggers a rolling deploy on the
target Airflow application.

The first time you run this, pass --cluster to specify the target.
The binding is saved to .skaledata.yaml so subsequent deploys just need:
  skale airflow deploy`,
	RunE: runAirflowDeploy,
}

var airflowRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Refresh secrets backend credentials",
	Long: `Re-mints short-lived cloud credentials for the secrets backend without
restarting containers. For GCP/Azure, running containers pick up the new
credential file automatically. For AWS, containers are restarted to pick
up the new environment variables.

Requires the project to be bound to a deployed instance (.skaledata.yaml).`,
	RunE: runAirflowRefresh,
}

var airflowRunCmd = &cobra.Command{
	Use:                "run [airflow-command] [args...]",
	Short:              "Run an Airflow CLI command",
	Long:               `Executes an Airflow CLI command inside the scheduler container. Example: skale airflow run dags list`,
	DisableFlagParsing: true,
	RunE:               runAirflowRun,
}

func init() {
	rootCmd.AddCommand(airflowCmd)
	airflowCmd.AddCommand(airflowInitCmd)
	airflowCmd.AddCommand(airflowStartCmd)
	airflowCmd.AddCommand(airflowStopCmd)
	airflowCmd.AddCommand(airflowRestartCmd)
	airflowCmd.AddCommand(airflowKillCmd)
	airflowCmd.AddCommand(airflowBashCmd)
	airflowCmd.AddCommand(airflowRunCmd)
	airflowCmd.AddCommand(airflowRefreshCmd)
	airflowCmd.AddCommand(airflowDeployCmd)

	airflowDeployCmd.Flags().String("cluster", "", "Cluster ID (saved to .skaledata.yaml after first use)")
	airflowDeployCmd.Flags().String("app", "", "Airflow instance name (for clusters with multiple Airflows)")
	airflowDeployCmd.Flags().String("tag", "", "Image tag (defaults to git SHA)")
	airflowDeployCmd.Flags().Bool("force-image", false, "Force a full image build even if only DAGs changed")
	airflowDeployCmd.Flags().Bool("force-dags", false, "Force a DAG-only sync even if image files changed")
}

func runAirflowInit(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	// Check if project already exists
	if airflow.IsProject(dir) {
		return fmt.Errorf("Airflow project already exists in this directory. Use 'skale airflow start' to run it.")
	}

	fmt.Println("Initializing Airflow project...")
	if err := airflow.Init(dir); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Airflow project created! Next steps:")
	fmt.Println("  skale airflow start     Start Airflow locally")
	fmt.Println("  Edit dags/              Add your DAGs")
	fmt.Println("  Edit requirements.txt   Add Python dependencies")
	return nil
}

func runAirflowStart(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if !airflow.IsProject(dir) {
		return fmt.Errorf("No Airflow project found. Run 'skale airflow init' first.")
	}

	// If project is bound to a deployed instance, fetch secrets
	var opts *airflow.StartOpts
	cfg := airflow.LoadConfig(dir)
	if cfg != nil && cfg.Cluster != "" {
		client, err := api.NewClient()
		if err == nil {
			appID, resolveErr := resolveAppID(client, cfg.Cluster, cfg.App)
			if resolveErr == nil && appID != "" {
				opts = &airflow.StartOpts{Client: client, AppID: appID}
			}
		}
	}

	return airflow.Start(dir, opts)
}

func runAirflowStop(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if !airflow.IsProject(dir) {
		return fmt.Errorf("No Airflow project found in the current directory.")
	}
	return airflow.Stop(dir)
}

func runAirflowRestart(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if !airflow.IsProject(dir) {
		return fmt.Errorf("No Airflow project found in the current directory.")
	}
	return airflow.Restart(dir)
}

func runAirflowKill(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if !airflow.IsProject(dir) {
		return fmt.Errorf("No Airflow project found in the current directory.")
	}
	return airflow.Kill(dir)
}

func runAirflowBash(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if !airflow.IsProject(dir) {
		return fmt.Errorf("No Airflow project found in the current directory.")
	}

	container := "scheduler"
	if len(args) > 0 {
		container = args[0]
	}

	return airflow.Bash(dir, container)
}

func runAirflowRun(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if !airflow.IsProject(dir) {
		return fmt.Errorf("No Airflow project found in the current directory.")
	}

	if len(args) == 0 {
		return fmt.Errorf("Specify an Airflow command. Example: skale airflow run dags list")
	}

	return airflow.Run(dir, args)
}

func runAirflowRefresh(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if !airflow.IsProject(dir) {
		return fmt.Errorf("No Airflow project found in the current directory.")
	}

	cfg := airflow.LoadConfig(dir)
	if cfg == nil || cfg.Cluster == "" {
		return fmt.Errorf("Project is not bound to a deployed instance. Run 'skale airflow deploy --cluster <id>' first.")
	}

	client, err := api.NewClient()
	if err != nil {
		return err
	}

	appID, err := resolveAppID(client, cfg.Cluster, cfg.App)
	if err != nil {
		return fmt.Errorf("resolve application: %w", err)
	}

	resp, err := airflow.RefreshSecrets(dir, client, appID)
	if err != nil {
		return err
	}

	// AWS uses env vars, so containers need a restart to pick them up
	if resp.CredentialType == "aws_sts" {
		fmt.Println("  Restarting Airflow services to pick up new AWS credentials...")
		if err := airflow.RestartServices(dir); err != nil {
			return fmt.Errorf("restart failed: %w", err)
		}
	}

	fmt.Println("Credentials refreshed.")
	return nil
}

func runAirflowDeploy(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if !airflow.IsProject(dir) {
		return fmt.Errorf("No Airflow project found. Run 'skale airflow init' first.")
	}

	client, err := api.NewClient()
	if err != nil {
		return err
	}

	clusterID, _ := cmd.Flags().GetString("cluster")
	appName, _ := cmd.Flags().GetString("app")
	tag, _ := cmd.Flags().GetString("tag")
	forceImage, _ := cmd.Flags().GetBool("force-image")
	forceDags, _ := cmd.Flags().GetBool("force-dags")

	// Read saved config
	cfg := airflow.LoadConfig(dir)
	if clusterID == "" && cfg != nil {
		clusterID = cfg.Cluster
	}
	if appName == "" && cfg != nil {
		appName = cfg.App
	}

	// Interactive cluster selection if still empty
	if clusterID == "" {
		clusterID, err = selectCluster(client)
		if err != nil {
			return err
		}
	}

	// Resolve airflow instance on the cluster
	if appName == "" {
		appName, err = selectAirflowApp(client, clusterID)
		if err != nil {
			return err
		}
	}

	// Save bindings for next time
	if err := airflow.SaveConfig(dir, &airflow.ProjectConfig{
		Cluster: clusterID,
		App:     appName,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save .skaledata.yaml: %v\n", err)
	}

	return airflow.Deploy(dir, client, airflow.DeployOpts{
		ClusterID:  clusterID,
		AppName:    appName,
		Tag:        tag,
		ForceImage: forceImage,
		ForceDags:  forceDags,
	})
}

// selectCluster prompts the user to pick a ready cluster.
func selectCluster(client *api.Client) (string, error) {
	var clusters []api.Cluster
	if err := client.Get("/clusters", &clusters); err != nil {
		return "", fmt.Errorf("list clusters: %w", err)
	}
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
	options := make([]prompt.Option, len(ready))
	for i, c := range ready {
		options[i] = prompt.Option{
			Label: fmt.Sprintf("%s (%s)", c.Name, c.Region),
			Value: c.PublicID,
		}
	}
	return prompt.Select("Select a cluster:", options)
}

// resolveAppID finds the internal application UUID for a given cluster + app name.
func resolveAppID(client *api.Client, clusterPublicID, appName string) (string, error) {
	// Get the cluster's internal ID
	var cluster api.Cluster
	if err := client.Get("/clusters/"+clusterPublicID, &cluster); err != nil {
		return "", err
	}

	var allApps []api.Application
	if err := client.Get("/applications", &allApps); err != nil {
		return "", err
	}

	for _, a := range allApps {
		if a.ClusterID != cluster.ID || a.AppType != "airflow" {
			continue
		}
		// Match by name, or take the first one if no name specified
		if appName == "" || a.Name == appName {
			return a.ID, nil
		}
	}
	return "", fmt.Errorf("no matching Airflow application found")
}

// selectAirflowApp finds airflow apps on a cluster and prompts if there are multiple.
func selectAirflowApp(client *api.Client, clusterPublicID string) (string, error) {
	// Get the cluster's internal ID (applications reference internal UUID, not public_id)
	var cluster api.Cluster
	if err := client.Get("/clusters/"+clusterPublicID, &cluster); err != nil {
		return "", fmt.Errorf("get cluster: %w", err)
	}

	var allApps []api.Application
	if err := client.Get("/applications", &allApps); err != nil {
		return "", fmt.Errorf("list applications: %w", err)
	}

	// Filter to airflow apps on this cluster
	var airflowApps []api.Application
	for _, a := range allApps {
		if a.AppType == "airflow" && a.ClusterID == cluster.ID {
			airflowApps = append(airflowApps, a)
		}
	}

	if len(airflowApps) == 0 {
		return "", fmt.Errorf("no Airflow applications found on this cluster")
	}
	if len(airflowApps) == 1 {
		fmt.Printf("Targeting: %s\n", airflowApps[0].Name)
		return airflowApps[0].Name, nil
	}

	// Multiple airflow instances — let user pick
	options := make([]prompt.Option, len(airflowApps))
	for i, a := range airflowApps {
		label := fmt.Sprintf("%s (%s)", a.Name, a.Status)
		options[i] = prompt.Option{Label: label, Value: a.Name}
	}
	return prompt.Select("Multiple Airflow instances found. Select target:", options)
}
