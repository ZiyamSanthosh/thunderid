// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

//go:build migration_integration

package harness

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const sourceImageRepository = "ghcr.io/thunder-id/thunderid"

type migrationTestConfig struct {
	SourceProductVersion string `yaml:"sourceProductVersion"`
	// SourceImage names a locally available image whose dbscripts create the source databases. It
	// replaces the published image selected by SourceProductVersion, which is then not required.
	SourceImage    string `yaml:"sourceImage"`
	DatabaseLayout string `yaml:"databaseLayout"`
}

type Environment struct {
	Host        string
	Port        string
	directory   string
	dockerEnv   []string
	database    string
	sourceImage string
	mounts      []string
}

func Start(t *testing.T, configPath string) Environment {
	t.Helper()
	testDirectory := migrationTestDirectory(t)
	config := loadMigrationTestConfig(t, configPath)
	projectName := fmt.Sprintf("thunderid-migration-%d", time.Now().UnixNano())
	sourceImage := sourceImageRepository + ":" + config.SourceProductVersion
	if config.SourceImage != "" {
		sourceImage = config.SourceImage
	}
	environment := append(os.Environ(),
		"COMPOSE_PROJECT_NAME="+projectName,
		"SOURCE_PRODUCT_VERSION="+config.SourceProductVersion,
		"SOURCE_THUNDERID_IMAGE="+sourceImage,
		"MIGRATION_DATABASE_LAYOUT="+config.DatabaseLayout,
	)
	database := ""
	if config.DatabaseLayout == "shared" {
		environment = append(environment,
			"COMPOSE_FILE=docker-compose.yml:docker-compose.shared.yml")
		database = "thunderid"
	}

	runDocker(t, testDirectory, environment, "info")
	runDocker(t, testDirectory, environment, "compose", "version")
	t.Cleanup(func() {
		command := exec.Command("docker", "compose", "down", "--volumes", "--remove-orphans", "--rmi", "local")
		command.Dir = testDirectory
		command.Env = environment
		if output, err := command.CombinedOutput(); err != nil {
			t.Logf("failed to clean migration integration environment: %v\n%s", err, output)
		}
	})

	runDocker(t, testDirectory, environment, "compose", "build", "migration-runner")
	runDocker(t, testDirectory, environment, "compose", "up", "--detach", "--wait", "postgres")
	runDocker(t, testDirectory, environment, "compose", "run", "--rm", "--no-deps", "migration-runner")

	address := strings.TrimSpace(runDocker(t, testDirectory, environment,
		"compose", "port", "postgres", "5432"))
	host, port, err := net.SplitHostPort(address)
	require.NoError(t, err, "parse the published PostgreSQL address %q", address)
	return Environment{
		Host: host, Port: port, directory: testDirectory,
		dockerEnv:   append([]string(nil), environment...),
		database:    database,
		sourceImage: sourceImage,
	}
}

// SourceFile returns a file shipped in the source image, relative to its working directory.
func (e Environment) SourceFile(t *testing.T, path string) string {
	t.Helper()
	output, err := exec.Command("docker", "run", "--rm", "--entrypoint", "cat", e.sourceImage, path).Output()
	require.NoError(t, err, "read %s from source image %s", path, e.sourceImage)
	return string(output)
}

func (e Environment) RunMigrationCommand(t *testing.T, action string) string {
	t.Helper()
	arguments := e.migrationCommandArguments(action)
	command := exec.Command("docker", arguments...) //nolint:gosec // The arguments are built by the harness.
	command.Dir = e.directory
	command.Env = e.dockerEnv
	output, err := command.CombinedOutput()
	if err != nil {
		// A blocked preflight only says so. The check report names the operation and the reason.
		report := ""
		if strings.Contains(string(output), "preflight is blocked") {
			phase := ""
			if fields := strings.Fields(action); len(fields) > 1 && fields[0] == "apply" {
				phase = " " + fields[1]
			}
			checkCommand := exec.Command( //nolint:gosec // The arguments are built by the harness.
				"docker", e.migrationCommandArguments("check"+phase)...)
			checkCommand.Dir = e.directory
			checkCommand.Env = e.dockerEnv
			checked, _ := checkCommand.CombinedOutput()
			report = "\nmigration check" + phase + ":\n" + string(checked)
		}
		t.Fatalf("docker %s failed: %v\n%s%s", strings.Join(arguments, " "), err, output, report)
	}
	if len(output) > 0 {
		t.Logf("docker %s:\n%s", strings.Join(arguments, " "), output)
	}
	return string(output)
}

func (e Environment) RunMigrationCommandExpectFailure(t *testing.T, action string) string {
	t.Helper()
	arguments := e.migrationCommandArguments(action)
	command := exec.Command("docker", arguments...)
	command.Dir = e.directory
	command.Env = e.dockerEnv
	output, err := command.CombinedOutput()
	require.Error(t, err, "docker %s unexpectedly succeeded", strings.Join(arguments, " "))
	t.Logf("docker %s failed as expected:\n%s", strings.Join(arguments, " "), output)
	return string(output)
}

func (e Environment) RunServerExpectCompatibilityFailure(t *testing.T) string {
	t.Helper()
	arguments := []string{
		"compose", "run", "--rm", "--no-deps",
		"--entrypoint", "/opt/thunderid/thunderid", "migration-runner",
		"-serverHome", "/opt/thunderid",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "docker", arguments...)
	command.Dir = e.directory
	command.Env = e.dockerEnv
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("ThunderID startup did not reject the incompatible databases within 30 seconds:\n%s", output)
	}
	require.Error(t, err, "ThunderID unexpectedly started with incompatible databases")
	t.Logf("ThunderID startup failed as expected:\n%s", output)
	return string(output)
}

func (e Environment) migrationCommandArguments(action string) []string {
	arguments := []string{"compose", "run", "--rm", "--no-deps"}
	arguments = append(arguments, e.mounts...)
	arguments = append(arguments,
		"--entrypoint", "/opt/thunderid/thunderid", "migration-runner",
		"-serverHome", "/opt/thunderid", "migration")
	return append(arguments, strings.Fields(action)...)
}

// Shared reports whether every store lives in one database.
func (e Environment) Shared() bool {
	return e.database != ""
}

// OpenDatabase opens the database of a store, or the one shared database.
func (e Environment) OpenDatabase(t *testing.T, database string) *sql.DB {
	t.Helper()
	if e.database != "" {
		database = e.database
	}
	return e.OpenNamedDatabase(t, database)
}

// OpenNamedDatabase opens a database by its exact name, such as the admin database or one the test created.
func (e Environment) OpenNamedDatabase(t *testing.T, database string) *sql.DB {
	t.Helper()
	dataSourceName := fmt.Sprintf(
		"host=%s port=%s user=migration_user password=migration_password dbname=%s sslmode=disable",
		e.Host,
		e.Port,
		database,
	)
	db, err := sql.Open("postgres", dataSourceName)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	require.NoError(t, db.Ping())
	return db
}

func AssertQueryValue(t *testing.T, db *sql.DB, query string, expected string, arguments ...any) {
	t.Helper()
	var actual string
	require.NoError(t, db.QueryRow(query, arguments...).Scan(&actual))
	require.Equal(t, expected, actual)
}

func AssertOperationCompleted(t *testing.T, db *sql.DB, operationID string) {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM "MIGRATION_PROGRESS" WHERE OPERATION_ID = $1 AND STATUS = 'completed'`,
		operationID,
	).Scan(&count))
	require.Equal(t, 1, count, "operation progress for %s", operationID)
}

func migrationTestDirectory(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Dir(filepath.Dir(filename))
}

func loadMigrationTestConfig(t *testing.T, path string) migrationTestConfig {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var config migrationTestConfig
	require.NoError(t, yaml.Unmarshal(data, &config))
	if config.SourceImage == "" {
		require.Regexp(t, regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`), config.SourceProductVersion,
			"sourceProductVersion must be a canonical product version")
	}
	if config.DatabaseLayout == "" {
		config.DatabaseLayout = "separate"
	}
	require.Contains(t, []string{"separate", "shared"}, config.DatabaseLayout,
		"databaseLayout must be separate or shared")
	return config
}

func runDocker(t *testing.T, directory string, environment []string, arguments ...string) string {
	t.Helper()
	command := exec.Command("docker", arguments...)
	command.Dir = directory
	command.Env = environment
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s failed: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	if len(output) > 0 {
		t.Logf("docker %s:\n%s", strings.Join(arguments, " "), output)
	}
	return string(output)
}

// Service is a server that compose started in the background.
type Service struct {
	environment Environment
	container   string
}

// RunPreviousServerBootstrap creates the default resources of the source release, as its own installation does,
// so that the databases hold the data a real installation has.
func (e Environment) RunPreviousServerBootstrap(t *testing.T) {
	t.Helper()
	runDocker(t, e.directory, e.dockerEnv, "compose", "run", "--rm", "--no-deps", "previous-server", "bootstrap")
}

// StartPreviousServer starts the server of the source release against the databases.
func (e Environment) StartPreviousServer(t *testing.T) Service {
	t.Helper()
	return e.startService(t, "previous-server", "serve")
}

// StartTargetServer starts the server built from this checkout against the databases.
func (e Environment) StartTargetServer(t *testing.T) Service {
	t.Helper()
	return e.startService(t, append(append([]string(nil), e.mounts...),
		"--entrypoint", "/bin/sh", "migration-runner", "/test/target-server.sh")...)
}

func (e Environment) startService(t *testing.T, arguments ...string) Service {
	t.Helper()
	command := append([]string{"compose", "run", "-d", "--no-deps", "--use-aliases"}, arguments...)
	output := strings.TrimSpace(runDocker(t, e.directory, e.dockerEnv, command...))
	lines := strings.Split(output, "\n")
	service := Service{environment: e, container: strings.TrimSpace(lines[len(lines)-1])}
	require.NotEmpty(t, service.container)
	t.Cleanup(func() { service.remove() })
	return service
}

func (s Service) remove() {
	_ = exec.Command("docker", "rm", "-f", s.container).Run() //nolint:gosec // The container name comes from compose.
}

// Stop removes the server.
func (s Service) Stop(t *testing.T) {
	t.Helper()
	runDocker(t, s.environment.directory, s.environment.dockerEnv, "rm", "-f", s.container)
}

// Logs returns what the server has written so far.
func (s Service) Logs(t *testing.T) string {
	t.Helper()
	output, err := exec.Command("docker", "logs", s.container).CombinedOutput() //nolint:gosec // Compose names it.
	require.NoError(t, err)
	return string(output)
}

// get reads a path of the server. The server listens on the address of its container, not on the loopback one.
func (s Service) get(path string) (string, error) {
	output, err := exec.Command("docker", "exec", s.container, "sh", "-c", //nolint:gosec
		`wget -qO- "http://$(hostname):8090`+path+`"`).CombinedOutput()
	return string(output), err
}

// WaitReady waits for the server to report its databases up.
func (s Service) WaitReady(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		body, err := s.get("/health/readiness")
		last = body
		if err == nil && strings.Contains(body, `"status":"UP"`) && !strings.Contains(body, `"status":"DOWN"`) {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("the server did not become ready within 90 seconds, last answer %q\n%s", last, s.Logs(t))
}

// Authenticate signs a user in through the Direct API and returns the assertion. It reads the user and its
// credentials from the databases, so it fails when the server cannot work against their schema.
func (s Service) Authenticate(t *testing.T, username, password string) string {
	t.Helper()
	body := fmt.Sprintf(`{"identifiers":{"username":%q},"credentials":{"password":%q}}`, username, password)
	output, err := exec.Command("docker", "exec", "-e", "BODY="+body, s.container, "sh", "-c", //nolint:gosec
		`wget -qO- --header="Content-Type: application/json" --header="Direct-Auth-Secret: secret" `+
			`--post-data="$BODY" "http://$(hostname):8090/auth/credentials/authenticate"`).CombinedOutput()
	require.NoError(t, err, "sign in as %s failed:\n%s\n%s", username, output, s.Logs(t))
	require.Contains(t, string(output), `"assertion":"`)
	return string(output)
}

// RequireNoErrorLogs fails when the server logged an error.
func (s Service) RequireNoErrorLogs(t *testing.T) {
	t.Helper()
	for _, line := range strings.Split(s.Logs(t), "\n") {
		if strings.Contains(line, "level=ERROR") {
			t.Fatalf("the server logged an error: %s", line)
		}
	}
}

// WithDeclaredOrganizationUnit returns the environment with one more organization unit declared in a resource
// file of the migration runner and of the target server, as an installation that keeps its resources
// declarative does instead of storing them in the database.
func (e Environment) WithDeclaredOrganizationUnit(t *testing.T, id, handle, name string) Environment {
	t.Helper()
	file := filepath.Join(t.TempDir(), handle+".yaml")
	content := fmt.Sprintf("resource_type: organization_unit\nid: %s\nhandle: %s\nname: %s\n", id, handle, name)
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
	e.mounts = append(append([]string(nil), e.mounts...),
		"-v", file+":/opt/thunderid/config/resources/organization_units/"+handle+".yaml:ro")
	return e
}
