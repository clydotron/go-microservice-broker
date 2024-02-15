package main

import (
	"fmt"
	"log"
	"net/http"
)

type App struct {
}

const webPort = "8080"

func main() {

	app := App{}
	log.Printf("Starting broker service on port: %s\n", webPort)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%s", webPort),
		Handler: app.routes(),
	}

	err := srv.ListenAndServe()
	if err != nil {
		log.Panic(err)
	}

	// TODO add machinery to cleanly shut this down
}
