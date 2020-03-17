package cronjobs

import (
	"context"
	"database/sql"
	"time"

	"github.com/Masterminds/squirrel"
	dbtypes "github.com/contiamo/go-base/pkg/db/serialization"
	"github.com/contiamo/go-base/pkg/tracing"
	"github.com/golang/protobuf/ptypes"
	"github.com/pkg/errors"
	"github.com/robfig/cron"
	uuid "github.com/satori/go.uuid"
	"github.com/sirupsen/logrus"
	"github.com/trusch/backbone-tools/pkg/api"
	"github.com/trusch/backbone-tools/pkg/sqlizers"
)

var (
	pollInterval      = 10 * time.Second
	heartbeatDeadline = 20 * time.Second
)

func NewServer(ctx context.Context, db *sql.DB, jobsServer api.JobsServer) (api.CronJobsServer, error) {
	srv := &cronjobsServer{
		Tracer:     tracing.NewTracer("cronjobs", "CronJobsServer"),
		db:         db,
		jobsServer: jobsServer,
	}
	err := srv.init(ctx)
	if err != nil {
		return nil, err
	}
	go srv.backend(ctx)
	return srv, nil
}

type cronjobsServer struct {
	tracing.Tracer
	db         squirrel.StdSqlCtx
	jobsServer api.JobsServer
}

func (s *cronjobsServer) init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS cronjobs(
  cronjob_id UUID PRIMARY KEY,
  queue TEXT NOT NULL,
  name TEXT UNIQUE,
  spec BYTEA,
  cron TEXT,
  labels JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  next_run_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
`)
	return err
}

func (s *cronjobsServer) backend(ctx context.Context) {
	err := s.scheduleJobs(ctx)
	if err != nil && err != sql.ErrNoRows {
		logrus.Errorf("failed to initially schedule jobs: %v", err)
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := s.scheduleJobs(ctx)
			if err != nil && err != sql.ErrNoRows {
				logrus.Errorf("failed to schedule jobs: %v", err)
			}
		}
	}
}

func (s *cronjobsServer) Create(ctx context.Context, req *api.CreateCronJobRequest) (cronjob *api.CronJob, err error) {
	span, ctx := s.StartSpan(ctx, "Create")
	defer func() {
		s.FinishSpan(span, err)
	}()
	span.SetTag("name", req.GetName())
	span.SetTag("cron", req.GetCron())
	span.SetTag("queue", req.GetQueue())
	span.SetTag("spec", string(req.GetSpec()))
	span.SetTag("labels", req.GetLabels())

	id := uuid.NewV4().String()
	span.SetTag("cronjob_id", id)

	now := time.Now()
	nowProto, err := ptypes.TimestampProto(now)
	if err != nil {
		return nil, err
	}

	if req.Labels == nil {
		req.Labels = make(map[string]string)
	}

	_, err = cron.ParseStandard(req.GetCron())
	if err != nil {
		return nil, err
	}

	_, err = s.getBuilder(s.db).Insert("cronjobs").Columns(
		"cronjob_id",
		"queue",
		"name",
		"labels",
		"spec",
		"cron",
		"created_at",
		"next_run_at",
	).Values(
		id,
		req.GetQueue(),
		req.GetName(),
		dbtypes.JSONBlob(req.GetLabels()),
		req.GetSpec(),
		req.GetCron(),
		now,
		now,
	).ExecContext(ctx)
	if err != nil {
		return nil, err
	}

	return &api.CronJob{
		Id:        id,
		Queue:     req.GetQueue(),
		Labels:    req.GetLabels(),
		Name:      req.GetName(),
		Spec:      req.GetSpec(),
		Cron:      req.GetCron(),
		CreatedAt: nowProto,
		NextRunAt: nowProto,
	}, s.scheduleJobs(ctx)

}

func (s *cronjobsServer) Get(ctx context.Context, req *api.GetRequest) (cronjob *api.CronJob, err error) {
	span, ctx := s.StartSpan(ctx, "Create")
	defer func() {
		s.FinishSpan(span, err)
	}()
	span.SetTag("cronjobs_id", req.GetId())
	span.SetTag("name", req.GetName())

	cronjob = &api.CronJob{}
	var (
		createdAt time.Time
		nextRunAt *time.Time
	)
	where := squirrel.Or{}
	if id := req.GetId(); id != "" {
		where = append(where, squirrel.Eq{"cronjob_id": id})
	}
	if name := req.GetName(); name != "" {
		where = append(where, squirrel.Eq{"name": name})
	}
	err = s.getBuilder(s.db).Select("cronjob_id", "name", "labels", "queue", "spec", "cron", "created_at", "next_run_at").
		From("cronjobs").
		Where(where).
		QueryRowContext(ctx).Scan(&cronjob.Id, &cronjob.Name, dbtypes.JSONBlob(&cronjob.Labels), &cronjob.Queue, &cronjob.Spec, &cronjob.Cron, &createdAt, &nextRunAt)
	if err != nil {
		return nil, err
	}
	cronjob.CreatedAt, err = ptypes.TimestampProto(createdAt)
	if err != nil {
		return nil, err
	}
	if nextRunAt != nil {
		cronjob.NextRunAt, err = ptypes.TimestampProto(*nextRunAt)
		if err != nil {
			return nil, err
		}
	}
	return cronjob, nil
}

func (s *cronjobsServer) Delete(ctx context.Context, req *api.DeleteRequest) (cronjob *api.CronJob, err error) {
	span, ctx := s.StartSpan(ctx, "Delete")
	defer func() {
		s.FinishSpan(span, err)
	}()
	span.SetTag("cronjobs_id", req.GetId())
	span.SetTag("name", req.GetName())

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
	cronjob, err = (&cronjobsServer{s.Tracer, tx, s.jobsServer}).Get(ctx, &api.GetRequest{Id: req.GetId(), Name: req.GetName()})
	if err != nil {
		return nil, err
	}

	_, err = s.getBuilder(tx).
		Delete("cronjobs").
		Where(squirrel.Eq{"cronjob_id": cronjob.GetId()}).
		ExecContext(ctx)
	if err != nil {
		return nil, err
	}

	return cronjob, nil
}

func (s *cronjobsServer) scheduleJobs(ctx context.Context) (err error) {
	span, ctx := s.StartSpan(ctx, "scheduleJobs")
	defer func() {
		s.FinishSpan(span, err)
	}()

	// setup tx
	rawDB, ok := s.db.(*sql.DB)
	if !ok {
		return errors.New("can not start transactions withing transactions")
	}
	tx, err := rawDB.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelSerializable,
	})
	defer func() {
		if err != nil {
			tx.Rollback()
			return
		}
	}()

	now := time.Now()

	rows, err := s.getBuilder(tx).Select("cronjob_id", "name", "queue", "spec", "cron", "labels").
		From("cronjobs").
		Where(squirrel.Lt{
			"next_run_at": now,
		}).
		QueryContext(ctx)
	if err != nil {
		return err
	}

	cronJobs := make([]api.CronJob, 0)
	for rows.Next() {
		var (
			cronjob api.CronJob
		)
		err = rows.Scan(&cronjob.Id, &cronjob.Name, &cronjob.Queue, &cronjob.Spec, &cronjob.Cron, dbtypes.JSONBlob(&cronjob.Labels))
		if err != nil {
			_ = rows.Close()
			return err
		}
		cronJobs = append(cronJobs, cronjob)
	}
	rows.Close()

	for _, cronjob := range cronJobs {
		cronSchedule, err := cron.ParseStandard(cronjob.Cron)
		if err != nil {
			return err
		}
		nextRunAt := cronSchedule.Next(time.Now())
		logrus.Infof("next run is at %v", nextRunAt)

		_, err = s.getBuilder(tx).
			Update("cronjobs").
			Set("next_run_at", nextRunAt).
			Where(squirrel.Eq{"cronjob_id": cronjob.GetId()}).
			ExecContext(ctx)
		if err != nil {
			return errors.Wrapf(err, "failed to update next_run_at")
		}
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	for _, cronjob := range cronJobs {
		labels := make(map[string]string)
		for k, v := range cronjob.GetLabels() {
			labels[k] = v
		}
		labels["@system/cronjob-id"] = cronjob.GetId()
		labels["@system/cronjob-name"] = cronjob.GetName()

		createdJob, err := s.jobsServer.Create(ctx, &api.CreateJobRequest{
			Queue:  cronjob.Queue,
			Spec:   cronjob.Spec,
			Labels: labels,
		})
		if err != nil {
			logrus.Error(err)
		} else {
			logrus.Infof("scheduled new job %s in queue %s", createdJob.GetId(), createdJob.GetQueue())
		}
	}
	return nil
}

func (s *cronjobsServer) getBuilder(db squirrel.BaseRunner) squirrel.StatementBuilderType {
	return squirrel.StatementBuilder.
		PlaceholderFormat(squirrel.Dollar).
		RunWith(db)
}

func (s *cronjobsServer) List(req *api.ListRequest, resp api.CronJobs_ListServer) (err error) {
	span, ctx := s.StartSpan(resp.Context(), "List")
	defer func() {
		s.FinishSpan(span, err)
	}()
	span.SetTag("queues", req.GetQueues())
	span.SetTag("labels", req.GetLabels())

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
	rows, err := s.getBuilder(s.db).
		Select("cronjob_id", "name", "labels", "queue", "spec", "cron", "created_at", "next_run_at").
		From("cronjobs").
		Where(filter).
		QueryContext(ctx)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			cronjob   api.CronJob
			createdAt time.Time
			nextRunAt *time.Time
		)
		err = rows.Scan(&cronjob.Id, &cronjob.Name, dbtypes.JSONBlob(&cronjob.Labels), &cronjob.Queue, &cronjob.Spec, &cronjob.Cron, &createdAt, &nextRunAt)
		if err != nil {
			return err
		}
		cronjob.CreatedAt, err = ptypes.TimestampProto(createdAt)
		if err != nil {
			return err
		}
		if nextRunAt != nil {
			cronjob.NextRunAt, err = ptypes.TimestampProto(*nextRunAt)
			if err != nil {
				return err
			}
		}
		if err = resp.Send(&cronjob); err != nil {
			return err
		}
	}
	return nil
}
