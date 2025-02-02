package irc

import (
	"bufio"
	"fmt"
	"hamirc/kiss"
	"io"
	"log"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type UserMap map[string]*User

// Server represents the IRC server
type Server struct {
	*sync.Mutex `json:"-"`
	Name        string
	Users       UserMap
	Channels    map[string]*Channel
	tnc         *kiss.TNC
	MOTD        func() string `json:"-"`
	// AutoJoin causes Local() users to automatically join channels they
	// get messages for.
	AutoJoin bool
}

func NewServer() *Server {
	return &Server{
		Mutex:    &sync.Mutex{},
		Name:     "server",
		Users:    make(UserMap),
		Channels: make(map[string]*Channel),
	}
}

func (s *Server) Nick(nick string) *User {
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

	for {
		conn, err := listener.Accept()
		log.Printf("New connection on %s\n", conn.RemoteAddr())
		if err != nil {
			log.Printf("Error accepting connection: %v\n", err)
			continue
		}
		go s.handleConnection(conn)
	}
}

func (s *Server) Channel(name string) *Channel {
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
			log.Printf("error reading from TNC port: %s", err)
			log.Printf("disconnecting from TNC")
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
		time.Sleep(time.Second * 30)
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
func (s *Server) handleCommand(user *User, line string) (cont bool) {
	cont = false
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
	switch command {
	case "CAP":
		s.reply(user, "CAP", "LS")
	case "NICK":
		oldNick := user.Nick
		s.changeNick(user, args[1])
		if oldNick == "" && user.Callsign != "" {
			s.acceptUser(user)
		}
	case "USER":
		// after we get a USER, send the welcome wagon
		if user.Callsign != "" {
			s.reply(user, ERR_ALREADYREGISTERED, "You may not reregister")
			return
		}
		if len(args) != 5 {
			s.reply(user, ERR_NEEDMOREPARAMS, "Need more params for USER")
			return
		}
		user.Callsign = args[1]
		user.RealName = args[4]
		if user.Nick != "" {
			s.acceptUser(user)
		}
	case "JOIN":
		if len(args) != 2 {
			s.reply(user, ERR_NEEDMOREPARAMS, user.Nick, "Not enough parameters")
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
			s.reply(user, "PONG", args[1])
		} else {
			s.reply(user, "PONG")
		}
	case "PONG":
		// does it matter?
	case "USERHOST":
		if len(args) > 1 {
			s.userHost(user, args[1:])
		}
	case "WHOIS":
		if len(args) == 1 {
			s.reply(user, ERR_NONICKNAMEGIVEN, user.Nick, "No nickname given")
			return
		}
		s.whois(user, args[1])
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
		s.setTopic(user, ch, strings.Join(args[2:], " "))
	case "MODE":
		// TODO: support ban / +b ?
		// Is it necessary? Most clients have an ignore feature
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

			if ch.Nick(user.Nick) == nil {
				s.reply(user, ERR_NOTONCHANNEL, user.Nick, chName, "you're not in that channel")
				continue
			}

			s.send(user, "PART", chName, reason)
			delete(ch.Users, user.Nick)
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

	newNickLower := strings.ToLower(newNick)
	oldNickLower := strings.ToLower(user.Nick)

	// Check if the new nickname is already in use
	// allow person to snag a remote user though
	existingUser, ok := s.Users[newNickLower]
	if ok && existingUser.Local() {
		s.reply(user, "433", user.Nick, newNick, "Nickname is already in use")
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
	for _, ch := range s.Channels {
		if _, ok := ch.Users[strings.ToLower(user.Nick)]; ok {
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
	for chName, ch := range s.Channels {
		log.Printf("Listing Channel: %s %s\n", chName, ch)
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
	ch.Topic = topic
	ch.TopicWho = user.Nick
	ch.TopicTime = time.Now()

	for _, u := range ch.Users {
		s.reply(u, RPL_TOPIC, u.Nick, ch.Name, ch.Topic)
	}

	// also push out topic change
	if user.Local() {
		fmt.Fprintf(s.tnc.Port(0), ":%s %s %s :%s", user.ID(), "TOPIC", ch.Name, ch.Topic)
	}
}
