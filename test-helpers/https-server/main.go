// Package main provides a simple HTTPS server for testing CA certificate injection.
// Usage: go run ./test-helpers/https-server --cert=server.crt --key=server.key --addr=:8443
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cert := flag.String("cert", "", "Path to TLS certificate file")
	key := flag.String("key", "", "Path to TLS key file")
	addr := flag.String("addr", ":8443", "Address to listen on")
	flag.Parse()

	if *cert == "" || *key == "" {
		fmt.Fprintln(os.Stderr, "Usage: https-server --cert=server.crt --key=server.key [--addr=:8443]")
		os.Exit(1)
	}

	// Simple handler that returns OK
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "OK")
	})

	// Start server in a goroutine
	server := &http.Server{Addr: *addr}
	go func() {
		log.Printf("Starting HTTPS server on %s", *addr)
		if err := server.ListenAndServeTLS(*cert, *key); err != http.ErrServerClosed {
			log.Fatalf("HTTPS server error: %v", err)
		}
	}()

	// Print ready message for test synchronization
	fmt.Println("READY")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down HTTPS server")
}
