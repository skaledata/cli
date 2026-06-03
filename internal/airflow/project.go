package airflow

import (
	"fmt"
	"os"
	"path/filepath"
)

// IsProject checks if the current directory looks like a SkaleData Airflow project.
func IsProject(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "Dockerfile"))
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, "dags"))
	return err == nil
}

// Init scaffolds a new Airflow project in the given directory.
func Init(dir string) error {
	// Create directories
	for _, d := range []string{"dags", "plugins", "tests"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}

	files := map[string]string{
		"Dockerfile":          DockerfileTemplate,
		"README.md":           ReadmeTemplate,
		"dags/example_dag.py": ExampleDAG,
		"requirements.txt":    RequirementsTxt,
		"packages.txt":        PackagesTxt,
		".gitignore":          Gitignore,
		".dockerignore":       Dockerignore,
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		// Don't overwrite existing files
		if _, err := os.Stat(path); err == nil {
			fmt.Printf("  skip %s (already exists)\n", name)
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
		fmt.Printf("  created %s\n", name)
	}

	return nil
}
