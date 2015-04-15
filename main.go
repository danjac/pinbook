package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"strconv"
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
	"net/http"
	"os"
	"path"
	"runtime"
	"time"
)

const (
	StaticDir  = "/home/danjac/Projects/react-tutorial/public"
	UploadsDir = "/home/danjac/Projects/react-tutorial/uploads"
	PageSize   = 6
)

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

type Pagination struct {
	Posts   []Post `json:"posts"`
	Total   int    `json:"total"`
	IsFirst bool   `json:"isFirst"`
	IsLast  bool   `json:"isLast"`
	Page    int    `json:"page"`
	Skip    int    `json:"-"`
}

func NewPagination(page int, total int) *Pagination {

	numPages := (total / PageSize)
	skip := (page - 1) * PageSize

	return &Pagination{
		Total:   total,
		IsFirst: page == 1,
		IsLast:  page == numPages,
		Page:    page,
		Skip:    skip,
	}

}

type Context struct {
	DB       *Database
	User     *User
	Request  *http.Request
	Response http.ResponseWriter
}

func (c *Context) GetObjectId(name string) bson.ObjectId {
	return bson.ObjectIdHex(mux.Vars(c.Request)[name])
}

func (c *Context) GetUser() error {

	c.User = nil

	session := sessions.GetSession(c.Request)
	userId := session.Get("userid")

	if userId == nil {
		return nil
	}

	user := &User{}

	if err := c.DB.Users.Find(bson.M{"_id": bson.ObjectIdHex(userId.(string))}).One(&user); err != nil {
		return nil
	}

	c.User = user
	return nil

}

func (c *Context) Query(name string) string {
	return c.Request.URL.Query().Get(name)
}

func (c *Context) Param(name string) string {
	return mux.Vars(c.Request)[name]
}

func (c *Context) ParseJSON(payload interface{}) error {
	return json.NewDecoder(c.Request.Body).Decode(payload)
}

func (c *Context) RenderJSON(payload interface{}, status int) error {
	c.Response.Header().Set("Content-Type", "application/json")
	c.Response.WriteHeader(status)
	return json.NewEncoder(c.Response).Encode(payload)
}

func (c *Context) GetSession() sessions.Session {
	return sessions.GetSession(c.Request)
}

func (c *Context) Error(msg string, status int) {
	http.Error(c.Response, msg, status)
}

func NewContext(db *Database, w http.ResponseWriter, r *http.Request) *Context {
	c := &Context{}
	c.DB = db
	c.Request = r
	c.Response = w
	return c
}

type handlerFunc func(*Context) error

func makeHandler(db *Database, h handlerFunc) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		c := NewContext(db, w, r)
		err := h(c)
		if err != nil {
			// http error handling here...
			panic(err)
		}
	}

}

func makeSecureHandler(db *Database, h handlerFunc) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		c := NewContext(db, w, r)

		err := c.GetUser()
		if err != nil {
			panic(err)
		}
		if c.User == nil {
			http.Error(w, "You must be logged in", http.StatusUnauthorized)
			return
		}
		err = h(c)
		if err != nil {
			panic(err)
		}
	}

}

func logoutHandler(c *Context) error {
	session := c.GetSession()
	session.Clear()
	http.Redirect(c.Response, c.Request, "/", 302)
	return nil
}

func indexPageHandler(c *Context) error {
	t, err := template.ParseFiles("templates/index.html")
	if err != nil {
		return err
	}
	if err := c.GetUser(); err != nil {
		return err
	}
	userJson, err := json.Marshal(c.User)
	if err != nil {
		return err
	}
	// get csrf token, user
	ctx := make(map[string]interface{})
	ctx["csrfToken"] = nosurf.Token(c.Request)
	ctx["user"] = template.JS(userJson)
	return t.Execute(c.Response, ctx)
}

func loginHandler(c *Context) error {

	s := &struct {
		Identity string `json:"identity"`
		Password string `json:"password"`
	}{}

	if err := c.ParseJSON(s); err != nil {
		return err
	}

	user := &User{}

	if err := c.DB.Users.Find(bson.M{"name": s.Identity}).One(&user); err != nil {
		return err
	}
	session := c.GetSession()
	session.Set("userid", user.Id.Hex())

	return c.RenderJSON(user, http.StatusOK)

}

func voteHandler(score int, c *Context) error {

	query := []bson.M{
		bson.M{"_id": c.GetObjectId("id")},
		bson.M{"$not": bson.M{"author": c.User.Id}},
		bson.M{"$not": bson.M{"$in": bson.M{"_id": c.User.Votes}}}}

	post := &Post{}

	if err := c.DB.Posts.Find(query).One(&post); err != nil {
		return err
	}

	if err := c.DB.Posts.UpdateId(post.Id, bson.M{"$inc": bson.M{"score": score}}); err != nil {
		return err
	}

	if err := c.DB.Users.UpdateId(post.AuthorId, bson.M{"$inc": bson.M{"totalScore": score}}); err != nil {
		return err
	}

	if err := c.DB.Users.UpdateId(c.User.Id, bson.M{"votes": append(c.User.Votes, post.Id)}); err != nil {
		return err
	}

	c.Response.WriteHeader(204)
	return nil
}

func downvoteHandler(c *Context) error {
	return voteHandler(-1, c)
}

func upvoteHandler(c *Context) error {
	return voteHandler(-1, c)
}

func deletePostHandler(c *Context) error {

	post := &Post{}
	query := bson.M{"_id": c.GetObjectId("id"), "author": c.User.Id}

	if err := c.DB.Posts.Find(query).One(&post); err != nil {
		http.NotFound(c.Response, c.Request)
		return nil
	}

	if err := os.Remove(path.Join(UploadsDir, post.Image)); err != nil {
		return err
	}

	if err := c.DB.Posts.Remove(query); err != nil {
		return err
	}

	c.Response.WriteHeader(http.StatusNoContent)
	return nil
}

func submitPostHandler(c *Context) error {

	form := &PostForm{}

	if err := c.ParseJSON(form); err != nil {
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
		c.Error("Unable to fetch image", 400)
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
		c.Error("Not a valid image", 400)
		return nil
	}

	if err != nil {
		c.Error("Unable to process", 400)
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
		c.Error("Not a valid image", 400)
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

	if err := c.DB.Users.UpdateId(c.User.Id, bson.M{"$inc": bson.M{"totalScore": 1}}); err != nil {
		return err
	}

	return c.RenderJSON(post, http.StatusOK)
}

func userHandler(c *Context) error {
	var posts []Post

	orderBy := c.Query("orderBy")
	if orderBy != "created" && orderBy != "score" {
		orderBy = "created"
	}

	user := &Author{}

	err := c.DB.Users.Find(bson.M{"name": c.Param("name")}).One(&user)

	q := c.DB.Posts.Find(bson.M{"author": user.Id})

	total, err := q.Count()

	if err != nil {
		return err
	}

	page, err := strconv.Atoi(c.Query("orderBy"))

	if err != nil {
		page = 1
	}

	result := NewPagination(page, total)

	err = q.Sort("-" + orderBy).Skip(result.Skip).Limit(12).All(&posts)

	if err != nil {
		return err
	}

	for _, post := range posts {
		post.Author = user
		result.Posts = append(result.Posts, post)
	}

	return c.RenderJSON(result, http.StatusOK)

}

func searchHandler(c *Context) error {
	var posts []Post

	orderBy := c.Query("orderBy")
	if orderBy != "created" && orderBy != "score" {
		orderBy = "created"
	}

	//page := r.URL.Query["page"]
	search := bson.M{"$regex": bson.RegEx{c.Query("q"), "i"}}

	q := c.DB.Posts.Find(bson.M{"$or": []bson.M{bson.M{"title": search}, bson.M{"url": search}}})

	total, err := q.Count()

	if err != nil {
		return err
	}

	err = q.Sort("-" + orderBy).Limit(12).All(&posts)

	if err != nil {
		return err
	}
	page, err := strconv.Atoi(c.Query("page"))

	if err != nil {
		page = 1
	}

	result := NewPagination(page, total)

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

	return c.RenderJSON(result, http.StatusOK)

}

func postsHandler(c *Context) error {

	var posts []Post

	orderBy := c.Query("orderBy")
	if orderBy != "created" && orderBy != "score" {
		orderBy = "created"
	}

	total, err := c.DB.Posts.Count()
	if err != nil {
		return err
	}
	page, err := strconv.Atoi(c.Query("page"))

	if err != nil {
		page = 1
	}

	result := NewPagination(page, total)

	err = c.DB.Posts.Find(nil).Sort("-" + orderBy).Skip(result.Skip).Limit(12).All(&posts)

	if err != nil {
		return err
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

	return c.RenderJSON(result, http.StatusOK)
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
	secure.HandleFunc("/downvote/{id}", makeSecureHandler(db, downvoteHandler)).Methods("PUT")
	secure.HandleFunc("/upvote/{id}", makeSecureHandler(db, upvoteHandler)).Methods("PUT")
	secure.HandleFunc("/{id}", makeSecureHandler(db, deletePostHandler)).Methods("DELETE")

	// public files

	serveStatic(router, "/static/", StaticDir)
	serveStatic(router, "/uploads/", UploadsDir)

	// main page

	main := router.PathPrefix("/").Subrouter()
	main.HandleFunc("/logout/", makeHandler(db, logoutHandler)).Methods("GET")
	main.HandleFunc(`/{rest:[a-zA-Z0-9=\-\/]*}`, makeHandler(db, indexPageHandler)).Methods("GET")

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
