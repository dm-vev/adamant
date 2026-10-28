package sctp

import (
	"encoding/binary"
	"errors"
	"slices"
	"time"
)

// Results of a re-configuration request (RFC 6525 section 4.4).
const (
	resultNothingToDo = 0
	resultPerformed   = 1
	resultDenied      = 2
	resultBadSequence = 5
	resultInProgress  = 6
)

// paramIncomingReset is a request to reset the streams this side sends on.
const paramIncomingReset = 14

var errResetInProgress = errors.New("sctp: a stream reset is in progress")

// resetState tracks the stream resets of both directions (RFC 6525).
type resetState struct {
	// nextOut is the sequence number of the next request of this side.
	nextOut uint32
	// out holds the streams of this side's request in progress, nil if
	// none; outSent is set once the request went out, with outSeq and the
	// last TSN it covers.
	out     []uint16
	outSent bool
	outSeq  uint32
	outLast uint32
	timer   time.Time

	// nextIn is the sequence number of the next request of the peer.
	nextIn uint32
	// waiting is a request of the peer waiting for the data sent before
	// it.
	waiting *incomingReset
}

type incomingReset struct {
	seq     uint32
	last    uint32
	streams []uint16
}

func (r *resetState) resetting(stream uint16) bool {
	return r.out != nil && slices.Contains(r.out, stream)
}

// ResetStreams resets streams this side sends on, as closing a data channel
// does (RFC 8832 section 6.7): the messages queued on them are sent first,
// then the peer is asked to reset them. Send fails on them until
// OnResetDone reports the reset.
func (a *Association) ResetStreams(streams []uint16, now time.Time) error {
	if a.err != nil {
		return a.err
	}
	if a.state != stateEstablished {
		return errState
	}
	if !a.reconfig {
		return errors.New("sctp: the peer does not support stream reset")
	}
	if a.reset.out != nil {
		return errResetInProgress
	}
	a.reset.out = slices.Clone(streams)
	a.reset.outSent = false
	a.sendResetRequestWhenReady(now)
	return nil
}

// sendResetRequestWhenReady sends this side's request once every message
// queued on its streams was cut into chunks, so that the last TSN it
// carries covers them.
func (a *Association) sendResetRequestWhenReady(now time.Time) {
	r := &a.reset
	if r.out == nil || r.outSent || !a.sending() {
		return
	}
	for _, m := range a.pending {
		if !m.abandoned && m.next < len(m.data) && slices.Contains(r.out, m.stream) {
			return
		}
	}
	r.outSent = true
	r.outSeq = r.nextOut
	r.nextOut++
	r.outLast = a.nextTSN - 1
	r.timer = now.Add(a.rto)
	a.queueResetRequest()
	a.flushControl()
}

func (a *Association) queueResetRequest() {
	r := &a.reset
	v := binary.BigEndian.AppendUint32(nil, r.outSeq)
	v = binary.BigEndian.AppendUint32(v, r.nextIn-1)
	v = binary.BigEndian.AppendUint32(v, r.outLast)
	for _, s := range r.out {
		v = binary.BigEndian.AppendUint16(v, s)
	}
	a.queueControl(chunkReconfig, 0, appendParam(nil, paramOutgoingReset, v))
}

func (a *Association) queueResetResponse(seq, result uint32) {
	v := binary.BigEndian.AppendUint32(nil, seq)
	v = binary.BigEndian.AppendUint32(v, result)
	a.queueControl(chunkReconfig, 0, appendParam(nil, paramReconfigResponse, v))
}

func (a *Association) handleReconfig(c chunk) {
	if a.state < stateEstablished {
		return
	}
	params, ok := parseParams(c.value)
	if !ok {
		return
	}
	for _, p := range params {
		switch p.typ {
		case paramOutgoingReset:
			a.handleOutgoingReset(p.value)
		case paramIncomingReset:
			if len(p.value) >= 4 {
				// This side resets its streams when it closes channels,
				// not when asked to.
				a.queueResetResponse(binary.BigEndian.Uint32(p.value), resultDenied)
			}
		case paramReconfigResponse:
			a.handleResetResponse(p.value)
		}
	}
}

// handleOutgoingReset handles the peer resetting streams it sends on. The
// reset waits for the data the peer sent before it; a request seen before
// is answered again without being applied twice.
func (a *Association) handleOutgoingReset(v []byte) {
	if len(v) < 12 || (len(v)-12)%2 != 0 {
		return
	}
	r := &a.reset
	seq := binary.BigEndian.Uint32(v)
	last := binary.BigEndian.Uint32(v[8:])
	switch {
	case seq == r.nextIn-1:
		// A retransmission of the request performed last.
		a.queueResetResponse(seq, resultPerformed)
		return
	case seq != r.nextIn:
		a.queueResetResponse(seq, resultBadSequence)
		return
	case r.waiting != nil:
		a.queueResetResponse(seq, resultInProgress)
		return
	}
	var streams []uint16
	for off := 12; off < len(v); off += 2 {
		streams = append(streams, binary.BigEndian.Uint16(v[off:]))
	}
	r.waiting = &incomingReset{seq: seq, last: last, streams: streams}
	if !a.performPendingReset() {
		a.queueResetResponse(seq, resultInProgress)
	}
}

// performPendingReset applies the peer's waiting reset once the data sent
// before it arrived, and reports whether it did.
func (a *Association) performPendingReset() bool {
	w := a.reset.waiting
	if w == nil || tsnLess(a.peerCum, w.last) {
		return false
	}
	a.reset.waiting = nil
	a.reset.nextIn++
	streams := w.streams
	if len(streams) == 0 {
		// No stream named: every stream is reset.
		for s := range a.streams {
			streams = append(streams, s)
		}
		slices.Sort(streams)
	}
	for _, s := range streams {
		if in := a.streams[s]; in != nil {
			for _, f := range in.held {
				a.held -= len(f.data)
			}
			delete(a.streams, s)
		}
	}
	a.unordered = slices.DeleteFunc(a.unordered, func(f inFragment) bool {
		if slices.Contains(streams, f.stream) {
			a.held -= len(f.data)
			return true
		}
		return false
	})
	a.queueResetResponse(w.seq, resultPerformed)
	if a.cfg.OnStreamReset != nil {
		a.cfg.OnStreamReset(streams)
	}
	return true
}

// handleResetResponse handles the peer's answer to this side's request.
func (a *Association) handleResetResponse(v []byte) {
	r := &a.reset
	if len(v) < 8 || r.out == nil || !r.outSent || binary.BigEndian.Uint32(v) != r.outSeq {
		return
	}
	switch binary.BigEndian.Uint32(v[4:]) {
	case resultInProgress:
		// The timer sends the request again.
		return
	case resultPerformed, resultNothingToDo:
		streams := r.out
		for _, s := range streams {
			delete(a.ssn, s)
		}
		r.out, r.outSent, r.timer = nil, false, time.Time{}
		if a.cfg.OnResetDone != nil {
			a.cfg.OnResetDone(streams)
		}
	default:
		r.out, r.outSent, r.timer = nil, false, time.Time{}
	}
}
