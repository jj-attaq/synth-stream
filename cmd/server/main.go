package main

import (
	"log"
	"os"

	"github.com/jj-attaq/synth-stream/internal/server"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	port := os.Getenv("PORT")

	srv, err := server.New(":" + port)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Server starting on :8080")
	srv.Start()
}
