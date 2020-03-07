package jobs

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	dbtypes "github.com/contiamo/go-base/pkg/db/serialization"
	"github.com/trusch/backbone-tools/pkg/api"
	"github.com/trusch/backbone-tools/pkg/sqlizers"
	"github.com/trusch/backbone-tools/pkg/ticker"
	"github.com/golang/protobuf/ptypes"
	"github.com/jackc/pgx/v4"
	uuid "github.com/satori/go.uuid"
	"github.com/sirupsen/logrus"
)

var (
	pollInterval      = 10 * time.Second
	heartbeatDeadline = 20 * time.Second
)

func NewServer(ctx context.Context, db *sql.DB, connectString string) (api.JobsServer, error) {
	srv := &jobsServer{db, connectString}
	return srv, srv.init(ctx)
}

type jobsServer struct {
	db            squirrel.StdSqlCtx
	connectString string
}

func (s *jobsServer) init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS jobs(
  job_id UUID PRIMARY KEY,
  queue TEXT NOT NULL,
  spec BYTEA,
  labels JSONB NOT NULL DEFAULT '{}',
  state BYTEA,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ
);
`)
	return err
}

func (s *jobsServer) getBuilder(db squirrel.BaseRunner) squirrel.StatementBuilderType {
	return squirrel.StatementBuilder.
		PlaceholderFormat(squirrel.Dollar).
		RunWith(db)
}

func (s *jobsServer) Create(ctx context.Context, req *api.CreateJobRequest) (*api.Job, error) {
	id := uuid.NewV4().String()
	now := time.Now()
	nowProto, err := ptypes.TimestampProto(now)
	if err != nil {
		return nil, err
	}

	if req.Labels == nil {
		req.Labels = make(map[string]string)
	}

	_, err = s.getBuilder(s.db).Insert("jobs").Columns(
		"job_id",
		"queue",
		"labels",
		"spec",
		"created_at",
	).Values(
		id,
		req.GetQueue(),
		dbtypes.JSONBlob(req.GetLabels()),
		req.GetSpec(),
		now,
	).ExecContext(ctx)
	if err != nil {
		return nil, err
	}

	_, err = s.db.ExecContext(ctx, `NOTIFY `+req.GetQueue())
	if err != nil {
		return nil, err
	}

	return &api.Job{
		Id:        id,
		Queue:     req.GetQueue(),
		Labels:    req.GetLabels(),
		Spec:      req.GetSpec(),
		CreatedAt: nowProto,
	}, nil
}

func (s *jobsServer) Listen(req *api.ListenRequest, resp api.Jobs_ListenServer) error {
	notifyConn, err := pgx.Connect(resp.Context(), s.connectString)
	if err != nil {
		return err
	}

	ticker := ticker.New(pollInterval, 0.1, notifyConn, req.GetQueue())
	if err := ticker.Start(resp.Context()); err != nil {
		return err
	}
	for {
		select {
		case <-resp.Context().Done():
			return resp.Context().Err()
		case <-ticker.C:
			var job *api.Job
			// start tx in lambda to use defer syntax
			err := func() (err error) {
				// setup tx
				rawDB, ok := s.db.(*sql.DB)
				if !ok {
					return errors.New("can not listen withing transactions")
				}
				tx, err := rawDB.BeginTx(resp.Context(), &sql.TxOptions{
					Isolation: sql.LevelSerializable,
				})
				if err != nil {
					return err
				}
				defer func() {
					if err != nil {
						logrus.Errorf("error while listening: %v", err)
						tx.Rollback()
						return
					}
					logrus.Infof("committing")
					err = tx.Commit()
				}()

				// get job
				job, err = s.getJob(resp.Context(), tx, req.GetQueue())
				if err != nil {
					return err
				}

				// set started_at
				now := time.Now()
				nowProto, err := ptypes.TimestampProto(now)
				if err != nil {
					return err
				}
				_, err = s.getBuilder(tx).Update("jobs").
					Set("started_at", now).
					Set("updated_at", now).
					Where(squirrel.Eq{"job_id": job.GetId()}).
					ExecContext(resp.Context())
				job.StartedAt = nowProto

				return err
			}()

			if err != nil {
				if err == sql.ErrNoRows {
					continue
				}
				if strings.HasPrefix(err.Error(), "pq: could not serialize access") {
					continue
				}
				return err
			}

			// send job to worker
			if job != nil {
				logrus.Infof("found job while listening: %+v", job)
				err = resp.Send(job)
				if err != nil {
					return err
				}
				job = nil
			}
		}
	}
}

func (s *jobsServer) getJob(ctx context.Context, tx *sql.Tx, queue string) (*api.Job, error) {
	var (
		job       api.Job
		createdAt time.Time
	)
	pred := squirrel.And{
		squirrel.Eq{
			"queue":       queue,
			"finished_at": nil,
		},
		squirrel.Or{
			// not started
			squirrel.Eq{"started_at": nil},
			// job got heartbeats but the last heartbeat is too long ago
			squirrel.Lt{"updated_at": time.Now().Add(-heartbeatDeadline)},
		},
	}
	err := s.getBuilder(tx).Select("job_id", "spec", "created_at", "labels").
		From("jobs").
		Where(pred).
		OrderBy("created_at ASC").
		Limit(1).
		QueryRowContext(ctx).Scan(&job.Id, &job.Spec, &createdAt, dbtypes.JSONBlob(&job.Labels))
	if err != nil {
		return nil, err
	}
	job.CreatedAt, err = ptypes.TimestampProto(createdAt)
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *jobsServer) Heartbeat(ctx context.Context, req *api.HeartbeatRequest) (*api.Job, error) {
	// setup tx
	rawDB, ok := s.db.(*sql.DB)
	if !ok {
		return nil, errors.New("can not start transactions withing transactions")
	}
	tx, err := rawDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
			return
		}
		err = tx.Commit()
	}()

	// get job
	job, err := (&jobsServer{tx, ""}).Get(ctx, &api.GetRequest{Id: req.GetJobId()})
	if err != nil {
		return nil, err
	}

	// update object
	now := time.Now()
	nowProto, err := ptypes.TimestampProto(now)
	if err != nil {
		return nil, err
	}
	job.UpdatedAt = nowProto
	job.State = req.GetState()
	if req.GetFinished() {
		job.FinishedAt = nowProto
	}

	// persist new values in db
	builder := s.getBuilder(tx).Update("jobs").
		Set("updated_at", now)
	if state := req.GetState(); state != nil {
		builder = builder.Set("state", state)
	}
	if req.GetFinished() {
		builder = builder.Set("finished_at", now)
	}
	builder = builder.Where(squirrel.Eq{
		"job_id": req.GetJobId(),
	})
	_, err = builder.ExecContext(ctx)
	if err != nil {
		return nil, err
	}

	return job, nil
}

func (s *jobsServer) Get(ctx context.Context, req *api.GetRequest) (*api.Job, error) {
	var (
		job        api.Job
		createdAt  time.Time
		updatedAt  *time.Time
		startedAt  *time.Time
		finishedAt *time.Time
	)
	err := s.getBuilder(s.db).Select("job_id", "spec", "state", "labels", "created_at", "updated_at", "started_at", "finished_at").
		From("jobs").
		Where(squirrel.Eq{
			"job_id": req.GetId(),
		}).
		QueryRowContext(ctx).Scan(&job.Id, &job.Spec, &job.State, dbtypes.JSONBlob(&job.Labels), &createdAt, &updatedAt, &startedAt, &finishedAt)
	if err != nil {
		return nil, err
	}
	job.CreatedAt, err = ptypes.TimestampProto(createdAt)
	if err != nil {
		return nil, err
	}
	if updatedAt != nil {
		job.UpdatedAt, err = ptypes.TimestampProto(*updatedAt)
		if err != nil {
			return nil, err
		}
	}
	if startedAt != nil {
		job.StartedAt, err = ptypes.TimestampProto(*startedAt)
		if err != nil {
			return nil, err
		}
	}
	if finishedAt != nil {
		job.FinishedAt, err = ptypes.TimestampProto(*finishedAt)
		if err != nil {
			return nil, err
		}
	}
	return &job, nil
}

func (s *jobsServer) Delete(ctx context.Context, req *api.DeleteRequest) (*api.Job, error) {
	// setup tx
	rawDB, ok := s.db.(*sql.DB)
	if !ok {
		return nil, errors.New("can not start transactions withing transactions")
	}
	tx, err := rawDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
			return
		}
		err = tx.Commit()
	}()

	// get job
	job, err := (&jobsServer{tx, ""}).Get(ctx, &api.GetRequest{Id: req.GetId(), Name: req.GetName()})
	if err != nil {
		return nil, err
	}

	_, err = s.getBuilder(tx).
		Delete("jobs").
		Where(squirrel.Eq{"job_id": job.GetId()}).
		ExecContext(ctx)
	if err != nil {
		return nil, err
	}

	return job, nil
}

func (s *jobsServer) List(req *api.ListRequest, resp api.Jobs_ListServer) error {
	if req.Labels == nil {
		req.Labels = make(map[string]string)
	}
	filter := squirrel.And{}
	if queues := req.GetQueues(); len(queues) > 0 {
		filter = append(filter, squirrel.Eq{
			"queue": req.GetQueues(),
		})
	}
	if labels := req.GetLabels(); len(labels) > 0 {
		filter = append(filter, sqlizers.JSONContains{
			"labels": dbtypes.JSONBlob(req.GetLabels()),
		})
	}
	if req.GetExcludeFinished() {
		filter = append(filter, squirrel.Eq{"finished_at": nil})
	}
	rows, err := s.getBuilder(s.db).
		Select("job_id", "spec", "state", "queue", "labels", "created_at", "updated_at", "started_at", "finished_at").
		From("jobs").
		Where(filter).
		OrderBy("created_at ASC").
		QueryContext(resp.Context())
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			job        api.Job
			createdAt  time.Time
			updatedAt  *time.Time
			startedAt  *time.Time
			finishedAt *time.Time
		)
		err = rows.Scan(&job.Id, &job.Spec, &job.State, &job.Queue, dbtypes.JSONBlob(&job.Labels), &createdAt, &updatedAt, &startedAt, &finishedAt)
		if err != nil {
			return err
		}
		job.CreatedAt, err = ptypes.TimestampProto(createdAt)
		if err != nil {
			return err
		}
		if updatedAt != nil {
			job.UpdatedAt, err = ptypes.TimestampProto(*updatedAt)
			if err != nil {
				return err
			}
		}
		if startedAt != nil {
			job.StartedAt, err = ptypes.TimestampProto(*startedAt)
			if err != nil {
				return err
			}
		}
		if finishedAt != nil {
			job.FinishedAt, err = ptypes.TimestampProto(*finishedAt)
			if err != nil {
				return err
			}
		}
		if err = resp.Send(&job); err != nil {
			return err
		}
	}
	return nil
}
