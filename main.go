package main

import (
	"errors"
	"flag"
	"log"
	"os"
	"os/exec"

	"github.com/sparques/hamirc/irc"
)

var (
	tncaddr   = flag.String("tnc", ":8001", "address of TNC")
	serve     = flag.String("serve", ":6667", "port and optionally address to listen on for IRC connections")
	statefile = flag.String("state", "serverState.json", "path to file for loading/saving server state")
	persist   = flag.Bool("persist", true, "if true, will load/save server state (users, channels, topics) to a file")
	mustload  = flag.Bool("mustload", true, "if true, loading the state must succeed or program will exit; this is to prevent a server state file from being overwritten by an empty server state.")
	autojoin  = flag.Bool("autojoin", true, "if true, will cause local users (those connected via TCP) to automatically join any channels that receive a message")
	tncport   = flag.Int("tncport", 0, "the TNC port to use; valid options: 0-7;")
)

func main() {
	flag.Parse()
	server := irc.NewServer()
	if *persist {
		err := server.Load(*statefile)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Println("Couldn't load server state:", err)
			if *mustload {
				log.Println("Exiting. -mustload was specified")
				os.Exit(1)
			}
		}
		go server.PersistState(*statefile)
	}
	// Automatically have local users join any newly seen channels
	server.AutoJoin = *autojoin
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
	err := server.ConnectTNC(*tncaddr, *tncport)
	if err != nil {
		log.Println(err)
		return
	}
	server.Serve(*serve)
}
