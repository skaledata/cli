package airflow

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/skaledata/cli/internal/api"
)

// DeployOpts configures a deploy.
type DeployOpts struct {
	ClusterID  string
	AppName    string // target airflow instance name (for multi-instance clusters)
	Tag        string
	ForceImage bool // always do an image build, even if only DAGs changed
	ForceDags  bool // always do a DAG sync, even if image files changed
}

// imageFiles are files/directories that trigger a full image build when changed.
var imageFiles = []string{
	"Dockerfile",
	"requirements.txt",
	"pyproject.toml",
	"setup.py",
	"setup.cfg",
	"packages",
	"plugins",
}

// DeployMode indicates which deploy path was chosen.
type DeployMode int

const (
	DeployModeImage   DeployMode = iota // full image build
	DeployModeDagSync                   // DAG-only blob storage sync
)

// detectDeployMode hashes files to determine whether this is a DAG-only
// change or requires a full image build.
func detectDeployMode(dir string) (DeployMode, error) {
	// Check if dags/ directory exists
	dagsDir := filepath.Join(dir, "dags")
	if _, err := os.Stat(dagsDir); os.IsNotExist(err) {
		return DeployModeImage, nil
	}

	// Check git status for changed files
	out, err := exec.Command("git", "-C", dir, "diff", "--name-only", "HEAD").Output()
	if err != nil {
		// Not a git repo or no commits — can't detect, default to image
		return DeployModeImage, nil
	}

	changedFiles := strings.Split(strings.TrimSpace(string(out)), "\n")

	// Also check untracked files
	untrackedOut, err := exec.Command("git", "-C", dir, "ls-files", "--others", "--exclude-standard").Output()
	if err == nil && len(strings.TrimSpace(string(untrackedOut))) > 0 {
		changedFiles = append(changedFiles, strings.Split(strings.TrimSpace(string(untrackedOut)), "\n")...)
	}

	if len(changedFiles) == 0 || (len(changedFiles) == 1 && changedFiles[0] == "") {
		// No changes — still do a DAG sync if dags/ exists
		return DeployModeDagSync, nil
	}

	// Check if any changed file triggers an image build
	for _, f := range changedFiles {
		if f == "" {
			continue
		}
		if isImageFile(f) {
			return DeployModeImage, nil
		}
	}

	return DeployModeDagSync, nil
}

// isImageFile checks whether a file path triggers a full image build.
func isImageFile(path string) bool {
	for _, img := range imageFiles {
		if path == img || strings.HasPrefix(path, img+"/") {
			return true
		}
	}
	// Any file not under dags/ triggers an image build
	if !strings.HasPrefix(path, "dags/") && !strings.HasPrefix(path, ".") {
		return true
	}
	return false
}

// Deploy auto-detects the deploy mode and executes accordingly.
func Deploy(dir string, client *api.Client, opts DeployOpts) error {
	mode := DeployModeImage

	if opts.ForceImage {
		mode = DeployModeImage
	} else if opts.ForceDags {
		mode = DeployModeDagSync
	} else {
		detected, err := detectDeployMode(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not detect changes, defaulting to image build: %v\n", err)
		} else {
			mode = detected
		}
	}

	if mode == DeployModeDagSync {
		return deployDags(dir, client, opts)
	}
	return deployImage(dir, client, opts)
}

// deployDags uploads the dags/ directory to blob storage via the API.
func deployDags(dir string, client *api.Client, opts DeployOpts) error {
	dagsDir := filepath.Join(dir, "dags")
	if _, err := os.Stat(dagsDir); os.IsNotExist(err) {
		return fmt.Errorf("no dags/ directory found — cannot do a DAG-only deploy")
	}

	fmt.Println("Detecting changes...")
	fmt.Println("  No image changes detected (Dockerfile, requirements.txt unchanged)")
	fmt.Println()

	// Resolve the application ID
	appID, err := resolveAppIDForDeploy(client, opts)
	if err != nil {
		return fmt.Errorf("resolve application: %w", err)
	}

	// Create tar.gz of the dags/ directory
	fmt.Println("Packaging DAGs...")
	tarData, fileCount, err := createDagsTarball(dagsDir)
	if err != nil {
		return fmt.Errorf("package DAGs: %w", err)
	}

	// Upload via API (multipart form upload)
	fmt.Printf("Uploading %d DAG files...\n", fileCount)
	var result struct {
		Status        string `json:"status"`
		FilesUploaded int    `json:"files_uploaded"`
		SyncURL       string `json:"sync_url"`
		Message       string `json:"message"`
	}

	if err := uploadDagsMultipart(client, appID, tarData, &result); err != nil {
		return fmt.Errorf("upload DAGs: %w", err)
	}

	fmt.Printf("  %d files uploaded to %s\n", result.FilesUploaded, result.SyncURL)
	fmt.Println("  " + result.Message)
	return nil
}

// deployImage performs the existing full image build + push + deploy flow.
func deployImage(dir string, client *api.Client, opts DeployOpts) error {
	// Resolve a version tag for auditability
	versionTag := opts.Tag
	if versionTag == "" {
		out, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
		if err == nil {
			versionTag = strings.TrimSpace(string(out))
		} else {
			versionTag = fmt.Sprintf("deploy-%d", time.Now().Unix())
		}
	}

	// Step 1: Get registry credentials
	fmt.Println("Getting registry credentials...")
	var regToken api.RegistryTokenResponse
	if err := client.Post("/clusters/"+opts.ClusterID+"/registry-token", nil, &regToken); err != nil {
		return fmt.Errorf("get registry token: %w", err)
	}

	// Use the app name as the image sub-path so multi-instance deploys don't collide
	imageName := "airflow"
	if opts.AppName != "" && opts.AppName != "airflow" {
		imageName = opts.AppName
	}
	currentURI := fmt.Sprintf("%s/%s:current", regToken.ArtifactRegistryURL, imageName)
	versionURI := fmt.Sprintf("%s/%s:%s", regToken.ArtifactRegistryURL, imageName, versionTag)

	// Step 2: Configure Docker auth — write credentials directly to a
	// temporary Docker config to bypass any gcloud/ecr credential helpers
	// that might be configured in the user's ~/.docker/config.json.
	fmt.Printf("Logging into %s...\n", regToken.Registry)
	tmpDockerCfg, err := writeDockerConfig(regToken.Registry, regToken.Username, regToken.Token)
	if err != nil {
		return fmt.Errorf("configure docker auth: %w", err)
	}
	defer os.RemoveAll(filepath.Dir(tmpDockerCfg))

	dockerArgs := func(args ...string) []string {
		return append([]string{"--config", filepath.Dir(tmpDockerCfg)}, args...)
	}

	// Step 3: Docker build (always target linux/amd64 for cloud deployment)
	// Use buildx directly (not through our temp config) for cross-platform builds.
	fmt.Printf("Building image...\n")
	buildCmd := exec.Command("docker", "buildx", "build",
		"--platform", "linux/amd64",
		"--provenance=false",
		"--load",
		"-t", currentURI,
		dir,
	)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	// Tag with the version tag for auditability
	tagCmd := exec.Command("docker", dockerArgs("tag", currentURI, versionURI)...)
	if err := tagCmd.Run(); err != nil {
		return fmt.Errorf("docker tag failed: %w", err)
	}

	// Step 4: Push both tags
	fmt.Printf("Pushing %s...\n", imageName)
	for _, uri := range []string{currentURI, versionURI} {
		pushCmd := exec.Command("docker", dockerArgs("push", uri)...)
		pushCmd.Stdout = os.Stdout
		pushCmd.Stderr = os.Stderr
		if err := pushCmd.Run(); err != nil {
			return fmt.Errorf("push %s failed: %w", uri, err)
		}
	}

	// Step 5: Trigger deploy via API
	target := "airflow"
	if opts.AppName != "" {
		target = opts.AppName
	}
	fmt.Printf("Deploying to %s...\n", target)

	req := api.DeployImageRequest{
		AppType:  "airflow",
		ImageTag: versionTag,
	}
	if opts.AppName != "" {
		req.AppName = &opts.AppName
	}

	var result api.DeployImageResponse
	if err := client.Post("/clusters/"+opts.ClusterID+"/deploy-image", req, &result); err != nil {
		return fmt.Errorf("deploy failed: %w", err)
	}

	fmt.Println()
	if result.Status == "deployed" {
		// Fast path — poll pods until rollout is complete
		namespace := opts.AppName
		if namespace == "" {
			namespace = "airflow"
		}
		waitForRollout(client, opts.ClusterID, namespace)
		fmt.Printf("Deployed! (%s)\n", versionTag)
	} else {
		fmt.Printf("Deploy initiated (%s) — first deploy takes ~2 min for Helm setup.\n", versionTag)
		fmt.Println("Subsequent deploys will be instant.")
	}
	fmt.Printf("  Image: %s\n", result.Image)
	return nil
}

// createDagsTarball creates a tar.gz archive of the dags/ directory.
// Returns the tar data, file count, and any error.
func createDagsTarball(dagsDir string) ([]byte, int, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	count := 0

	err := filepath.WalkDir(dagsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip __pycache__ and hidden files
		if d.IsDir() && (d.Name() == "__pycache__" || strings.HasPrefix(d.Name(), ".")) {
			return filepath.SkipDir
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}

		relPath, err := filepath.Rel(filepath.Dir(dagsDir), path)
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}

		count++
		return nil
	})

	if err != nil {
		return nil, 0, err
	}

	if err := tw.Close(); err != nil {
		return nil, 0, err
	}
	if err := gw.Close(); err != nil {
		return nil, 0, err
	}

	return buf.Bytes(), count, nil
}

// uploadDagsMultipart uploads a tar.gz file to the DAGs endpoint via multipart form.
func uploadDagsMultipart(client *api.Client, appID string, tarData []byte, result any) error {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", "dags.tar.gz")
	if err != nil {
		return err
	}
	if _, err := part.Write(tarData); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	return client.PostMultipart("/applications/"+appID+"/dags", writer.FormDataContentType(), buf.Bytes(), result)
}

// resolveAppIDForDeploy finds the application UUID for DAG deploys.
func resolveAppIDForDeploy(client *api.Client, opts DeployOpts) (string, error) {
	// Get the cluster's internal ID
	var cluster api.Cluster
	if err := client.Get("/clusters/"+opts.ClusterID, &cluster); err != nil {
		return "", fmt.Errorf("get cluster: %w", err)
	}

	var allApps []api.Application
	if err := client.Get("/applications", &allApps); err != nil {
		return "", fmt.Errorf("list applications: %w", err)
	}

	for _, a := range allApps {
		if a.ClusterID != cluster.ID || a.AppType != "airflow" {
			continue
		}
		if opts.AppName == "" || a.Name == opts.AppName {
			return a.ID, nil
		}
	}
	return "", fmt.Errorf("no matching Airflow application found on cluster %s", opts.ClusterID)
}

// waitForRollout polls pod status until all pods are ready or timeout.
func waitForRollout(client *api.Client, clusterID, namespace string) {
	path := fmt.Sprintf("/clusters/%s/pods?namespace=%s", clusterID, namespace)
	deadline := time.Now().Add(3 * time.Minute)

	// Brief pause to let the restart annotation propagate
	time.Sleep(2 * time.Second)

	for time.Now().Before(deadline) {
		var podList api.PodListResponse
		if err := client.Get(path, &podList); err != nil {
			// Cluster might be in a transient state, keep trying
			time.Sleep(3 * time.Second)
			continue
		}

		// Count airflow pods (skip redis)
		total := 0
		ready := 0
		for _, p := range podList.Pods {
			if strings.Contains(p.Name, "redis") {
				continue
			}
			total++
			if p.Ready && p.Status == "Running" {
				ready++
			}
		}

		if total > 0 && ready == total {
			fmt.Printf("\r  Pods ready: %d/%d\n", ready, total)
			return
		}
		fmt.Printf("\r  Pods ready: %d/%d...", ready, total)
		time.Sleep(5 * time.Second)
	}
	fmt.Println("\n  Rollout still in progress — check the UI for status.")
}

// writeDockerConfig creates a temporary Docker config directory with auth
// credentials for the given registry. Returns the path to the config.json.
// This bypasses any credential helpers (gcloud, ecr-login, etc.) that
// might be configured in the user's default Docker config.
func writeDockerConfig(registry, username, password string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "skale-docker-*")
	if err != nil {
		return "", err
	}

	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	config := map[string]any{
		"auths": map[string]any{
			registry: map[string]string{
				"auth": auth,
			},
		},
	}

	data, err := json.Marshal(config)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	cfgPath := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	return cfgPath, nil
}

