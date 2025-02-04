package irc

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// var IDRE = regexp.MustCompile("([A-Za-z|_][A-Za-z0-9_\-\[\]\^\{\}\|~]+)@")

// User represents a connected IRC client
type User struct {
	Nick     string
	Callsign string
	RealName string
	LastSeen time.Time
	conn     net.Conn
	local    bool

	buf *bufio.Writer
}

func NewUser(nick string, wr io.Writer) *User {
	return &User{
		Nick: nick,
		buf:  bufio.NewWriter(wr),
	}
}

// Write writes to the user, in discrete lines, buffering if we did
// not get a line-feed.
func (u *User) Write(buf []byte) (n int, err error) {
	var n2 int
	for len(buf) > 0 {
		i := bytes.Index(buf, []byte{'\n'})
		if i == -1 {
			n2, err = u.buf.Write(buf)
			n += n2
			return
		}
		n2, err = u.buf.Write(buf[:i+1])
		n += n2
		u.buf.Flush()
		buf = buf[i+1:]
	}

	return len(buf), nil
}

// ID generates a user id in the form of <nick>!<user>@<Real Name>. ID converts spaces to underscores
// for Real Name.
func (u *User) ID() string {
	return fmt.Sprintf("%s!%s@%s", u.Nick, u.Callsign, strings.Join(strings.Fields(u.RealName), "_"))
}

// Parse breaks apart a nick!user@host identifier into its constituent parts and
// populates the User struct with the values. Since The majority of users should be
// remote via TNC/Radio, tracking hostname doesn't make much sense, so we use the
// host field to store the Real Name. When we encode a user identifier via the ID()
// method, spaces are converted to underscores.
func (u *User) Parse(id string) {
	// We could do more rigorous nick/id RFC-compliance checking with, perhaps, a regex.
	// But why bother?

	//fmt.Sscanf(id, "%s!%s@%s", &u.Nick, &u.Callsign, &u.RealName)
	fields := strings.FieldsFunc(id, func(r rune) bool {
		if r == '!' || r == '@' {
			return true
		}
		return false
	})
	// don't populate anything if we can't do this simple bit of parsing
	// chances are we were sent junk or the message was garbled
	if len(fields) != 3 {
		return
	}
	u.Nick = fields[0]
	u.Callsign = fields[1]
	u.RealName = fields[2]
}

// Local returns true if the user is connected via TCP. This is used
// to distinguish users sending messages via radio. Local() for radio
// users will return false.
func (u *User) Local() bool {
	return u.local
}

// Status returns H or G if a user is "Here" or "Gone". This is based on
// LastSeen time. Any messaages within the last hour marks a user as here
// and gone otherwise. Only PRIVMSG and NOTICE update LastSeen time.
func (u *User) Status() string {
	if time.Since(u.LastSeen) < time.Hour {
		return "H"
	}
	return "G"
}

func (u *User) String() string {
	return u.Nick
}

/*
func (u *User) MarshalText() (text []byte, err error) {
	return []byte(u.ID()), nil
}

func (u *User) UnmarshalText(text []byte) (err error) {
	u.Parse(string(text))
	return nil
}
*/
