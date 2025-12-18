// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// The tsnet-services example demonstrates how to use tsnet with Services.
// TODO: explain that a Service must be defined for the tailent and link to KB
// on defining a Service
//
// To use it, generate an auth key from the Tailscale admin panel and
// run the demo with the key:
//
//	TS_AUTHKEY=<yourkey> go run tsnet-services.go
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"

	"tailscale.com/tailcfg"
	"tailscale.com/tsnet"
)

var (
	svcName = flag.String("service", "", "the name of your Service, e.g. svc:demo-service")
	port    = flag.Uint("port", 0, "the port to listen on")
)

// TODO: this worked several times, then my host got stuck in 'Partially configured: has-config, config-valid'

func main() {
	flag.Parse()
	if *svcName == "" {
		log.Fatal("a Service name must be provided")
	}
	if *port == 0 {
		log.Fatal("the listening port must be provided")
	}
	if *port > math.MaxUint16 {
		log.Fatal("invalid port number")
	}

	s := &tsnet.Server{
		Dir:      "./services-demo-config",
		Hostname: "tsnet-services-demo",
	}
	defer s.Close()

	ln, err := s.ListenService(*svcName, uint16(*port))
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()

	fmt.Printf("Listening on http://%v\n", tailcfg.AsServiceName(*svcName).WithoutPrefix())

	// TODO: maybe just respond to TCP connections? (since we don't know the port)
	//   Actually, let's hard-code port 80 and provide an example Service definition to use
	err = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "<html><body><h1>Hello, tailnet!</h1>")
	}))
	log.Fatal(err)
}
