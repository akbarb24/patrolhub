package service

import (
	"errors"

	"github.com/jackc/pgx/v5"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrNightClosed = errors.New("night is closed")
)

// notFound maps "no rows" to ErrNotFound and passes other errors through.
func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
