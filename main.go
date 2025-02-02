package main

import (
	"hamirc/irc"
	"log"
	"os/exec"
)

func main() {
	server := irc.NewServer()
	// Automatically have local users join any newly seen channels
	err := server.Load("serverState.json")
	if err != nil {
		log.Println("Couldn't load server state:", err)
	}
	go server.PersistState("serverState.json")
	server.AutoJoin = true
	server.MOTD = func() string {
		cmd := exec.Command("fortune")
		if cmd.Err != nil {
			return "No news is good news."
		}
		out, err := cmd.Output()
		if err != nil {
			return "I can't believe you've done this."
		}
		return string(out)
	}
	server.ConnectTNC(":8001")
	server.Serve(":6667")
}
