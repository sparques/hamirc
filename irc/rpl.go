package irc

// IRC RPL_ numeric constants
const (
	RPL_WELCOME  = "001" // Welcome to the Internet Relay Network
	RPL_YOURHOST = "002" // Your host is running version...
	RPL_CREATED  = "003" // Server creation date
	RPL_MYINFO   = "004" // Server information
	RPL_ISUPPORT = "005" // Supported server features
	RPL_BOUNCE   = "010" // Bounce to a different server

	RPL_USERHOST = "302" // User host information
	RPL_ISON     = "303" // ISON response
	RPL_AWAY     = "301" // Away message
	RPL_UNAWAY   = "305" // You are no longer marked as away
	RPL_NOWAWAY  = "306" // You have been marked as away

	RPL_WHOISUSER     = "311" // WHOIS user information
	RPL_WHOISSERVER   = "312" // WHOIS server information
	RPL_WHOISOPERATOR = "313" // WHOIS operator status
	RPL_WHOISIDLE     = "317" // WHOIS idle time
	RPL_ENDOFWHOIS    = "318" // End of WHOIS list
	RPL_WHOISCHANNELS = "319" // Channels the user is on

	RPL_LISTSTART     = "321" // Start of channel listing
	RPL_LIST          = "322" // Channel listing
	RPL_LISTEND       = "323" // End of channel listing
	RPL_CHANNELMODEIS = "324" // Channel mode
	RPL_CREATIONTIME  = "329" // Channel creation time

	RPL_NOTOPIC      = "331" // No topic is set
	RPL_TOPIC        = "332" // Channel topic
	RPL_TOPICWHOTIME = "333" // Who set the topic and when (often non-standard)

	RPL_INVITING  = "341" // Invitation to channel
	RPL_SUMMONING = "342" // Summoning a user (obsolete)

	RPL_VERSION  = "351" // Server version
	RPL_WHOREPLY = "352" // WHO reply
	RPL_ENDOFWHO = "315" // End of WHO list

	RPL_NAMREPLY   = "353" // Names in a channel
	RPL_ENDOFNAMES = "366" // End of NAMES list

	RPL_LINKS      = "364" // Links list
	RPL_ENDOFLINKS = "365" // End of LINKS list

	RPL_BANLIST      = "367" // Ban list
	RPL_ENDOFBANLIST = "368" // End of ban list
	RPL_INFO         = "371" // INFO response
	RPL_MOTD         = "372" // Message of the Day
	RPL_ENDOFINFO    = "374" // End of INFO list
	RPL_MOTDSTART    = "375" // Start of MOTD
	RPL_ENDOFMOTD    = "376" // End of MOTD

	RPL_YOUREOPER = "381" // You are now an IRC operator
	RPL_REHASHING = "382" // Rehashing server configuration
	RPL_TIME      = "391" // Server time

	RPL_USERSTART  = "392" // Start of user listing (obsolete)
	RPL_USERS      = "393" // User list (obsolete)
	RPL_ENDOFUSERS = "394" // End of user listing (obsolete)
	RPL_NOUSERS    = "395" // No users (obsolete)
)

const (
	ERR_NOSUCHNICK        = "401"
	ERR_NOSUCHCHANNEL     = "403"
	ERR_NONICKNAMEGIVEN   = "431"
	ERR_NOTONCHANNEL      = "442"
	ERR_NOTREGISTERED     = "451"
	ERR_NEEDMOREPARAMS    = "461"
	ERR_ALREADYREGISTERED = "462"
	ERR_UNKNOWNMODE       = "472"
	ERR_CHANOPRIVSNEEDED  = "482"
)
