package events

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	dbtypes "github.com/contiamo/go-base/pkg/db/serialization"
	"github.com/contiamo/go-base/pkg/tracing"
	"github.com/golang/protobuf/ptypes"
	"github.com/jackc/pgx/v4"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/trusch/backbone-tools/pkg/api"
	"github.com/trusch/backbone-tools/pkg/sqlizers"
	"github.com/trusch/backbone-tools/pkg/ticker"
)

var (
	pollInterval = 10 * time.Second
)

func NewServer(ctx context.Context, db *sql.DB, connectString string) (api.EventsServer, error) {
	srv := &eventsServer{
		Tracer:        tracing.NewTracer("events", "EventsServer"),
		db:            db,
		connectString: connectString,
	}
	return srv, srv.init(ctx)
}

type eventsServer struct {
	tracing.Tracer
	db            squirrel.StdSqlCtx
	connectString string
}

func (s *eventsServer) init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS events(
  event_id UUID PRIMARY KEY,
  topic TEXT NOT NULL,
  payload BYTEA,
  labels JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  sequence SERIAL
);
`)
	return err
}

func (s *eventsServer) getBuilder(db squirrel.BaseRunner) squirrel.StatementBuilderType {
	return squirrel.StatementBuilder.
		PlaceholderFormat(squirrel.Dollar).
		RunWith(db)
}

func (s *eventsServer) Publish(ctx context.Context, req *api.PublishRequest) (event *api.Event, err error) {
	span, ctx := s.StartSpan(ctx, "Publish")
	defer func() {
		s.FinishSpan(span, err)
	}()

	span.SetTag("topic", req.GetTopic())
	span.SetTag("labels", req.GetLabels())
	span.SetTag("payload", req.GetPayload())

	id := uuid.NewV4().String()
	now := time.Now()
	nowProto, err := ptypes.TimestampProto(now)
	if err != nil {
		return nil, err
	}

	if req.Labels == nil {
		req.Labels = make(map[string]string)
	}

	builder := s.getBuilder(s.db).Insert("events").Columns(
		"event_id",
		"topic",
		"labels",
		"payload",
		"created_at",
	).Values(
		id,
		req.GetTopic(),
		dbtypes.JSONBlob(req.GetLabels()),
		req.GetPayload(),
		now,
	).Suffix("RETURNING \"sequence\"")

	fmt.Println(builder.ToSql())

	row := builder.QueryRowContext(ctx)
	var seq uint64
	if err := row.Scan(&seq); err != nil {
		return nil, errors.Wrap(err, "failed to insert event")
	}

	_, err = s.db.ExecContext(ctx, `NOTIFY events_`+strings.Replace(req.GetTopic(), "-", "_", -1))
	if err != nil {
		return nil, err
	}

	return &api.Event{
		Id:        id,
		Topic:     req.GetTopic(),
		Labels:    req.GetLabels(),
		Payload:   req.GetPayload(),
		CreatedAt: nowProto,
		Sequence:  seq,
	}, nil

}

func (s *eventsServer) Subscribe(req *api.SubscribeRequest, resp api.Events_SubscribeServer) (err error) {
	span, ctx := s.StartSpan(resp.Context(), "Subscribe")
	defer func() {
		s.FinishSpan(span, err)
	}()

	notifyConn, err := pgx.Connect(ctx, s.connectString)
	if err != nil {
		return err
	}
	ticker := ticker.New(pollInterval, 0.1, notifyConn, "events_"+strings.Replace(req.GetTopic(), "-", "_", -1))
	if err := ticker.Start(ctx); err != nil {
		return err
	}
	lastSequence := req.GetSinceSequence()
	timestamp := req.GetSinceCreatedAt()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			filter := squirrel.And{
				squirrel.Eq{
					"topic": req.GetTopic(),
				},
			}
			if lastSequence > 0 {
				filter = append(filter, squirrel.Gt{
					"sequence": lastSequence,
				})
			}
			if labels := req.GetLabels(); labels != nil {
				filter = append(filter, sqlizers.JSONContains{
					"labels": dbtypes.JSONBlob(labels),
				})
			}
			if timestamp != nil {
				ts, err := ptypes.Timestamp(timestamp)
				if err != nil {
					return err
				}
				filter = append(filter, squirrel.GtOrEq{
					"created_at": ts,
				})
			}

			rows, err := s.getBuilder(s.db).
				Select("event_id", "labels", "payload", "created_at", "sequence").
				From("events").
				Where(filter).
				QueryContext(ctx)
			if err != nil {
				return err
			}

			for rows.Next() {
				var (
					event     = api.Event{Topic: req.GetTopic()}
					createdAt time.Time
				)
				err = rows.Scan(&event.Id, dbtypes.JSONBlob(&event.Labels), &event.Payload, &createdAt, &event.Sequence)
				if err != nil {
					return err
				}
				event.CreatedAt, err = ptypes.TimestampProto(createdAt)
				if err != nil {
					return err
				}
				span, _ := s.StartSpan(ctx, "sendEvent")
				err = resp.Send(&event)
				lastSequence = event.Sequence
				timestamp = nil
				s.FinishSpan(span, err)
				if err != nil {
					return err
				}
			}
		}
	}
}
