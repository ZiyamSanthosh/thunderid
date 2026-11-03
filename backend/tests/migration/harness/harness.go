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
	DatabaseLayout       string `yaml:"databaseLayout"`
}

type Environment struct {
	Host      string
	Port      string
	directory string
	dockerEnv []string
	database  string
}

func Start(t *testing.T, configPath string) Environment {
	t.Helper()
	testDirectory := migrationTestDirectory(t)
	config := loadMigrationTestConfig(t, configPath)
	projectName := fmt.Sprintf("thunderid-migration-%d", time.Now().UnixNano())
	environment := append(os.Environ(),
		"COMPOSE_PROJECT_NAME="+projectName,
		"SOURCE_PRODUCT_VERSION="+config.SourceProductVersion,
		"SOURCE_THUNDERID_IMAGE="+sourceImageRepository+":"+config.SourceProductVersion,
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
		dockerEnv: append([]string(nil), environment...),
		database:  database,
	}
}

func (e Environment) RunMigrationCommand(t *testing.T, action string) string {
	t.Helper()
	return runDocker(t, e.directory, e.dockerEnv, migrationCommandArguments(action)...)
}

func (e Environment) RunMigrationCommandExpectFailure(t *testing.T, action string) string {
	t.Helper()
	arguments := migrationCommandArguments(action)
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

func migrationCommandArguments(action string) []string {
	arguments := []string{
		"compose", "run", "--rm", "--no-deps",
		"--entrypoint", "/opt/thunderid/thunderid", "migration-runner",
		"-serverHome", "/opt/thunderid", "migration",
	}
	return append(arguments, strings.Fields(action)...)
}

func (e Environment) OpenDatabase(t *testing.T, database string) *sql.DB {
	t.Helper()
	if e.database != "" {
		database = e.database
	}
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
	require.Regexp(t, regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`), config.SourceProductVersion,
		"sourceProductVersion must be a canonical product version")
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
