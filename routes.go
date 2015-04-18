package pinbook

import (
	"encoding/json"
	"fmt"
	"github.com/julienschmidt/httprouter"
	"github.com/justinas/nosurf"
	"github.com/nfnt/resize"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/mgo.v2/bson"
	"html/template"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path"
	"strconv"
	"time"
)

const PageSize = 6

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

type handlerFunc func(*Context) error

func makeHandler(app *App, h handlerFunc, authRequired bool) httprouter.Handle {

	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {

		c := NewContext(app, w, r, ps)

		if authRequired {
			if err := c.GetUser(); err != nil {
				c.HandleError(err)
				return
			}
			if c.User == nil {
				c.Status(http.StatusUnauthorized)
				return
			}
		}
		err := h(c)
		if err != nil {
			// http error handling here...
			switch e := err.(type) {
			/*
				case mgo.ErrNotFound:
					http.NotFound(c.Response, c.Request)
					return
			*/
			case *ErrorMap:
				c.JSON(e.Errors, http.StatusBadRequest)
				return
			default:
				c.HandleError(e)
			}
		}
	}

}

func logoutHandler(c *Context) error {
	c.Logout()
	c.Redirect("/")
	return nil
}

func indexPageHandler(c *Context) error {
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
	return c.Render("templates/index.html", ctx, http.StatusOK)
}

func loginHandler(c *Context) error {

	s := &struct {
		Identity string `json:"identity"`
		Password string `json:"password"`
	}{}

	if err := c.DecodeJSON(s); err != nil {
		return err
	}

	user := &User{}

	if err := c.DB.Users.Find(bson.M{"name": s.Identity}).One(&user); err != nil {
		return err
	}
	c.Login(user)
	return c.JSON(user, http.StatusOK)

}

func voteHandler(score int, c *Context) error {

	query := bson.M{"$and": []bson.M{
		bson.M{"_id": c.GetObjectId("id")},
		bson.M{"author": bson.M{"$ne": c.User.Id}},
		bson.M{"_id": bson.M{"$nin": c.User.Votes}}}}

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

	if err := c.DB.Users.UpdateId(c.User.Id, bson.M{"$set": bson.M{"votes": append(c.User.Votes, post.Id)}}); err != nil {
		return err
	}

	c.Status(http.StatusNoContent)
	return nil
}

func downvoteHandler(c *Context) error {
	return voteHandler(-1, c)
}

func upvoteHandler(c *Context) error {
	return voteHandler(1, c)
}

func deletePostHandler(c *Context) error {

	post := &Post{}
	query := bson.M{"_id": c.GetObjectId("id"), "author": c.User.Id}

	if err := c.DB.Posts.Find(query).One(&post); err != nil {
		http.NotFound(c.Response, c.Request)
		return nil
	}

	if err := os.Remove(path.Join(c.Cfg.UploadsDir, post.Image)); err != nil {
		return err
	}

	if err := c.DB.Posts.Remove(query); err != nil {
		return err
	}

	c.Status(http.StatusNoContent)
	return nil
}

func submitPostHandler(c *Context) error {

	form := &PostForm{}

	if err := c.Validate(form); err != nil {
		return err
	}

	// fetch image

	resp, err := http.Get(form.Image)
	if err != nil {
		c.String("Unable to find image", http.StatusBadRequest)
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
		c.String("Not a valid image", http.StatusBadRequest)
		return nil
	}

	if err != nil {
		c.String("Unable to process image", http.StatusBadRequest)
		return nil
	}

	t := resize.Thumbnail(300, 500, img, resize.NearestNeighbor)

	postId := bson.NewObjectId()

	filename := fmt.Sprintf("%s%s", postId.Hex(), ext)
	fullPath := path.Join(c.Cfg.UploadsDir, filename)

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
		c.String("Not a valid image", http.StatusBadRequest)
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

	return c.JSON(post, http.StatusOK)
}

func userHandler(c *Context) error {
	var posts []Post

	orderBy := c.Query("orderBy")
	if orderBy != "created" && orderBy != "score" {
		orderBy = "created"
	}

	user := &Author{}

	err := c.DB.Users.Find(bson.M{"name": c.Params.ByName("name")}).One(&user)

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

	return c.JSON(result, http.StatusOK)

}

func userExists(field string, c *Context) error {
	value := c.Query(field)
	if value == "" {
		c.String("No value", http.StatusBadRequest)
		return nil
	}
	count, err := c.DB.Users.Find(bson.M{field: value}).Count()
	if err != nil {
		return err
	}

	payload := make(map[string]bool)
	payload["exists"] = count > 0
	return c.JSON(payload, http.StatusOK)
}

func emailExists(c *Context) error {
	return userExists("email", c)
}

func nameExists(c *Context) error {
	return userExists("name", c)
}

func signupHandler(c *Context) error {

	form := &SignupForm{}
	if err := c.Validate(form); err != nil {
		return err
	}

	password, err := bcrypt.GenerateFromPassword([]byte(form.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user := &User{
		Id:       bson.NewObjectId(),
		Created:  time.Now(),
		Name:     form.Name,
		Email:    form.Email,
		Password: string(password),
	}

	if err := c.DB.Users.Insert(user); err != nil {
		return err
	}

	session := c.GetSession()
	session.Set("userid", user.Id.Hex())

	return c.JSON(user, http.StatusOK)

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

	return c.JSON(result, http.StatusOK)

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

	return c.JSON(result, http.StatusOK)
}

func getRouter(app *App) http.Handler {

	router := httprouter.New()

	// public api

	router.GET("/api/posts/", makeHandler(app, postsHandler, false))
	router.GET("/api/search/", makeHandler(app, searchHandler, false))
	router.GET("/api/user/:name", makeHandler(app, userHandler, false))
	router.POST("/api/login/", makeHandler(app, loginHandler, false))

	// private api

	router.POST("/api/auth/submit/", makeHandler(app, submitPostHandler, true))
	router.PUT("/api/auth/downvote/:id", makeHandler(app, downvoteHandler, true))
	router.PUT("/api/auth//upvote/:id", makeHandler(app, upvoteHandler, true))
	router.DELETE("/api/auth/:id", makeHandler(app, deletePostHandler, true))

	// static files

	router.ServeFiles("/static/*filepath", http.Dir(app.Cfg.StaticDir))
	router.ServeFiles("/uploads/*filepath", http.Dir(app.Cfg.UploadsDir))

	// main pages

	router.GET("/logout/", makeHandler(app, logoutHandler, false))

	indexPage := makeHandler(app, indexPageHandler, false)

	mainRoutes := []string{
		"/",
		"/login",
		"/search",
		"/latest",
		"/user/:name",
		"/submit",
	}

	for _, route := range mainRoutes {
		router.GET(route, indexPage)
	}

	return router

}
