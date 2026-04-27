package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var initProjectCmd = &cobra.Command{
	Use:   "init <type> <name>",
	Short: "Scaffold a new project",
	Long:  `Generate a ready-to-deploy project directory for the given app type (airflow, airbyte, docs).`,
	Args:  cobra.ExactArgs(2),
	RunE:  runInitProject,
}

func init() {
	rootCmd.AddCommand(initProjectCmd)
	initProjectCmd.Flags().String("cluster", "", "Cluster ID to bind to (saved in .skaledata.yaml)")
}

func runInitProject(cmd *cobra.Command, args []string) error {
	appType := args[0]
	name := args[1]
	clusterID, _ := cmd.Flags().GetString("cluster")

	switch appType {
	case "airflow":
		return scaffoldAirflow(name, clusterID)
	case "airbyte":
		return scaffoldAirbyte(name, clusterID)
	case "docs":
		return scaffoldDocs(name, clusterID)
	default:
		return fmt.Errorf("unknown project type %q — supported: airflow, airbyte, docs", appType)
	}
}

func scaffoldAirflow(name, clusterID string) error {
	dir := name
	if err := os.MkdirAll(filepath.Join(dir, "dags"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755); err != nil {
		return err
	}

	files := map[string]string{
		filepath.Join(dir, "Dockerfile"): `FROM apache/airflow:2.10.4-python3.11

COPY requirements.txt /opt/airflow/requirements.txt
RUN pip install --no-cache-dir -r /opt/airflow/requirements.txt

COPY dags/ /opt/airflow/dags/
`,
		filepath.Join(dir, "requirements.txt"): `# Add your Python dependencies here
# apache-airflow-providers-google
# pandas
`,
		filepath.Join(dir, "dags", "example_dag.py"): `"""Example DAG — replace with your own."""
from datetime import datetime

from airflow import DAG
from airflow.operators.python import PythonOperator


def hello():
    print("Hello from SkaleData!")


with DAG(
    dag_id="example_dag",
    start_date=datetime(2024, 1, 1),
    schedule="@daily",
    catchup=False,
) as dag:
    PythonOperator(task_id="hello", python_callable=hello)
`,
		filepath.Join(dir, ".github", "workflows", "deploy.yml"): fmt.Sprintf(`name: Deploy to SkaleData

on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Deploy to SkaleData
        uses: skaledata/deploy-action@v1
        with:
          api-key: ${{ "{{" }} secrets.SKALEDATA_API_KEY {{ "}}" }}
          cluster-id: %s
          app-type: airflow
`, clusterIDOrPlaceholder(clusterID)),
		filepath.Join(dir, ".skaledata.yaml"): fmt.Sprintf(`cluster: %s
app_type: airflow
`, clusterIDOrPlaceholder(clusterID)),
		filepath.Join(dir, ".gitignore"): `__pycache__/
*.pyc
.env
`,
	}

	return writeProjectFiles(dir, files)
}

func scaffoldAirbyte(name, clusterID string) error {
	dir := name
	if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755); err != nil {
		return err
	}

	files := map[string]string{
		filepath.Join(dir, "connectors.yaml"): `# Airbyte connector configuration
# Define your sources and destinations here
sources: []
destinations: []
`,
		filepath.Join(dir, ".github", "workflows", "deploy.yml"): fmt.Sprintf(`name: Deploy to SkaleData

on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Deploy to SkaleData
        uses: skaledata/deploy-action@v1
        with:
          api-key: ${{ "{{" }} secrets.SKALEDATA_API_KEY {{ "}}" }}
          cluster-id: %s
          app-type: airbyte
`, clusterIDOrPlaceholder(clusterID)),
		filepath.Join(dir, ".skaledata.yaml"): fmt.Sprintf(`cluster: %s
app_type: airbyte
`, clusterIDOrPlaceholder(clusterID)),
	}

	return writeProjectFiles(dir, files)
}

func scaffoldDocs(name, clusterID string) error {
	dir := name
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755); err != nil {
		return err
	}

	files := map[string]string{
		filepath.Join(dir, "Dockerfile"): `FROM squidfunk/mkdocs-material:latest

COPY docs/ /docs/docs/
COPY mkdocs.yml /docs/mkdocs.yml
`,
		filepath.Join(dir, "mkdocs.yml"): fmt.Sprintf(`site_name: %s
theme:
  name: material
`, name),
		filepath.Join(dir, "docs", "index.md"): fmt.Sprintf(`# %s

Welcome to your documentation site.
`, name),
		filepath.Join(dir, ".github", "workflows", "deploy.yml"): fmt.Sprintf(`name: Deploy to SkaleData

on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Deploy to SkaleData
        uses: skaledata/deploy-action@v1
        with:
          api-key: ${{ "{{" }} secrets.SKALEDATA_API_KEY {{ "}}" }}
          cluster-id: %s
          app-type: docs
`, clusterIDOrPlaceholder(clusterID)),
		filepath.Join(dir, ".skaledata.yaml"): fmt.Sprintf(`cluster: %s
app_type: docs
`, clusterIDOrPlaceholder(clusterID)),
	}

	return writeProjectFiles(dir, files)
}

func clusterIDOrPlaceholder(id string) string {
	if id != "" {
		return id
	}
	return "<YOUR_CLUSTER_ID>"
}

func writeProjectFiles(dir string, files map[string]string) error {
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}

	fmt.Printf("Project created in ./%s\n", dir)
	fmt.Println("\nNext steps:")
	fmt.Println("  cd", dir)
	fmt.Println("  skaledata deploy")
	return nil
}
