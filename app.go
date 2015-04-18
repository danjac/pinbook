package pinbook

import (
	"fmt"
	"github.com/codegangsta/negroni"
	"github.com/goincremental/negroni-sessions"
	"github.com/goincremental/negroni-sessions/cookiestore"
	"github.com/justinas/nosurf"
	"gopkg.in/mgo.v2"
	"runtime"
)

type Config struct {
	DbHost     string
	DbName     string
	SecretKey  string
	Port       int
	StaticDir  string
	UploadsDir string
}

type App struct {
	Cfg *Config
	DB  *Database
}

func ServeApp(cfg *Config) error {

	runtime.GOMAXPROCS((runtime.NumCPU() * 2) + 1)

	session, err := mgo.Dial(cfg.DbHost)
	if err != nil {
		return err
	}
	defer session.Close()

	app := &App{}
	app.DB = NewDatabase(session.DB(cfg.DbName))
	app.Cfg = cfg

	router := getRouter(app)

	n := negroni.Classic()

	store := cookiestore.New([]byte(app.Cfg.SecretKey))
	n.Use(sessions.Sessions("default", store))

	n.UseHandler(nosurf.New(router))

	n.Run(fmt.Sprintf(":%d", app.Cfg.Port))
	return nil
}
