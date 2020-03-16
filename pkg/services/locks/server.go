package locks

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/trusch/backbone-tools/pkg/api"
	"github.com/trusch/backbone-tools/pkg/ticker"
	"github.com/jackc/pgx/v4"
)

var (
	pollInterval = 10 * time.Second
	holdDeadline = 20 * time.Second
)

func NewServer(ctx context.Context, db *sql.DB, connectString string) (api.LocksServer, error) {
	srv := &locksServer{db, connectString}
	return srv, srv.init(ctx)
}

type locksServer struct {
	db            squirrel.StdSqlCtx
	connectString string
}

func (s *locksServer) init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS locks(
  lock_id TEXT PRIMARY KEY,
  updated_at TIMESTAMPTZ
);
`)
	return err
}

func (s *locksServer) getBuilder(db squirrel.StdSqlCtx) squirrel.StatementBuilderType {
	return squirrel.StatementBuilder.
		PlaceholderFormat(squirrel.Dollar).
		RunWith(db)
}

func (s *locksServer) Aquire(ctx context.Context, req *api.AquireRequest) (*api.AquireResponse, error) {
	notifyConn, err := pgx.Connect(ctx, s.connectString)
	if err != nil {
		return nil, err
	}
	ticker := ticker.New(pollInterval, 0.1, notifyConn, "locks")
	if err := ticker.Start(ctx); err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			err := func() (err error) {
				// setup tx
				rawDB, ok := s.db.(*sql.DB)
				if !ok {
					return errors.New("can not listen withing transactions")
				}
				tx, err := rawDB.BeginTx(ctx, &sql.TxOptions{
					Isolation: sql.LevelSerializable,
				})
				if err != nil {
					return err
				}
				defer func() {
					if err != nil {
						tx.Rollback()
						return
					}
					err = tx.Commit()
				}()
				err = s.withTx(tx).getLock(ctx, req.GetId())
				if err != nil {
					return err
				}
				return nil
			}()
			if err != nil {
				if err == errLocked {
					break
				}
				return nil, err
			}
			return &api.AquireResponse{
				Id: req.GetId(),
			}, nil
		}
	}
}

func (s *locksServer) Hold(ctx context.Context, req *api.HoldRequest) (*api.HoldResponse, error) {
	_, err := s.getBuilder(s.db).Update("locks").Set("updated_at", time.Now()).ExecContext(ctx)
	if err != nil {
		return nil, err
	}
	return &api.HoldResponse{Id: req.GetId()}, nil
}

func (s *locksServer) Release(ctx context.Context, req *api.ReleaseRequest) (*api.ReleaseResponse, error) {
	_, err := s.getBuilder(s.db).Update("locks").Set("updated_at", time.Time{}).ExecContext(ctx)
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `NOTIFY locks`)
	if err != nil {
		return nil, err
	}
	return &api.ReleaseResponse{Id: req.GetId()}, nil
}

var errLocked = errors.New("can't get lock")

func (s *locksServer) getLock(ctx context.Context, id string) error {
	var (
		lockID    string
		updatedAt time.Time
	)
	err := s.getBuilder(s.db).Select("lock_id", "updated_at").From("locks").Where(squirrel.Eq{
		"lock_id": id,
	}).QueryRowContext(ctx).Scan(&lockID, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			_, err = s.getBuilder(s.db).
				Insert("locks").
				Columns("lock_id", "updated_at").
				Values(id, time.Now()).
				ExecContext(ctx)
			if err != nil {
				return err
			}
			return nil
		}
		return err
	}
	if time.Now().Sub(updatedAt) > holdDeadline {
		_, err = s.getBuilder(s.db).Update("locks").Set("updated_at", time.Now()).ExecContext(ctx)
		if err != nil {
			return err
		}
		return nil
	}
	return errLocked
}

func (s *locksServer) withTx(tx *sql.Tx) *locksServer {
	return &locksServer{tx, s.connectString}
}
