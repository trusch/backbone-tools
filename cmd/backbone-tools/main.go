package main

import (
	"context"
	"database/sql"
	"os"
	"os/signal"

	"github.com/cenkalti/backoff"
	grpcserver "github.com/contiamo/goserver/grpc"
	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"

	"github.com/trusch/backbone-tools/pkg/api"
	"github.com/trusch/backbone-tools/pkg/cronjobs"
	"github.com/trusch/backbone-tools/pkg/events"
	"github.com/trusch/backbone-tools/pkg/jobs"
	"github.com/trusch/backbone-tools/pkg/locks"
)

var (
	dbStr      = pflag.String("db", "postgres://postgres@localhost:5432?sslmode=disable", "postgres connect string")
	listenAddr = pflag.String("listen", ":3001", "listening address")
)

func main() {
	pflag.Parse()
	ctx, cancel := context.WithCancel(context.Background())

	var (
		db         *sql.DB
		err        error
		grpcServer *grpc.Server
	)

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
	grpcServer, err = grpcserver.New(&grpcserver.Config{
		Options: []grpcserver.Option{
			grpcserver.WithCredentials("", "", ""),
			grpcserver.WithLogging("backbone-tools"),
			grpcserver.WithMetrics(),
			grpcserver.WithRecovery(),
			grpcserver.WithReflection(),
		},
		Extras: []grpc.ServerOption{
			grpc.MaxSendMsgSize(1 << 12),
		},
		Register: func(srv *grpc.Server) {
			jobsServer, err := jobs.NewServer(ctx, db, *dbStr)
			if err != nil {
				logrus.Fatal(err)
			}
			api.RegisterJobsServer(srv, jobsServer)

			cronjobsServer, err := cronjobs.NewServer(ctx, db, jobsServer)
			if err != nil {
				logrus.Fatal(err)
			}
			api.RegisterCronJobsServer(srv, cronjobsServer)

			locksServer, err := locks.NewServer(ctx, db, *dbStr)
			if err != nil {
				logrus.Fatal(err)
			}
			api.RegisterLocksServer(srv, locksServer)

			eventsServer, err := events.NewServer(ctx, db, *dbStr)
			if err != nil {
				logrus.Fatal(err)
			}
			api.RegisterEventsServer(srv, eventsServer)
		},
	})

	if err != nil {
		logrus.Fatal(err)
	}

	// start server
	if err := grpcserver.ListenAndServe(ctx, *listenAddr, grpcServer); err != nil {
		logrus.Fatal(err)
	}
}
