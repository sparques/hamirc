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
	"go.bug.st/serial"
)

type UserMap map[string]*User

func nickKey(nick string) string {
	return strings.ToLower(nick)
}

func channelKey(channel string) string {
	return strings.ToLower(channel)
}

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
	Debug    bool
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
	return s.Users[nickKey(nick)]
}

func (s *Server) Serve(listenAddr string) error {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	defer listener.Close()

	log.Printf("%s server started. Listening on %s", s.Name, listenAddr)
	go s.handleTNC()

	go s.PingPong()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				if strings.HasSuffix(err.Error(), "use of closed network connection") {
					return
				}
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

func (s *Server) debugf(format string, v ...any) {
	if s.Debug {
		log.Printf(format, v...)
	}
}

func (s *Server) Channel(name string) *Channel {
	s.Lock()
	defer s.Unlock()
	key := channelKey(name)
	ch, ok := s.Channels[key]
	if ok {
		return ch
	}
	s.Channels[key] = NewChannel(name)
	return s.Channels[key]
}

func parse(line string) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	var args []string
	if strings.HasPrefix(line, ":") {
		prefix, rest, ok := strings.Cut(line[1:], " ")
		args = append(args, prefix)
		if !ok {
			return args
		}
		line = strings.TrimSpace(rest)
	}

	for line != "" {
		if strings.HasPrefix(line, ":") {
			args = append(args, line[1:])
			break
		}
		arg, rest, ok := strings.Cut(line, " ")
		args = append(args, arg)
		if !ok {
			break
		}
		line = strings.TrimSpace(rest)
	}

	return args
}

// ConnectTNC connects to a TNC via tcp or serial.
func (s *Server) ConnectTNC(addr string, tncport int) (err error) {
	if strings.HasPrefix(addr, "/dev/") {
		mode := &serial.Mode{
			BaudRate: 115200,
		}
		parts := strings.Split(addr, ":")
		if len(parts) == 2 {
			mode.BaudRate, err = strconv.Atoi(parts[1])
			if err != nil {
				return fmt.Errorf("could not extract baudrate from addr: %w", err)
			}
		}

		port, err := serial.Open(parts[0], mode)
		if err != nil {
			return fmt.Errorf("could not open serial port for kiss tnc: %w", err)
		}
		log.Printf("Connected to TNC port %d at %s, %d baud", tncport, parts[0], mode.BaudRate)
		s.tnc = kiss.NewTNC(port)
		s.tncport = tncport
		return nil
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("could not connect to kiss tnc: %w", err)
	}
	log.Printf("Connected to TNC port %d at %s", tncport, addr)
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

		s.debugf("<TNC> %v", args)

		if len(args) < 3 {
			continue
		}

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
			s.Lock()
			if existingUser = s.Users[nickKey(incomingUser.Nick)]; existingUser == nil {
				s.Users[nickKey(incomingUser.Nick)] = incomingUser
			} else {
				incomingUser = existingUser
			}
			s.Unlock()
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
				var usersToJoin []*User
				s.Lock()
				for _, u := range s.Users {
					_, ok := ch.Users[nickKey(u.Nick)]
					if u.Local() && !ok {
						usersToJoin = append(usersToJoin, u)
					}
				}
				s.Unlock()
				for _, u := range usersToJoin {
					s.joinChannel(u, args[2])
				}
			}
		}

		if args[1] == "TOPIC" {
			s.setTopic(incomingUser, s.Channel(args[2]), strings.Join(args[3:], " "))
		} else {
			if len(args) < 4 {
				continue
			}
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
		if s.handleCommand(user, strings.TrimSpace(scanner.Text())) {
			s.removeUser(user)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("<%s@%s> Disconnected: %s\n", user.ID(), conn.RemoteAddr(), err)
	}
	s.removeUser(user)
}

func (s *Server) removeUser(user *User) {
	s.Lock()
	defer s.Unlock()

	delete(s.Users, nickKey(user.Nick))
	for _, ch := range s.Channels {
		// need to add quit message
		delete(ch.Users, nickKey(user.Nick))
	}
}

func (s *Server) acceptUser(user *User) {
	if user.Nick == "" {
		return
	}
	s.reply(user, RPL_WELCOME, user.Nick, "Connected.")
	s.reply(user, RPL_YOURHOST, user.Nick, fmt.Sprintf("Your host is %s.", s.Name))
	s.reply(user, RPL_CREATED, user.Nick, "Server is ready.")

	log.Printf("Accepted user %s.\n", user.ID())
	s.Lock()
	s.Users[nickKey(user.Nick)] = user
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
	if len(args) == 0 {
		return
	}
	command := strings.ToUpper(args[0])

	remoteAddr := "<unknown>"
	if user.conn != nil {
		remoteAddr = user.conn.RemoteAddr().String()
	}
	s.debugf("<%s@%s> %s", user.Nick, remoteAddr, args)
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
	lineArgs := append([]string(nil), args...)
	if len(lineArgs) > 1 && !strings.HasPrefix(lineArgs[len(lineArgs)-1], ":") {
		lineArgs[len(lineArgs)-1] = ":" + lineArgs[len(lineArgs)-1]
	}
	fmt.Fprintf(user, ":%s %s\r\n", s.Name, strings.Join(lineArgs, " "))
}

// changeNick changes a user's nickname
func (s *Server) changeNick(user *User, newNick string) {
	s.Lock()
	defer s.Unlock()

	newNickLower := nickKey(newNick)
	oldNickLower := nickKey(user.Nick)

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
	if oldNickLower != "" {
		delete(s.Users, oldNickLower)
		s.Users[newNickLower] = user
	} else if user.Callsign != "" {
		s.Users[newNickLower] = user
	}

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
		if ch, ok := s.Channels[channelKey(mask)]; ok {
			for _, u := range ch.Users {
				fmt.Fprintf(user, ":%s 352 %s %s %s * * %s %s :1 %s\n", s.Name, user.Nick, ch.Name, u.Callsign, u.Nick, u.Status(), u.RealName)
			}
		}
	case mask == "*":
		s.debugf("Listing all users for %s", user.Nick)
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

	// update LastSeen
	sender.LastSeen = time.Now()
	senderID := sender.ID()

	var tncWriter io.Writer
	if sender.Local() && s.tnc != nil {
		tncWriter = s.tnc.Port(uint8(s.tncport))
	}

	var recipients []*User
	if strings.HasPrefix(target, "#") {
		ch, ok := s.Channels[channelKey(target)]
		if !ok {
			s.Unlock()
			return
		}
		for _, u := range ch.Users {
			if u.Nick == sender.Nick && cmd != "PART" {
				continue
			}
			recipients = append(recipients, u)
		}
	} else if targetUser, ok := s.Users[nickKey(target)]; ok {
		recipients = append(recipients, targetUser)
	} else {
		s.Unlock()
		return
	}
	s.Unlock()

	// Transmit local messages via radio after releasing the server lock.
	if tncWriter != nil {
		fmt.Fprintf(tncWriter, ":%s %s %s :%s", senderID, cmd, target, msg)
	}

	for _, recipient := range recipients {
		fmt.Fprintf(recipient, ":%s %s %s :%s\n", senderID, cmd, target, msg)
	}
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
	key := channelKey(channelName)
	channel, exists := s.Channels[key]
	if !exists {
		channel = NewChannel(channelName)
		s.Channels[key] = channel
	}
	channel.Users[nickKey(user.Nick)] = user

	recipients := make([]*User, 0, len(channel.Users))
	names := make([]string, 0, len(channel.Users))
	for _, u := range channel.Users {
		recipients = append(recipients, u)
		names = append(names, u.Nick)
	}
	topic := channel.Topic
	s.Unlock()

	userID := user.ID()
	for _, recipient := range recipients {
		fmt.Fprintf(recipient, ":%s JOIN :%s\r\n", userID, channelName)
	}

	if topic == "" {
		s.reply(user, RPL_NOTOPIC, user.Nick, channelName, "No topic is set")
	} else {
		s.reply(user, RPL_TOPIC, user.Nick, channelName, topic)
	}

	fmt.Fprintf(user, ":%s 353 %s = %s :", s.Name, user.Nick, channelName)

	for _, name := range names {
		fmt.Fprintf(user, "%s ", name)
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
		u, ok := s.Users[nickKey(nick)]
		if !ok {
			continue
		}
		fmt.Fprintf(user, "%s=-%s@%s ", nick, u.Callsign, strings.ReplaceAll(u.RealName, " ", "_"))
	}
	fmt.Fprintf(user, "\r\n")
}

func (s *Server) quit(user *User, reason string) {
	s.Lock()
	channels := make([]string, 0, len(s.Channels))
	for _, ch := range s.Channels {
		if _, ok := ch.Users[nickKey(user.Nick)]; ok {
			channels = append(channels, ch.Name)
		}
	}
	s.Unlock()

	for _, channel := range channels {
		s.send(user, "QUIT", channel, reason)
	}
}

func (s *Server) topic(user *User, channel string) {
	s.Lock()
	defer s.Unlock()
	// TODO: Figure out a way to share topics
	// When topic is set, might have to broadcast out something like
	// :<user.ID()> TOPIC <channel> <topic>
	ch, ok := s.Channels[channelKey(channel)]
	if !ok {
		s.reply(user, ERR_NOSUCHCHANNEL, user.Nick, channel, "no such channel")
		return
	}

	if ch.Topic == "" {
		s.reply(user, RPL_NOTOPIC, user.Nick, ch.Name, "No topic is set")
	} else {
		s.reply(user, RPL_TOPIC, user.Nick, ch.Name, ch.Topic)
		s.reply(user, RPL_TOPICWHOTIME, user.Nick, ch.Name, ch.TopicWho, strconv.Itoa(int(ch.TopicTime.Unix())))
	}
}

func (s *Server) listChannels(user *User) {
	s.Lock()
	defer s.Unlock()
	// LIST filters are intentionally ignored for now.
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
	if ch == nil {
		return
	}
	s.Lock()

	ch.Topic = topic
	ch.TopicWho = user.Nick
	ch.TopicTime = time.Now()

	recipients := make([]*User, 0, len(ch.Users))
	for _, u := range ch.Users {
		recipients = append(recipients, u)
	}
	chName := ch.Name
	userID := user.ID()
	var tncWriter io.Writer
	if user.Local() && s.tnc != nil {
		tncWriter = s.tnc.Port(uint8(s.tncport))
	}
	s.Unlock()

	for _, recipient := range recipients {
		s.reply(recipient, RPL_TOPIC, recipient.Nick, chName, topic)
	}

	// also push out topic change
	if tncWriter != nil {
		fmt.Fprintf(tncWriter, ":%s %s %s :%s", userID, "TOPIC", chName, topic)
	}
}
