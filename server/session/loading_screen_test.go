package session

import (
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

func TestLoadingScreenAcknowledgementOrdering(t *testing.T) {
	for _, test := range []struct {
		name               string
		expected, received uint32
		complete, invalid  bool
	}{
		{name: "current completes transition", expected: 12, received: 12, complete: true},
		{name: "previous leaves current transition pending", expected: 12, received: 11},
		{name: "future remains invalid", expected: 12, received: 13, invalid: true},
		{name: "previous across sequence wrap", expected: 1, received: ^uint32(0)},
		{name: "future across sequence wrap", expected: ^uint32(0), received: 1, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := &Session{}
			s.changingDimension.Store(true)
			h := &ServerBoundLoadingScreenHandler{}
			h.expectedID.Store(test.expected)
			err := h.Handle(&v2193.ServerBoundLoadingScreenPacket{Type: v2193.LoadingScreenTypeEnd, LoadingScreenID: proto.Some(test.received)}, s, nil, nil)
			if (err != nil) != test.invalid {
				t.Fatalf("acknowledgement error = %v, invalid = %v", err, test.invalid)
			}
			if got := s.ChangingDimension(); got == test.complete {
				t.Fatalf("changing dimension = %v, complete = %v", got, test.complete)
			}
			wantID := test.expected
			if test.complete {
				wantID = 0
			}
			if got := h.expectedID.Load(); got != wantID {
				t.Fatalf("pending loading screen = %d, want %d", got, wantID)
			}
		})
	}
}
