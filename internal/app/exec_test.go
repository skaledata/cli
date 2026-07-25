package app

import (
	"testing"

	"github.com/skaledata/cli/internal/api"
)

func TestEnvPrefix(t *testing.T) {
	cases := map[string]string{
		"airflow":   "AIRFLOW",
		"airbyte":   "AIRBYTE",
		"super-set": "SUPER_SET",
		"data.hub":  "DATA_HUB",
		"":          "",
		"_weird_":   "WEIRD",
		"a1b2":      "A1B2",
	}
	for in, want := range cases {
		if got := envPrefix(in); got != want {
			t.Errorf("envPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTokenEnv(t *testing.T) {
	tok := &api.AppTokenResponse{
		Token:   "tok123",
		AppType: "airflow",
		BaseURL: "https://airflow.example.com",
	}
	env := TokenEnv(tok)

	want := map[string]bool{
		"SKALE_APP_URL=https://airflow.example.com": false,
		"SKALE_APP_TOKEN=tok123":                    false,
		"SKALE_APP_TYPE=airflow":                    false,
		"AIRFLOW_URL=https://airflow.example.com":   false,
		"AIRFLOW_TOKEN=tok123":                      false,
	}
	for _, e := range env {
		if _, ok := want[e]; ok {
			want[e] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("TokenEnv missing %q; got %v", k, env)
		}
	}
}

func TestExecExitCodePropagation(t *testing.T) {
	tok := &api.AppTokenResponse{Token: "tok123", AppType: "airflow", BaseURL: "http://x"}

	// Child sees injected env AND we propagate its non-zero exit code.
	code, err := Exec(tok, []string{"sh", "-c",
		`test "$SKALE_APP_TOKEN" = "tok123" && test "$AIRFLOW_URL" = "http://x" && exit 7`})
	if err != nil {
		t.Fatalf("Exec returned setup error: %v", err)
	}
	if code != 7 {
		t.Fatalf("exit code = %d, want 7 (env not injected or code not propagated)", code)
	}

	// Success path.
	code, err = Exec(tok, []string{"true"})
	if err != nil || code != 0 {
		t.Fatalf("Exec(true) = (%d, %v), want (0, nil)", code, err)
	}
}

func TestExecEmptyArgv(t *testing.T) {
	if _, err := Exec(&api.AppTokenResponse{}, nil); err == nil {
		t.Fatal("expected error for empty argv")
	}
}
