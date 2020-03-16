package main

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"os/signal"

	"github.com/cenkalti/backoff"
	"github.com/contiamo/goserver"
	grpcserver "github.com/contiamo/goserver/grpc"
	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"

	"github.com/contiamo/go-base/pkg/tracing"
	"github.com/trusch/backbone-tools/pkg/api"
	"github.com/trusch/backbone-tools/pkg/services/cronjobs"
	"github.com/trusch/backbone-tools/pkg/services/events"
	"github.com/trusch/backbone-tools/pkg/services/jobs"
	"github.com/trusch/backbone-tools/pkg/services/locks"
)

var (
	dbStr      = pflag.String("db", "postgres://postgres@localhost:5432?sslmode=disable", "postgres connect string")
	listenAddr = pflag.String("listen", ":3001", "listening address")
	components = pflag.StringSlice("components", []string{"jobs", "cronjobs", "locks", "events"}, "list of components to start up")
	key        = pflag.String("key", "", "x509 key file")
	cert       = pflag.String("cert", "", "x509 cert file")
	ca         = pflag.String("ca", "", "x509 ca cert file")
	metrics    = pflag.String("metrics", ":8080", "metrics endpoint")
	logLevel   = pflag.String("log-level", "INFO", "log level")
)

func main() {
	pflag.Parse()
	ctx, cancel := context.WithCancel(context.Background())

	var (
		db         *sql.DB
		err        error
		grpcServer *grpc.Server
	)

	lvl, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logrus.Fatal(err)
	}
	logrus.SetLevel(lvl)

	if err := tracing.InitJaeger("backbone-tools"); err != nil {
		logrus.Fatal(err)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		cancel()
		if grpcServer != nil {
			grpcServer.Stop()
		}
	}()

	err = backoff.Retry(func() error {
		select {
		case <-ctx.Done():
			return backoff.Permanent(ctx.Err())
		default:
			db, err = sql.Open("postgres", *dbStr)
			if err != nil {
				logrus.Warn(err)
				return err
			}
			err = db.Ping()
			if err != nil {
				logrus.Warn(err)
				return err
			}
			return nil
		}
	}, backoff.NewExponentialBackOff())
	if err != nil {
		logrus.Fatal(err)
	}

	// setup grpc server with options
	opts := []grpcserver.Option{
		grpcserver.WithCredentials(*cert, *key, *ca),
		grpcserver.WithLogging("backbone-tools"),
		grpcserver.WithMetrics(),
		grpcserver.WithRecovery(),
		grpcserver.WithReflection(),
		grpcserver.WithTracing("", "backbone-tools"),
	}

	grpcServer, err = grpcserver.New(&grpcserver.Config{
		Options: opts,
		Extras: []grpc.ServerOption{
			grpc.MaxSendMsgSize(1 << 12),
		},
		Register: func(srv *grpc.Server) {
			var jobsServer api.JobsServer
			for _, componentName := range *components {
				switch componentName {
				case "jobs":
					jobsServer, err = jobs.NewServer(ctx, db, *dbStr)
					if err != nil {
						logrus.Fatal(err)
					}
					api.RegisterJobsServer(srv, jobsServer)
				case "cronjobs":
					cronjobsServer, err := cronjobs.NewServer(ctx, db, jobsServer)
					if err != nil {
						logrus.Fatal(err)
					}
					api.RegisterCronJobsServer(srv, cronjobsServer)
				case "locks":
					locksServer, err := locks.NewServer(ctx, db, *dbStr)
					if err != nil {
						logrus.Fatal(err)
					}
					api.RegisterLocksServer(srv, locksServer)
				case "events":
					eventsServer, err := events.NewServer(ctx, db, *dbStr)
					if err != nil {
						logrus.Fatal(err)
					}
					api.RegisterEventsServer(srv, eventsServer)
				}
			}
		},
	})

	if err != nil {
		logrus.Fatal(err)
	}

	go func() {
		logrus.Infof("started metrics on %s", *metrics)
		err := goserver.ListenAndServeMonitoring(ctx, *metrics, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := db.Ping(); err == nil {
				w.Write([]byte(`{"ok":true}`))
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"ok":false}`))
		}))
		if err != nil {
			logrus.Fatal(err)
		}
	}()
	// start server
	if err := grpcserver.ListenAndServe(ctx, *listenAddr, grpcServer); err != nil {
		logrus.Fatal(err)
	}
}
