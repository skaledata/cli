// Package app resolves SkaleData application instances and mints/consumes
// short-lived, app-scoped credentials for calling an app's own REST API.
package app

import (
	"fmt"

	"github.com/skaledata/cli/internal/api"
)

// ResolveAppID finds the internal application UUID for a given cluster
// public ID, app type (e.g. "airflow", "airbyte"), and optional instance
// name. When name is empty, the first matching app of that type on the
// cluster is returned. Applications reference the cluster's internal UUID,
// so we first look up the cluster to translate its public ID.
func ResolveAppID(client *api.Client, clusterPublicID, appType, name string) (string, error) {
	var cluster api.Cluster
	if err := client.Get("/clusters/"+clusterPublicID, &cluster); err != nil {
		return "", err
	}

	var allApps []api.Application
	if err := client.Get("/applications", &allApps); err != nil {
		return "", err
	}

	for _, a := range allApps {
		if a.ClusterID != cluster.ID || a.AppType != appType {
			continue
		}
		if name == "" || a.Name == name {
			return a.ID, nil
		}
	}

	if name != "" {
		return "", fmt.Errorf("no %s application named %q found on cluster %s", appType, name, clusterPublicID)
	}
	return "", fmt.Errorf("no %s application found on cluster %s", appType, clusterPublicID)
}
