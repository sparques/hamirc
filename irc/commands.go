package irc

import "strings"

type serverCommand func(s *Server, user *User, args []string) (quit bool)

var cmdSet = map[string]serverCommand{
	"CAP":      capabilities,
	"JOIN":     join,
	"LIST":     list,
	"MODE":     mode,
	"MOTD":     motd,
	"NICK":     nick,
	"NOTICE":   notice,
	"PART":     part,
	"PING":     ping,
	"PONG":     pong,
	"PRIVMSG":  privmsg,
	"TOPIC":    topic,
	"USER":     user,
	"USERHOST": userhost,
	"QUIT":     quit,
	"WHO":      who,
	"WHOIS":    whois,
}

func capabilities(s *Server, user *User, args []string) (quit bool) {
	s.reply(user, "CAP", "LS")
	return
}

func nick(s *Server, user *User, args []string) (quit bool) {
	oldNick := user.Nick
	s.changeNick(user, args[1])
	if oldNick == "" && user.Callsign != "" {
		s.acceptUser(user)
	}
	return
}

func user(s *Server, user *User, args []string) (quit bool) {
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
	return
}

func join(s *Server, user *User, args []string) (quit bool) {
	if len(args) != 2 {
		s.reply(user, ERR_NEEDMOREPARAMS, user.Nick, "Not enough parameters")
		return
	}
	for _, ch := range strings.Split(args[1], ",") {
		s.joinChannel(user, ch)
	}
	return
}

func who(s *Server, user *User, args []string) (quit bool) {
	mask := "*"
	if len(args) == 2 {
		mask = args[1]
	}
	// list users according to mask
	s.listUsers(user, mask)
	return
}

func notice(s *Server, user *User, args []string) (quit bool) {
	if len(args) < 2 {
		s.reply(user, ERR_NEEDMOREPARAMS, user.Nick, "Not enough parameters")
		return
	}
	s.send(user, "NOTICE", args[1], args[2])
	return
}

func privmsg(s *Server, user *User, args []string) (quit bool) {
	if len(args) < 2 {
		s.reply(user, ERR_NEEDMOREPARAMS, user.Nick, "Not enough parameters")
		return
	}
	s.send(user, "PRIVMSG", args[1], args[2])
	return
}

func ping(s *Server, user *User, args []string) (quit bool) {
	if len(args) > 1 {
		s.reply(user, "PONG", args[1])
	} else {
		s.reply(user, "PONG")
	}
	return
}

func pong(s *Server, user *User, args []string) (quit bool) {
	//meh
	return
}

func userhost(s *Server, user *User, args []string) (quit bool) {
	if len(args) > 1 {
		s.userHost(user, args[1:])
	}
	return
}

func whois(s *Server, user *User, args []string) (quit bool) {
	if len(args) == 1 {
		s.reply(user, ERR_NONICKNAMEGIVEN, user.Nick, "No nickname given")
		return
	}
	s.whois(user, args[1])
	return
}

func list(s *Server, user *User, args []string) (quit bool) {
	// no need to support the arguments to list, right?
	s.listChannels(user)
	return
}
func topic(s *Server, user *User, args []string) (quit bool) {
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
	return
}

func mode(s *Server, user *User, args []string) (quit bool) {
	s.reply(user, ERR_UNKNOWNMODE, args[1], "Server doesn't support modes")
	return
}

func motd(s *Server, user *User, args []string) (quit bool) {
	s.motd(user)
	return
}

func part(s *Server, user *User, args []string) (quit bool) {
	s.Lock()
	defer s.Unlock()
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
	return
}

func quit(s *Server, user *User, args []string) (quit bool) {
	if len(args) == 1 {
		s.quit(user, "Client disconnected.")
	} else {
		s.quit(user, strings.Join(args[1:], " "))
	}
	return true

	return
}

/*
func(s *Server, user *User, args []string) (quit bool) {
 	return
 }
*/
