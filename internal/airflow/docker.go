package airflow

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/skaledata/cli/internal/api"
)

// composeFilePath returns the path to the generated compose file inside .skale/.
func composeFilePath(dir string) string {
	return filepath.Join(dir, ".skale", "docker-compose.yml")
}

// ensureCompose writes the embedded docker-compose.yml to .skale/.
func ensureCompose(dir string) error {
	skaleDir := filepath.Join(dir, ".skale")
	if err := os.MkdirAll(skaleDir, 0o755); err != nil {
		return fmt.Errorf("create .skale/: %w", err)
	}
	if err := os.WriteFile(composeFilePath(dir), []byte(ComposeTemplate), 0o644); err != nil {
		return fmt.Errorf("write docker-compose.yml: %w", err)
	}
	// Write webserver_config.py to project root (mounted into container, disables login)
	wcPath := filepath.Join(dir, "webserver_config.py")
	if err := os.WriteFile(wcPath, []byte(WebserverConfig), 0o644); err != nil {
		return fmt.Errorf("write webserver_config.py: %w", err)
	}
	// Ensure credential placeholders exist so volume mounts don't fail.
	// Real credentials are written by FetchAndWriteSecrets when a project is
	// bound to a deployed instance.
	for _, name := range []string{"gcp-credentials.json", "gcp-access-token", "azure-token.json"} {
		credPath := filepath.Join(skaleDir, name)
		if _, err := os.Stat(credPath); os.IsNotExist(err) {
			if err := os.WriteFile(credPath, []byte("{}"), 0o600); err != nil {
				return fmt.Errorf("write %s placeholder: %w", name, err)
			}
		}
	}
	return nil
}

// StartOpts configures the start command.
type StartOpts struct {
	Client *api.Client // nil = no secrets fetching (offline mode)
	AppID  string      // application ID for dev-credentials
}

// Start builds the image and starts all services.
// If opts is provided with a Client and AppID, secrets backend credentials
// are fetched and written before starting.
func Start(dir string, opts *StartOpts) error {
	if err := ensureCompose(dir); err != nil {
		return err
	}

	// Fetch and write secrets backend credentials if bound to a deployed instance
	if opts != nil && opts.Client != nil && opts.AppID != "" {
		resp, err := FetchAndWriteSecrets(dir, opts.Client, opts.AppID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not fetch dev credentials: %v\n", err)
			fmt.Println("  Starting without secrets backend — add --cluster to configure.")
		} else {
			printSecretsInfo(resp)
		}
	}

	fmt.Println("Building Airflow image...")
	if err := compose(dir, "build"); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	fmt.Println("Starting Airflow...")
	if err := compose(dir, "up", "-d"); err != nil {
		return fmt.Errorf("start failed: %w", err)
	}

	fmt.Println("Waiting for Airflow to be ready...")
	if err := waitForHealth("http://localhost:8080/api/v2/version", 3*time.Minute); err != nil {
		fmt.Println("\nAPI server not healthy yet — it may still be starting up.")
		fmt.Println("Check logs with: skale airflow logs")
	} else {
		fmt.Println()
		fmt.Println("Airflow is running!")
		fmt.Println("  UI:        http://localhost:8080")
		fmt.Println("  Postgres:  localhost:5432 (airflow/airflow)")
	}

	return nil
}

func printSecretsInfo(resp *api.DevCredentialsResponse) {
	backendNames := map[string]string{
		"gcp_service_account_key": "Google Secret Manager",
		"aws_sts":                 "AWS Secrets Manager",
		"azure_client_secret":     "Azure Key Vault",
		"azure_ad_token":          "Azure Key Vault",
	}
	name := backendNames[resp.CredentialType]
	if name == "" {
		name = resp.SecretsBackend
	}
	fmt.Printf("\n  Secrets backend: %s\n", name)

	if cp, ok := resp.BackendKwargs["connections_prefix"]; ok {
		fmt.Printf("  Connections prefix: %s\n", cp)
	}
	if vp, ok := resp.BackendKwargs["variables_prefix"]; ok {
		fmt.Printf("  Variables prefix: %s\n", vp)
	}

	if expiry, ok := resp.Credentials["expiry"]; ok && expiry != nil {
		fmt.Printf("  Credentials expire: %s\n", expiry)
		fmt.Println("  Run `skale airflow refresh` to renew")
	}
	fmt.Println()
}

// RestartServices restarts the airflow services without rebuilding.
// Used after credential refresh to pick up new env vars (AWS).
func RestartServices(dir string) error {
	if err := ensureCompose(dir); err != nil {
		return err
	}
	return compose(dir, "restart", "api-server", "scheduler", "dag-processor", "triggerer")
}

// Restart stops and restarts all services without rebuilding.
func Restart(dir string) error {
	if err := ensureCompose(dir); err != nil {
		return err
	}
	fmt.Println("Restarting Airflow...")
	return compose(dir, "restart")
}

// Stop gracefully stops all services (preserves volumes).
func Stop(dir string) error {
	if err := ensureCompose(dir); err != nil {
		return err
	}
	fmt.Println("Stopping Airflow...")
	return compose(dir, "stop")
}

// Kill stops and removes all containers + volumes.
func Kill(dir string) error {
	if err := ensureCompose(dir); err != nil {
		return err
	}
	fmt.Println("Killing Airflow (removing containers and volumes)...")
	if err := compose(dir, "down", "--volumes", "--remove-orphans"); err != nil {
		return err
	}
	os.RemoveAll(filepath.Join(dir, ".skale"))
	return nil
}

// Bash opens an interactive shell in a container.
func Bash(dir string, container string) error {
	if err := ensureCompose(dir); err != nil {
		return err
	}
	f := composeFilePath(dir)
	cmd := exec.Command("docker", "compose", "-f", f, "exec", container, "bash")
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Run executes an airflow CLI command inside the scheduler container.
func Run(dir string, airflowArgs []string) error {
	if err := ensureCompose(dir); err != nil {
		return err
	}
	f := composeFilePath(dir)
	args := append([]string{"compose", "-f", f, "exec", "-T", "scheduler", "airflow"}, airflowArgs...)
	cmd := exec.Command("docker", args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Logs streams logs from all services.
func Logs(dir string, follow bool) error {
	if err := ensureCompose(dir); err != nil {
		return err
	}
	f := composeFilePath(dir)
	args := []string{"compose", "-f", f, "logs"}
	if follow {
		args = append(args, "-f")
	}
	cmd := exec.Command("docker", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// compose runs a docker compose command using the generated compose file.
func compose(dir string, args ...string) error {
	f := composeFilePath(dir)
	fullArgs := append([]string{"compose", "-f", f}, args...)
	cmd := exec.Command("docker", fullArgs...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// waitForHealth polls a health endpoint until it returns 200 or times out.
func waitForHealth(url string, timeout time.Duration) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(3 * time.Second)
		fmt.Print(".")
	}
	return fmt.Errorf("health check timed out after %s", timeout)
}
