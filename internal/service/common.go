package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/pkg/domain"
)

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", domain.ErrInvalid, fmt.Sprintf(format, a...))
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505":
			return domain.ErrConflict
		case "23503", "23514", "22P02":
			return fmt.Errorf("%w: %s", domain.ErrInvalid, pg.Message)
		}
	}
	return err
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func enumToDB(v int32, names map[int32]string, def string) string {
	if v == 0 {
		return def
	}
	if n, ok := names[v]; ok {
		return strings.ToLower(n)
	}
	return def
}

func enumFromDB(s string, values map[string]int32) int32 {
	return values[strings.ToUpper(s)]
}

func tsTime(t *timestamppb.Timestamp) *time.Time {
	if t == nil || (t.Seconds == 0 && t.Nanos == 0) {
		return nil
	}
	v := t.AsTime()
	return &v
}

func pageArgs(page, size int32) domain.Page { return domain.NewPage(page, size) }
