package irc

import (
	"strings"
)

type serverCommand func(s *Server, user *User, args []string) (quit bool)

var cmdSet = map[string]serverCommand{
	"CAP":      capabilities,
	"ECHO":     echo,
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
	// We don't support much...
	s.reply(user, "CAP", "*", "LS", "")
	return
}

func echo(s *Server, user *User, args []string) (quit bool) {
	// send whatever the user has sent back to the user
	if len(args) == 1 {
		return
	}
	s.reply(user, args[1:]...)
	return
}

func nick(s *Server, user *User, args []string) (quit bool) {
	if len(args) < 2 || args[1] == "" {
		s.reply(user, ERR_NONICKNAMEGIVEN, user.Nick, "No nickname given")
		return
	}
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
		s.reply(user, ERR_ALREADYREGISTERED, replyNick(user), "You may not reregister")
		return
	}
	if len(args) != 5 {
		s.reply(user, ERR_NEEDMOREPARAMS, replyNick(user), "USER", "Need more params")
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
		s.reply(user, ERR_NEEDMOREPARAMS, replyNick(user), "JOIN", "Not enough parameters")
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
	s.reply(user, RPL_ENDOFWHO, user.Nick, mask, "End of /WHO list")
	return
}

func notice(s *Server, user *User, args []string) (quit bool) {
	if len(args) < 3 {
		s.reply(user, ERR_NEEDMOREPARAMS, replyNick(user), "NOTICE", "Not enough parameters")
		return
	}
	s.send(user, "NOTICE", args[1], args[2])
	return
}

func privmsg(s *Server, user *User, args []string) (quit bool) {
	if len(args) < 3 {
		s.reply(user, ERR_NEEDMOREPARAMS, replyNick(user), "PRIVMSG", "Not enough parameters")
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
		s.reply(user, ERR_NONICKNAMEGIVEN, replyNick(user), "No nickname given")
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
		s.reply(user, ERR_NEEDMOREPARAMS, replyNick(user), "TOPIC", "Not enough parameters")
		return
	}
	s.Lock()
	ch, ok := s.Channels[channelKey(args[1])]
	s.Unlock()
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
	if len(args) < 2 {
		s.reply(user, ERR_NEEDMOREPARAMS, replyNick(user), "MODE", "Not enough parameters")
		return
	}
	if strings.HasPrefix(args[1], "#") && len(args) == 2 {
		s.reply(user, RPL_CHANNELMODEIS, user.Nick, args[1], "+")
		return
	}
	mode := args[1]
	if len(args) > 2 {
		mode = args[2]
	}
	s.reply(user, ERR_UNKNOWNMODE, replyNick(user), mode, "Server doesn't support modes")
	return
}

func motd(s *Server, user *User, args []string) (quit bool) {
	s.motd(user)
	return
}

func part(s *Server, user *User, args []string) (quit bool) {
	if len(args) == 1 {
		s.reply(user, ERR_NEEDMOREPARAMS, replyNick(user), "PART", "Not enough parameters")
		return
	}

	reason := "leaving channel"
	if len(args) == 3 {
		reason = args[2]
	}
	for _, chName := range strings.Split(args[1], ",") {
		s.Lock()
		ch, ok := s.Channels[channelKey(chName)]
		if !ok {
			s.Unlock()
			s.reply(user, ERR_NOSUCHCHANNEL, user.Nick, chName, "no such channel")
			continue

		}

		if _, ok := ch.Users[nickKey(user.Nick)]; !ok {
			s.Unlock()
			s.reply(user, ERR_NOTONCHANNEL, user.Nick, chName, "you're not in that channel")
			continue
		}
		s.Unlock()

		s.send(user, "PART", chName, reason)

		s.Lock()
		delete(ch.Users, nickKey(user.Nick))
		s.Unlock()
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
}

/*
func(s *Server, user *User, args []string) (quit bool) {
 	return
 }
*/
