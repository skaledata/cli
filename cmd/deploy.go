package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/skaledata/cli/internal/api"
	"go.yaml.in/yaml/v3"

	"github.com/spf13/cobra"
)

// SkaleDataConfig represents the .skaledata.yaml file.
type SkaleDataConfig struct {
	Cluster string `yaml:"cluster"`
	AppType string `yaml:"app_type"`
}

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Build, push, and deploy to your cluster",
	Long: `Reads .skaledata.yaml for cluster binding and app type, then:
  1. Gets registry credentials from the API
  2. Builds a Docker image
  3. Pushes to the cluster's container registry
  4. Triggers a rolling deploy

Flags override .skaledata.yaml values.`,
	RunE: runDeploy,
}

var deployStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the latest deploy job status",
	RunE:  runDeployStatus,
}

func init() {
	rootCmd.AddCommand(deployCmd)
	deployCmd.AddCommand(deployStatusCmd)

	deployCmd.Flags().String("cluster", "", "Cluster ID (overrides .skaledata.yaml)")
	deployCmd.Flags().String("tag", "", "Image tag (defaults to git SHA)")
	deployCmd.Flags().String("app-type", "", "App type (overrides .skaledata.yaml)")
	deployCmd.Flags().String("dockerfile", "Dockerfile", "Path to Dockerfile")
}

func loadProjectConfig() *SkaleDataConfig {
	data, err := os.ReadFile(".skaledata.yaml")
	if err != nil {
		return nil
	}
	var cfg SkaleDataConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return &cfg
}

func runDeploy(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	cfg := loadProjectConfig()

	clusterID, _ := cmd.Flags().GetString("cluster")
	tag, _ := cmd.Flags().GetString("tag")
	appType, _ := cmd.Flags().GetString("app-type")
	dockerfile, _ := cmd.Flags().GetString("dockerfile")

	// Resolve from .skaledata.yaml
	if clusterID == "" && cfg != nil {
		clusterID = cfg.Cluster
	}
	if appType == "" && cfg != nil {
		appType = cfg.AppType
	}
	if appType == "" {
		appType = "airflow"
	}

	if clusterID == "" {
		clusterID, err = promptClusterSelection(client)
		if err != nil {
			return err
		}
	}

	// Default tag to git short SHA
	if tag == "" {
		out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
		if err == nil {
			tag = strings.TrimSpace(string(out))
		} else {
			tag = fmt.Sprintf("deploy-%d", time.Now().Unix())
		}
	}

	// Step 1: Get registry credentials
	fmt.Println("Getting registry credentials...")
	var regToken api.RegistryTokenResponse
	if err := client.Post("/clusters/"+clusterID+"/registry-token", nil, &regToken); err != nil {
		return fmt.Errorf("get registry token: %w", err)
	}

	imageURI := fmt.Sprintf("%s/%s:%s", regToken.ArtifactRegistryURL, appType, tag)

	// Step 2: Docker login
	fmt.Printf("Logging into %s...\n", regToken.Registry)
	loginCmd := exec.Command("docker", "login", regToken.Registry,
		"-u", regToken.Username, "--password-stdin")
	loginCmd.Stdin = strings.NewReader(regToken.Token)
	loginCmd.Stderr = os.Stderr
	if err := loginCmd.Run(); err != nil {
		return fmt.Errorf("docker login failed: %w", err)
	}

	// Step 3: Docker build
	fmt.Printf("Building %s...\n", imageURI)
	buildCmd := exec.Command("docker", "build", "-f", dockerfile, "-t", imageURI, ".")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	// Step 4: Docker push
	fmt.Printf("Pushing %s...\n", imageURI)
	pushCmd := exec.Command("docker", "push", imageURI)
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("docker push failed: %w", err)
	}

	// Step 5: Trigger deploy via API
	fmt.Println("Triggering deploy...")
	var result api.DeployImageResponse
	if err := client.Post("/clusters/"+clusterID+"/deploy-image", api.DeployImageRequest{
		AppType:  appType,
		ImageTag: tag,
	}, &result); err != nil {
		return fmt.Errorf("deploy failed: %w", err)
	}

	fmt.Printf("Deploy %s: %s\n", result.Status, result.Image)
	return nil
}

func runDeployStatus(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	cfg := loadProjectConfig()
	clusterID, _ := cmd.Flags().GetString("cluster")
	if clusterID == "" && cfg != nil {
		clusterID = cfg.Cluster
	}
	if clusterID == "" {
		return fmt.Errorf("--cluster or .skaledata.yaml required")
	}

	var job *api.Job
	if err := client.Get("/clusters/"+clusterID+"/latest-job", &job); err != nil {
		return err
	}
	if job == nil {
		fmt.Println("No jobs found for this cluster.")
		return nil
	}

	fmt.Printf("Type:    %s\n", job.Type)
	fmt.Printf("Status:  %s\n", job.Status)
	fmt.Printf("Created: %s\n", job.CreatedAt.Format(time.RFC3339))
	if job.StartedAt != nil {
		fmt.Printf("Started: %s\n", job.StartedAt.Format(time.RFC3339))
	}
	if job.CompletedAt != nil {
		fmt.Printf("Done:    %s\n", job.CompletedAt.Format(time.RFC3339))
	}
	if job.ErrorMessage != nil {
		fmt.Printf("Error:   %s\n", *job.ErrorMessage)
	}
	return nil
}
