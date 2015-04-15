package pinbook

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"time"
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
