package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"
)

// NOTES
// For auth, this is a good library: https://github.com/markbates/goth

func main() {
	godotenv.Load()
	port := os.Getenv("PORT")
	if port == "" {
		log.Fatal("Port must be set")
	}

	mux := http.NewServeMux()

	srv := http.Server{
		Addr:    fmt.Sprintf(":%s", port),
		Handler: mux,
	}

	log.Printf("Serving on port: %s\n", port)
	log.Fatal(srv.ListenAndServe())
}
