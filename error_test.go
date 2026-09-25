package youtrack_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/youtrack"
)

func TestAnErrorIsItsCode(t *testing.T) {
	t.Parallel()
	sentinels := map[youtrack.Code]error{
		youtrack.CodeBadUsage:        youtrack.ErrBadUsage,
		youtrack.CodeUnknownName:     youtrack.ErrUnknownName,
		youtrack.CodeMissingRequired: youtrack.ErrMissingRequired,
		youtrack.CodeNotFound:        youtrack.ErrNotFound,
		youtrack.CodeDenied:          youtrack.ErrDenied,
		youtrack.CodeRejected:        youtrack.ErrRejected,
		youtrack.CodeUpstreamFailed:  youtrack.ErrUpstreamFailed,
		youtrack.CodeUpstreamInvalid: youtrack.ErrUpstreamInvalid,
		youtrack.CodeWriteUncertain:  youtrack.ErrWriteUncertain,
	}
	for code, sentinel := range sentinels {
		t.Run(string(code), func(t *testing.T) {
			t.Parallel()
			err := fmt.Errorf("wrapped: %w", &youtrack.Error{Code: code, Message: "First", AfterWrite: true})

			for other, unlike := range sentinels {
				assert.Equal(t, other == code, errors.Is(err, unlike), "errors.Is %s", other)
			}
			assert.ErrorIs(t, err, sentinel)
		})
	}
}

func TestAnErrorUnwrapsToTheFailureOfTheTransport(t *testing.T) {
	t.Parallel()

	err := &youtrack.Error{Code: youtrack.CodeUpstreamFailed, Err: context.DeadlineExceeded}

	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestMayHaveWrittenHoldsForAnUncertainWriteAndAnErrorAfterAWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  youtrack.Error
		want bool
	}{
		{name: "an uncertain write", err: youtrack.Error{Code: youtrack.CodeWriteUncertain}, want: true},
		{name: "an error after a write", err: youtrack.Error{Code: youtrack.CodeUpstreamInvalid, AfterWrite: true}, want: true},
		{name: "an uncertain write after a write", err: youtrack.Error{Code: youtrack.CodeWriteUncertain, AfterWrite: true}, want: true},
		{name: "a refusal", err: youtrack.Error{Code: youtrack.CodeRejected}},
		{name: "a failure of the server", err: youtrack.Error{Code: youtrack.CodeUpstreamFailed}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, tc.err.MayHaveWritten())
		})
	}
}
