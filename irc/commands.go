package irc

import (
	"fmt"
	"log"
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

	"FREQUENCY":   frequency,
	"SETHARDWARE": sethardware,
}

func capabilities(s *Server, user *User, args []string) (quit bool) {
	// We don't support any capabilities. Only answer the subcommands that
	// expect a reply; answering CAP END makes some clients loop.
	if len(args) < 2 {
		return
	}
	switch sub := strings.ToUpper(args[1]); sub {
	case "LS", "LIST":
		s.reply(user, "CAP", replyNick(user), sub, "")
	case "REQ":
		s.reply(user, "CAP", replyNick(user), "NAK", strings.Join(args[2:], " "))
	}
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

		s.partChannel(user, chName, reason)
		s.Lock()
		user.partedChannels = append(user.partedChannels, channelKey(chName))
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

func frequency(s *Server, user *User, args []string) (quit bool) {
	var rx, tx string
	switch len(args) {
	case 2:
		rx, tx = args[1], args[1]
	case 3:
		rx, tx = args[1], args[2]
	default:
		s.reply(user, ERR_NEEDMOREPARAMS, replyNick(user), "FREQUENCY", "Usage: FREQUENCY <rx> [tx]")
		return
	}

	frame := fmt.Sprintf("AT+DMOSETGROUP=1,%s,%s,0000,0,0000", tx, rx)
	reply, err := s.SetHardware(frame)
	if err != nil {
		log.Printf("SetHardware %s: %v", frame, err)
		s.reply(user, "FREQUENCY", fmt.Sprintf("Failed to change frequency: %v", err))
		return
	}
	log.Printf("SetHardware: %s; Reply: %s", frame, reply)

	s.reply(user, "FREQUENCY", fmt.Sprintf("Changed Frequency: RX: %s; TX: %s", rx, tx))

	return false
}

func sethardware(s *Server, user *User, args []string) (quit bool) {
	if len(args) < 2 {
		s.reply(user, ERR_NEEDMOREPARAMS, replyNick(user), "SETHARDWARE", "Not enough parameters")
		return
	}
	reply, err := s.SetHardware(strings.Join(args[1:], " "))
	if err != nil {
		s.reply(user, "SETHARDWARE", err.Error())
		return
	}

	s.reply(user, "SETHARDWARE", reply)

	return
}

/*
func(s *Server, user *User, args []string) (quit bool) {
 	return
 }
*/
