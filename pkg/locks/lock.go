package locks

import (
	"context"
	"time"

	"github.com/trusch/backbone-tools/pkg/api"
	"github.com/sirupsen/logrus"
)

const (
	renewInterval = 5 * time.Second
)

// Lock creates a lock and holds it until the context expires or is canceled
// if the lock is not available it will block until the lock can be taken or the context is canceled
func Lock(ctx context.Context, cli api.LocksClient, id string) error {
	_, err := cli.Aquire(ctx, &api.AquireRequest{Id: id})
	if err != nil {
		return err
	}
	go func() {
		ticker := time.NewTicker(renewInterval)
		defer ticker.Stop()
		if err != nil {
			logrus.Error(err)
			return
		}
		for {
			select {
			case <-ctx.Done():
				_, err = cli.Release(context.Background(), &api.ReleaseRequest{Id: id})
				if err != nil {
					logrus.Error(err)
				}
				return
			case <-ticker.C:
				_, err = cli.Hold(ctx, &api.HoldRequest{Id: id})
			}
		}
	}()
	return nil
}
