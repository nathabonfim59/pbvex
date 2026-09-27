// A local demonstration only: reservations, admissions and events are kept
// in memory and disappear on restart. One socket serves both the policy
// protocol (/v1/check, /v1/admit, /v1/events) and the storage byte quota
// protocol (/v1/storage/...), so an enabled PBVex deployment can enforce
// storage quotas against this single endpoint.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nathabonfim59/pbvex/backend/hosting"
	"github.com/nathabonfim59/pbvex/backend/hosting/storagequota"
)

func main() {
	socket := flag.String("socket", "", "absolute Unix socket path in a private directory")
	records := flag.Int("quotaRecords", 10000, "maximum in-memory reservation/credit records (the reference quota service never evicts live records; a full service returns 503)")
	quotaBytes := flag.Int64("quotaBytes", 1<<30, "total demo storage byte capacity (1 GiB by default)")
	flag.Parse()
	if err := (hosting.Config{Enabled: true, SocketPath: *socket}).Validate(); err != nil {
		log.Fatal(err)
	}
	// Refuse to remove existing paths, including sockets belonging to another process.
	l, err := net.Listen("unix", *socket)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()
	if err := os.Chmod(*socket, 0600); err != nil {
		log.Fatal(err)
	}
	service := hosting.NewReferenceService(10000)
	if err := service.SetPolicy("demo-1", map[string]bool{hosting.FunctionExecute: true}); err != nil {
		log.Fatal(err)
	}
	// Bounded in-memory demo capacity: the reference quota service is a
	// compatibility fixture, NOT a durable ledger. It loses all state on
	// restart, never evicts live reservations and performs no
	// reconciliation; production providers implement the same routes behind
	// a durable service instead.
	quota := storagequota.NewReferenceQuotaService(*records, *quotaBytes)
	log.Printf("demo storage quota capacity: %d bytes, %d in-memory records; state is lost on restart", *quotaBytes, *records)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/hello":
			// One composed handshake for the shared socket: the capability
			// list is what an enabled PBVex deployment requires at startup
			// (storage.reserve must be present or startup fails).
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(hosting.Hello{
				Version:        hosting.Version,
				Implementation: "pbvex-example-policy-storagequota",
				Capabilities:   []string{hosting.FunctionExecute, storagequota.CapabilityStorageReserve},
			})
		case strings.HasPrefix(r.URL.Path, "/v1/storage/"):
			quota.ServeHTTP(w, r)
		default:
			service.ServeHTTP(w, r)
		}
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 4096}
	log.Fatal(server.Serve(l))
}
