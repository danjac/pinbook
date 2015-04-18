package pinbook

import (
	"encoding/json"
	"github.com/goincremental/negroni-sessions"
	"github.com/julienschmidt/httprouter"
	"gopkg.in/mgo.v2/bson"
	"html/template"
	"log"
	"net/http"
)

type TemplateContext map[string]interface{}

type Context struct {
	*App
	User     *User
	Params   httprouter.Params
	Request  *http.Request
	Response http.ResponseWriter
}

func (c *Context) GetObjectId(name string) bson.ObjectId {
	return bson.ObjectIdHex(c.Params.ByName(name))
}

type ErrorMap struct {
	Errors map[string]string `json:"errors"`
}

func (m *ErrorMap) Add(field string, msg string) {
	m.Errors[field] = msg
}

func (m *ErrorMap) IsEmpty() bool {
	return len(m.Errors) == 0
}

func (m ErrorMap) Error() string {
	return "This form contains errors"
}

type Validator interface {
	Validate(*Context, *ErrorMap) error
}

func (c *Context) Validate(v Validator) error {

	if err := c.DecodeJSON(v); err != nil {
		return err
	}

	errors := &ErrorMap{make(map[string]string)}
	if err := v.Validate(c, errors); err != nil {
		return err
	}
	if errors.IsEmpty() {
		return nil
	}
	return errors
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

func (c *Context) Login(user *User) {
	session := c.GetSession()
	session.Set("userid", user.Id.Hex())
}

func (c *Context) Logout() {
	session := c.GetSession()
	session.Clear()
}

func (c *Context) Redirect(url string) {
	http.Redirect(c.Response, c.Request, url, http.StatusSeeOther)
}

func (c *Context) Query(name string) string {
	return c.Request.URL.Query().Get(name)
}

func (c *Context) DecodeJSON(payload interface{}) error {
	return json.NewDecoder(c.Request.Body).Decode(payload)
}

func (c *Context) JSON(payload interface{}, status int) error {
	c.Response.Header().Set("Content-Type", "application/json")
	c.Response.WriteHeader(status)
	return json.NewEncoder(c.Response).Encode(payload)
}

func (c *Context) String(msg string, status int) {
	c.Response.Header().Set("Content-Type", "text/plain")
	c.Response.WriteHeader(status)
	c.Response.Write([]byte(msg))
}

func (c *Context) Render(name string, ctx TemplateContext, status int) error {
	t, err := template.ParseFiles(name)
	if err != nil {
		return err
	}
	return t.Execute(c.Response, ctx)
}

func (c *Context) Status(status int) {
	c.String(http.StatusText(status), status)
}

func (c *Context) NotFound() {
	http.NotFound(c.Response, c.Request)
}

func (c *Context) HandleError(err error) {
	log.Print(err)
	c.Status(http.StatusInternalServerError)
}

func (c *Context) GetSession() sessions.Session {
	return sessions.GetSession(c.Request)
}

func (c *Context) Error(msg string, status int) {
	http.Error(c.Response, msg, status)
}

func NewContext(app *App, w http.ResponseWriter, r *http.Request, ps httprouter.Params) *Context {
	c := &Context{}
	c.App = app
	c.Request = r
	c.Response = w
	c.Params = ps
	return c
}
