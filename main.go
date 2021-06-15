package main

import (
	"net/http"
	"os"

	"github.com/fatih/color"
	"github.com/jay-dee7/parachute/cache"
	"github.com/jay-dee7/parachute/config"
	"github.com/jay-dee7/parachute/server/registry/v2"
	"github.com/jay-dee7/parachute/skynet"
	"github.com/labstack/echo-contrib/prometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog"
)

func main() {
	var configPath string
	if len(os.Args) != 2 {
		configPath = "./"
	}

	config, err := config.Load(configPath)
	if err != nil {
		color.Red("error reading config file: %s", err.Error())
		os.Exit(1)
	}

	var errSig chan error
	e := echo.New()
	p := prometheus.NewPrometheus("echo", nil)
	p.Use(e)
	e.HideBanner = true
	e.HidePort = true

	l := setupLogger()
	localCache, err := cache.New("/tmp/badger")
	if err != nil {
		l.Err(err).Send()
		return
	}
	defer localCache.Close()

	skynetClient := skynet.NewClient(config)

	reg, err := registry.NewRegistry(skynetClient, l, localCache, e.Logger)
	if err != nil {
		l.Err(err).Send()
		return
	}

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Skipper: func(echo.Context) bool {
			return false
		},
		Format:           "method=${method}, uri=${uri}, status=${status} latency=${latency}\n",
		Output:           os.Stdout,
	}))

	e.Use(middleware.Recover())

	internal := e.Group("/internal")

	internal.Add(http.MethodGet, "/metadata", localCache.Metadata)

	router := e.Group("/v2/:username/:imagename")

	// ALL THE HEAD METHODS //
	// HEAD /v2/<name>/blobs/<digest>
	router.Add(http.MethodHead, "/blobs/:digest", reg.LayerExists) // (LayerExists) should be called reference/digest

	// HEAD /v2/<name>/manifests/<reference>
	router.Add(http.MethodHead, "/manifests/:reference", reg.ManifestExists) //should be called reference/digest

	// ALL THE PUT METHODS
	// PUT /v2/<name>/blobs/uploads/<uuid>?digest=<digest>
	// router.Add(http.MethodPut, "/blobs/uploads/:uuid", reg.MonolithicUpload)


	router.Add(http.MethodPut, "/blobs/uploads/", reg.CompleteUpload)

	// PUT /v2/<name>/blobs/uploads/<uuid>?digest=<digest>
	router.Add(http.MethodPut, "/blobs/uploads/:reference", reg.CompleteUpload)

	// PUT /v2/<name>/manifests/<reference>
	router.Add(http.MethodPut, "/manifests/:reference", reg.PushManifest)

	// POST METHODS
	// POST /v2/<name>/blobs/uploads/
	router.Add(http.MethodPost, "/blobs/uploads/", reg.StartUpload)

	// POST /v2/<name>/blobs/uploads/
	router.Add(http.MethodPost, "/blobs/uploads/:uuid", reg.PushLayer)

	// PATCH

	// PATCH /v2/<name>/blobs/uploads/<uuid>
	router.Add(http.MethodPatch, "/blobs/uploads/:buggu", reg.ChunkedUpload)
	// router.Add(http.MethodPatch, "/blobs/uploads/", reg.ChunkedUpload)

	// GET
	// GET /v2/<name>/manifests/<reference>
	router.Add(http.MethodGet, "/manifests/:reference", reg.PullManifest)

	// GET /v2/<name>/blobs/<digest>
	router.Add(http.MethodGet, "/blobs/:digest", reg.PullLayer)

	// GET GET /v2/<name>/blobs/uploads/<uuid>
	router.Add(http.MethodGet, "/blobs/uploads/:uuid", reg.UploadProgress)

	// router.Add(http.MethodGet, "/blobs/:digest", reg.DownloadBlob)

	e.Add(http.MethodGet, "/v2/", reg.ApiVersion)

	e.Start(config.Address())

// 	go func() {
// 		if err := e.Start(config.Address()); err != nil && err != http.ErrServerClosed {
// 			e.Logger.Fatal("shutting down the server")
// 		}
// 	}()

// 	// Wait for interrupt signal to gracefully shutdown the server with a timeout of 10 seconds.
// 	// Use a buffered channel to avoid missing signals as recommended for signal.Notify
// 	quit := make(chan os.Signal, 1)
// 	signal.Notify(quit, os.Interrupt)
// 	<-quit
// 	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()

// 	if err := e.Shutdown(ctx); err != nil {
// 		e.Logger.Fatal(err)
// 	}

	color.Yellow("docker registry server stopped: %s", <-errSig)
}

func setupLogger() zerolog.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	l := zerolog.New(os.Stdout)
	l.With().Caller().Logger()

	return l
}
