package ticker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v4"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestTicker(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, "postgres://localhost:5432?user=postgres")
	require.NoError(t, err)
	ticker := New(1*time.Second, 0.2, conn, "notifications")
	ticker.Start(ctx)
	last := time.Now()
	for {
		_, ok := <-ticker.C
		if !ok {
			break
		}
		now := time.Now()
		fmt.Println(now.Sub(last))
		last = now
	}
}
