package main

import (
	"context"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jj-attaq/synth-stream/internal/api"
	"github.com/jj-attaq/synth-stream/internal/db"
	"github.com/jj-attaq/synth-stream/internal/server"
	"github.com/joho/godotenv"
)

func main() {
	// if err := godotenv.Load(); err != nil {
	// 	log.Fatal("error loading .env file")
	// }
	godotenv.Load()

	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("could not connect to database: %v", err)
	}
	defer pool.Close()

	queries := db.New(pool)

	srv, err := server.New(":"+os.Getenv("PORT"), os.Getenv("JWT_SECRET"), os.Getenv("CERT_FILE"), os.Getenv("KEY_FILE"))
	if err != nil {
		log.Fatal(err)
	}

	apiSrv := api.New(queries, os.Getenv("JWT_SECRET"), ":"+os.Getenv("API_PORT"))

	log.Printf("TCP server starting on :%s", os.Getenv("PORT"))
	go srv.Start()

	log.Printf("API server starting on :%s", os.Getenv("API_PORT"))
	if err := apiSrv.Start(); err != nil {
		log.Fatalf("API server error: %v", err)
	}
}
