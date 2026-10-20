// Copyright 2025-2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

// Package main is the entry point for starting the server.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/logout/backchannel"
	"github.com/thunder-id/thunderid/internal/system/cache"
	"github.com/thunder-id/thunderid/internal/system/config"
	"github.com/thunder-id/thunderid/internal/system/constants"
	sysContext "github.com/thunder-id/thunderid/internal/system/context"
	"github.com/thunder-id/thunderid/internal/system/database/provider"
	"github.com/thunder-id/thunderid/internal/system/jose/jwt"
	"github.com/thunder-id/thunderid/internal/system/kmprovider/common"
	"github.com/thunder-id/thunderid/internal/system/log"
	"github.com/thunder-id/thunderid/internal/system/mcp"
	"github.com/thunder-id/thunderid/internal/system/middleware"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
	"github.com/thunder-id/thunderid/internal/system/revocationcache"
	"github.com/thunder-id/thunderid/internal/system/security"
)

// shutdownTimeout defines the timeout duration for graceful shutdown.
const shutdownTimeout = 5 * time.Second

var (
	netListen = net.Listen
	tlsListen = tls.Listen
)

func main() {
	// Server bootstrap/shutdown logging has no request scope, so context.Background() is used.
	ctx := context.Background()
	startupStartedAt := time.Now()
	logger := log.GetLogger()

	flag.String("resources", "", "Path to declarative resources YAML file")
	serverHome := getThunderHome(ctx, logger)

	cfg := initThunderConfigurations(ctx, logger, serverHome)
	if cfg == nil {
		logger.Fatal(ctx, "Failed to initialize configurations")
	}

	// Apply the configured log level from deployment.yaml, now that config is loaded.
	if cfg.Log.Level != "" {
		if err := logger.SetLevel(cfg.Log.Level); err != nil {
			logger.Fatal(ctx, "Invalid log level in configuration", log.Error(err))
		}
	}

	// Apply the configured log output (console and/or rotating file), now that the
	// server home is known and the file path can be resolved.
	if err := logger.Configure(cfg.Log.BuildOutputOptions(serverHome)); err != nil {
		logger.Fatal(ctx, "Failed to configure log output", log.Error(err))
	}

	if isMigrationInvocation() {
		result, commandErr := runMigrationCommand(ctx, flag.Args(), cfg, provider.GetDBProvider())
		closeErr := provider.GetDBProviderCloser().Close()
		if result != nil {
			logMigrationCommandResult(ctx, logger, result, commandErr == nil)
		}
		if commandErr != nil {
			logger.Fatal(ctx, "Migration command failed", log.Error(commandErr))
		}
		if closeErr != nil {
			logger.Fatal(ctx, "Failed to close migration database connections", log.Error(closeErr))
		}
		return
	}

	startupCompatibility, err := runStartupCompatibilityCheck(ctx, cfg, provider.GetDBProvider())
	if err != nil {
		logger.Fatal(ctx, "Failed to check database compatibility before startup", log.Error(err))
	}
	if startupCompatibility == nil {
		logger.Warn(ctx, "Migration compatibility startup gate is not available for the configured database providers")
	} else {
		logMigrationCompatibility(ctx, logger, *startupCompatibility)
		if !startupCompatibility.Compatible() {
			logger.Fatal(ctx, "ThunderID cannot start because its databases are not compatible with this release")
		}
	}

	// Initialize the cache manager.
	cacheManager := cache.Initialize(cfg.Cache, cfg.Server.Identifier)

	// Initialize system permission strings before any service or middleware uses them.
	security.InitSystemPermissions(cfg.Server.SecurityConfig.SystemPermissionPrefix)

	// Create a new HTTP multiplexer.
	mux := http.NewServeMux()
	if mux == nil {
		logger.Fatal(ctx, "Failed to initialize multiplexer")
	}

	// Register the services.
	jwtService, runtimeCryptoSvc, importService, mcpServer, backchannelDispatcher := registerServices(mux, cacheManager)

	// When invoked as the bootstrap one-shot (`thunderid bootstrap`), create the
	// default resources in-process and exit without starting the HTTP server.
	if isBootstrapInvocation() {
		if err := runBootstrap(ctx, logger, serverHome, importService, cacheManager); err != nil {
			logger.Error(ctx, "In-process bootstrap failed; exiting", log.Error(err))
			os.Exit(1)
		}
		logger.Info(ctx, "In-process bootstrap finished successfully")
		return
	}

	// Initialize the Resource Server token-revocation cache. The initial deny-list snapshot is loaded
	// synchronously so enforcement is live before the first request; if that load fails the server
	// still starts and the syncer repopulates the cache on its next tick.
	revocationEnforcer, revocationSyncer := initRevocationCache(ctx, logger, cfg)
	revocationSyncer.Start(ctx)

	if backchannelDispatcher != nil {
		backchannelDispatcher.Start(ctx)
	}

	// Mount the MCP server's routes now that the revocation enforcer exists — DefaultGuard uses it
	// to authenticate MCP requests with the same verification and revocation logic as the REST gate.
	mcpGuard, mcpResourceMeta := mcp.DefaultGuard(jwtService, revocationEnforcer)
	mcp.Initialize(mux, mcpServer, mcpGuard, mcpResourceMeta)

	// Register static file handlers for frontend applications.
	registerStaticFileHandlers(ctx, logger, mux, serverHome)

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Create the HTTP server.
	server := createHTTPServer(ctx, logger, cfg, mux, jwtService, revocationEnforcer)
	var ln net.Listener
	if cfg.Server.HTTPOnly {
		logger.Info(ctx, "TLS is not enabled, starting server without TLS")
		ln = createListener(ctx, logger, server)
	} else {
		tlsConfigProvider, ok := runtimeCryptoSvc.(common.TLSConfigProvider)
		if !ok {
			logger.Fatal(ctx, "Runtime crypto provider does not support TLS material retrieval")
		}
		tlsConfig := loadCertConfig(ctx, logger, tlsConfigProvider)
		ln = createTLSListener(ctx, logger, server, tlsConfig)
	}

	serverURL := config.GetServerURL(&cfg.Server)
	consoleURL := fmt.Sprintf("%s/console", strings.TrimSuffix(serverURL, "/"))
	logger.Info(ctx, "ThunderID Server URL", log.String("url", serverURL))
	logger.Info(ctx, "ThunderID Console URL", log.String("url", consoleURL))

	// Start server in a goroutine
	go func() {
		startupDuration := time.Since(startupStartedAt)
		logger.Info(ctx, "ThunderID Server started", log.String("startup_time", startupDuration.String()))
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger.Fatal(ctx, "Failed to serve requests", log.Error(err))
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	logger.Info(ctx, "Shutting down server...")
	gracefulShutdown(ctx, logger, server, cacheManager, revocationSyncer, backchannelDispatcher)
}

func logMigrationCommandResult(
	ctx context.Context,
	logger *log.Logger,
	result *migrationCommandResult,
	successful bool,
) {
	logMigrationInitialization(ctx, logger, result.initialization)
	if result.compatibility != nil {
		fmt.Fprint(os.Stdout, formatMigrationCompatibility(*result.compatibility))
	}
	if result.preflight != nil {
		fmt.Fprint(os.Stdout, formatMigrationPreflight(*result.preflight))
	}
	if result.workflowCheck != nil {
		fmt.Fprint(os.Stdout, formatMigrationWorkflowPreflight(*result.workflowCheck))
	}
	if result.application != nil {
		logMigrationApplication(ctx, logger, *result.application, successful)
	}
	if result.workflow != nil {
		logMigrationWorkflow(ctx, logger, *result.workflow, successful)
	}
	if result.rollback != nil {
		logMigrationRollback(ctx, logger, *result.rollback, successful)
	}
	if result.compatibility == nil && result.preflight == nil && result.workflowCheck == nil &&
		result.application == nil && result.workflow == nil && result.rollback == nil {
		logger.Info(ctx, "Migration command result",
			log.String("action", result.action), log.Bool("successful", successful))
	}
}

func logMigrationRollback(
	ctx context.Context,
	logger *log.Logger,
	result engine.MigrationRollbackResult,
	successful bool,
) {
	for _, execution := range result.Operations {
		logger.Info(ctx, "Migration rollback operation completed",
			log.String("operation", execution.Progress.OperationID),
			log.String("executionTarget", execution.Progress.ExecutionTargetID),
			log.String("runID", execution.Progress.RunID),
			log.Bool("executionSkipped", execution.ExecutionSkipped),
			log.Int("committedBatches", execution.CommittedBatches))
	}
	message := "Migration rollback completed"
	if !successful {
		message = "Migration rollback incomplete"
	}
	logger.Info(ctx, message,
		log.String("runID", result.RunID),
		log.String("phase", string(result.Phase)),
		log.String("scope", string(result.Scope)),
		log.Int("operations", len(result.Operations)),
		log.Int("restoredStores", len(result.Finalizations)))
}

func logMigrationWorkflow(
	ctx context.Context,
	logger *log.Logger,
	result engine.MigrationWorkflowResult,
	successful bool,
) {
	for index, phase := range result.Phases {
		phaseSuccessful := successful || index < len(result.Phases)-1
		logMigrationApplication(ctx, logger, phase, phaseSuccessful)
	}
	message := "Pre-cutover migration workflow completed"
	if !successful {
		message = "Pre-cutover migration workflow incomplete"
	}
	logger.Info(ctx, message,
		log.Int("phases", len(result.Phases)),
		log.Bool("stoppedBeforeMigrate", true))
}

func logMigrationInitialization(
	ctx context.Context,
	logger *log.Logger,
	results []engine.InitializationResult,
) {
	for _, initialization := range results {
		logger.Info(ctx, "Migration metadata initialized",
			log.String("store", string(initialization.State.StoreID)),
			log.String("completedLevel", initialization.State.CompletedLevel),
			log.String("stage", string(initialization.State.Stage)),
			log.Bool("created", initialization.Created))
	}
}

func logMigrationCompatibility(
	ctx context.Context,
	logger *log.Logger,
	report engine.CompatibilityReport,
) {
	for _, store := range report.Stores {
		fields := []log.Field{
			log.String("store", string(store.StoreID)),
			log.Bool("compatible", store.Compatible()),
			log.Bool("skipped", store.Skipped),
			log.String("completedLevel", store.CompletedLevel),
			log.String("stage", string(store.Stage)),
			log.String("requiredLevel", store.RequiredLevel),
			log.String("status", string(store.Status)),
			log.String("reason", string(store.Reason)),
		}
		if store.Err != nil {
			fields = append(fields, log.Error(store.Err))
		}
		message := "Migration compatibility checked"
		if store.Skipped {
			message = "Migration compatibility skipped"
		}
		logger.Info(ctx, message, fields...)
	}
	logger.Info(ctx, "Migration compatibility status", log.Bool("compatible", report.Compatible()))
	if report.NextMigrationRelease != "" {
		logger.Info(ctx, "Migration route available",
			log.String("nextMigrationRelease", report.NextMigrationRelease),
			log.String("migrationRoute", strings.Join(report.MigrationRoute, ",")))
	} else if report.RouteErr != nil {
		logger.Info(ctx, "Migration route unavailable", log.Error(report.RouteErr))
	}
}

func logMigrationApplication(
	ctx context.Context,
	logger *log.Logger,
	result engine.MigrationRunResult,
	successful bool,
) {
	for _, resumption := range result.Resumptions {
		logger.Info(ctx, "Migration store resumed",
			log.String("store", string(resumption.State.StoreID)),
			log.String("runID", resumption.State.ActiveRunID),
			log.String("targetLevel", resumption.State.TargetLevel))
	}
	for _, execution := range result.Operations {
		logger.Info(ctx, "Migration operation completed",
			log.String("operation", execution.Progress.OperationID),
			log.String("executionTarget", execution.Progress.ExecutionTargetID),
			log.String("runID", execution.Progress.RunID),
			log.Bool("executionSkipped", execution.ExecutionSkipped),
			log.Int("committedBatches", execution.CommittedBatches))
	}
	for _, finalization := range result.Finalizations {
		logger.Info(ctx, "Migration store finalized",
			log.String("store", string(finalization.State.StoreID)),
			log.String("completedLevel", finalization.State.CompletedLevel),
			log.Bool("finalized", finalization.Finalized))
	}
	message := "Migration apply completed"
	if result.Resumed {
		message = "Migration resume completed"
	}
	if !successful {
		message = "Migration apply incomplete"
		if result.Resumed {
			message = "Migration resume incomplete"
		}
	}
	logger.Info(ctx, message,
		log.String("runID", result.RunID),
		log.String("phase", string(result.Phase)),
		log.String("targetVersion", result.Preflight.TargetVersion),
		log.Bool("alreadyAtTarget", result.AlreadyAtTarget),
		log.Int("operations", len(result.Operations)),
		log.Int("finalizedStores", len(result.Finalizations)))
}

// initRevocationCache builds the Resource Server token-revocation enforcer and its background syncer
// from the server security configuration. An unsupported source configuration fails startup; a
// failed initial deny-list load does not — the server starts and the syncer populates the cache later.
func initRevocationCache(ctx context.Context, logger *log.Logger,
	cfg *config.Config) (revocationcache.EnforcerInterface, revocationcache.Syncer) {
	rc := cfg.Server.SecurityConfig.TokenRevocation
	enforcer, syncer, err := revocationcache.Initialize(revocationcache.Config{
		Enabled:      rc.IsEnabled(),
		Source:       rc.Source,
		SyncInterval: time.Duration(rc.SyncIntervalSeconds) * time.Second,
	})
	if err != nil {
		logger.Fatal(ctx, "Failed to initialize token revocation cache", log.Error(err))
	}
	return enforcer, syncer
}

// getThunderHome retrieves and return the home directory.
func getThunderHome(ctx context.Context, logger *log.Logger) string {
	// Parse project directory from command line arguments.
	projectHome := ""
	projectHomeFlag := flag.String("serverHome", "", "Path to ThunderID home directory")
	flag.Parse()

	if *projectHomeFlag != "" {
		logger.Info(ctx, "Using serverHome from command line argument",
			log.String("serverHome", *projectHomeFlag))
		projectHome = *projectHomeFlag
	} else {
		// If no command line argument is provided, use the current working directory.
		dir, dirErr := os.Getwd()
		if dirErr != nil {
			logger.Fatal(ctx, "Failed to get current working directory", log.Error(dirErr))
		}
		projectHome = dir
	}

	return projectHome
}

// initThunderConfigurations initializes the configurations.
func initThunderConfigurations(ctx context.Context, logger *log.Logger, serverHome string) *config.Config {
	// Load the configurations.
	configFilePath := path.Join(serverHome, "deployment.yaml")
	defaultConfigPath := path.Join(serverHome, "config/default.json")
	cfg, err := config.LoadConfig(configFilePath, defaultConfigPath, serverHome)
	if err != nil {
		logger.Fatal(ctx, "Failed to load configurations", log.Error(err))
	}

	// Initialize runtime configurations.
	if err := config.InitializeServerRuntime(serverHome, cfg); err != nil {
		logger.Fatal(ctx, "Failed to initialize server runtime", log.Error(err))
	}

	return cfg
}

// loadCertConfig loads the TLS material via the runtime crypto provider.
func loadCertConfig(ctx context.Context, logger *log.Logger, runtimeSvc common.TLSConfigProvider) *tls.Config {
	mat, err := runtimeSvc.GetTLSMaterial(ctx)
	if err != nil {
		logger.Fatal(ctx, "Failed to load TLS material", log.Error(err))
	}
	// #nosec G402 -- MinVersion is set to TLS 1.2 or higher by GetTLSMaterial
	return &tls.Config{
		Certificates: []tls.Certificate{mat.Certificate},
		MinVersion:   mat.MinVersion,
	}
}

// accessLogExcludePaths returns the path prefixes excluded from the access log: the always-excluded
// Gate and Console frontend paths plus any configured extras. Empty and root ("/") entries are
// dropped because they match every request and would silently disable all access logging.
func accessLogExcludePaths(configured []string) []string {
	paths := []string{"/gate/", "/console/"}
	for _, prefix := range configured {
		if prefix != "" && prefix != "/" {
			paths = append(paths, prefix)
		}
	}
	return paths
}

// createHTTPServer creates and configures an HTTP server with common settings.
func createHTTPServer(ctx context.Context, logger *log.Logger, cfg *config.Config, mux *http.ServeMux,
	jwtService jwt.JWTServiceInterface, revocationEnforcer revocationcache.EnforcerInterface) *http.Server {
	securityMiddleware := createSecurityMiddleware(ctx, logger, cfg, mux, jwtService, revocationEnforcer)
	operatingModeMiddleware := middleware.RestrictedModeMiddleware(
		cfg.OperatingMode.IsRestricted())(securityMiddleware)

	// Build the middleware chain with proper execution order.
	// Request flow: CorrelationID (outermost) -> DeploymentID -> SecurityHeaders -> AccessLog ->
	// RestrictedMode -> Security -> Route Handler (innermost)
	// Note: Middlewares are wrapped in reverse order - the last added will execute first.
	// The Gate and Console frontend paths are always excluded from the access log to keep it
	// focused on API traffic. Additional prefixes can be excluded via log.access.exclude_paths.
	handler := log.AccessLogHandler(logger, accessLogExcludePaths(cfg.Log.Access.ExcludePaths), operatingModeMiddleware)
	handler = middleware.SecurityHeadersMiddleware()(handler)
	// Outside the security layer, so that every request carries the deployment id it acts for by the
	// time any store is reached.
	handler = middleware.DeploymentIDMiddleware(handler)
	handler = middleware.CorrelationIDMiddleware(handler)

	// Build the server address using hostname and port from the configurations.
	serverAddr := fmt.Sprintf("%s:%d", cfg.Server.Hostname, cfg.Server.Port)

	server := &http.Server{
		Addr:              serverAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second, // Mitigate Slowloris attacks
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          log.NewServerErrorLog(logger),
	}

	return server
}

// createListener creates and returns a listener for the HTTP server.
func createListener(ctx context.Context, logger *log.Logger, server *http.Server) net.Listener {
	ln, err := netListen("tcp", server.Addr)
	if err != nil {
		logger.Fatal(ctx, "Failed to start HTTP listener", log.Error(err))
	}
	return ln
}

// createTLSListener creates and returns a TLS listener for the HTTPS server.
func createTLSListener(ctx context.Context, logger *log.Logger, server *http.Server,
	tlsConfig *tls.Config) net.Listener {
	ln, err := tlsListen("tcp", server.Addr, tlsConfig)
	if err != nil {
		logger.Fatal(ctx, "Failed to start TLS listener", log.Error(err))
	}
	return ln
}

func createSecurityMiddleware(ctx context.Context, logger *log.Logger, cfg *config.Config,
	mux *http.ServeMux, jwtService jwt.JWTServiceInterface,
	revocationEnforcer revocationcache.EnforcerInterface) http.Handler {
	// Record which posture is in force, so it is not inferred from a missing config key. Info, not
	// Warn: leaving the audience unchecked is the documented default, and config validation already
	// rejects the one value that would be a mistake.
	expectedAud := ""
	if restAudience := cfg.Server.SecurityConfig.REST.Audience; restAudience != nil {
		expectedAud = *restAudience
		logger.Info(ctx, "REST API audience validation enabled")
	} else {
		logger.Info(ctx, "REST API audience validation not enabled, accepting tokens for any audience")
	}

	middlewareFunc, err := security.Initialize(jwtService, revocationEnforcer, expectedAud,
		cfg.Server.SecurityConfig.ManagementAPIKeyHash)
	if err != nil {
		logger.Fatal(ctx, "Failed to initialize security middleware", log.Error(err))
	}
	return middlewareFunc(mux)
}

// gracefulShutdown handles the graceful shutdown of all components.
func gracefulShutdown(
	ctx context.Context,
	logger *log.Logger,
	server *http.Server,
	cacheManager cache.CacheManagerInterface,
	revocationSyncer revocationcache.Syncer,
	backchannelDispatcher backchannel.DispatcherInterface,
) {
	ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	// Shutdown HTTP server
	if err := server.Shutdown(ctx); err != nil {
		logger.Error(ctx, "Error during server shutdown", log.Error(err))
	} else {
		logger.Debug(ctx, "HTTP server shutdown completed")
	}

	// Stop the token-revocation cache syncer.
	revocationSyncer.Stop()

	// Stop back-channel logout delivery before observability shuts down, so the final delivery
	// outcomes still reach a subscriber.
	if backchannelDispatcher != nil {
		backchannelDispatcher.Stop()
	}

	// Shutdown services
	unregisterServices()

	// Close database connections
	dbCloser := provider.GetDBProviderCloser()
	if err := dbCloser.Close(); err != nil {
		logger.Error(ctx, "Error closing database connections", log.Error(err))
	} else {
		logger.Debug(ctx, "Database connections closed successfully")
	}

	if cacheManager != nil {
		cacheManager.Close()
		logger.Debug(ctx, "Cache manager closed successfully")
	}

	// Close the log file writer before the final shutdown log line.
	if err := logger.Close(); err != nil {
		logger.Error(ctx, "Error closing log file", log.Error(err))
	}

	logger.Info(ctx, "Server shutdown completed")
}

// registerStaticFileHandlers registers static file handlers for frontend applications.
func registerStaticFileHandlers(ctx context.Context, logger *log.Logger, mux *http.ServeMux, serverHome string) {
	// Override the OS-level MIME mapping so .js/.mjs files are served as
	// application/javascript. Most proxies (Envoy, NGINX, Cloudflare) only
	// compress application/javascript in their default allowlists, not
	// text/javascript, which is Go's default on some systems.
	_ = mime.AddExtensionType(".js", "application/javascript; charset=utf-8")
	_ = mime.AddExtensionType(".mjs", "application/javascript; charset=utf-8")

	// Serve gate application from /gate
	gateDir := path.Join(serverHome, "apps", "gate")
	if handler, err := createStaticFileHandler("/gate/", gateDir, logger); err != nil {
		logger.Warn(ctx, "Gate application not registered", log.String("directory", gateDir), log.Error(err))
	} else {
		logger.Debug(ctx, "Registering static file handler for Gate application",
			log.String("path", "/gate/"), log.String("directory", gateDir))
		mux.Handle("/gate/", handler)
	}

	// Serve console application from /console
	consoleDir := path.Join(serverHome, "apps", "console")
	if handler, err := createStaticFileHandler("/console/", consoleDir, logger); err != nil {
		logger.Warn(ctx, "Console application not registered", log.String("directory", consoleDir), log.Error(err))
	} else {
		logger.Debug(ctx, "Registering static file handler for Console application",
			log.String("path", "/console/"), log.String("directory", consoleDir))
		mux.Handle("/console/", handler)
	}
}

// createStaticFileHandler creates a handler for serving static files with SPA fallback.
//
// All filesystem access is performed through an os.Root anchored at `directory`, which
// confines every lookup below that directory: any request path that would escape it fails
// with a path-escape error rather than reaching the filesystem.
func createStaticFileHandler(routePrefix, directory string, logger *log.Logger) (http.Handler, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	rootFS := root.FS()
	fileServer := http.FileServerFS(rootFS)

	// serveIndex serves index.html with no-cache headers, substituting the request's CSP nonce
	// (see SecurityHeadersMiddleware) for the placeholder in its <meta property="csp-nonce"> tag, so
	// the frontend can apply the same nonce to its own inline <style> tags. It reports whether
	// index.html existed and was served.
	serveIndex := func(w http.ResponseWriter, r *http.Request) bool {
		content, err := fs.ReadFile(rootFS, "index.html")
		if err != nil {
			return false
		}
		nonce := sysContext.GetCSPNonce(r.Context())
		content = bytes.ReplaceAll(content, []byte(constants.CSPNoncePlaceholder), []byte(nonce))

		w.Header().Set(constants.CacheControlHeaderName, constants.CacheControlNoCacheComposite)
		w.Header().Set(constants.PragmaHeaderName, constants.PragmaNoCache)
		w.Header().Set(constants.ExpiresHeaderName, constants.ExpiresZero)
		w.Header().Set(constants.ContentTypeHeaderName, constants.ContentTypeHTML)
		_, _ = w.Write(content)
		return true
	}

	return http.StripPrefix(routePrefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle the application root by explicitly serving index.html.
		if r.URL.Path == "/" || r.URL.Path == "" {
			if serveIndex(w, r) {
				return
			}
		}

		// Resolve the request against the served directory.
		name := strings.TrimPrefix(r.URL.Path, "/")
		isIndexHTML := name == "index.html"

		if name != "" {
			if _, err := root.Stat(name); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					// For SPA routing, serve index.html for non-existent in-bounds paths.
					logger.Debug(r.Context(), "Serving index.html for SPA routing",
						log.String("requested_path", r.URL.Path),
						log.String("route_prefix", routePrefix))
					if serveIndex(w, r) {
						return
					}
				} else {
					// The path escapes the served directory (or is otherwise invalid).
					logger.Warn(r.Context(), "Rejected request with out-of-bounds path",
						log.String("requested_path", r.URL.Path),
						log.String("route_prefix", routePrefix))
					http.NotFound(w, r)
					return
				}
			}
		}

		// Serve index.html directly with no-cache headers when requested.
		if isIndexHTML {
			if serveIndex(w, r) {
				return
			}
		}

		// Serve the requested file or directory listing.
		fileServer.ServeHTTP(w, r)
	})), nil
}
