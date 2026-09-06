package pbvex

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/nathabonfim59/pbvex/backend/hosting"
	"github.com/nathabonfim59/pbvex/backend/internal/runtime"
)

// hostedEnvironmentResolver gates every host environment lookup behind the
// per-name "environment.read/<name>" capability before delegating to the
// configured resolver, or to the standard default (os.LookupEnv) when none is
// configured. Literal manifest bindings never reach a resolver. There is no
// unrestricted fallback: a denied or unavailable capability fails the binding.
func hostedEnvironmentResolver(client *hosting.Client, logger *slog.Logger, next runtime.EnvironmentResolver) runtime.EnvironmentResolver {
	return func(ctx context.Context, name string) (string, bool, error) {
		// The composed capability is validated locally before any socket
		// traffic so malformed or oversized binding names cannot probe the
		// provider with invalid requests.
		capability := hosting.EnvironmentRead + "/" + name
		if name == "" || !hosting.ValidToken(capability) {
			return "", false, fmt.Errorf("environment variable name %q is not a valid hosting policy capability", name)
		}
		if ctx == nil {
			ctx = context.Background()
		}
		if err := client.Require(ctx, capability); err != nil {
			if logger != nil {
				logger.Error("hosting policy rejected environment read", "capability", capability)
			}
			return "", false, fmt.Errorf("environment read %q rejected by hosting policy: %w", capability, err)
		}
		if next != nil {
			return next(ctx, name)
		}
		value, ok := os.LookupEnv(name)
		return value, ok, nil
	}
}
