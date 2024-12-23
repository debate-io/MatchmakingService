package infr

import (
	"context"
	"fmt"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func NewPostgresDatabase(dsn string, appName string, logger *zap.Logger) (*pg.DB, error) {
	options, err := pg.ParseURL(dsn)
	if err != nil {
		return nil, errors.Wrap(err, "can't conn to infr")
	}

	options.ApplicationName = fmt.Sprintf("[%s]", appName)
	options.TLSConfig = nil

	db := pg.Connect(options)
	db.AddQueryHook(QueryLogger{Logger: logger})

	return db, nil
}

type QueryLogger struct {
	Logger *zap.Logger
}

func (l QueryLogger) BeforeQuery(ctx context.Context, event *pg.QueryEvent) (context.Context, error) {
	return ctx, nil
}

func (l QueryLogger) AfterQuery(ctx context.Context, q *pg.QueryEvent) error {
	sql, err := q.FormattedQuery()
	if err != nil {
		l.Logger.Error("SQL error", zap.String("sql", string(sql)), zap.Error(err))
	} else {
		l.Logger.Debug(fmt.Sprintf("SQL: %s", sql))
	}

	return nil
}
