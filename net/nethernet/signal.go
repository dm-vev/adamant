package nethernet

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Types of the signals, as the game words them.
const (
	// SignalOffer carries the offer of a client.
	SignalOffer = "CONNECTREQUEST"
	// SignalAnswer carries the answer of a server.
	SignalAnswer = "CONNECTRESPONSE"
	// SignalCandidate carries a candidate one side trickles.
	SignalCandidate = "CANDIDATEADD"
	// SignalError carries the code of an error of either side.
	SignalError = "CONNECTERROR"
)

// Signal is a message of the negotiation of a NetherNet connection that
// goes over a signaling service, as the game sends them over Xbox Live: an
// offer, an answer, a candidate or an error of one connection.
type Signal struct {
	Type         string
	ConnectionID uint64
	Data         string
	// NetworkID is the network of the peer: the one that sent the signal,
	// or the one it goes to.
	NetworkID string
}

// String returns the signal as it goes over a service: its type,
// connection ID and data, separated by spaces.
func (s Signal) String() string {
	return s.Type + " " + strconv.FormatUint(s.ConnectionID, 10) + " " + s.Data
}

var errSignal = errors.New("nethernet: malformed signal")

// ParseSignal parses a signal as String formats it, leaving its NetworkID
// empty. Unknown types, connection IDs that are not decimal and data past
// the size of a description are refused.
func ParseSignal(text string) (Signal, error) {
	if len(text) > maxDescription+64 {
		return Signal{}, errSignal
	}
	typ, rest, ok := strings.Cut(text, " ")
	if !ok {
		return Signal{}, errSignal
	}
	id, data, ok := strings.Cut(rest, " ")
	if !ok {
		return Signal{}, errSignal
	}
	switch typ {
	case SignalOffer, SignalAnswer, SignalCandidate, SignalError:
	default:
		return Signal{}, fmt.Errorf("%w: type %q", errSignal, typ)
	}
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return Signal{}, fmt.Errorf("%w: connection ID %q", errSignal, id)
	}
	return Signal{Type: typ, ConnectionID: n, Data: data}, nil
}

// Signaling carries the signals of NetherNet connections over a service
// rather than the HTTP of a Listener, as Xbox Live does.
type Signaling interface {
	// ReadSignal returns the next signal a peer sent, with the network it
	// came from, and an error once the signaling ended.
	ReadSignal() (Signal, error)
	// WriteSignal sends a signal to the network of its NetworkID.
	WriteSignal(s Signal) error
}
