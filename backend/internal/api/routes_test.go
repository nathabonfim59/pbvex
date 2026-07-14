package api

import "testing"

func TestIsReservedPlatformPath(t *testing.T) {
	for _, path := range []string{
		"/api/pbvex/call",
		"/api/pbvex/realtime",
		"/api/pbvex/deployments",
		"/api/pbvex/deployments/id/activate",
		"/api/pbvex/jobs/id",
		"/custom/files/upload/token",
		"/custom/files/id",
	} {
		if !IsReservedPlatformPath(path, "/custom/files") {
			t.Fatalf("expected reserved path %q", path)
		}
	}
	for _, path := range []string{"/api/pbvex/custom-action", "/api/pbvex/caller", "/custom/filename"} {
		if IsReservedPlatformPath(path, "/custom/files") {
			t.Fatalf("unexpected reserved path %q", path)
		}
	}
}
