package worker

import (
	"context"

	"github.com/trusch/backbone-tools/pkg/api"
	"github.com/sirupsen/logrus"
)

type WorkerCallback func(ctx context.Context, spec []byte, state chan<- []byte) error

func New(cli api.JobsClient, queue string, cb WorkerCallback) *Worker {
	return &Worker{cli, queue, cb}
}

type Worker struct {
	cli   api.JobsClient
	queue string
	cb    WorkerCallback
}

func (w *Worker) Work(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			err := func() error {
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				resp, err := w.cli.Listen(ctx, &api.ListenRequest{
					Queue: w.queue,
				})
				if err != nil {
					return err
				}
				defer resp.CloseSend()
				for {
					job, err := resp.Recv()
					if err != nil {
						return err
					}
					ch := make(chan []byte)
					var cbError error
					go func() {
						ctx, cancel := context.WithCancel(ctx)
						defer cancel()
						cbError = w.cb(ctx, job.Spec, ch)
						close(ch)
					}()
					for state := range ch {
						_, err = w.cli.Heartbeat(ctx, &api.HeartbeatRequest{
							JobId: job.GetId(),
							State: state,
						})
						if err != nil {
							return err
						}
					}
					if cbError == nil {
						_, err = w.cli.Heartbeat(ctx, &api.HeartbeatRequest{
							JobId:    job.GetId(),
							Finished: true,
						})
						if err != nil {
							return err
						}
					}
					return cbError
				}
			}()
			if err != nil {
				logrus.Error(err)
			}
		}
	}
}
