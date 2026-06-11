package main

import (
	"log"
	"net/http"

	"github.com/henry-insomniac/dev-time-server/internal/api"
	"github.com/henry-insomniac/dev-time-server/internal/buildinfo"
	"github.com/henry-insomniac/dev-time-server/internal/config"
)

func main() {
	loaded := config.Load()
	log.Printf("starting %s on %s", buildinfo.ServiceName(), loaded.ServerAddr)

	if err := http.ListenAndServe(loaded.ServerAddr, api.NewRouter()); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
