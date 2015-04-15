package main

import (
	"github.com/danjac/pinbook"
)

const (
	StaticDir  = "webapp/public"
	UploadsDir = "webapp/uploads"
)

func main() {

	cfg := &pinbook.Config{
		DbHost:     "localhost",
		DbName:     "react-tutorial",
		SecretKey:  "seekret!",
		Port:       6543,
		UploadsDir: UploadsDir,
		StaticDir:  StaticDir,
	}

	if err := pinbook.ServeApp(cfg); err != nil {
		panic(err)
	}
}
