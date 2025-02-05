package irc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sparques/hamirc/kiss"
)

type UserMap map[string]*User

// Server represents the IRC server
type Server struct {
	*sync.Mutex `json:"-"`
	Name        string
	Users       UserMap
	Channels    map[string]*Channel
	MOTD        func() string `json:"-"`
	// AutoJoin causes Local() users to automatically join channels they
	// get messages for.
	AutoJoin bool
	exitch   chan error
	tnc      *kiss.TNC
	tncport  int
}

func NewServer() *Server {
	return &Server{
		Mutex:    &sync.Mutex{},
		Name:     "server",
		Users:    make(UserMap),
		Channels: make(map[string]*Channel),
		exitch:   make(chan error),
	}
}

func (s *Server) Nick(nick string) *User {
	s.Lock()
	defer s.Unlock()
	return s.Users[strings.ToLower(nick)]
}

func (s *Server) Serve(listenAddr string) error {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	defer listener.Close()

	go s.handleTNC()

	go s.PingPong()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("Error accepting connection: %v\n", err)
				continue
			}
			log.Printf("New connection on %s\n", conn.RemoteAddr())
			go s.handleConnection(conn)
		}
	}()

	return <-s.exitch
}

func (s *Server) Exit(err error) {
	s.exitch <- err
}
func (s *Server) Channel(name string) *Channel {
	s.Lock()
	defer s.Unlock()
	ch, ok := s.Channels[name]
	if ok {
		return ch
	}
	s.Channels[name] = NewChannel(name)
	return s.Channels[name]
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
func (s *Server) ConnectTNC(addr string, tncport int) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("could not connect to kiss tnc: %w", err)
	}
	s.tnc = kiss.NewTNC(conn)
	s.tncport = tncport
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
	defer s.Exit(errors.New("lost connection to TNC"))
	// Just use port zero
	port := s.tnc.Port(uint8(s.tncport))
	// send our out going
	// read incoming messages
	buf := make([]byte, 1024*512)
	for {
		buf = buf[0:1024]
		n, err := port.Read(buf)
		if err != nil {
			log.Printf("error reading from TNC port: %s", err)
			return
		}
		// TODO: error handling.
		buf = buf[:n]

		// replace \n just in case...
		args := parse(strings.ReplaceAll(string(buf), "\n", " "))
		args[0], _ = strings.CutPrefix(args[0], ":")

		// only let PRIVMSG, NOTICE, and topic through
		if !slices.Contains([]string{"PRIVMSG", "NOTICE", "TOPIC"}, args[1]) {
			continue
		}

		// track seen users
		incomingUser := NewUser("", io.Discard)
		incomingUser.Parse(args[0])

		if incomingUser.Nick == "" {
			continue
		}
		// add user to server if not previously seen
		if existingUser := s.Nick(incomingUser.Nick); existingUser == nil {
			s.Users[strings.ToLower(incomingUser.Nick)] = incomingUser
		} else {
			incomingUser = existingUser
		}

		// do user-level ban check here?

		// if target is channel
		if strings.HasPrefix(args[2], "#") {
			// create channel if it doesn't exist
			ch := s.Channel(args[2])

			// add user to channel if not already there
			if nil == ch.Nick(incomingUser.Nick) {
				s.joinChannel(incomingUser, ch.Name)
			}

			if s.AutoJoin {
				for _, u := range s.Users {
					_, ok := ch.Users[u.Nick]
					if u.Local() && !ok {
						s.joinChannel(u, args[2])
					}
				}
			}
		}

		if args[1] == "TOPIC" {
			s.setTopic(incomingUser, s.Channel(args[2]), strings.Join(args[3:], " "))
		} else {
			s.send(incomingUser, args[1], args[2], args[3])
		}
	}

}

// handleConnection handles an incoming connection
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)

	var hijack = struct {
		io.Writer
		io.Reader
	}{
		// Writer: io.MultiWriter(conn, os.Stdout),
		Writer: io.MultiWriter(conn),
		Reader: conn,
	}

	user := NewUser("", hijack)
	user.local = true
	user.conn = conn

	// Handle commands
	for scanner.Scan() {
		if scanner.Err() != nil {
			log.Printf("<%s@%s> Disconnected\n", user.ID(), conn.RemoteAddr())
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

	delete(s.Users, strings.ToLower(user.Nick))
	for _, ch := range s.Channels {
		// need to add quit message
		delete(ch.Users, user.Nick)
	}
}

func (s *Server) acceptUser(user *User) {
	if user.Nick == "" {
		return
	}
	s.reply(user, RPL_WELCOME, user.Nick, "Connected.")
	s.reply(user, RPL_YOURHOST, user.Nick, "Your host is an abomination.")
	s.reply(user, RPL_CREATED, user.Nick, "Server was created within the last century.")

	log.Printf("Accepted user %s.\n", user.ID())
	s.Lock()
	s.Users[strings.ToLower(user.Nick)] = user
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
		time.Sleep(2 * time.Minute)
		s.Lock()
		for _, user := range s.Users {
			if user.Local() {
				fmt.Fprintf(user, "PING :LAG%d\n", time.Now().Unix())
			}
		}
		s.Unlock()
	}
}

// handleCommand processes IRC commands
func (s *Server) handleCommand(user *User, line string) (quit bool) {
	args := parse(line)
	command := strings.ToUpper(args[0])

	//log.Printf("<%s@%s> %s\n", user.Nick, user.Conn.RemoteAddr(), line)

	log.Printf("<%s@%s> %s\n", user.Nick, user.conn.RemoteAddr(), args)
	if user.Nick == "" {
		switch command {
		case "NICK", "USER", "CAP":
		default:
			s.reply(user, ERR_NOTREGISTERED, "*", command, "You have not registered")
			return
		}
	}

	if cmdFunc, ok := cmdSet[command]; ok {
		return cmdFunc(s, user, args)
	} else {
		s.reply(user, ERR_UNKNOWNCOMMAND, user.Nick, command, "Unknown command")
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

	newNickLower := strings.ToLower(newNick)
	oldNickLower := strings.ToLower(user.Nick)

	// Check if the new nickname is already in use
	// allow person to snag a remote user though
	existingUser, ok := s.Users[newNickLower]
	if ok && existingUser.Local() {
		s.reply(user, ERR_NICKNAMEINUSE, user.Nick, newNick, "Nickname is already in use")
		return
	}

	// Update the server's user list
	oldNick := user.Nick
	user.Nick = newNick

	for _, ch := range s.Channels {
		if _, ok := ch.Users[oldNickLower]; ok {
			ch.Users[newNickLower] = user
			delete(ch.Users, oldNickLower)
		}
	}

	if oldNick == "" {
		oldNick = newNick
	}

	fmt.Fprintf(user, ":%s NICK :%s\r\n", oldNick, newNick)
}

func (s *Server) listUsers(user *User, mask string) {
	s.Lock()
	defer s.Unlock()

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

func (s *Server) send(sender *User, cmd, target, msg string) {
	s.Lock()
	defer s.Unlock()

	// update LastSeen
	sender.LastSeen = time.Now()

	// Transmit message via radio
	if sender.Local() {
		fmt.Fprintf(s.tnc.Port(uint8(s.tncport)), ":%s %s %s :%s", sender.ID(), cmd, target, msg)
	}

	if strings.HasPrefix(target, "#") {
		ch, ok := s.Channels[target]
		if !ok {
			return
		}
		for _, u := range ch.Users {
			if u.Nick == sender.Nick && cmd != "PART" {
				continue
			}
			fmt.Fprintf(u, ":%s %s %s :%s\n", sender.ID(), cmd, target, msg)
		}
		return
	}

	targetUser, ok := s.Users[target]
	if !ok {
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
	channel.Users[strings.ToLower(user.Nick)] = user
	channel.Unlock()

	for _, u := range channel.Users {
		fmt.Fprintf(u, ":%s JOIN :%s\r\n", user.ID(), channelName)
	}
	s.Unlock()

	if channel.Topic == "" {
		s.reply(user, RPL_NOTOPIC, s.Name, channelName, "No topic is set")
	} else {
		s.reply(user, RPL_TOPIC, s.Name, channelName, channel.Topic)
	}

	fmt.Fprintf(user, ":%s 353 %s = %s :", s.Name, user.Nick, channelName)

	for _, u := range channel.Users {
		fmt.Fprintf(user, "%s ", u.Nick)
	}
	fmt.Fprintf(user, "\r\n")
	s.reply(user, RPL_ENDOFNAMES, user.Nick, channelName, "End of /NAMES list")
}

func (s *Server) userHost(user *User, nicks []string) {
	s.Lock()
	defer s.Unlock()

	//:irc.example.com 302 Sparques :Nick1=-user1@host1 Nick2=+user2@host2
	fmt.Fprintf(user, ":%s 302 %s :", s.Name, user.Nick)

	for _, nick := range nicks {
		u, ok := s.Users[nick]
		if !ok {
			continue
		}
		fmt.Fprintf(user, "%s=-%s@%s ", nick, u.Callsign, strings.ReplaceAll(u.RealName, " ", "_"))
	}
	fmt.Fprintf(user, "\r\n")
}

func (s *Server) quit(user *User, reason string) {
	s.Lock()
	defer s.Unlock()
	for _, ch := range s.Channels {
		if _, ok := ch.Users[strings.ToLower(user.Nick)]; ok {
			s.send(user, "QUIT", ch.Name, reason)
		}
	}
}

func (s *Server) topic(user *User, channel string) {
	s.Lock()
	defer s.Unlock()
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
	s.Lock()
	defer s.Unlock()
	// we don't support filters or anything because why bother
	s.reply(user, RPL_LISTSTART, "Channel", "Users Name")
	for _, ch := range s.Channels {
		s.reply(user, RPL_LIST, user.Nick, ch.Name, strconv.Itoa(len(ch.Users)), ch.Topic)
	}
	s.reply(user, RPL_LISTEND, "End of /LIST")
}

func (s *Server) whois(user *User, nickList string) {
	for _, nick := range strings.Split(nickList, ",") {
		u := s.Nick(nick)
		if u == nil {
			s.reply(user, ERR_NOSUCHNICK, nick, ":No such nick")
			continue
		}
		s.reply(user, RPL_WHOISUSER, user.Nick, u.Nick, u.Callsign, "*", u.RealName)
	}
	s.reply(user, RPL_ENDOFWHOIS, nickList, "End of /WHOIS list")
}

func (s *Server) setTopic(user *User, ch *Channel, topic string) {
	s.Lock()
	defer s.Unlock()

	ch.Topic = topic
	ch.TopicWho = user.Nick
	ch.TopicTime = time.Now()

	for _, u := range ch.Users {
		s.reply(u, RPL_TOPIC, u.Nick, ch.Name, ch.Topic)
	}

	// also push out topic change
	if user.Local() {
		fmt.Fprintf(s.tnc.Port(uint8(s.tncport)), ":%s %s %s :%s", user.ID(), "TOPIC", ch.Name, ch.Topic)
	}
}
