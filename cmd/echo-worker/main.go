package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/trusch/backbone-tools/pkg/api"
	"github.com/trusch/backbone-tools/pkg/locks"
	"github.com/trusch/backbone-tools/pkg/worker"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials"
)

var (
	addr  = pflag.String("addr", "localhost:3001", "jobs server address")
	cert  = pflag.String("cert", "", "client cert")
	key   = pflag.String("key", "", "client secret")
	queue = pflag.String("queue", "example", "queue to listen on")

	conn      *grpc.ClientConn
	jobsCli   api.JobsClient
	locksCli  api.LocksClient
	eventsCli api.EventsClient

	err error
)

func init() {
	// parse flags
	pflag.Parse()

	logrus.WithFields(logrus.Fields{
		"addr":  *addr,
		"cert":  *cert,
		"key":   *key,
		"queue": *queue,
	}).Info("parsed flags")

	logrus.Info("try connecting to backbone-tools server...")
	conn, err = connect(*addr, *cert, *key)
	logrus.Infof("connected.")
	jobsCli = api.NewJobsClient(conn)
	locksCli = api.NewLocksClient(conn)
	eventsCli = api.NewEventsClient(conn)
}

func main() {
	// instanciate worker
	logrus.Info("start worker")
	w := worker.New(jobsCli, *queue, echoWorker)
	err = w.Work(context.Background())
	if err != nil {
		logrus.Fatal(err)
	}
}

func echoWorker(ctx context.Context, spec []byte, state chan<- []byte) error {
	logrus.WithField("spec", string(spec)).Info("got job, start working")

	// take a lock to guarantee that only one worker at a time works on a queue
	// the lock is automatically released when this function returns, since the context is scoped accordingly
	logrus.Info("try to aquire a lock...")
	err := locks.Lock(ctx, locksCli, "echo-lock")
	if err != nil {
		return err
	}
	logrus.Info("got the lock.")

	// publish an example event
	logrus.Info("publish an event...")
	_, err = eventsCli.Publish(ctx, &api.PublishRequest{
		Topic:   "echo-work-accepted",
		Payload: spec,
	})
	if err != nil {
		return err
	}
	logrus.Info("successfully published the event")

	// parse the spec
	logrus.Info("parse and print the spec (the actual work done by this example worker)")
	var doc interface{}
	err = json.Unmarshal(spec, &doc)
	if err != nil {
		return err
	}

	// pretty print it
	bs, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(bs))

	logrus.Info("send some state update about this job to the backbone-tools server")
	// report some optional state
	state <- []byte("I saw it!")

	logrus.Info("finished handling the job")
	// return nil to actually indicate that the job is finished now
	return nil
}

func connect(addr, cert, key string) (*grpc.ClientConn, error) {
	var (
		opt = grpc.WithInsecure()
	)
	if creds, err := credentials.NewServerTLSFromFile(cert, key); err == nil {
		opt = grpc.WithTransportCredentials(creds)
	}
	conn, err := grpc.Dial(addr, opt, grpc.WithBlock(), grpc.WithConnectParams(grpc.ConnectParams{
		Backoff:           backoff.DefaultConfig,
		MinConnectTimeout: 2 * time.Second,
	}))
	if err != nil {
		return nil, err
	}
	return conn, nil
}
