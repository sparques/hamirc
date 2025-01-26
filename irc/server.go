package irc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"hamirc/kiss"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// User represents a connected IRC client
type User struct {
	Nick     string
	Callsign string
	RealName string
	LastSeen time.Time
	Conn     net.Conn
	local    bool

	buf *bufio.Writer
}

func NewUser(nick string, wr io.Writer) *User {
	return &User{
		Nick: nick,
		buf:  bufio.NewWriter(wr),
	}
}

func (u *User) ID() string {
	return fmt.Sprintf("%s!%s@%s", u.Nick, u.Callsign, strings.Join(strings.Fields(u.RealName), "_"))
}

func (u *User) Parse(id string) {
	fmt.Sscanf(id, "%s!%s@%s ", &u.Nick, &u.Callsign, &u.RealName)
}

// Local returns true if the user is connected via TCP. This is used
// to distinguish users sending messages via radio. Local() for radio
// users will return false.
func (u *User) Local() bool {
	return u.local
}

func (u *User) Status() string {
	if time.Since(u.LastSeen) < time.Hour {
		return "H"
	}
	return "G"
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

// Channel represents an IRC channel
type Channel struct {
	*sync.Mutex
	Name      string
	Users     map[*User]*User
	Topic     string
	TopicTime time.Time
	TopicWho  string
}

func NewChannel(name string) *Channel {
	return &Channel{
		Mutex: &sync.Mutex{},
		Name:  name,
		Users: make(map[*User]*User),
	}
}

func (c *Channel) UserByNick(nick string) *User {
	nick = strings.ToLower(nick)
	for u := range c.Users {
		if strings.ToLower(u.Nick) == nick {
			return u
		}
	}
	return nil
}

// Server represents the IRC server
type Server struct {
	*sync.Mutex
	Name     string
	Users    map[*User]*User
	Channels map[string]*Channel
	tnc      *kiss.TNC
	MOTD     func() string
	// AutoJoin causes Local() users to automatically join channels they
	// get messages for.
	AutoJoin bool
}

func NewServer() *Server {
	return &Server{
		Mutex:    &sync.Mutex{},
		Name:     "server",
		Users:    make(map[*User]*User),
		Channels: make(map[string]*Channel),
	}
}

func (s *Server) Serve(listenAddr string) error {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	defer listener.Close()

	go s.handleTNC()

	go s.PingPong()

	for {
		conn, err := listener.Accept()
		log.Printf("New connection on %s\n", conn.RemoteAddr())
		if err != nil {
			fmt.Printf("Error accepting connection: %v\n", err)
			continue
		}
		go s.handleConnection(conn)
	}
}

// Save saves the radiouser list and channels to path.
// This can be restored with Load(path).
func (s *Server) Save(path string) error {
	s.Lock()
	defer s.Unlock()
	fh, err := os.Create(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	// enc := gob.NewEncoder(fh)
	enc := json.NewEncoder(fh)
	err = enc.Encode(s)
	if err != nil {
		return err
	}

	return nil
}

func (s *Server) Load(path string) error {
	s.Lock()
	defer s.Unlock()
	fh, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer fh.Close()
	dec := json.NewDecoder(fh)
	err = dec.Decode(&s)
	if err != nil {
		return err
	}
	return nil
}

func parse(line string) []string {
	args := strings.Split(line, " ")
	for i := range args {
		if strings.HasPrefix(args[i], ":") {
			if i == 0 {
				args[i], _ = strings.CutPrefix(args[i], ":")
				continue
			}
			args = strings.SplitN(line, " ", i+1)
			args[len(args)-1], _ = strings.CutPrefix(args[len(args)-1], ":")
			break
		}
	}
	return args
}

// ConnectTNC connects to a TNC (let's be honest here, it's direwolf) via
// tcp.
func (s *Server) ConnectTNC(addr string) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("could not connect to kiss tnc: %w", err)
	}
	s.tnc = kiss.NewTNC(conn)
	return nil
}

// OpenTNC opens a file (likely a pty) for a TNC. This can be used for a
// real hardware serial port TNC, or direwolf's pty interface to its
// kiss TNC.
func (s *Server) OpenTNC(path string) error {
	fh, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("could not open %s kiss tnc: %w", path, err)
	}
	s.tnc = kiss.NewTNC(fh)
	return nil
}

func (s *Server) handleTNC() {
	// Just use port zero
	port := s.tnc.Port(0)
	// send our out going
	// read incoming messages
	buf := make([]byte, 1024*512)
	for {
		buf = buf[0:1024]
		n, err := port.Read(buf)
		if err != nil {
			return
		}
		// TODO: error handling.
		buf = buf[:n]

		// replace \n just in case...
		args := parse(strings.ReplaceAll(string(buf), "\n", " "))
		args[0], _ = strings.CutPrefix(args[0], ":")

		// only let PRIVMSG and NOTICE through
		if args[1] != "PRIVMSG" && args[1] != "NOTICE" {
			continue
		}

		// track seen users
		incomingUser := NewUser("", io.Discard)
		incomingUser.Parse(args[0])

		if incomingUser.Nick == "" {
			continue
		}
		// add user to server if not previously seen
		if nil == s.UserByNick(incomingUser.Nick) {
			s.Users[incomingUser] = incomingUser
		} else {
			incomingUser = s.UserByNick(incomingUser.Nick)
		}

		// do ban check here?

		// if target is channel
		if strings.HasPrefix(args[2], "#") {
			// create channel if it doesn't exist
			if _, ok := s.Channels[args[2]]; !ok {
				s.Channels[args[2]] = NewChannel(args[2])
			}

			// add user to channel if not already there
			if nil == s.Channels[args[2]].UserByNick(incomingUser.Nick) {
				s.joinChannel(incomingUser, args[2])
			}

			if s.AutoJoin {
				for u := range s.Users {
					if u.Local() {
						s.joinChannel(u, args[2])
					}
				}
			}
		}

		if args[1] == "PRIVMSG" {
			s.Privmsg(incomingUser, args[2], args[3])
		} else {
			s.Notice(incomingUser, args[2], args[3])
		}
	}

}

// handleConnection handles an incoming connection
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)

	// Assign a temporary nickname
	var hijack = struct {
		io.Writer
		io.Reader
	}{
		Writer: io.MultiWriter(conn, os.Stdout),
		Reader: conn,
	}

	user := NewUser("", hijack)
	user.local = true
	user.Conn = conn

	// Handle commands
	for scanner.Scan() {
		if scanner.Err() != nil {
			fmt.Printf("User %s disconnected.\n", user.ID())
			s.removeUser(user)
			return
		}
		if s.handleCommand(user, strings.TrimSpace(scanner.Text())) {
			s.removeUser(user)
			return
		}
	}
}

func (s *Server) removeUser(user *User) {
	s.Lock()
	defer s.Unlock()

	delete(s.Users, user)
	for _, ch := range s.Channels {
		// need to add quit message
		delete(ch.Users, user)
	}
}

func (s *Server) acceptUser(user *User) {
	s.reply(user, RPL_WELCOME, user.Nick, "Connected.")
	s.reply(user, RPL_YOURHOST, user.Nick, "Your host is an abomination.")
	s.reply(user, RPL_CREATED, user.Nick, "Server was created within the last century.")

	log.Printf("Accepted user %s.\n", user.ID())
	s.Lock()
	s.Users[user] = user
	s.Unlock()

	s.motd(user)
}

func (s *Server) motd(user *User) {
	if s.MOTD == nil {
		return
	}
	s.reply(user, RPL_MOTDSTART, user.Nick, "- Message of the Day")
	for _, line := range strings.Split(s.MOTD(), "\n") {
		s.reply(user, RPL_MOTD, user.Nick, line)
	}
	s.reply(user, RPL_ENDOFMOTD, user.Nick, "End of /MOTD command.")
}

func (s *Server) PingPong() {
	for {
		time.Sleep(time.Second * 30)
		s.Lock()
		for _, user := range s.Users {
			fmt.Fprintf(user, "PING :LAG%d\n", time.Now().Unix())
		}
		s.Unlock()
	}
}

// handleCommand processes IRC commands
func (s *Server) handleCommand(user *User, line string) (cont bool) {
	cont = false
	args := parse(line)
	command := strings.ToUpper(args[0])

	//log.Printf("<%s@%s> %s\n", user.Nick, user.Conn.RemoteAddr(), line)

	log.Printf(">>%s\n", args)
	if user.Nick == "" && command != "NICK" && command != "USER" {
		s.reply(user, ERR_NOTREGISTERED, "*", command, "You have not registered")
		return
	}
	switch command {
	case "CAP":
		s.reply(user, "CAP", "LS")
	case "NICK":
		s.changeNick(user, args[1])
		//s.setNick
	case "USER":
		// after we get a USER, send the welcome wagon
		user.Callsign = args[1]
		user.RealName = args[4]
		s.acceptUser(user)
	case "JOIN":
		if len(args) == 1 {
			fmt.Fprintf(user, ":%s 461 %s JOIN :Not enough parameters\n", s.Name, user.Nick)
			return
		}
		for _, ch := range strings.Split(args[1], ",") {
			s.joinChannel(user, ch)
		}
	case "WHO":
		mask := "*"
		if len(args) == 2 {
			mask = args[1]
		}
		// list users according to mask
		s.listUsers(user, mask)
	case "NOTICE", "PRIVMSG":
		s.send(user, command, args[1], args[2])
	case "PING":
		if len(args) > 1 {
			s.reply(user, "PONG", s.Name, args[1])
		} else {
			s.reply(user, "PONG")
		}
	case "PONG":
		// does it matter?
	case "USERHOST":
		if len(args) > 1 {
			s.userHost(user, args[1:])
		}
	case "LIST":
		s.listChannels(user)
	case "TOPIC":
		if len(args) < 2 {
			s.reply(user, ERR_NEEDMOREPARAMS, user.Nick, "TOPIC requires 2 or more params")
			return
		}
		ch, ok := s.Channels[args[1]]
		if !ok {
			s.reply(user, ERR_NOSUCHCHANNEL, user.Nick, args[1], "no such channel")
			return
		}

		if len(args) == 2 {
			s.topic(user, args[1])
			return
		}

		ch.Topic = strings.Join(args[2:], " ")
		ch.TopicWho = user.Nick
		ch.TopicTime = time.Now()

		for u := range ch.Users {
			s.reply(u, RPL_TOPIC, u.Nick, ch.Name, ch.Topic)
		}

	case "MODE":
		// TODO: support ban / +b
		// we don't want to support modes other than +b
		s.reply(user, ERR_UNKNOWNMODE, args[1], "Server doesn't support modes")
	case "MOTD":
		s.motd(user)
	case "PART":
		if len(args) == 1 {
			s.reply(user, ERR_NEEDMOREPARAMS, user.Nick, "PART requires 1 or more params")
			return
		}

		reason := "leaving channel"
		if len(args) == 3 {
			reason = args[2]
		}
		for _, chName := range strings.Split(args[1], ",") {
			ch, ok := s.Channels[chName]
			if !ok {
				s.reply(user, ERR_NOSUCHCHANNEL, user.Nick, chName, "no such channel")
				continue

			}

			if ch.Users[user] == nil {
				s.reply(user, ERR_NOTONCHANNEL, user.Nick, chName, "you're not in that channel")
				continue
			}

			s.send(user, "PART", chName, reason)
			delete(ch.Users, user)
		}

	case "QUIT":
		if len(args) == 1 {
			s.quit(user, "Client disconnected.")
		} else {
			s.quit(user, strings.Join(args[1:], " "))
		}
		return true
	case "SAVE":
		err := s.Save("serverState.gob")
		log.Printf("Saved server state: %s", err)
	default:
		// fmt.Fprintf(user, ":%s 421 %s %s :Unknown command\r\n", s.Name, user.Nick, command)
		s.reply(user, "421", user.Nick, command, "Unknown command")
	}

	return
}

func (s *Server) reply(user *User, args ...string) {
	if len(args) == 0 {
		return
	}
	if len(args) > 1 {
		args[len(args)-1] = ":" + args[len(args)-1]
	}
	fmt.Fprintf(user, ":%s %s\r\n", s.Name, strings.Join(args, " "))
}

// changeNick changes a user's nickname
func (s *Server) changeNick(user *User, newNick string) {
	s.Lock()
	defer s.Unlock()

	// Check if the new nickname is already in use
	if nil != s.UserByNick(newNick) {
		s.reply(user, "433", user.Nick, newNick, "Nickname is already in use")
		return
	}

	// Update the server's user list
	oldNick := user.Nick
	user.Nick = newNick

	if oldNick == "" {
		oldNick = newNick
	}

	fmt.Fprintf(user, ":%s NICK :%s\r\n", oldNick, newNick)
}

func (s *Server) listUsers(user *User, mask string) {
	// who response:Is there
	// 352 <channel> <user> <host> <server> <nick> <status> :<hopcount> <realname>

	switch {
	case strings.HasPrefix(mask, "#"):
		if ch, ok := s.Channels[mask]; ok {
			for _, u := range ch.Users {
				fmt.Fprintf(user, ":%s 352 %s %s %s * * %s %s :1 %s\n", s.Name, user.Nick, ch.Name, u.Callsign, u.Nick, u.Status(), u.RealName)
			}
		}
	case mask == "*":
		log.Printf("All users...")
		for _, u := range s.Users {
			fmt.Fprintf(user, ":%s 352 %s * %s * * %s %s :1 %s\n", s.Name, user.Nick, u.Callsign, u.Nick, u.Status(), u.RealName)
		}
	default:
		// treat as user
		for _, u := range s.Users {
			if u.ID() == mask || u.Nick == mask {
				// server caller channel user host server nick status :hopcount realname
				fmt.Fprintf(user, ":%s 352 %s * %s * * %s %s :1 %s\n", s.Name, user.Nick, u.Callsign, u.Nick, u.Status(), u.RealName)
				return
			}
		}
	}
}

func (s *Server) UserByNick(nick string) *User {
	nick = strings.ToLower(nick)
	for u := range s.Users {
		if strings.ToLower(u.Nick) == nick {
			return u
		}
	}
	return nil
}

func (s *Server) send(sender *User, cmd, target, msg string) {
	// update LastSeen
	sender.LastSeen = time.Now()

	// Transmit message via radio
	if sender.Local() {
		fmt.Fprintf(s.tnc.Port(0), ":%s %s %s :%s", sender.ID(), cmd, target, msg)
	}

	if strings.HasPrefix(target, "#") {
		ch, ok := s.Channels[target]
		if !ok {
			return
		}
		for u := range ch.Users {
			if u == sender && cmd != "PART" {
				continue
			}
			fmt.Fprintf(u, ":%s %s %s :%s\n", sender.ID(), cmd, target, msg)
		}
		return
	}

	targetUser := s.UserByNick(target)
	if targetUser == nil {
		return
	}
	fmt.Fprintf(targetUser, ":%s %s %s :%s\n", sender.ID(), cmd, target, msg)
}

func (s *Server) Notice(sender *User, target string, msg string) {
	s.send(sender, "NOTICE", target, msg)
}

func (s *Server) Privmsg(sender *User, target string, msg string) {
	s.send(sender, "PRIVMSG", target, msg)
}

// joinChannel adds a user to a channel
func (s *Server) joinChannel(user *User, channelName string) {
	s.Lock()
	channel, exists := s.Channels[channelName]
	if !exists {
		channel = NewChannel(channelName)
		s.Channels[channelName] = channel
	}
	channel.Lock()
	channel.Users[user] = user
	channel.Unlock()

	for u := range channel.Users {
		fmt.Fprintf(u, ":%s JOIN :%s\r\n", user.ID(), channelName)
	}
	s.Unlock()

	if channel.Topic == "" {
		s.reply(user, RPL_NOTOPIC, s.Name, channelName, "No topic is set")
	} else {
		s.reply(user, RPL_TOPIC, s.Name, channelName, channel.Topic)
	}

	fmt.Fprintf(user, ":%s 353 %s = %s :", s.Name, user.Nick, channelName)

	for u := range channel.Users {
		fmt.Fprintf(user, "%s ", u.Nick)
	}
	fmt.Fprintf(user, "\r\n")
	s.reply(user, RPL_ENDOFNAMES, user.Nick, channelName)
}

func (s *Server) userHost(user *User, nicks []string) {
	//:irc.example.com 302 Sparques :Nick1=-user1@host1 Nick2=+user2@host2
	fmt.Fprintf(user, ":%s 302 %s :", s.Name, user.Nick)

	for _, nick := range nicks {
		u := s.UserByNick(nick)
		if u == nil {
			continue
		}
		fmt.Fprintf(user, "%s=-%s@%s ", nick, u.Callsign, strings.ReplaceAll(u.RealName, " ", "_"))
	}
	fmt.Fprintf(user, "\r\n")
}

func (s *Server) quit(user *User, reason string) {
	for _, ch := range s.Channels {
		if _, ok := ch.Users[user]; ok {
			s.send(user, "QUIT", ch.Name, reason)
		}
	}
}

func (s *Server) topic(user *User, channel string) {
	// TODO: Figure out a way to share topics
	// When topic is set, might have to broadcast out something like
	// :<user.ID()> TOPIC <channel> <topic>
	ch, ok := s.Channels[channel]
	if !ok {
		s.reply(user, ERR_NOSUCHCHANNEL, user.Nick, channel, "no such channel")
		return
	}

	if ch.Topic == "" {
		s.reply(user, RPL_NOTOPIC, s.Name, ch.Name, "No topic is set")
	} else {
		s.reply(user, RPL_TOPIC, user.Nick, ch.Name, ch.Topic)
		s.reply(user, RPL_TOPICWHOTIME, user.Nick, ch.Name, ch.TopicWho, strconv.Itoa(int(ch.TopicTime.Unix())))
	}
}

func (s *Server) listChannels(user *User) {
	// we don't support filters or anything because why bother
	s.reply(user, RPL_LISTSTART, "Channel", "Users Name")
	for _, ch := range s.Channels {
		s.reply(user, RPL_LIST, user.Nick, ch.Name, strconv.Itoa(len(ch.Users)), ch.Topic)
	}
	s.reply(user, RPL_LISTEND, "End of /LIST")
}
