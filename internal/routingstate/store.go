package routingstate

import (
	"context"
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("routing state not found")

type ValidationError struct {
	message string
	cause   error
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	return e.message
}

func (e *ValidationError) Unwrap() error { return e.cause }

func newValidationError(message string, err error) error {
	return &ValidationError{message: message, cause: err}
}

func IsValidationError(err error) bool {
	var target *ValidationError
	return errors.As(err, &target)
}

type Store interface {
	List(ctx context.Context) ([]Entry, error)
	Upsert(ctx context.Context, entry Entry) error
	Delete(ctx context.Context, key string) error
	Close() error
}

func collectEntries(next func() (Entry, bool, error), rowsErr func() error) ([]Entry, error) {
	result := make([]Entry, 0)
	for {
		entry, ok, err := next()
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		result = append(result, entry)
	}
	if err := rowsErr(); err != nil {
		return nil, fmt.Errorf("iterate routing state: %w", err)
	}
	return result, nil
}
