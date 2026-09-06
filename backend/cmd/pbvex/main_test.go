package main

import (
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

func TestManagedS3FromEnvDisabledByDefault(t *testing.T) {
	cfg, err := managedS3FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled {
		t.Fatal("managed storage enabled without configuration")
	}
}

func TestManagedS3FromEnvExplicitFalseWithoutCredentials(t *testing.T) {
	// Deployment environments that cannot unset variables must be able to
	// disable managed storage with an explicit false.
	t.Setenv("PBVEX_HOST_STORAGE_S3_ENABLED", "false")
	cfg, err := managedS3FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled {
		t.Fatal("explicit false did not disable managed storage")
	}
}

func TestManagedS3FromEnvValidConfiguration(t *testing.T) {
	t.Setenv("PBVEX_HOST_STORAGE_S3_ENABLED", "true")
	t.Setenv("PBVEX_HOST_STORAGE_S3_BUCKET", "bucket")
	t.Setenv("PBVEX_HOST_STORAGE_S3_REGION", "auto")
	t.Setenv("PBVEX_HOST_STORAGE_S3_ENDPOINT", "https://s3.example")
	t.Setenv("PBVEX_HOST_STORAGE_S3_ACCESS_KEY", "ak")
	t.Setenv("PBVEX_HOST_STORAGE_S3_SECRET", "sk")
	t.Setenv("PBVEX_HOST_STORAGE_S3_FORCE_PATH_STYLE", "true")

	cfg, err := managedS3FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	want := core.S3Config{
		Enabled:        true,
		Bucket:         "bucket",
		Region:         "auto",
		Endpoint:       "https://s3.example",
		AccessKey:      "ak",
		Secret:         "sk",
		ForcePathStyle: true,
	}
	if cfg != want {
		t.Fatalf("parsed config = %+v", cfg)
	}
}

func TestManagedS3FromEnvRejectsInvalidConfigurations(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(t *testing.T)
		fragme string
	}{
		{
			name: "invalid enabled boolean",
			setup: func(t *testing.T) {
				t.Setenv("PBVEX_HOST_STORAGE_S3_ENABLED", "sometimes")
			},
			fragme: "invalid boolean for PBVEX_HOST_STORAGE_S3_ENABLED",
		},
		{
			name: "explicit false with credentials present",
			setup: func(t *testing.T) {
				t.Setenv("PBVEX_HOST_STORAGE_S3_ENABLED", "false")
				t.Setenv("PBVEX_HOST_STORAGE_S3_BUCKET", "bucket")
			},
			fragme: "conflicts with PBVEX_HOST_STORAGE_S3_BUCKET",
		},
		{
			name: "missing required credentials",
			setup: func(t *testing.T) {
				t.Setenv("PBVEX_HOST_STORAGE_S3_ENABLED", "true")
			},
			fragme: "invalid PBVEX_HOST_STORAGE_S3_* configuration",
		},
		{
			name: "invalid force path style boolean",
			setup: func(t *testing.T) {
				t.Setenv("PBVEX_HOST_STORAGE_S3_ENABLED", "true")
				t.Setenv("PBVEX_HOST_STORAGE_S3_BUCKET", "bucket")
				t.Setenv("PBVEX_HOST_STORAGE_S3_REGION", "auto")
				t.Setenv("PBVEX_HOST_STORAGE_S3_ENDPOINT", "https://s3.example")
				t.Setenv("PBVEX_HOST_STORAGE_S3_ACCESS_KEY", "ak")
				t.Setenv("PBVEX_HOST_STORAGE_S3_SECRET", "sk")
				t.Setenv("PBVEX_HOST_STORAGE_S3_FORCE_PATH_STYLE", "maybe")
			},
			fragme: "invalid boolean for PBVEX_HOST_STORAGE_S3_FORCE_PATH_STYLE",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup(t)
			_, err := managedS3FromEnv()
			if err == nil {
				t.Fatal("expected configuration error")
			}
			if !strings.Contains(err.Error(), tc.fragme) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.fragme)
			}
		})
	}
}
