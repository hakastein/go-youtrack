package youtrack_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/hakastein/youtrack"
)

func TestAnErrorIsItsCodeAndNoOther(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code     youtrack.Code
		sentinel error
		other    error
	}{
		{code: youtrack.CodeBadUsage, sentinel: youtrack.ErrBadUsage, other: youtrack.ErrUnknownName},
		{code: youtrack.CodeUnknownName, sentinel: youtrack.ErrUnknownName, other: youtrack.ErrMissingRequired},
		{code: youtrack.CodeMissingRequired, sentinel: youtrack.ErrMissingRequired, other: youtrack.ErrNotFound},
		{code: youtrack.CodeNotFound, sentinel: youtrack.ErrNotFound, other: youtrack.ErrDenied},
		{code: youtrack.CodeDenied, sentinel: youtrack.ErrDenied, other: youtrack.ErrRejected},
		{code: youtrack.CodeRejected, sentinel: youtrack.ErrRejected, other: youtrack.ErrUpstreamFailed},
		{code: youtrack.CodeUpstreamFailed, sentinel: youtrack.ErrUpstreamFailed, other: youtrack.ErrUpstreamInvalid},
		{code: youtrack.CodeUpstreamInvalid, sentinel: youtrack.ErrUpstreamInvalid, other: youtrack.ErrWriteUncertain},
		{code: youtrack.CodeWriteUncertain, sentinel: youtrack.ErrWriteUncertain, other: youtrack.ErrBadUsage},
	}
	for _, tc := range tests {
		t.Run(string(tc.code), func(t *testing.T) {
			t.Parallel()

			err := fmt.Errorf("wrapped: %w", &youtrack.Error{Code: tc.code, Message: "First", AfterWrite: true})

			assert.ErrorIs(t, err, tc.sentinel)
			assert.NotErrorIs(t, err, tc.other)
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
