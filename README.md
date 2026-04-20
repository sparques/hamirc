[![Coverage Status](https://coveralls.io/repos/github/sparques/hamirc/badge.svg?branch=master)](https://coveralls.io/github/sparques/hamirc?branch=master)
[![Go ReportCard](https://goreportcard.com/badge/sparques/hamirc)](https://goreportcard.com/report/sparques/hamirc)
[![GoDoc](https://godoc.org/github.com/golang/gddo?status.svg)](https://pkg.go.dev/github.com/sparques/hamirc)

# HAMIRC

hamirc is a minimal IRC server that can send and receive messages via a KISS TNC. 

hamirc **should** work well with both point-to-point simplex and with repeaters.

# Demo

<youtube link goes here>

# How? 

## Technical Details

hamirc keeps almost everything local. Everything is decentralized. Users and channels are tracked locally. IRC PRIVMSGs are transmitted over the air and newly seen channels and users are automatically added to the server.

Much of this works because one can assume operators operating in good faith, as licensees risk their license, unlike the global Internet. 

### Features
1. Channels and PMs work as expected
2. UTF-8 Unicode is supported (including emojis 👍)
3. Multiple "local" users supported
4. User queries and DCC/CTCP are explicitly not supported (why would you even try over 1200 baud?!)
5. Server-state persistence: user lists and channels are preserved between restarts
6. Retro-tastic chatting fun

### Pipeline

Here's the setup:

	[IRC Client of your Choice] <--> [hamirc] <--> [TNC] <--> [Ham Radio]

The TNC I use (and you should probably use as well) is Direwolf with a CM108-based USB soundcard, modified to have PTT capability.

### Protocol

# Getting Started

1. Download a hamirc release or compile for yourself.
2. Get your radio and TNC ready.
  - If you're in ITU region 2, set radio to 145.5MHz (NB: still figuring best frequency to use)
  	- Check your local band plan for the best frequency to use and coordinate with your fellow hams.
  - For direwolf, use the default options of 1200 baud AFSK 1200/2200
  - It's a fool's errand to rely on VOX for transmitting, be sure you have PTT ability with your TNC unless you just want to monitor.
3. Start hamirc.
  - hamirc will connect to direwolf via localhost:8001 and start listening for IRC connections on port 6667.
4. Setup your IRC client
  - Set your nick to whatever you want
  - Set your username / ident to your callsign (this is important, we're relying on this to serve as radio identification)
  - Set your real name. You may not want to, but remember your real name can be found via your callsign, so why not make it easier for everyone else?
  - Connect to localhost:6667
5. You should be good to go.

## Runtime Options

Common options:

- `-tnc`: KISS TNC address. Defaults to `:8001`. Use `/dev/path:baud`, `COM3`, `COM3:baud`, or `serial:path:baud` for a serial device.
- `-tncport`: KISS TNC port, `0` through `7`. Defaults to `0`.
- `-serve`: IRC listen address. Defaults to `:6667`.
- `-name`: server name sent to IRC clients. Defaults to `hamirc`.
- `-state`: server state file path. Defaults to `serverState.json`.
- `-persist`: load and save users, channels, and topics. Defaults to `true`.
- `-mustload`: exit if an existing state file cannot be loaded. Defaults to `true`.
- `-autojoin`: automatically join local users to channels heard over radio. Defaults to `true`.
- `-debug`: log raw IRC and TNC traffic. Defaults to `false`.

The default behavior is to automatically add all "local users" (that is, users connected via an IRC client on the localhost) to any channel for which a message is received. 

This feature, AutoJoin, can be disabled and hamirc will still track channels in the background for which it has received a message. These channels can be viewed with the standard IRC /LIST command. Chances are traffic will be light enough it's best to leave AutoJoin on so you can see what's going on.

If you want to join a channel and see if anyone's around, you can simply do a "/JOIN #channel" and send a message.

hamirc implements a very limited subset of the IRC protocol. Please file an issue if your preferred IRC client has any major issues. Thus far, testing has been done with konversation, kvirc, weechat, and irssi. Corner cases still abound, so file those issues.

# Why?

Why not? IRC is a very simple, text oriented protocol. There is a plethora of clients available. Practically speaking, only PRIVMSGs need to be pushed out over the air and, with a slight change in field use, they already contain all the information needed.

Ultimately, it's because I wanted to. I like IRC and I like ham radio, and I wish there were more things like hamirc in existence. So I made it exist.

# But what about...

If you see a flaw in this, I'm more than happy to accept pull requests or discuss how things should work via a github issue.

## Windows Notes

Direwolf's TCP KISS interface works the same as on other platforms:

```
hamirc.exe -tnc localhost:8001 -serve :6667
```

Serial KISS TNCs can be opened directly with Windows COM names:

```
hamirc.exe -tnc COM3
hamirc.exe -tnc COM3:57600
hamirc.exe -tnc \\.\COM12:57600
```

The explicit `serial:` prefix can be used when you want to force serial handling:

```
hamirc.exe -tnc serial:COM3:57600
```

The default serial baud rate is 115200.

# Helping

There should be better Windows testing. This whole thing is already pretty niche--it will take a licensed Amateur Radio operator who is also interested in IRC. If that is further restricted to Linux users, I suspect I'll be forever be idling in empty channels.

So better support of windows is where I could stand the most help if you're interested. Other than that, testing out IRC clients and just using hamirc will be a massive help.

# Questions for Users (for you)

1. Do you think 1200 baud AFSK 1200/2200 is the right choice? 
	- IRC messages are pretty short and there's no CRC or FEC in use. My intent is to use VHF and UHF, so the slower, typically used with VHF/UHF 1200 baud AFSK encoding makes sense to me.
2. What frequency or frequencies should be standardized? 
	- I've been testing via 145.5 MHz and no one's come on to blast me, so either my area's exceptionally quiet on this frequency or this is a good one to use. I am in ITU Region 2 (North America) where digital communication is permitted on the simplex calling frequence (145.5MHz). Check your local band plan / country specific regulations to make sure you're staying legal.
3. I don't think hamirc should be linked with any real, internet connected IRC servers. It may be okay to use with Internet-linked repeaters.
	- If you disagree, please let me know why.
