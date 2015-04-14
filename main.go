package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	//validator "github.com/asaskevich/govalidator"
	"github.com/codegangsta/negroni"
	"github.com/goincremental/negroni-sessions"
	"github.com/goincremental/negroni-sessions/cookiestore"
	"github.com/gorilla/mux"
	"github.com/justinas/nosurf"
	"github.com/nfnt/resize"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"html/template"
	"log"
	"net/http"
	"os"
	"path"
	"runtime"
	"time"
)

const StaticDir = "/home/danjac/Projects/react-tutorial/public"
const UploadsDir = "/home/danjac/Projects/react-tutorial/uploads"

type Database struct {
	*mgo.Database
	Users *mgo.Collection
	Posts *mgo.Collection
}

func NewDatabase(db *mgo.Database) *Database {
	return &Database{
		db,
		db.C("users"),
		db.C("posts"),
	}
}

type User struct {
	Id         bson.ObjectId   `bson:"_id" json:"_id"`
	Name       string          `json:"name"`
	Email      string          `json:"email" valid:"email"`
	TotalScore int64           `json:"totalScore"`
	Votes      []bson.ObjectId `json:"votes"`
}

type Author struct {
	Id   bson.ObjectId `bson:"_id" json:"_id"`
	Name string        `json:"name"`
}

type Post struct {
	Id       bson.ObjectId `bson:"_id" json:"_id"`
	Title    string        `json:"title"`
	URL      string        `json:"url"`
	Image    string        `json:"image" valid:"url"`
	Comment  string        `json:"comment"`
	Score    int64         `json:"score"`
	Created  time.Time     `json:"created"`
	AuthorId bson.ObjectId `bson:"author" json:"-"`
	Author   *Author       `bson:"-" json:"author"`
}

type PostForm struct {
	Title   string `json:"title"`
	URL     string `json:"url" valid:"url"`
	Image   string `json:"image" valid:"url"`
	Comment string `json:"comment"`
}

type Result struct {
	Posts   []Post `json:"posts"`
	Total   int    `json:"total"`
	IsFirst bool   `json:"isFirst"`
	IsLast  bool   `json:"isLast"`
	Page    int    `json:"page"`
}

type Context struct {
	DB   *Database
	User *User
}

type handlerFunc func(*Context, http.ResponseWriter, *http.Request) error

func makeHandler(db *Database, h handlerFunc) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		c := &Context{}
		c.DB = db

		err := h(c, w, r)
		if err != nil {
			// http error handling here...
			panic(err)
		}
	}

}

func makeSecureHandler(db *Database, h handlerFunc) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		c := &Context{}
		c.DB = db
		user, err := getUser(c, r)
		if err != nil {
			panic(err)
		}
		if user == nil {
			http.Error(w, "You must be logged in", http.StatusUnauthorized)
			return
		}
		c.User = user
		err = h(c, w, r)
		if err != nil {
			panic(err)
		}
	}

}

func getUser(c *Context, r *http.Request) (*User, error) {

	session := sessions.GetSession(r)
	userId := session.Get("userid")

	if userId == nil {
		return nil, nil
	}

	user := &User{}

	if err := c.DB.Users.Find(bson.M{"_id": bson.ObjectIdHex(userId.(string))}).One(&user); err != nil {
		return nil, nil
	}
	return user, nil

}

func logout(c *Context, w http.ResponseWriter, r *http.Request) error {
	session := sessions.GetSession(r)
	session.Clear()
	http.Redirect(w, r, "/", 302)
	return nil
}

func indexPage(c *Context, w http.ResponseWriter, r *http.Request) error {
	t, err := template.ParseFiles("templates/index.html")
	if err != nil {
		return err
	}
	user, err := getUser(c, r)
	if err != nil {
		return err
	}
	userJson, err := json.Marshal(user)
	if err != nil {
		return err
	}
	// get csrf token, user
	ctx := make(map[string]interface{})
	ctx["csrfToken"] = nosurf.Token(r)
	ctx["user"] = template.JS(userJson)
	return t.Execute(w, ctx)
}

func parseJSON(payload interface{}, r *http.Request) error {
	return json.NewDecoder(r.Body).Decode(payload)
}

func renderJSON(payload interface{}, status int, w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(payload)
}

func loginHandler(c *Context, w http.ResponseWriter, r *http.Request) error {

	s := &struct {
		Identity string `json:"identity"`
		Password string `json:"password"`
	}{}

	if err := parseJSON(s, r); err != nil {
		return err
	}

	user := &User{}

	if err := c.DB.Users.Find(bson.M{"name": s.Identity}).One(&user); err != nil {
		return err
	}
	session := sessions.GetSession(r)
	session.Set("userid", user.Id.Hex())

	return renderJSON(user, http.StatusOK, w)

}

func deletePostHandler(c *Context, w http.ResponseWriter, r *http.Request) error {

	postId := mux.Vars(r)["id"]
	if postId == "" {
		http.NotFound(w, r)
		return nil
	}

	post := &Post{}
	query := bson.M{"_id": bson.ObjectIdHex(postId), "author": c.User.Id}

	if err := c.DB.Posts.Find(query).One(&post); err != nil {
		http.NotFound(w, r)
		return nil
	}

	if err := os.Remove(path.Join(UploadsDir, post.Image)); err != nil {
		return err
	}

	if err := c.DB.Posts.Remove(query); err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func submitPostHandler(c *Context, w http.ResponseWriter, r *http.Request) error {

	form := &PostForm{}

	if err := parseJSON(form, r); err != nil {
		return err
	}

	/*
		_, err = validator.ValidateStruct(form)
		if err != nil {
			log.Print(err)
			return renderJSON(err, 400, w)
		}
	*/

	// fetch image

	resp, err := http.Get(form.Image)
	if err != nil {
		http.Error(w, "Unable to fetch image", 400)
		return nil
	}
	defer resp.Body.Close()

	var img image.Image

	ext := path.Ext(form.Image)

	switch ext {
	case ".jpg":
		img, err = jpeg.Decode(resp.Body)
	case ".png":
		img, err = png.Decode(resp.Body)
	default:
		http.Error(w, "Not a valid image", 400)
		return nil
	}

	if err != nil {
		http.Error(w, "Unable to process", 400)
		return nil
	}

	t := resize.Thumbnail(300, 500, img, resize.NearestNeighbor)

	postId := bson.NewObjectId()

	filename := fmt.Sprintf("%s%s", postId.Hex(), ext)
	fullPath := path.Join(UploadsDir, filename)

	out, err := os.Create(fullPath)
	if err != nil {
		return err
	}
	defer out.Close()

	switch ext {
	case ".jpg":
		jpeg.Encode(out, t, nil)
	case ".png":
		png.Encode(out, t)
	default:
		http.Error(w, "Not a valid image", 400)
		return nil
	}

	// save image
	post := &Post{
		Id:       postId,
		Title:    form.Title,
		Image:    filename,
		URL:      form.URL,
		Comment:  form.Comment,
		AuthorId: c.User.Id,
		Created:  time.Now(),
		Score:    1,
	}
	if err := c.DB.Posts.Insert(post); err != nil {
		return err
	}

	if err := c.DB.Users.Update(bson.M{"_id": c.User.Id}, bson.M{"$inc": bson.M{"totalScore": 1}}); err != nil {
		return err
	}

	return renderJSON(post, http.StatusOK, w)
}

func userHandler(c *Context, w http.ResponseWriter, r *http.Request) error {
	var posts []Post

	orderBy := r.URL.Query().Get("orderBy")
	if orderBy != "created" && orderBy != "score" {
		orderBy = "created"
	}

	//page := r.URL.Query["page"]

	name := mux.Vars(r)["name"]

	user := &Author{}

	err := c.DB.Users.Find(bson.M{"name": name}).One(&user)

	q := c.DB.Posts.Find(bson.M{"author": user.Id})

	total, err := q.Count()

	if err != nil {
		return err
	}

	err = q.Sort("-" + orderBy).Limit(12).All(&posts)

	if err != nil {
		return err
	}

	result := &Result{
		Total:   total,
		IsFirst: true,
		IsLast:  true,
		Page:    1,
	}

	for _, post := range posts {
		post.Author = user
		result.Posts = append(result.Posts, post)
	}

	return renderJSON(result, http.StatusOK, w)

}

func searchHandler(c *Context, w http.ResponseWriter, r *http.Request) error {
	var posts []Post

	orderBy := r.URL.Query().Get("orderBy")
	if orderBy != "created" && orderBy != "score" {
		orderBy = "created"
	}

	//page := r.URL.Query["page"]
	search := bson.M{"$regex": bson.RegEx{r.URL.Query().Get("q"), "i"}}
	log.Print(search)

	q := c.DB.Posts.Find(bson.M{"$or": []bson.M{bson.M{"title": search}, bson.M{"url": search}}})

	total, err := q.Count()

	if err != nil {
		return err
	}

	err = q.Sort("-" + orderBy).Limit(12).All(&posts)

	if err != nil {
		return err
	}

	result := &Result{
		Total:   total,
		IsFirst: true,
		IsLast:  true,
		Page:    1,
	}

	for _, post := range posts {
		err = c.DB.Users.Find(
			bson.M{"_id": post.AuthorId}).Select(
			bson.M{"_id": 1, "name": 1}).One(
			&post.Author)
		if err != nil {
			return err
		}
		result.Posts = append(result.Posts, post)
	}

	return renderJSON(result, http.StatusOK, w)

}

func postsHandler(c *Context, w http.ResponseWriter, r *http.Request) error {

	var posts []Post

	orderBy := r.URL.Query().Get("orderBy")
	if orderBy != "created" && orderBy != "score" {
		orderBy = "created"
	}

	//page := r.URL.Query["page"]

	total, err := c.DB.Posts.Count()
	if err != nil {
		return err
	}

	err = c.DB.Posts.Find(nil).Sort("-" + orderBy).Limit(12).All(&posts)

	if err != nil {
		return err
	}

	result := &Result{
		Total:   total,
		IsFirst: true,
		IsLast:  true,
		Page:    1,
	}

	for _, post := range posts {
		err = c.DB.Users.Find(
			bson.M{"_id": post.AuthorId}).Select(
			bson.M{"_id": 1, "name": 1}).One(
			&post.Author)
		if err != nil {
			return err
		}
		result.Posts = append(result.Posts, post)
	}

	return renderJSON(result, http.StatusOK, w)
}

func serveStatic(router *mux.Router, prefix string, dirname string) {
	router.PathPrefix(prefix).Handler(http.StripPrefix(prefix, http.FileServer(http.Dir(dirname))))
}

func getRouter(db *Database) *mux.Router {

	router := mux.NewRouter()

	// public api

	api := router.PathPrefix("/api").Subrouter()
	api.HandleFunc("/posts/", makeHandler(db, postsHandler)).Methods("GET")
	api.HandleFunc("/search/", makeHandler(db, searchHandler)).Methods("GET")
	api.HandleFunc("/user/{name}", makeHandler(db, userHandler)).Methods("GET")
	api.HandleFunc("/login/", makeHandler(db, loginHandler)).Methods("POST")

	// private api

	secure := api.PathPrefix("/auth/").Subrouter()
	secure.HandleFunc("/submit/", makeSecureHandler(db, submitPostHandler)).Methods("POST")
	secure.HandleFunc("/{id}", makeSecureHandler(db, deletePostHandler)).Methods("DELETE")

	// public files

	serveStatic(router, "/static/", StaticDir)
	serveStatic(router, "/uploads/", UploadsDir)

	// main page

	main := router.PathPrefix("/").Subrouter()
	main.HandleFunc("/logout/", makeHandler(db, logout)).Methods("GET")
	main.HandleFunc(`/{rest:[a-zA-Z0-9=\-\/]*}`, makeHandler(db, indexPage)).Methods("GET")

	return router

}

func main() {

	// db connection

	session, err := mgo.Dial("localhost")
	if err != nil {
		panic(err)
	}
	defer session.Close()

	db := NewDatabase(session.DB("react-tutorial"))

	router := getRouter(db)

	runtime.GOMAXPROCS((runtime.NumCPU() * 2) + 1)

	n := negroni.Classic()

	store := cookiestore.New([]byte("secret123"))
	n.Use(sessions.Sessions("default", store))

	n.UseHandler(nosurf.New(router))

	n.Run(":6543")
}
