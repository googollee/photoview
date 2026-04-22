package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"

	"github.com/photoview/photoview/api/database"
	"github.com/photoview/photoview/api/dataloader"
	"github.com/photoview/photoview/api/globalinit"
	"github.com/photoview/photoview/api/graphql/auth"
	graphql_endpoint "github.com/photoview/photoview/api/graphql/endpoint"
	"github.com/photoview/photoview/api/log"
	"github.com/photoview/photoview/api/routes"
	"github.com/photoview/photoview/api/scanner/externaltools/exif"
	"github.com/photoview/photoview/api/scanner/face_detection"
	"github.com/photoview/photoview/api/scanner/media_encoding/executable_worker"
	"github.com/photoview/photoview/api/scanner/periodic_scanner"
	"github.com/photoview/photoview/api/scanner/scanner_queue"
	"github.com/photoview/photoview/api/server"
	"github.com/photoview/photoview/api/utils"
)

func main() {
	ctx := context.Background()

	log.Info(ctx, "Starting Photoview...")

	if err := godotenv.Load(); err != nil {
		log.Warn(ctx, "No .env file found. If Photoview runs in Docker, this is expected and correct.")
	}

	terminate, err := globalinit.Initialize(ctx)
	defer terminate(ctx)
	if err != nil {
		log.Error(ctx, "initialize error", "errors", err)
	}

	terminateWorkers := executable_worker.Initialize()
	defer terminateWorkers()

	devMode := utils.DevelopmentMode()

	db, err := database.SetupDatabase()
	if err != nil {
		log.Error(ctx, "Could not connect to database", "error", err)
		os.Exit(255)
	}

	// Migrate database
	if err := database.MigrateDatabase(db); err != nil {
		log.Error(ctx, "Could not migrate database", "error", err)
		os.Exit(255)
	}

	exifCleanup, err := exif.Initialize()
	if err != nil {
		log.Error(ctx, "Could not initialize exif parser", "error", err)
		os.Exit(255)
	}
	defer exifCleanup()

	if err := scanner_queue.InitializeScannerQueue(db); err != nil {
		log.Error(ctx, "Could not initialize scanner queue", "error", err)
		os.Exit(255)
	}

	if err := periodic_scanner.InitializePeriodicScanner(db); err != nil {
		log.Error(ctx, "Could not initialize periodic scanner", "error", err)
		os.Exit(255)
	}

	if err := face_detection.InitializeFaceDetector(db); err != nil {
		log.Error(ctx, "Could not initialize face detector", "error", err)
		os.Exit(255)
	}

	rootRouter := mux.NewRouter()
	rootRouter.Use(dataloader.Middleware(db))
	rootRouter.Use(auth.Middleware(db))
	rootRouter.Use(server.LoggingMiddleware)
	rootRouter.Use(server.CORSMiddleware(devMode))

	apiListenURL := utils.ApiListenUrl()

	endpointRouter := rootRouter.PathPrefix(apiListenURL.Path).Subrouter()

	if devMode {
		endpointRouter.Handle("/", playground.Handler("GraphQL playground", path.Join(apiListenURL.Path, "graphql")))
	} else {
		endpointRouter.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
			w.Write([]byte("photoview api endpoint"))
		})
	}

	endpointRouter.Handle("/graphql", handlers.CompressHandler(graphql_endpoint.GraphqlEndpoint(db)))

	photoRouter := endpointRouter.PathPrefix("/photo").Subrouter()
	routes.RegisterPhotoRoutes(db, photoRouter)

	videoRouter := endpointRouter.PathPrefix("/video").Subrouter()
	routes.RegisterVideoRoutes(db, videoRouter)

	downloadsRouter := endpointRouter.PathPrefix("/download").Subrouter()
	routes.RegisterDownloadRoutes(db, downloadsRouter)

	shouldServeUI := utils.ShouldServeUI()

	if shouldServeUI {
		spa, err := routes.NewSpaHandler(utils.UIPath(), "index.html")
		if err != nil {
			log.Error(ctx, "Could not configure UI handler", "error", err)
			os.Exit(255)
		}
		rootRouter.PathPrefix("/").Handler(spa)
	}

	if devMode {
		log.Info(ctx, "🚀 Graphql playground ready", "url", apiListenURL.String())
	} else {
		log.Info(ctx, "Photoview API endpoint listening", "url", apiListenURL.String())

		apiEndpoint := utils.ApiEndpointUrl()
		log.Info(ctx, "Photoview API public endpoint ready", "endpoint", apiEndpoint.String())

		logUIendpointURL(ctx)

		if !shouldServeUI {
			log.Info(ctx, "Notice: UI is not served by the API", utils.EnvServeUI.GetName(), "0")
		}
	}

	srv := &http.Server{
		Addr:    apiListenURL.Host,
		Handler: rootRouter,
	}

	setupGracefulShutdown(ctx, srv)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error(ctx, "HTTP server failed", "error", err)
		os.Exit(255)
	}
}

func setupGracefulShutdown(ctx context.Context, svr *http.Server) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		ctx, cancel := context.WithTimeout(ctx, time.Minute) // Wait for 1m to shutdown
		defer cancel()

		log.Info(ctx, "Shutting down Photoview...")

		// Shutdown scanners in correct order
		periodic_scanner.ShutdownPeriodicScanner()
		scanner_queue.CloseScannerQueue()

		if err := svr.Shutdown(ctx); err != nil {
			log.Error(ctx, "Server shutdown error", "error", err)
		} else {
			log.Info(ctx, "Shutdown complete")
		}
	}()
}

func logUIendpointURL(ctx context.Context) {
	if uiEndpoint := utils.UiEndpointUrl(); uiEndpoint != nil {
		log.Info(nil, "Photoview UI public endpoint ready", "endpoint", uiEndpoint.String())
	} else {
		log.Info(nil, "Photoview UI public endpoint ready", "endpoint", "/")
	}
}
