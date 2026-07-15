package runtime

import (
	"context"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
)

type manifestContextKey struct{}

// ManifestFromContext exposes the authenticated deployment snapshot to host capabilities.
func ManifestFromContext(ctx context.Context) (deploy.DeploymentManifest, bool) {
	manifest, ok := ctx.Value(manifestContextKey{}).(deploy.DeploymentManifest)
	return manifest, ok
}
