package app

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/skaledata/cli/internal/api"
)

// TokenEnv returns the environment variables to inject for an app token.
// It sets generic SKALE_APP_* vars plus app-specific aliases (e.g.
// AIRFLOW_URL / AIRFLOW_TOKEN) so common clients and SDKs pick them up
// with no extra configuration.
func TokenEnv(tok *api.AppTokenResponse) []string {
	env := []string{
		"SKALE_APP_URL=" + tok.BaseURL,
		"SKALE_APP_TOKEN=" + tok.Token,
		"SKALE_APP_TYPE=" + tok.AppType,
	}
	if prefix := envPrefix(tok.AppType); prefix != "" {
		env = append(env,
			prefix+"_URL="+tok.BaseURL,
			prefix+"_TOKEN="+tok.Token,
		)
	}
	return env
}

// envPrefix turns an app type into a shell-safe env var prefix, e.g.
// "airflow" -> "AIRFLOW". Returns "" if nothing usable remains.
func envPrefix(appType string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(appType) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

// Exec runs argv with the app-token environment injected into the child
// process only (never written to disk or the parent env), streaming
// stdio through. It returns the child's exit code. Setup failures (empty
// argv, command not found) return a non-nil error.
//
// SIGINT/SIGTERM are handled so the parent survives long enough to reap
// the child and propagate its exit code: the child shares the terminal's
// process group and receives the signal directly, while the parent drains
// and waits.
func Exec(tok *api.AppTokenResponse, argv []string) (int, error) {
	if len(argv) == 0 {
		return 1, fmt.Errorf("no command to run")
	}

	c := exec.Command(argv[0], argv[1:]...)
	c.Env = append(os.Environ(), TokenEnv(tok)...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		// Drain so the parent doesn't die on Ctrl-C; the child, in the
		// same process group, gets the signal too and decides how to exit.
		for range sigCh {
		}
	}()

	if err := c.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}
