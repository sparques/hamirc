package irc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFileReturnsNotExist(t *testing.T) {
	server := NewServer()

	err := server.Load(filepath.Join(t.TempDir(), "missing.json"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load missing file error = %v, want os.ErrNotExist", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	server := NewServer()
	server.Name = "hamirc"

	user := NewUser("Maple", nil)
	user.Callsign = "N0CALL"
	user.RealName = "Maple Station"
	user.LastSeen = time.Unix(1234, 0).UTC()
	server.Users["maple"] = user

	channel := server.Channel("#HamIRC")
	channel.Topic = "Net tonight"
	channel.TopicWho = "Maple"
	channel.TopicTime = time.Unix(5678, 0).UTC()
	channel.Users["Maple"] = user

	if err := server.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded := NewServer()
	if err := loaded.Load(path); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	gotUser := loaded.Nick("MAPLE")
	if gotUser == nil {
		t.Fatal("loaded user not found by case-insensitive nick")
	}
	if gotUser.Callsign != "N0CALL" || gotUser.RealName != "Maple Station" {
		t.Fatalf("loaded user = %#v", gotUser)
	}

	gotChannel := loaded.Channels["#hamirc"]
	if gotChannel == nil {
		t.Fatal("loaded channel not found")
	}
	if gotChannel.Topic != "Net tonight" || gotChannel.TopicWho != "Maple" {
		t.Fatalf("loaded channel = %#v", gotChannel)
	}
	if gotChannel.Users["maple"] != gotUser {
		t.Fatalf("channel user was not normalized to canonical loaded user")
	}
}
