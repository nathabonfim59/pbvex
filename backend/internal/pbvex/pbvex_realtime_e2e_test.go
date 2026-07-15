package pbvex

import (
	"bufio"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// TestRealtimeClientE2E runs a real @pbvex/client FetchRealtimeTransport
// client against the Go HTTP server. It is skipped if Node or pnpm is not
// available; if dist is absent, it explicitly builds the SDK packages first.
func TestRealtimeClientE2E(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available in PATH")
	}
	pnpm, err := exec.LookPath("pnpm")
	if err != nil {
		t.Skip("pnpm not available in PATH")
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	repoRoot := filepath.Join(wd, "..", "..", "..")
	repoRoot, err = filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}

	clientDist := filepath.Join(repoRoot, "packages", "client", "dist", "index.js")
	protocolDist := filepath.Join(repoRoot, "packages", "protocol", "dist", "index.js")

	// Always build from source so the E2E runs against the current SDK/POST contract.
	buildCmd := exec.Command(pnpm, "--filter", "@pbvex/client", "build")
	buildCmd.Dir = repoRoot
	buildOut, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("client build failed: %v\n%s", err, string(buildOut))
	}

	if _, err := os.Stat(clientDist); err != nil {
		t.Fatalf("client dist missing after build: %s", clientDist)
	}
	if _, err := os.Stat(protocolDist); err != nil {
		t.Fatalf("protocol dist missing after build: %s", protocolDist)
	}

	app, service := newTestApp(t)

	bundle := testBundleJS
	resp, err := service.Upload(testUploadRequest("realtime", bundle))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	baseRouter, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("failed to create router: %v", err)
	}
	serveEvent := &core.ServeEvent{App: app, Router: baseRouter}
	if err := app.OnServe().Trigger(serveEvent); err != nil {
		t.Fatalf("serve trigger failed: %v", err)
	}
	mux, err := baseRouter.BuildMux()
	if err != nil {
		t.Fatalf("failed to build mux: %v", err)
	}

	server := httptest.NewServer(mux)
	defer server.Close()

	script, err := os.CreateTemp("", "pbvex-realtime-e2e-*.mjs")
	if err != nil {
		t.Fatalf("failed to create temp script: %v", err)
	}
	defer os.Remove(script.Name())

	scriptSrc := fmt.Sprintf(`
import { Client } from %q;

const baseUrl = process.env.PBVEX_BASE_URL;
if (!baseUrl) {
  console.error('PBVEX_BASE_URL not set');
  process.exit(1);
}

const client = new Client(baseUrl);
let received = false;

const unsub = client.watch('hello', { name: 'world' }, {
  onUpdate: (result) => {
    if (result.isLoading) return;
    if (received) return;
    received = true;
    console.log('RESULT ' + JSON.stringify(result));
    unsub();
    client.close();
    process.exit(0);
  },
  onError: (error) => {
    console.log('ERROR ' + error.message);
    client.close();
    process.exit(1);
  },
  onConnectionStateChange: (state) => {
    console.log('STATE ' + state);
  },
});

setTimeout(() => {
  console.error('timeout waiting for realtime update');
  client.close();
  process.exit(1);
}, 10000);
`, "file://"+clientDist)

	if _, err := script.WriteString(scriptSrc); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}
	if err := script.Close(); err != nil {
		t.Fatalf("failed to close script: %v", err)
	}

	cmd := exec.Command(node, script.Name())
	cmd.Env = append(os.Environ(), "PBVEX_BASE_URL="+server.URL)
	cmd.Dir = filepath.Join(repoRoot, "packages", "client")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("failed to create stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start node: %v", err)
	}

	stderrCh := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(stderr)
		stderrCh <- b
	}()

	scanner := bufio.NewScanner(stdout)
	resultFound := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "STATE ") {
			t.Logf("client state: %s", strings.TrimPrefix(line, "STATE "))
			continue
		}
		if strings.HasPrefix(line, "ERROR ") {
			t.Fatalf("client error: %s", strings.TrimPrefix(line, "ERROR "))
		}
		if strings.HasPrefix(line, "RESULT ") {
			payload := strings.TrimPrefix(line, "RESULT ")
			if !strings.Contains(payload, `"Hello, world!"`) {
				t.Fatalf("unexpected result: %s", payload)
			}
			resultFound = true
			break
		}
		t.Logf("client stdout: %s", line)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil && !resultFound {
			t.Fatalf("node process failed: %v", err)
		}
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		t.Fatal("timeout waiting for node process")
	}

	stderrBytes := <-stderrCh
	if !resultFound {
		t.Fatalf("no result received; stderr: %s", string(stderrBytes))
	}
}
