package ticker

import (
	"context"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v4"
	"github.com/kamilsk/retry/jitter"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Ticker is an advanced time.Ticker supporting jitter and postgres notifications
type Ticker struct {
	C              chan struct{}
	interval       time.Duration
	jitter         jitter.Transformation
	stop           chan struct{}
	db             *pgx.Conn
	dbEventChannel string
}

// New creates a new ticker sleeping randomly for (interval +/- jitter*interval)
func New(interval time.Duration, jitterFactor float64, db *pgx.Conn, dbEventChannel string) *Ticker {
	t := &Ticker{
		interval:       interval,
		jitter:         jitter.Deviation(rand.New(rand.NewSource(time.Now().Unix())), jitterFactor),
		db:             db,
		dbEventChannel: dbEventChannel,
	}
	return t
}

// Start starts the ticker
func (t *Ticker) Start(ctx context.Context) error {
	t.C = make(chan struct{})
	dbNotifyChannel := make(chan struct{})
	if t.db != nil {
		_, err := t.db.Exec(ctx, "LISTEN "+t.dbEventChannel)
		if err != nil {
			return err
		}
		go func() {
			defer func() { logrus.Debug("returning from db notification listener") }()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					if _, err := t.db.WaitForNotification(ctx); err == nil {
						dbNotifyChannel <- struct{}{}
					} else {
						logrus.Error(errors.Wrap(err, "failed to wait for notifications"))
						return
					}
				}
			}
		}()
	}

	go func() {
		t.C <- struct{}{} // initial tick comes immediatly
		defer func() {
			logrus.Debug("returning from ticker main loop listener")
			close(dbNotifyChannel)
			close(t.C)
		}()
		for {
			duration := t.jitter(t.interval)
			timer := time.NewTimer(duration)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				logrus.Debug("tick because of timer")
				t.C <- struct{}{}
			case <-dbNotifyChannel:
				logrus.Debug("tick because of database notification")
				t.C <- struct{}{}
			}
			timer.Stop()
		}
	}()

	return nil
}
