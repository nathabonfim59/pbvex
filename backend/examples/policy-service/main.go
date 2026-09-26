// A local demonstration only: reservations/events disappear on restart.
package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/nathabonfim59/pbvex/backend/hosting"
)

func main() {
	socket := flag.String("socket", "", "absolute Unix socket path in a private directory")
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
	server := &http.Server{Handler: service, ReadHeaderTimeout: time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 4096}
	log.Fatal(server.Serve(l))
}
