package irc

import (
	"strings"
	"sync"
	"time"
)

type ChanUserMap map[string]*User

// Channel represents an IRC channel
type Channel struct {
	*sync.Mutex `json:"-"`
	Name        string
	Users       ChanUserMap
	Topic       string
	TopicTime   time.Time
	TopicWho    string
}

func NewChannel(name string) *Channel {
	return &Channel{
		Mutex: &sync.Mutex{},
		Name:  name,
		Users: make(ChanUserMap),
	}
}

func (ch *Channel) Nick(nick string) *User {
	ch.Lock()
	defer ch.Unlock()
	return ch.Users[strings.ToLower(nick)]
}
