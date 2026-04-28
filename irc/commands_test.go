package irc

import (
	"bytes"
	"strings"
	"testing"
)

func TestRegistrationAcceptsUserAfterNickAndUser(t *testing.T) {
	server := NewServer()
	server.Name = "hamirc"
	var out bytes.Buffer
	user := NewUser("", &out)

	if quit := server.handleCommand(user, "USER N0CALL 0 * :Maple Station"); quit {
		t.Fatal("USER caused quit")
	}
	if user.Callsign != "N0CALL" || user.RealName != "Maple Station" {
		t.Fatalf("user fields = callsign %q realname %q", user.Callsign, user.RealName)
	}

	if quit := server.handleCommand(user, "NICK Maple"); quit {
		t.Fatal("NICK caused quit")
	}
	if server.Nick("maple") != user {
		t.Fatal("registered user was not stored under nick")
	}
	if !strings.Contains(out.String(), " 001 Maple :Connected.") {
		t.Fatalf("welcome reply missing from %q", out.String())
	}
	if strings.Contains(out.String(), " NICK :Maple") {
		t.Fatalf("initial NICK produced nick-change message: %q", out.String())
	}
}

func TestUnregisteredCommandIsRejected(t *testing.T) {
	server := NewServer()
	var out bytes.Buffer
	user := NewUser("", &out)

	if quit := server.handleCommand(user, "PRIVMSG #hamirc :hello"); quit {
		t.Fatal("PRIVMSG caused quit")
	}
	if !strings.Contains(out.String(), " 451 * PRIVMSG :You have not registered") {
		t.Fatalf("unregistered rejection = %q", out.String())
	}
}

func TestDuplicateLocalNickIsRejected(t *testing.T) {
	server := NewServer()
	existing := NewUser("Maple", &bytes.Buffer{})
	existing.local = true
	server.Users[nickKey(existing.Nick)] = existing

	var out bytes.Buffer
	user := NewUser("", &out)
	if quit := server.handleCommand(user, "NICK Maple"); quit {
		t.Fatal("NICK caused quit")
	}
	if user.Nick != "" {
		t.Fatalf("duplicate nick changed user nick to %q", user.Nick)
	}
	if !strings.Contains(out.String(), " 433 * Maple :Nickname is already in use") {
		t.Fatalf("duplicate nick reply = %q", out.String())
	}
}

func TestPartBroadcastsAndRemovesUser(t *testing.T) {
	server := NewServer()
	server.Name = "hamirc"

	var senderOut, otherOut bytes.Buffer
	sender := NewUser("Maple", &senderOut)
	sender.Callsign = "N0CALL"
	other := NewUser("Birch", &otherOut)

	channel := server.Channel("#hamirc")
	channel.Users[nickKey(sender.Nick)] = sender
	channel.Users[nickKey(other.Nick)] = other

	if quit := part(server, sender, []string{"PART", "#hamirc", "done"}); quit {
		t.Fatal("PART caused quit")
	}
	if _, ok := channel.Users[nickKey(sender.Nick)]; ok {
		t.Fatal("sender was not removed from channel")
	}
	want := ":Maple!N0CALL@ PART #hamirc :done\r\n"
	if senderOut.String() != want {
		t.Fatalf("sender PART message = %q, want %q", senderOut.String(), want)
	}
	if otherOut.String() != want {
		t.Fatalf("other PART message = %q, want %q", otherOut.String(), want)
	}
}

func TestWhoEndsResponse(t *testing.T) {
	server := NewServer()
	server.Name = "hamirc"
	var out bytes.Buffer
	user := NewUser("Maple", &out)
	user.Callsign = "N0CALL"
	server.Users[nickKey(user.Nick)] = user

	if quit := who(server, user, []string{"WHO", "*"}); quit {
		t.Fatal("WHO caused quit")
	}
	got := out.String()
	if !strings.Contains(got, ":hamirc 352 Maple * N0CALL * * Maple G :1 \r\n") {
		t.Fatalf("WHO reply missing or malformed: %q", got)
	}
	if !strings.Contains(got, ":hamirc 315 Maple * :End of /WHO list\r\n") {
		t.Fatalf("WHO end reply missing: %q", got)
	}
}

func TestWhoisMissingNickIncludesRequesterAndEnd(t *testing.T) {
	server := NewServer()
	server.Name = "hamirc"
	var out bytes.Buffer
	user := NewUser("Maple", &out)

	if quit := whois(server, user, []string{"WHOIS", "Missing"}); quit {
		t.Fatal("WHOIS caused quit")
	}
	got := out.String()
	if !strings.Contains(got, ":hamirc 401 Maple Missing :No such nick\r\n") {
		t.Fatalf("WHOIS no-such-nick malformed: %q", got)
	}
	if !strings.Contains(got, ":hamirc 318 Maple Missing :End of /WHOIS list\r\n") {
		t.Fatalf("WHOIS end malformed: %q", got)
	}
}

func TestModeChannelQueryReturnsChannelMode(t *testing.T) {
	server := NewServer()
	server.Name = "hamirc"
	var out bytes.Buffer
	user := NewUser("Maple", &out)

	if quit := mode(server, user, []string{"MODE", "#hamirc"}); quit {
		t.Fatal("MODE caused quit")
	}
	if out.String() != ":hamirc 324 Maple #hamirc :+\r\n" {
		t.Fatalf("MODE channel reply = %q", out.String())
	}
}

func TestNickChangeBroadcastsToChannelMembers(t *testing.T) {
	server := NewServer()
	server.Name = "hamirc"

	var userOut, otherOut bytes.Buffer
	user := NewUser("Maple", &userOut)
	other := NewUser("Birch", &otherOut)
	server.Users[nickKey(user.Nick)] = user
	server.Users[nickKey(other.Nick)] = other
	channel := server.Channel("#hamirc")
	channel.Users[nickKey(user.Nick)] = user
	channel.Users[nickKey(other.Nick)] = other

	if quit := nick(server, user, []string{"NICK", "Cedar"}); quit {
		t.Fatal("NICK caused quit")
	}
	if server.Nick("Maple") != nil {
		t.Fatal("old nick still resolves")
	}
	if server.Nick("Cedar") != user {
		t.Fatal("new nick does not resolve to user")
	}
	want := ":Maple NICK :Cedar\r\n"
	if userOut.String() != want {
		t.Fatalf("user nick-change message = %q, want %q", userOut.String(), want)
	}
	if otherOut.String() != want {
		t.Fatalf("other nick-change message = %q, want %q", otherOut.String(), want)
	}
}
