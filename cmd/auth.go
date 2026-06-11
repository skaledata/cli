package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/pkg/browser"
	"github.com/skaledata/cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authentication commands",
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in via browser (Clerk OAuth)",
	Long:  `Opens your browser to the SkaleData login page. After authenticating, the CLI receives an auth token via a localhost callback.`,
	RunE:  runLogin,
}

var setKeyCmd = &cobra.Command{
	Use:   "set-key <api-key>",
	Short: "Store an API key for CI/headless use",
	Long:  `Stores an API key (e.g. sk_xxx) in your config file. This is used for CI pipelines and headless environments where browser login isn't possible.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runSetKey,
}

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Clear stored credentials",
	RunE:  runLogout,
}

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.AddCommand(loginCmd)
	authCmd.AddCommand(setKeyCmd)
	authCmd.AddCommand(logoutCmd)

	// Also add `skale login` as a top-level alias for convenience
	rootCmd.AddCommand(&cobra.Command{
		Use:   "login",
		Short: "Log in via browser (alias for 'auth login')",
		RunE:  runLogin,
	})
}

func runLogin(cmd *cobra.Command, args []string) error {
	// Start a local HTTP server to receive the callback
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start callback server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	// Generate a random state parameter for CSRF protection
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return fmt.Errorf("generate state: %w", err)
	}
	state := hex.EncodeToString(stateBytes)

	tokenCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "Invalid state parameter", http.StatusBadRequest)
			errCh <- fmt.Errorf("CSRF state mismatch")
			return
		}

		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "No token received", http.StatusBadRequest)
			errCh <- fmt.Errorf("no token in callback")
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<h2>Authentication successful!</h2>
			<p>You can close this tab and return to the terminal.</p>
			<script>window.close()</script>
		</body></html>`)

		tokenCh <- token
	})

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Build the login URL — the frontend has a /cli-auth route that
	// handles Clerk sign-in and redirects to our callback with a token
	apiURL := viper.GetString("api_url")
	// Derive the frontend URL from the API URL (api.X -> app.X)
	frontendURL := strings.Replace(apiURL, "api.", "app.", 1)
	loginURL := fmt.Sprintf("%s/cli-auth?port=%d&state=%s", frontendURL, port, state)

	fmt.Printf("Opening browser to log in...\n")
	fmt.Printf("If it doesn't open, visit: %s\n\n", loginURL)

	if err := browser.OpenURL(loginURL); err != nil {
		fmt.Fprintf(os.Stderr, "Could not open browser: %v\n", err)
		fmt.Printf("Please open this URL manually: %s\n", loginURL)
	}

	fmt.Println("Waiting for authentication...")

	select {
	case token := <-tokenCh:
		server.Close()

		// Store the session JWT directly. Clerk's `skale-cli` JWT template
		// (configured in the dashboard) gives this an 8h TTL — long enough
		// to cover a workday, short enough that a leaked config file rots
		// same-day. For headless / CI use, `skale auth set-key` still
		// stores a long-lived API key.
		if err := config.SaveToken(token); err != nil {
			return fmt.Errorf("save token: %w", err)
		}
		// Clear any long-lived API key from a previous login so the new
		// session token actually takes effect (api_key takes precedence
		// over token in GetAuthHeader).
		_ = config.SaveAPIKey("")

		fmt.Println("Logged in successfully! (session valid for ~8 hours)")
		return nil
	case err := <-errCh:
		server.Close()
		return fmt.Errorf("login failed: %w", err)
	}
}

func runSetKey(cmd *cobra.Command, args []string) error {
	key := args[0]
	if !strings.HasPrefix(key, "sk_") {
		fmt.Fprintf(os.Stderr, "Warning: API keys typically start with 'sk_'. Storing anyway.\n")
	}

	if err := config.SaveAPIKey(key); err != nil {
		return fmt.Errorf("save API key: %w", err)
	}
	fmt.Println("API key saved.")
	return nil
}

func runLogout(cmd *cobra.Command, args []string) error {
	if err := config.SaveToken(""); err != nil {
		return err
	}
	if err := config.SaveAPIKey(""); err != nil {
		return err
	}
	fmt.Println("Credentials cleared.")
	return nil
}
