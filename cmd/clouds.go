package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/skaledata/cli/internal/api"
	"github.com/skaledata/cli/internal/prompt"
	"github.com/spf13/cobra"
)

var cloudsCmd = &cobra.Command{
	Use:     "clouds",
	Aliases: []string{"cloud"},
	Short:   "Manage cloud connections",
}

var cloudsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List connected clouds",
	RunE:  runCloudsList,
}

var cloudsSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Connect a cloud account (interactive)",
	Long: `Interactively set up a cloud connection. Detects your active cloud CLI
session (gcloud, aws, or az), runs the SkaleData setup script to create
the deployer role/service account, then registers the cloud with SkaleData
and verifies the connection.

Requires an active cloud CLI session:
  - GCP: gcloud auth login
  - AWS: aws configure / aws sso login
  - Azure: az login`,
	RunE: runCloudsSetup,
}

var cloudsVerifyCmd = &cobra.Command{
	Use:   "verify <cloud-id>",
	Short: "Re-run verification on a cloud",
	Args:  cobra.ExactArgs(1),
	RunE:  runCloudsVerify,
}

func init() {
	rootCmd.AddCommand(cloudsCmd)
	cloudsCmd.AddCommand(cloudsListCmd)
	cloudsCmd.AddCommand(cloudsSetupCmd)
	cloudsCmd.AddCommand(cloudsVerifyCmd)

	cloudsSetupCmd.Flags().String("provider", "", "Cloud provider: gcp, aws, or azure")
	cloudsSetupCmd.Flags().String("name", "", "Display name for this cloud connection")
	cloudsSetupCmd.Flags().String("region", "", "Default region")
	// GCP
	cloudsSetupCmd.Flags().String("project-id", "", "GCP project ID")
	// AWS
	cloudsSetupCmd.Flags().String("aws-region", "", "AWS region (default: us-east-1)")
	// Azure
	cloudsSetupCmd.Flags().String("azure-resource-group", "skaledata-deployer", "Azure resource group name")
}

func runCloudsList(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	var clouds []api.Cloud
	if err := client.Get("/clouds", &clouds); err != nil {
		return err
	}

	if len(clouds) == 0 {
		fmt.Println("No clouds connected. Run: skale clouds setup")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tPROVIDER\tSTATUS\tACCOUNT")
	for _, c := range clouds {
		account := cloudAccount(&c)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", c.ID, c.DisplayName, c.Provider, c.Status, account)
	}
	return w.Flush()
}

func runCloudsSetup(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	provider, _ := cmd.Flags().GetString("provider")
	displayName, _ := cmd.Flags().GetString("name")
	region, _ := cmd.Flags().GetString("region")

	// Auto-detect provider if not specified
	if provider == "" {
		provider, err = detectOrPromptProvider()
		if err != nil {
			return err
		}
	}

	switch provider {
	case "gcp":
		return setupGCP(cmd, client, displayName, region)
	case "aws":
		return setupAWS(cmd, client, displayName, region)
	case "azure":
		return setupAzure(cmd, client, displayName, region)
	default:
		return fmt.Errorf("unsupported provider %q — use gcp, aws, or azure", provider)
	}
}

func runCloudsVerify(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	var cloud api.Cloud
	if err := client.Post("/clouds/"+args[0]+"/verify", nil, &cloud); err != nil {
		return err
	}

	fmt.Printf("Cloud %q (%s): %s\n", cloud.DisplayName, cloud.Provider, cloud.Status)
	return nil
}

// --- GCP setup ---

func setupGCP(cmd *cobra.Command, client *api.Client, displayName, region string) error {
	projectID, _ := cmd.Flags().GetString("project-id")

	// Try to detect project from gcloud
	if projectID == "" {
		out, err := exec.Command("gcloud", "config", "get-value", "project").Output()
		if err == nil {
			projectID = strings.TrimSpace(string(out))
		}
	}
	if projectID == "" {
		var err error
		projectID, err = promptInput("GCP project ID")
		if err != nil {
			return err
		}
	}

	if displayName == "" {
		displayName = "GCP — " + projectID
	}
	if region == "" {
		region = "us-central1"
	}

	saEmail := fmt.Sprintf("skaledata-deployer@%s.iam.gserviceaccount.com", projectID)

	// Step 1: Run setup script
	fmt.Printf("\nSetting up SkaleData deployer in project %s:\n", projectID)
	if err := runSetupScript(client, "gcp", projectID); err != nil {
		return err
	}

	// Step 2: Test connection
	fmt.Println("\n  Verifying connection...")
	var testResult api.CloudTestResponse
	if err := client.Post(
		fmt.Sprintf("/clouds/test?provider=gcp&project_id=%s", projectID),
		nil, &testResult,
	); err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	if !testResult.Impersonation.OK {
		errMsg := "unknown error"
		if testResult.Impersonation.Error != nil {
			errMsg = *testResult.Impersonation.Error
		}
		return fmt.Errorf("impersonation test failed: %s\nEnsure the setup script ran successfully and IAM propagation is complete (may take 60s)", errMsg)
	}

	printPreflightResults(testResult.Preflight)

	// Step 3: Register cloud
	fmt.Println("  Registering cloud...")
	var cloud api.Cloud
	if err := client.Post("/clouds", api.CloudCreateRequest{
		Provider:    "gcp",
		DisplayName: displayName,
		ProjectID:   &projectID,
		SAEmail:     &saEmail,
		Region:      region,
	}, &cloud); err != nil {
		return fmt.Errorf("register cloud: %w", err)
	}

	fmt.Println()
	fmt.Printf("Cloud connected!  %s (%s)\n", cloud.DisplayName, cloud.Status)
	fmt.Printf("\nNext step:\n")
	fmt.Printf("  skale clusters create --cloud %s\n", cloud.ID)
	return nil
}

// --- AWS setup ---

func setupAWS(cmd *cobra.Command, client *api.Client, displayName, region string) error {
	awsRegion, _ := cmd.Flags().GetString("aws-region")

	// Detect account ID from AWS CLI
	out, err := exec.Command("aws", "sts", "get-caller-identity", "--query", "Account", "--output", "text").Output()
	if err != nil {
		return fmt.Errorf("could not detect AWS account — ensure 'aws' CLI is authenticated: %w", err)
	}
	accountID := strings.TrimSpace(string(out))

	if awsRegion == "" {
		out, err := exec.Command("aws", "configure", "get", "region").Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			awsRegion = strings.TrimSpace(string(out))
		} else {
			awsRegion = "us-east-1"
		}
	}

	roleARN := fmt.Sprintf("arn:aws:iam::%s:role/SkaleDataDeployer", accountID)

	if displayName == "" {
		displayName = "AWS — " + accountID
	}
	if region == "" {
		region = awsRegion
	}

	// Step 1: Run setup script
	fmt.Printf("\nSetting up SkaleData deployer in AWS account %s (%s):\n", accountID, awsRegion)
	if err := runSetupScript(client, "aws", ""); err != nil {
		return err
	}

	// Step 2: Test connection
	fmt.Println("\n  Verifying connection...")
	var testResult api.CloudTestResponse
	if err := client.Post(
		fmt.Sprintf("/clouds/test?provider=aws&aws_role_arn=%s", roleARN),
		nil, &testResult,
	); err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	if !testResult.Impersonation.OK {
		errMsg := "unknown error"
		if testResult.Impersonation.Error != nil {
			errMsg = *testResult.Impersonation.Error
		}
		return fmt.Errorf("assume role test failed: %s\nEnsure the setup script ran successfully and IAM propagation is complete (may take 60s)", errMsg)
	}

	printPreflightResults(testResult.Preflight)

	// Step 3: Register cloud
	fmt.Println("  Registering cloud...")
	var cloud api.Cloud
	if err := client.Post("/clouds", api.CloudCreateRequest{
		Provider:     "aws",
		DisplayName:  displayName,
		AWSAccountID: &accountID,
		AWSRoleARN:   &roleARN,
		AWSRegion:    &awsRegion,
		Region:       region,
	}, &cloud); err != nil {
		return fmt.Errorf("register cloud: %w", err)
	}

	fmt.Println()
	fmt.Printf("Cloud connected!  %s (%s)\n", cloud.DisplayName, cloud.Status)
	fmt.Printf("\nNext step:\n")
	fmt.Printf("  skale clusters create --cloud %s\n", cloud.ID)
	return nil
}

// --- Azure setup ---

func setupAzure(cmd *cobra.Command, client *api.Client, displayName, region string) error {
	resourceGroup, _ := cmd.Flags().GetString("azure-resource-group")

	// Detect subscription + tenant from az CLI
	subOut, err := exec.Command("az", "account", "show", "--query", "id", "-o", "tsv").Output()
	if err != nil {
		return fmt.Errorf("could not detect Azure subscription — ensure 'az' CLI is authenticated: %w", err)
	}
	subscriptionID := strings.TrimSpace(string(subOut))

	tenantOut, err := exec.Command("az", "account", "show", "--query", "tenantId", "-o", "tsv").Output()
	if err != nil {
		return fmt.Errorf("could not detect Azure tenant: %w", err)
	}
	tenantID := strings.TrimSpace(string(tenantOut))

	if displayName == "" {
		displayName = "Azure — " + subscriptionID[:8]
	}
	if region == "" {
		region = "eastus"
	}

	// Step 1: Run setup script
	fmt.Printf("\nSetting up SkaleData deployer in Azure subscription %s:\n", subscriptionID)
	if err := runSetupScript(client, "azure", ""); err != nil {
		return err
	}

	// Detect the client ID from the app registration the script created
	fmt.Println("\n  Detecting app registration...")
	appOut, err := exec.Command("az", "ad", "app", "list", "--display-name", "SkaleData Deployer", "--query", "[0].appId", "-o", "tsv").Output()
	if err != nil || strings.TrimSpace(string(appOut)) == "" {
		return fmt.Errorf("could not find 'SkaleData Deployer' app registration — did the setup script complete?")
	}
	clientID := strings.TrimSpace(string(appOut))

	// Step 2: Test connection
	fmt.Println("  Verifying connection...")
	var testResult api.CloudTestResponse
	if err := client.Post(
		fmt.Sprintf("/clouds/test?provider=azure&azure_tenant_id=%s&azure_client_id=%s&azure_subscription_id=%s",
			tenantID, clientID, subscriptionID),
		nil, &testResult,
	); err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	if !testResult.Impersonation.OK {
		errMsg := "unknown error"
		if testResult.Impersonation.Error != nil {
			errMsg = *testResult.Impersonation.Error
		}
		return fmt.Errorf("Azure auth test failed: %s\nEnsure the setup script ran successfully and federation propagation is complete (may take 60s)", errMsg)
	}

	printPreflightResults(testResult.Preflight)

	// Step 3: Register cloud
	fmt.Println("  Registering cloud...")
	azureLocation := region
	var cloud api.Cloud
	if err := client.Post("/clouds", api.CloudCreateRequest{
		Provider:            "azure",
		DisplayName:         displayName,
		AzureSubscriptionID: &subscriptionID,
		AzureTenantID:       &tenantID,
		AzureClientID:       &clientID,
		AzureResourceGroup:  &resourceGroup,
		AzureLocation:       &azureLocation,
		Region:              region,
	}, &cloud); err != nil {
		return fmt.Errorf("register cloud: %w", err)
	}

	fmt.Println()
	fmt.Printf("Cloud connected!  %s (%s)\n", cloud.DisplayName, cloud.Status)
	fmt.Printf("\nNext step:\n")
	fmt.Printf("  skale clusters create --cloud %s\n", cloud.ID)
	return nil
}

// --- Helpers ---

func detectOrPromptProvider() (string, error) {
	options := []prompt.Option{
		{Label: "AWS", Value: "aws"},
		{Label: "GCP (Google Cloud)", Value: "gcp"},
		{Label: "Azure", Value: "azure"},
	}

	return prompt.Select("Select a cloud provider:", options)
}

func runSetupScript(client *api.Client, provider, projectID string) error {
	// Fetch the setup script from the API
	path := fmt.Sprintf("/clouds/setup-script?provider=%s", provider)
	if projectID != "" {
		path += fmt.Sprintf("&project_id=%s", projectID)
	}

	script, err := client.GetText(path)
	if err != nil {
		return fmt.Errorf("fetch setup script: %w", err)
	}

	// Run the script, parsing "==>" lines as progress steps.
	// All other output is captured and only shown on failure.
	bashCmd := exec.Command("bash", "-c", script)

	// Capture stderr for error reporting
	var stderrBuf strings.Builder
	var stdoutBuf strings.Builder

	// Use a pipe for stdout so we can parse progress lines
	stdoutPipe, err := bashCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	bashCmd.Stderr = &stderrBuf

	if err := bashCmd.Start(); err != nil {
		return fmt.Errorf("start setup script: %w", err)
	}

	scanner := bufio.NewScanner(stdoutPipe)
	for scanner.Scan() {
		line := scanner.Text()
		stdoutBuf.WriteString(line + "\n")
		if strings.HasPrefix(line, "==>") {
			// Show progress step
			fmt.Printf("  %s\n", strings.TrimPrefix(line, "==> "))
		}
		// Swallow all other output (IAM policy dumps, etc.)
	}

	if err := bashCmd.Wait(); err != nil {
		// Show captured output on failure to help debug
		fmt.Fprintf(os.Stderr, "\n--- setup script output ---\n%s", stdoutBuf.String())
		if stderrBuf.Len() > 0 {
			fmt.Fprintf(os.Stderr, "\n--- stderr ---\n%s", stderrBuf.String())
		}
		return fmt.Errorf("setup script failed: %w", err)
	}

	return nil
}

func printPreflightResults(preflight map[string]any) {
	if preflight == nil {
		return
	}

	allPassed, _ := preflight["all_passed"].(bool)
	if allPassed {
		fmt.Println("  Pre-flight checks: all passed")
		return
	}

	fmt.Println("  Pre-flight checks:")
	checks, ok := preflight["checks"].([]any)
	if !ok {
		return
	}
	for _, c := range checks {
		check, ok := c.(map[string]any)
		if !ok {
			continue
		}
		name, _ := check["name"].(string)
		passed, _ := check["passed"].(bool)
		if passed {
			fmt.Printf("    [PASS] %s\n", name)
		} else {
			fmt.Printf("    [FAIL] %s\n", name)
			if msg, ok := check["message"].(string); ok {
				fmt.Printf("           %s\n", msg)
			}
		}
	}
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

func cloudAccount(c *api.Cloud) string {
	switch c.Provider {
	case "gcp":
		if c.ProjectID != nil {
			return *c.ProjectID
		}
	case "aws":
		if c.AWSAccountID != nil {
			return *c.AWSAccountID
		}
	case "azure":
		if c.AzureSubscriptionID != nil {
			return *c.AzureSubscriptionID
		}
	}
	return "-"
}
