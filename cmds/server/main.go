package main

import (
	"github.com/danjac/pinbook"
	"github.com/joho/godotenv"
	"log"
	"os"
	"strconv"
)

const (
	StaticDir  = "ui/public"
	UploadsDir = "ui/uploads"
)

func getEnv(name string, defaultValue string) string {
	value := os.Getenv(name)
	if value == "" {
		return defaultValue
	}
	return value
}

func main() {

	err := godotenv.Load()

	if err != nil {
		log.Print("Error loading .env file")
	}

	port, _ := strconv.Atoi(getEnv("PORT", "6543"))

	cfg := &pinbook.Config{
		Port:       port,
		DbHost:     getEnv("DB_HOST", "localhost"),
		DbName:     getEnv("DB_NAME", "react-tutorial"),
		SecretKey:  getEnv("SECRET_KEY", "seekret!"),
		UploadsDir: getEnv("UPLOADS_DIR", UploadsDir),
		StaticDir:  getEnv("STATIC_DIR", StaticDir),
	}

	if err := pinbook.ServeApp(cfg); err != nil {
		log.Fatal(err)
	}
}
