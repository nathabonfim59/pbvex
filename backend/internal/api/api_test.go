package api

import "testing"

func TestReservedPlatformPathGuard(t *testing.T) {
	for _, path := range []string{
		"/call", "/call/nested", "/realtime", "/deployments/id", "/jobs/run", "/storage/file", "/admin/tools",
	} {
		if !isReservedPlatformPath(path) {
			t.Errorf("reserved path %q was not guarded", path)
		}
	}
	for _, path := range []string{"/callback", "/jobs-board", "/public/admin"} {
		if isReservedPlatformPath(path) {
			t.Errorf("non-reserved path %q was guarded", path)
		}
	}
}

func TestUserResponsesCannotSetCORSHeaders(t *testing.T) {
	for _, header := range []string{
		"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials", "Access-Control-Allow-Methods",
		"Access-Control-Allow-Headers", "Access-Control-Expose-Headers", "Access-Control-Max-Age",
	} {
		if !isCORSResponseHeader(header) {
			t.Errorf("CORS response header %q is not protected", header)
		}
	}
}
