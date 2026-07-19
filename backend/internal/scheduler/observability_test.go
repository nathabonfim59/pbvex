package scheduler

import (
	"errors"
	"testing"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
)

func TestPersistedHandlerFailureMessagesAreSafe(t *testing.T) {
	unexpected := &handledHandlerFailure{err: errors.New("provider token secret")}
	if got := persistedErrorMessage(unexpected); got != "Function invocation failed." {
		t.Fatalf("unexpected persisted message %q", got)
	}

	expected := &handledHandlerFailure{err: &deploy.ApplicationError{Category: deploy.ApplicationErrorConflict}}
	if got := persistedErrorMessage(expected); got != "application error: conflict" {
		t.Fatalf("expected application category, got %q", got)
	}
}
