package netconn

import "github.com/dm-vev/adamant/net/metrics"

// Metrics of the logins of the process.
var (
	loginsStarted = metrics.Default.Counter("adamant_logins_started_total",
		"Clients that opened a connection and began to log in.")
	loginsDone = metrics.Default.Counter("adamant_logins_total",
		"Clients that logged in and took their resource packs.")
	loginsFailed = metrics.Default.Counter("adamant_logins_failed_total",
		"Clients whose login failed or that left during it.")
	loginsFull = metrics.Default.Counter("adamant_logins_refused_full_total",
		"Clients refused for the server was full.")
	loginSeconds = metrics.Default.Histogram("adamant_login_seconds",
		"Time from the connection opening to the client logged in.",
		0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30)
	undecodable = metrics.Default.Counter("adamant_packets_undecodable_total",
		"Packets of clients that did not decode, each closing its connection.")
)
