package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-fuego/fuego"
	"github.com/kenners/zotrem/pkg/api"
	"github.com/kenners/zotrem/pkg/db"
	"github.com/kenners/zotrem/pkg/sync"
	"github.com/kenners/zotrem/pkg/web"
	"github.com/spf13/cobra"
)

var (
	commit  = "none"
	date    = "unknown"
	version = "dev"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type app struct {
	engine *sync.Engine
	log    *slog.Logger
	store  *db.DB
}

func defaultDataDir() string {
	if dir := os.Getenv("ZOTREM_DATA_DIR"); dir != "" {
		return dir
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "zotrem")
}

func openApp(dataDir string) (*app, error) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	store, err := db.New(filepath.Join(dataDir, "zotrem.db"))
	if err != nil {
		return nil, errors.Wrap(err, "open database")
	}
	engine := sync.New(store, logger, sync.Options{
		RemarkableAuthHost: os.Getenv("ZOTREM_REMARKABLE_AUTH_HOST"),
		RemarkableSyncHost: os.Getenv("ZOTREM_REMARKABLE_SYNC_HOST"),
		ZoteroBaseURL:      os.Getenv("ZOTREM_ZOTERO_BASE_URL"),
	})
	return &app{engine: engine, log: logger, store: store}, nil
}

func newRootCmd() *cobra.Command {
	var dataDir string
	root := &cobra.Command{
		Short:   "Sync PDFs between Zotero and a reMarkable tablet",
		Use:     "zotrem",
		Version: fmt.Sprintf("%s (%s, %s)", version, commit, date),
	}
	root.PersistentFlags().StringVar(&dataDir, "data-dir", defaultDataDir(), "directory for the database (also ZOTREM_DATA_DIR)")

	var port int
	var bind string
	serve := &cobra.Command{
		Short: "Run the web UI and the sync scheduler",
		Use:   "serve",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openApp(dataDir)
			if err != nil {
				return err
			}
			defer a.store.Close()
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			go a.engine.RunScheduler(ctx)

			password := os.Getenv("ZOTREM_PASSWORD")
			addr := fmt.Sprintf("%s:%d", bind, port)
			srv := fuego.NewServer(
				fuego.WithAddr(addr),
				fuego.WithGlobalMiddlewares(web.BasicAuth(password)),
				fuego.WithEngineOptions(fuego.WithOpenAPIConfig(fuego.OpenAPIConfig{
					DisableLocalSave: true,
					Info:             &openapi3.Info{Title: "zotrem API", Version: version},
				})),
			)
			api.Register(srv, api.Deps{Engine: a.engine, Store: a.store, Version: version})
			srv.Mux.Handle("/", web.New(a.store, a.engine, a.log).Handler())
			a.log.Info("zotrem listening", "addr", addr, "data", dataDir, "password", password != "")
			go func() {
				<-ctx.Done()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = srv.Shutdown(shutdownCtx)
			}()
			if err := srv.Run(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	serve.Flags().IntVarP(&port, "port", "p", 8000, "HTTP listen port")
	serve.Flags().StringVar(&bind, "bind", "127.0.0.1", "address to bind (use 0.0.0.0 to expose on the network)")

	syncCmd := &cobra.Command{
		Short: "Run one sync and exit",
		Use:   "sync",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openApp(dataDir)
			if err != nil {
				return err
			}
			defer a.store.Close()
			run, err := a.engine.Run(cmd.Context(), db.TriggerManual)
			if err != nil {
				return err
			}
			fmt.Printf("sync %s: %d sent, %d pulled, %d removed\n", run.Status, run.Pushed, run.Pulled, run.Removed)
			return nil
		},
	}

	pair := &cobra.Command{
		Args:  cobra.ExactArgs(1),
		Short: "Pair with the reMarkable cloud using a one-time code from my.remarkable.com/device/browser/connect",
		Use:   "pair CODE",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openApp(dataDir)
			if err != nil {
				return err
			}
			defer a.store.Close()
			token, err := a.engine.Register(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			settings, err := a.store.Settings()
			if err != nil {
				return err
			}
			settings.RemarkableDeviceToken = token
			settings.RemarkableUserToken = ""
			if err := a.store.SaveSettings(settings); err != nil {
				return err
			}
			fmt.Println("paired with the reMarkable cloud")
			return nil
		},
	}

	root.AddCommand(serve, syncCmd, pair)
	return root
}
