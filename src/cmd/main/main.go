package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/twistingmercury/mnemonic/internal/config"
	"github.com/twistingmercury/mnemonic/internal/server"
	"github.com/twistingmercury/mnemonic/internal/version"
)

var verFlag = pflag.Bool("version", false, "Displays current version information for mnemonic")
var healthFlag = pflag.Bool("health", false, "Get the current health of the service")

// @title Mnemonic API
// @version 1.0
// @description REST API for the Mnemonic agent-pattern-skill management service
// @host localhost:8080
// @BasePath /v1/api
// @schemes http
func main() {
	pflag.Parse()

	if *verFlag {
		println(version.Print())
		os.Exit(0)
	}

	if *healthFlag {
		exitCode := checkHealth()
		os.Exit(exitCode)
	}

	cfg, err := config.Load()

	if err != nil {
		log.Fatalf("failed to load configuration: %s", err)
	}

	// Health checks are initialized inside ListenAndServe after database
	// connections are established, so no separate health.Initialize call
	// is needed here.

	if err := server.ListenAndServe(cfg); err != nil {
		log.Fatalf("exited with err: %s\n", err.Error())
	}
}

// healthCheckTimeout is the HTTP client timeout for the CLI health probe.
// Kept short because Docker healthcheck has its own outer timeout.
const healthCheckTimeout = 3 * time.Second

// checkHealth makes an HTTP GET request to the running server's /health
// endpoint and reports the result. It is designed for use as a Docker
// HEALTHCHECK command in scratch/static containers where curl is unavailable.
func checkHealth() (exitCode int) {
	port := resolveServerPort()
	url := fmt.Sprintf("http://localhost:%d%s", port, config.DefaultHealthPath)

	client := &http.Client{Timeout: healthCheckTimeout}

	resp, err := client.Get(url) // #nosec G107 -- URL is constructed from local config, not user input
	if err != nil {
		fmt.Fprintf(os.Stderr, "unhealthy: %v\n", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusOK {
		fmt.Println(formatHealthStatus(body))
		return 0
	}

	fmt.Fprintf(os.Stderr, "unhealthy: HTTP %d\n", resp.StatusCode)
	if len(body) > 0 {
		fmt.Fprintln(os.Stderr, formatHealthStatus(body))
	}
	return 1
}

// resolveServerPort reads the server port from config sources (env vars,
// config file, defaults) without performing full validation. This avoids
// requiring database credentials just to run a health probe.
func resolveServerPort() int {
	v := viper.New()
	config.SetDefaults(v)

	// Allow env-var override (e.g. MNEMONIC_SERVER_PORT maps to server.port).
	v.SetEnvPrefix(config.EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	return v.GetInt("server.port")
}

// formatHealthStatus returns a human-readable one-line summary from the
// heartbeat JSON response. Falls back to the raw body on parse failure.
func formatHealthStatus(body []byte) string {
	var resp struct {
		Status string `json:"status"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal(body, &resp); err == nil && resp.Status != "" {
		return fmt.Sprintf("%s: %s", resp.Name, resp.Status)
	}
	return string(body)
}
