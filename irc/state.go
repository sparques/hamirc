package irc

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

func (s *Server) PersistState(path string) {
	var (
	// lastStateHash uint64
	// buf = bytes.NewBuffer(nil)
	// hash          = fnv.New64a()
	)

	// jenc := json.NewEncoder(buf)
	for {
		// time.Sleep(time.Minute)
		time.Sleep(time.Second * 10)
		// buf.Reset()
		/*
			hash.Reset()
			s.Lock()
			err := jenc.Encode(s)
			s.Unlock()
			if err != nil {
				log.Printf("could not marshal json: %s", err)
			}
				currentState := hash.Sum64()
				if currentState == lastStateHash {
					continue
				}
		*/
		s.Save(path)
		//lastStateHash = currentState
	}
}

// Save saves the radiouser list and channels to path.
// This can be restored with Load(path).
func (s *Server) Save(path string) error {
	s.Lock()
	defer s.Unlock()
	fh, err := os.Create(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	enc := json.NewEncoder(fh)
	enc.SetIndent("", "  ")
	err = enc.Encode(s)
	if err != nil {
		return err
	}

	return nil
}

func (s *Server) Load(path string) error {
	s.Lock()
	defer s.Unlock()

	if len(s.Users) > 0 {
		return errors.New("cannot load server state with connected users")
	}

	fh, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer fh.Close()
	dec := json.NewDecoder(fh)
	err = dec.Decode(s)
	if err != nil {
		return err
	}
	// cycle through Users, set their non-exported fields
	for _, user := range s.Users {
		user.buf = bufio.NewWriter(io.Discard)
	}
	// cycle through the channels and correct the user maps, instantiate locks
	for _, ch := range s.Channels {
		ch.Mutex = &sync.Mutex{}
		if ch.Users == nil {
			ch.Users = make(map[string]*User)
		}
		for tmpNick := range ch.Users {
			actualUser := s.Nick(tmpNick)
			if actualUser == nil {
				delete(ch.Users, tmpNick)
				continue
			}
			ch.Users[strings.ToLower(actualUser.Nick)] = actualUser
		}
	}
	return nil
}

func (ucm ChanUserMap) MarshalJSON() ([]byte, error) {
	nicks := make([]string, 0, len(ucm))

	for nick := range ucm {
		nicks = append(nicks, nick)
	}

	if len(nicks) == 0 {
		return []byte("[]"), nil
	}

	return json.Marshal(nicks)

	for _, user := range ucm {
		if user.Local() || user.Nick == "" {
			continue
		}
		nicks = append(nicks, user.Nick)
	}
	slices.Sort(nicks)
	nicks = slices.Compact(nicks)
	return json.Marshal(nicks)
}

func (ucm *ChanUserMap) UnmarshalJSON(data []byte) error {
	userChanMap := make(ChanUserMap)
	var nicks []string
	err := json.Unmarshal(data, &nicks)
	if err != nil {
		return err
	}
	for _, nick := range nicks {
		user := NewUser(nick, io.Discard)
		userChanMap[strings.ToLower(user.Nick)] = user
	}
	*ucm = userChanMap
	return nil
}

/*
func (um UserMap) MarshalJSON() ([]byte, error) {
	nickMap := make(map[string]*User, len(um))
	for _, user := range um {
		// only persist non-local users
		if user.Local() {
			continue
		}
		nickMap[user.Nick] = user
	}
	return json.Marshal(nickMap)
}

func (um UserMap) UnmarshalJSON(data []byte) error {
	var nickMap map[string]*User
	err := json.Unmarshal(data, &nickMap)
	if err != nil {
		return err
	}
	for _, user := range nickMap {
		um[strings.ToLower(user.Nick)] = user
	}
	return nil
}
*/
