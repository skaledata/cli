package cmd

import (
	"bufio"
	"encoding/json"
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
	cloudsSetupCmd.Flags().String("project-id", "", "GCP project ID (default: prompt to select)")
	// AWS
	cloudsSetupCmd.Flags().String("aws-region", "", "AWS region (default: prompt with detected)")
	cloudsSetupCmd.Flags().String("aws-profile", "", "AWS named profile (default: prompt to select)")
	// Azure
	cloudsSetupCmd.Flags().String("azure-resource-group", "skaledata-deployer", "Azure resource group name")
	cloudsSetupCmd.Flags().String("azure-subscription", "", "Azure subscription ID (default: prompt to select)")
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
	projectIDFlag, _ := cmd.Flags().GetString("project-id")
	projectID := projectIDFlag

	if projectID == "" {
		var err error
		projectID, err = selectGCPProject()
		if err != nil {
			return err
		}
	}

	// Make the setup script (gcloud commands) use the chosen project regardless
	// of the user's currently active gcloud config.
	os.Setenv("CLOUDSDK_CORE_PROJECT", projectID)

	if displayName == "" {
		var err error
		displayName, err = promptInputDefault("Display name", "GCP — "+projectID)
		if err != nil {
			return err
		}
	}
	if region == "" {
		var err error
		region, err = promptInputDefault("Region", "us-central1")
		if err != nil {
			return err
		}
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
	awsProfileFlag, _ := cmd.Flags().GetString("aws-profile")
	awsRegionFlag, _ := cmd.Flags().GetString("aws-region")

	// Select profile (or use flag) and resolve account ID
	awsProfile := awsProfileFlag
	var accountID string
	if awsProfile == "" {
		var err error
		awsProfile, accountID, err = selectAWSProfile()
		if err != nil {
			return err
		}
	} else {
		out, err := exec.Command("aws", "sts", "get-caller-identity",
			"--query", "Account", "--output", "text", "--profile", awsProfile).Output()
		if err != nil {
			return fmt.Errorf("could not use AWS profile %q — check credentials: %w", awsProfile, err)
		}
		accountID = strings.TrimSpace(string(out))
	}

	// Propagate the chosen profile to the setup script and any subsequent aws calls.
	os.Setenv("AWS_PROFILE", awsProfile)

	// Region default: --aws-region → `aws configure get region` for this profile → us-east-1
	awsRegion := awsRegionFlag
	if awsRegion == "" {
		out, err := exec.Command("aws", "configure", "get", "region", "--profile", awsProfile).Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			awsRegion = strings.TrimSpace(string(out))
		} else {
			awsRegion = "us-east-1"
		}
	}

	roleARN := fmt.Sprintf("arn:aws:iam::%s:role/SkaleDataDeployer", accountID)

	if displayName == "" {
		var err error
		displayName, err = promptInputDefault("Display name", "AWS — "+accountID)
		if err != nil {
			return err
		}
	}
	if awsRegionFlag == "" {
		var err error
		awsRegion, err = promptInputDefault("AWS region", awsRegion)
		if err != nil {
			return err
		}
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
	resourceGroupFlag, _ := cmd.Flags().GetString("azure-resource-group")
	subFlag, _ := cmd.Flags().GetString("azure-subscription")

	// Select subscription (or use flag) and resolve tenant
	var subscriptionID, tenantID string
	if subFlag == "" {
		var err error
		subscriptionID, tenantID, err = selectAzureSubscription()
		if err != nil {
			return err
		}
	} else {
		subscriptionID = subFlag
		out, err := exec.Command("az", "account", "show",
			"--subscription", subscriptionID, "--query", "tenantId", "-o", "tsv").Output()
		if err != nil {
			return fmt.Errorf("could not look up tenant for subscription %q: %w", subscriptionID, err)
		}
		tenantID = strings.TrimSpace(string(out))
	}

	// Make az CLI calls (including the setup script) target the chosen subscription.
	// Skip the mutating `az account set` if it's already the active sub.
	if cur, err := exec.Command("az", "account", "show", "--query", "id", "-o", "tsv").Output(); err != nil ||
		strings.TrimSpace(string(cur)) != subscriptionID {
		if err := exec.Command("az", "account", "set", "--subscription", subscriptionID).Run(); err != nil {
			return fmt.Errorf("set active Azure subscription: %w", err)
		}
	}

	if displayName == "" {
		var err error
		displayName, err = promptInputDefault("Display name", "Azure — "+subscriptionID[:8])
		if err != nil {
			return err
		}
	}
	if region == "" {
		var err error
		region, err = promptInputDefault("Location", "eastus")
		if err != nil {
			return err
		}
	}
	resourceGroup := resourceGroupFlag
	if !cmd.Flags().Changed("azure-resource-group") {
		var err error
		resourceGroup, err = promptInputDefault("Resource group", resourceGroupFlag)
		if err != nil {
			return err
		}
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

// promptInputDefault prompts with a default in brackets. Empty input accepts
// the default. An empty default still requires input.
func promptInputDefault(label, defaultVal string) (string, error) {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("%s: ", label)
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	val := strings.TrimSpace(scanner.Text())
	if val == "" {
		if defaultVal == "" {
			return "", fmt.Errorf("%s cannot be empty", label)
		}
		return defaultVal, nil
	}
	return val, nil
}

// --- Profile/credential selectors ---

type awsProfileInfo struct {
	Name      string
	AccountID string
	Err       string
}

// selectAWSProfile lists configured AWS profiles, probes each for an account
// ID via STS, and lets the user pick one. Returns the profile name + account
// ID. Skips the picker when exactly one profile has valid credentials.
func selectAWSProfile() (string, string, error) {
	out, err := exec.Command("aws", "configure", "list-profiles").Output()
	if err != nil {
		return "", "", fmt.Errorf("list AWS profiles — ensure 'aws' CLI v2 is installed and configured: %w", err)
	}
	names := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(names) == 0 || (len(names) == 1 && names[0] == "") {
		return "", "", fmt.Errorf("no AWS profiles configured — run 'aws configure' or 'aws sso configure'")
	}

	profiles := make([]awsProfileInfo, 0, len(names))
	valid := 0
	for _, name := range names {
		info := awsProfileInfo{Name: name}
		idOut, err := exec.Command("aws", "sts", "get-caller-identity",
			"--query", "Account", "--output", "text", "--profile", name).Output()
		if err != nil {
			info.Err = "creds invalid or expired"
		} else {
			info.AccountID = strings.TrimSpace(string(idOut))
			valid++
		}
		profiles = append(profiles, info)
	}

	if valid == 0 {
		return "", "", fmt.Errorf("no AWS profile has valid credentials — try 'aws sso login' or 'aws configure'")
	}
	if valid == 1 {
		for _, p := range profiles {
			if p.AccountID != "" {
				fmt.Printf("  Using AWS profile: %s (account %s)\n", p.Name, p.AccountID)
				return p.Name, p.AccountID, nil
			}
		}
	}

	options := make([]prompt.Option, 0, valid)
	for _, p := range profiles {
		if p.AccountID == "" {
			continue
		}
		options = append(options, prompt.Option{
			Label: fmt.Sprintf("%s (account %s)", p.Name, p.AccountID),
			Value: p.Name,
		})
	}
	selected, err := prompt.Select("Select AWS profile:", options)
	if err != nil {
		return "", "", err
	}
	for _, p := range profiles {
		if p.Name == selected {
			return p.Name, p.AccountID, nil
		}
	}
	return "", "", fmt.Errorf("unexpected: selected profile not found in list")
}

// selectGCPProject lists projects the user has access to and lets them pick.
// The active gcloud project (if any) is shown first.
func selectGCPProject() (string, error) {
	out, err := exec.Command("gcloud", "projects", "list", "--format=value(projectId)").Output()
	if err != nil {
		return "", fmt.Errorf("list GCP projects — ensure 'gcloud' CLI is installed and authenticated: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return "", fmt.Errorf("no GCP projects accessible — run 'gcloud auth login'")
	}

	active := ""
	if out, err := exec.Command("gcloud", "config", "get-value", "project").Output(); err == nil {
		active = strings.TrimSpace(string(out))
	}

	if len(lines) == 1 {
		fmt.Printf("  Using GCP project: %s\n", lines[0])
		return lines[0], nil
	}

	options := make([]prompt.Option, 0, len(lines))
	if active != "" {
		for _, name := range lines {
			if name == active {
				options = append(options, prompt.Option{Label: name + " (active)", Value: name})
				break
			}
		}
	}
	for _, name := range lines {
		if name == active {
			continue
		}
		options = append(options, prompt.Option{Label: name, Value: name})
	}
	return prompt.Select("Select GCP project:", options)
}

type azureSubInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TenantID  string `json:"tenantId"`
	IsDefault bool   `json:"isDefault"`
}

// selectAzureSubscription lists subscriptions visible to az CLI and lets the
// user pick. Returns subscription ID + tenant ID. The default subscription
// (if any) is shown first.
func selectAzureSubscription() (string, string, error) {
	out, err := exec.Command("az", "account", "list", "--output", "json").Output()
	if err != nil {
		return "", "", fmt.Errorf("list Azure subscriptions — ensure 'az' CLI is installed and authenticated: %w", err)
	}
	var subs []azureSubInfo
	if err := json.Unmarshal(out, &subs); err != nil {
		return "", "", fmt.Errorf("parse az account list output: %w", err)
	}
	if len(subs) == 0 {
		return "", "", fmt.Errorf("no Azure subscriptions — run 'az login'")
	}

	if len(subs) == 1 {
		fmt.Printf("  Using Azure subscription: %s (%s)\n", subs[0].Name, subs[0].ID)
		return subs[0].ID, subs[0].TenantID, nil
	}

	options := make([]prompt.Option, 0, len(subs))
	for _, s := range subs {
		if s.IsDefault {
			options = append(options, prompt.Option{
				Label: fmt.Sprintf("%s — %s (default)", s.Name, s.ID),
				Value: s.ID,
			})
		}
	}
	for _, s := range subs {
		if s.IsDefault {
			continue
		}
		options = append(options, prompt.Option{
			Label: fmt.Sprintf("%s — %s", s.Name, s.ID),
			Value: s.ID,
		})
	}
	selectedID, err := prompt.Select("Select Azure subscription:", options)
	if err != nil {
		return "", "", err
	}
	for _, s := range subs {
		if s.ID == selectedID {
			return s.ID, s.TenantID, nil
		}
	}
	return "", "", fmt.Errorf("unexpected: selected subscription not found in list")
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
