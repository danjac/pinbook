package pinbook

import (
	"encoding/json"
	//validator "github.com/asaskevich/govalidator"
	"github.com/goincremental/negroni-sessions"
	"github.com/gorilla/mux"
	"gopkg.in/mgo.v2/bson"
	"net/http"
)

type Context struct {
	*App
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

func NewContext(app *App, w http.ResponseWriter, r *http.Request) *Context {
	c := &Context{}
	c.App = app
	c.Request = r
	c.Response = w
	return c
}
