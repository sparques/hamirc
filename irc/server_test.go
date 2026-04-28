package irc

import (
	"bytes"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sparques/kiss"
)

func runSingleTNCFrame(t *testing.T, server *Server, payload string) {
	t.Helper()

	serverConn, peerConn := net.Pipe()
	server.exitch = make(chan error, 1)
	server.tnc = kiss.NewTNC(serverConn)
	server.tncport = 0

	done := make(chan struct{})
	go func() {
		server.handleTNC()
		close(done)
	}()

	frame := kiss.FrameEncode(0, []byte(payload))
	if _, err := peerConn.Write(frame); err != nil {
		t.Fatalf("writing TNC frame: %v", err)
	}
	if err := peerConn.Close(); err != nil {
		t.Fatalf("closing TNC peer: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for handleTNC to return")
	}

	select {
	case <-server.exitch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for handleTNC exit signal")
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
	}{
		{
			name: "empty",
			line: "   ",
			want: nil,
		},
		{
			name: "command only",
			line: "PING",
			want: []string{"PING"},
		},
		{
			name: "params and trailing",
			line: "PRIVMSG #hamirc :hello over radio",
			want: []string{"PRIVMSG", "#hamirc", "hello over radio"},
		},
		{
			name: "prefix params and trailing",
			line: ":nick!call@Real_Name PRIVMSG #hamirc :hello over radio",
			want: []string{"nick!call@Real_Name", "PRIVMSG", "#hamirc", "hello over radio"},
		},
		{
			name: "extra spaces",
			line: "  USER   N0CALL   0   *   :Real Name  ",
			want: []string{"USER", "N0CALL", "0", "*", "Real Name"},
		},
		{
			name: "prefix only",
			line: ":nick!call@host",
			want: []string{"nick!call@host"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parse(tt.line)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parse(%q) = %#v, want %#v", tt.line, got, tt.want)
			}
		})
	}
}

func TestParseSerialTNCAddress(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		wantPort string
		wantBaud int
		wantOK   bool
		wantErr  bool
	}{
		{
			name:     "tcp address",
			addr:     "localhost:8001",
			wantBaud: defaultTNCBaud,
			wantOK:   false,
		},
		{
			name:     "linux serial default baud",
			addr:     "/dev/ttyUSB0",
			wantPort: "/dev/ttyUSB0",
			wantBaud: defaultTNCBaud,
			wantOK:   true,
		},
		{
			name:     "linux serial explicit baud",
			addr:     "/dev/ttyUSB0:9600",
			wantPort: "/dev/ttyUSB0",
			wantBaud: 9600,
			wantOK:   true,
		},
		{
			name:     "windows com default baud",
			addr:     "COM3",
			wantPort: "COM3",
			wantBaud: defaultTNCBaud,
			wantOK:   true,
		},
		{
			name:     "windows com explicit baud",
			addr:     "COM12:57600",
			wantPort: "COM12",
			wantBaud: 57600,
			wantOK:   true,
		},
		{
			name:     "windows extended com path",
			addr:     `\\.\COM12:57600`,
			wantPort: `\\.\COM12`,
			wantBaud: 57600,
			wantOK:   true,
		},
		{
			name:     "forced serial",
			addr:     "serial:COM4:38400",
			wantPort: "COM4",
			wantBaud: 38400,
			wantOK:   true,
		},
		{
			name:     "bad baud",
			addr:     "COM4:fast",
			wantBaud: defaultTNCBaud,
			wantOK:   true,
			wantErr:  true,
		},
		{
			name:     "empty forced serial",
			addr:     "serial:",
			wantBaud: defaultTNCBaud,
			wantOK:   true,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPort, gotBaud, gotOK, err := parseSerialTNCAddress(tt.addr)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if gotOK != tt.wantOK {
					t.Fatalf("ok = %v, want %v", gotOK, tt.wantOK)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotPort != tt.wantPort || gotBaud != tt.wantBaud || gotOK != tt.wantOK {
				t.Fatalf("parseSerialTNCAddress(%q) = %q, %d, %v; want %q, %d, %v",
					tt.addr, gotPort, gotBaud, gotOK, tt.wantPort, tt.wantBaud, tt.wantOK)
			}
		})
	}
}

func TestHandleCommandIgnoresEmptyLineAndReportsMissingNick(t *testing.T) {
	server := NewServer()
	var out bytes.Buffer
	user := NewUser("", &out)

	if quit := server.handleCommand(user, "   "); quit {
		t.Fatal("empty command caused quit")
	}
	if out.Len() != 0 {
		t.Fatalf("empty command wrote %q", out.String())
	}

	if quit := server.handleCommand(user, "NICK"); quit {
		t.Fatal("malformed NICK caused quit")
	}
	if !strings.Contains(out.String(), " 431 ") {
		t.Fatalf("malformed NICK response = %q, want 431", out.String())
	}
}

func TestReplyDoesNotMutateArgsOrDoublePrefixTrailingColon(t *testing.T) {
	server := NewServer()
	server.Name = "hamirc"
	var out bytes.Buffer
	user := NewUser("Maple", &out)

	args := []string{ERR_NOSUCHNICK, "Maple", "Other", ":No such nick"}
	server.reply(user, args...)

	if args[3] != ":No such nick" {
		t.Fatalf("reply mutated args: %#v", args)
	}
	want := ":hamirc 401 Maple Other :No such nick\r\n"
	if out.String() != want {
		t.Fatalf("reply = %q, want %q", out.String(), want)
	}
}

func TestAcceptUserUsesPlainWelcomeText(t *testing.T) {
	server := NewServer()
	server.Name = "hamirc"
	var out bytes.Buffer
	user := NewUser("Maple", &out)
	user.Callsign = "N0CALL"

	server.acceptUser(user)

	got := out.String()
	if !strings.Contains(got, ":hamirc 002 Maple :Your host is hamirc.") {
		t.Fatalf("welcome host line missing from %q", got)
	}
	if strings.Contains(got, "abomination") || strings.Contains(got, "last century") {
		t.Fatalf("welcome text still contains joke copy: %q", got)
	}
}

func TestSendChannelMessageSkipsSenderAndUpdatesLastSeen(t *testing.T) {
	server := NewServer()
	server.Name = "hamirc"

	var senderOut, firstOut, secondOut bytes.Buffer
	sender := NewUser("Maple", &senderOut)
	sender.Callsign = "N0CALL"
	sender.RealName = "Maple Station"
	first := NewUser("Birch", &firstOut)
	second := NewUser("Cedar", &secondOut)

	channel := server.Channel("#HamIRC")
	channel.Users[nickKey(sender.Nick)] = sender
	channel.Users[nickKey(first.Nick)] = first
	channel.Users[nickKey(second.Nick)] = second

	before := time.Now()
	server.send(sender, "PRIVMSG", "#hamirc", "hello net")

	if !sender.LastSeen.After(before) {
		t.Fatalf("LastSeen was not updated: %s <= %s", sender.LastSeen, before)
	}
	if senderOut.String() != "" {
		t.Fatalf("sender received own PRIVMSG: %q", senderOut.String())
	}
	want := ":Maple!N0CALL@Maple_Station PRIVMSG #hamirc :hello net\r\n"
	if firstOut.String() != want {
		t.Fatalf("first recipient = %q, want %q", firstOut.String(), want)
	}
	if secondOut.String() != want {
		t.Fatalf("second recipient = %q, want %q", secondOut.String(), want)
	}
}

func TestSendLocalMessageWithoutTNCStillDeliversLocally(t *testing.T) {
	server := NewServer()
	server.Name = "hamirc"

	var recipientOut bytes.Buffer
	sender := NewUser("Maple", io.Discard)
	sender.local = true
	sender.Callsign = "N0CALL"
	recipient := NewUser("Birch", &recipientOut)
	server.Users[nickKey(sender.Nick)] = sender
	server.Users[nickKey(recipient.Nick)] = recipient

	server.send(sender, "NOTICE", "Birch", "copy")

	want := ":Maple!N0CALL@ NOTICE Birch :copy\r\n"
	if recipientOut.String() != want {
		t.Fatalf("recipient = %q, want %q", recipientOut.String(), want)
	}
}

func TestUserWriteReportsBytesWritten(t *testing.T) {
	var out bytes.Buffer
	user := NewUser("Maple", &out)

	n, err := user.Write([]byte("hello\n"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len("hello\n") {
		t.Fatalf("Write n = %d, want %d", n, len("hello\n"))
	}
	if out.String() != "hello\n" {
		t.Fatalf("output = %q, want hello newline", out.String())
	}
}

func TestJoinChannelBroadcastsAndSendsNames(t *testing.T) {
	server := NewServer()
	server.Name = "hamirc"

	var joiningOut, existingOut bytes.Buffer
	joining := NewUser("Maple", &joiningOut)
	joining.Callsign = "N0CALL"
	existing := NewUser("Birch", &existingOut)
	existing.Callsign = "K1ABC"

	channel := server.Channel("#hamirc")
	channel.Users[nickKey(existing.Nick)] = existing

	server.joinChannel(joining, "#hamirc")

	joinLine := ":Maple!N0CALL@ JOIN :#hamirc\r\n"
	if !strings.Contains(joiningOut.String(), joinLine) {
		t.Fatalf("joining user did not receive JOIN line: %q", joiningOut.String())
	}
	if !strings.Contains(existingOut.String(), joinLine) {
		t.Fatalf("existing user did not receive JOIN line: %q", existingOut.String())
	}
	if !strings.Contains(joiningOut.String(), " 353 Maple = #hamirc :") {
		t.Fatalf("joining user did not receive names reply: %q", joiningOut.String())
	}
	if !strings.Contains(joiningOut.String(), " 366 Maple #hamirc :End of /NAMES list") {
		t.Fatalf("joining user did not receive end names reply: %q", joiningOut.String())
	}
}

func TestPartChannelBroadcastsAndRemovesRemoteUser(t *testing.T) {
	server := NewServer()
	server.Name = "hamirc"

	var remoteOut, localOut bytes.Buffer
	remote := NewUser("Maple", &remoteOut)
	remote.Callsign = "N0CALL"
	local := NewUser("Birch", &localOut)
	local.Callsign = "K1ABC"

	channel := server.Channel("#hamirc")
	channel.Users[nickKey(remote.Nick)] = remote
	channel.Users[nickKey(local.Nick)] = local

	if ok := server.partChannel(remote, "#hamirc", "73"); !ok {
		t.Fatal("partChannel returned false")
	}
	if _, ok := channel.Users[nickKey(remote.Nick)]; ok {
		t.Fatal("remote user was not removed from channel")
	}

	want := ":Maple!N0CALL@ PART #hamirc :73\r\n"
	if remoteOut.String() != want {
		t.Fatalf("remote PART message = %q, want %q", remoteOut.String(), want)
	}
	if localOut.String() != want {
		t.Fatalf("local PART message = %q, want %q", localOut.String(), want)
	}
}

func TestHandleTNCAutoJoinSkipsPartedChannel(t *testing.T) {
	server := NewServer()
	server.Name = "hamirc"
	server.AutoJoin = true

	var localOut bytes.Buffer
	local := NewUser("Maple", &localOut)
	local.local = true
	local.Callsign = "N0CALL"
	server.Users[nickKey(local.Nick)] = local

	channel := server.Channel("#hamirc")
	channel.Users[nickKey(local.Nick)] = local

	if quit := part(server, local, []string{"PART", "#hamirc", "done"}); quit {
		t.Fatal("PART caused quit")
	}
	localOut.Reset()

	runSingleTNCFrame(t, server, ":Birch!K1ABC@Birch_Station PRIVMSG #hamirc :hello")

	if _, ok := channel.Users[nickKey(local.Nick)]; ok {
		t.Fatal("local user was auto-joined back to parted channel")
	}
	if strings.Contains(localOut.String(), " JOIN :#hamirc\r\n") {
		t.Fatalf("local user received unexpected JOIN after part: %q", localOut.String())
	}
}

func TestHandleTNCAutoJoinStillJoinsUnpartedChannel(t *testing.T) {
	server := NewServer()
	server.Name = "hamirc"
	server.AutoJoin = true

	var localOut bytes.Buffer
	local := NewUser("Maple", &localOut)
	local.local = true
	local.Callsign = "N0CALL"
	server.Users[nickKey(local.Nick)] = local

	partedChannel := server.Channel("#hamirc")
	partedChannel.Users[nickKey(local.Nick)] = local

	if quit := part(server, local, []string{"PART", "#hamirc", "done"}); quit {
		t.Fatal("PART caused quit")
	}
	localOut.Reset()

	runSingleTNCFrame(t, server, ":Birch!K1ABC@Birch_Station PRIVMSG #newchan :hello")

	newChannel := server.Channel("#newchan")
	if _, ok := newChannel.Users[nickKey(local.Nick)]; !ok {
		t.Fatal("local user was not auto-joined to unparted channel")
	}
	if !strings.Contains(localOut.String(), " JOIN :#newchan\r\n") {
		t.Fatalf("local user did not receive JOIN for unparted channel: %q", localOut.String())
	}
}

func TestHandleTNCPARTDoesNotAutoJoinLocalUsers(t *testing.T) {
	server := NewServer()
	server.Name = "hamirc"
	server.AutoJoin = true

	var localOut bytes.Buffer
	local := NewUser("Maple", &localOut)
	local.local = true
	local.Callsign = "N0CALL"
	server.Users[nickKey(local.Nick)] = local

	runSingleTNCFrame(t, server, ":Birch!K1ABC@Birch_Station PART #hamirc :73")

	channel := server.Channel("#hamirc")
	if _, ok := channel.Users[nickKey(local.Nick)]; ok {
		t.Fatal("local user was auto-joined by inbound PART")
	}
	if strings.Contains(localOut.String(), " JOIN :#hamirc\r\n") {
		t.Fatalf("local user received unexpected JOIN from inbound PART: %q", localOut.String())
	}
}

func TestSetTopicLocalWithoutTNCBroadcastsLocally(t *testing.T) {
	server := NewServer()
	server.Name = "hamirc"

	var setterOut, otherOut bytes.Buffer
	setter := NewUser("Maple", &setterOut)
	setter.local = true
	setter.Callsign = "N0CALL"
	other := NewUser("Birch", &otherOut)

	channel := server.Channel("#hamirc")
	channel.Users[nickKey(setter.Nick)] = setter
	channel.Users[nickKey(other.Nick)] = other

	server.setTopic(setter, channel, "check in")

	if channel.Topic != "check in" {
		t.Fatalf("topic = %q, want check in", channel.Topic)
	}
	wantSetter := ":hamirc 332 Maple #hamirc :check in\r\n"
	if setterOut.String() != wantSetter {
		t.Fatalf("setter topic reply = %q, want %q", setterOut.String(), wantSetter)
	}
	wantOther := ":hamirc 332 Birch #hamirc :check in\r\n"
	if otherOut.String() != wantOther {
		t.Fatalf("other topic reply = %q, want %q", otherOut.String(), wantOther)
	}
}

func TestListChannelsIncludesClientOnStartAndEnd(t *testing.T) {
	server := NewServer()
	server.Name = "hamirc"
	var out bytes.Buffer
	user := NewUser("Maple", &out)

	channel := server.Channel("#hamirc")
	channel.Topic = "net"

	server.listChannels(user)

	got := out.String()
	if !strings.Contains(got, ":hamirc 321 Maple Channel :Users Name\r\n") {
		t.Fatalf("LIST start reply missing client nick: %q", got)
	}
	if !strings.Contains(got, ":hamirc 322 Maple #hamirc 0 :net\r\n") {
		t.Fatalf("LIST channel reply missing or malformed: %q", got)
	}
	if !strings.Contains(got, ":hamirc 323 Maple :End of /LIST\r\n") {
		t.Fatalf("LIST end reply missing client nick: %q", got)
	}
}
