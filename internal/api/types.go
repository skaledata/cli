package api

import "time"

// Cluster represents a SkaleData cluster.
type Cluster struct {
	ID                   string            `json:"id"`
	PublicID             string            `json:"public_id"`
	OrgID                string            `json:"org_id"`
	CloudID              string            `json:"cloud_id"`
	Name                 string            `json:"name"`
	Region               string            `json:"region"`
	Status               string            `json:"status"`
	ClusterEndpoint      *string           `json:"cluster_endpoint"`
	ScaffoldVersion      string            `json:"scaffold_version"`
	EnableAirflow        bool              `json:"enable_airflow"`
	EnableAirbyte        bool              `json:"enable_airbyte"`
	EnableDocs           bool              `json:"enable_docs"`
	EnableSlackbot       bool              `json:"enable_slackbot"`
	EnableSuperset       bool              `json:"enable_superset"`
	EnableDatahub        bool              `json:"enable_datahub"`
	EnableTailscale      bool              `json:"enable_tailscale"`
	Config               map[string]any    `json:"config"`
	TerraformStatePrefix string            `json:"terraform_state_prefix"`
	ClusterName          *string           `json:"cluster_name"`
	NatIP                *string           `json:"nat_ip"`
	ArtifactRegistryURL  *string           `json:"artifact_registry_url"`
	WIFProvider          *string           `json:"wif_provider"`
	GithubActionsSA      *string           `json:"github_actions_sa"`
	ErrorMessage         *string           `json:"error_message"`
	LastAppliedAt        *time.Time        `json:"last_applied_at"`
	CreatedAt            time.Time         `json:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
}

// EnabledApps returns a list of enabled app names.
func (c *Cluster) EnabledApps() []string {
	var apps []string
	if c.EnableAirflow {
		apps = append(apps, "airflow")
	}
	if c.EnableAirbyte {
		apps = append(apps, "airbyte")
	}
	if c.EnableDocs {
		apps = append(apps, "docs")
	}
	if c.EnableSlackbot {
		apps = append(apps, "slackbot")
	}
	if c.EnableSuperset {
		apps = append(apps, "superset")
	}
	if c.EnableDatahub {
		apps = append(apps, "datahub")
	}
	return apps
}

// ClusterCreateRequest is the body for POST /clusters.
type ClusterCreateRequest struct {
	CloudID         string         `json:"cloud_id"`
	Name            string         `json:"name"`
	Region          string         `json:"region"`
	ScaffoldVersion string         `json:"scaffold_version"`
	EnableAirflow   bool           `json:"enable_airflow"`
	EnableAirbyte   bool           `json:"enable_airbyte"`
	EnableDocs      bool           `json:"enable_docs"`
	EnableSlackbot  bool           `json:"enable_slackbot"`
	EnableSuperset  bool           `json:"enable_superset"`
	EnableDatahub   bool           `json:"enable_datahub"`
	Config          map[string]any `json:"config,omitempty"`
}

// Cloud represents a connected cloud account.
type Cloud struct {
	ID                  string  `json:"id"`
	OrgID               string  `json:"org_id"`
	Provider            string  `json:"provider"`
	DisplayName         string  `json:"display_name"`
	Status              string  `json:"status"`
	ProjectID           *string `json:"project_id"`
	Region              *string `json:"region"`
	SAEmail             *string `json:"sa_email"`
	AWSAccountID        *string `json:"aws_account_id"`
	AWSRoleARN          *string `json:"aws_role_arn"`
	AWSRegion           *string `json:"aws_region"`
	AzureSubscriptionID *string `json:"azure_subscription_id"`
	AzureTenantID       *string `json:"azure_tenant_id"`
	AzureClientID       *string `json:"azure_client_id"`
	AzureResourceGroup  *string `json:"azure_resource_group"`
	AzureLocation       *string `json:"azure_location"`
}

// Application represents a deployed application.
type Application struct {
	ID          string         `json:"id"`
	ClusterID   string         `json:"cluster_id"`
	AppType     string         `json:"app_type"`
	Name        string         `json:"name"`
	Namespace   string         `json:"namespace"`
	PublicID    string         `json:"public_id"`
	Status      string         `json:"status"`
	Config      map[string]any `json:"config"`
	IngressPath *string        `json:"ingress_path"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// Job represents a Celery job (provision, upgrade, destroy).
type Job struct {
	ID            string     `json:"id"`
	ClusterID     string     `json:"cluster_id"`
	Type          string     `json:"type"`
	Status        string     `json:"status"`
	StartedAt     *time.Time `json:"started_at"`
	CompletedAt   *time.Time `json:"completed_at"`
	ErrorMessage  *string    `json:"error_message"`
	Logs          *string    `json:"logs"`
	CeleryTaskID  *string    `json:"celery_task_id"`
	CreatedAt     time.Time  `json:"created_at"`
}

// DataPlaneTokenResponse from POST /clusters/{id}/data-plane-token.
type DataPlaneTokenResponse struct {
	URL       string `json:"url"`
	ExpiresIn int    `json:"expires_in"`
}

// DeployImageRequest is the body for POST /clusters/{id}/deploy-image.
type DeployImageRequest struct {
	AppType  string  `json:"app_type"`
	AppName  *string `json:"app_name,omitempty"`
	ImageTag string  `json:"image_tag"`
}

// DeployImageResponse from POST /clusters/{id}/deploy-image.
type DeployImageResponse struct {
	Status string `json:"status"`
	Image  string `json:"image"`
	App    string `json:"app,omitempty"`
}

// RegistryTokenResponse from POST /clusters/{id}/registry-token.
type RegistryTokenResponse struct {
	Token               string `json:"token"`
	Username            string `json:"username"`
	Registry            string `json:"registry"`
	ArtifactRegistryURL string `json:"artifact_registry_url"`
	ExpiresIn           int    `json:"expires_in"`
}

// AllowedVersionsResponse from GET /clusters/allowed-versions.
type AllowedVersionsResponse struct {
	AllowedVersions []string `json:"allowed_versions"`
	DefaultVersion  string   `json:"default_version"`
}

// SubscriptionResponse from GET /billing/subscription.
type SubscriptionResponse struct {
	OrgID              string `json:"org_id"`
	Plan               string `json:"plan"`
	PlanName           string `json:"plan_name"`
	Status             string `json:"status"`
	MaxClusters        *int   `json:"max_clusters"`
	BillingExempt      bool   `json:"billing_exempt"`
	TrialDaysRemaining *int   `json:"trial_days_remaining"`
}

// PodSummary from the pods endpoint.
type PodSummary struct {
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	Ready        bool     `json:"ready"`
	RestartCount int      `json:"restart_count"`
	Containers   []string `json:"containers"`
	Age          string   `json:"age"`
}

// PodListResponse from GET /clusters/{id}/pods.
type PodListResponse struct {
	Namespace string       `json:"namespace"`
	Pods      []PodSummary `json:"pods"`
}

// AddAppRequest is the body for POST /clusters/{id}/add-app.
type AddAppRequest struct {
	Name   string         `json:"name,omitempty"`
	Config map[string]any `json:"config,omitempty"`
}

// CloudCreateRequest is the body for POST /clouds.
type CloudCreateRequest struct {
	Provider            string  `json:"provider"`
	DisplayName         string  `json:"display_name"`
	ProjectID           *string `json:"project_id,omitempty"`
	SAEmail             *string `json:"sa_email,omitempty"`
	Region              string  `json:"region,omitempty"`
	AWSAccountID        *string `json:"aws_account_id,omitempty"`
	AWSRoleARN          *string `json:"aws_role_arn,omitempty"`
	AWSRegion           *string `json:"aws_region,omitempty"`
	AzureSubscriptionID *string `json:"azure_subscription_id,omitempty"`
	AzureTenantID       *string `json:"azure_tenant_id,omitempty"`
	AzureClientID       *string `json:"azure_client_id,omitempty"`
	AzureResourceGroup  *string `json:"azure_resource_group,omitempty"`
	AzureLocation       *string `json:"azure_location,omitempty"`
}

// DevCredentialsResponse from POST /applications/{id}/dev-credentials.
type DevCredentialsResponse struct {
	CloudProvider  string         `json:"cloud_provider"`
	SecretsBackend string         `json:"secrets_backend"`
	BackendKwargs  map[string]any `json:"backend_kwargs"`
	Credentials    map[string]any `json:"credentials"`
	CredentialType string         `json:"credential_type"`
}

// CloudTestResponse from POST /clouds/test.
type CloudTestResponse struct {
	Impersonation struct {
		OK    bool    `json:"ok"`
		Error *string `json:"error"`
	} `json:"impersonation"`
	Preflight map[string]any `json:"preflight"`
}
