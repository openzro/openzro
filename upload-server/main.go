package main

import (
	"errors"
	"log"
	"net/http"

	"github.com/openzro/openzro/upload-server/server"
	"github.com/openzro/openzro/util"
)

func main() {
	err := util.InitLog("info", util.LogConsole)
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	srv := server.NewServer()
	if err = srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Failed to start server: %v", err)
	}
}
