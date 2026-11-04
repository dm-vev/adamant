package server

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/audit"
	"github.com/df-mc/dragonfly/server/config"
)

// banFields are what a ban holds beside the player or the address, as
// Java's banned-players.json writes them: when it was made, by whom, and
// until when. A zero time is unknown for created and for ever for expires.
type banFields struct {
	created time.Time
	source  string
	expires time.Time
}

// expired reports whether the ban stopped applying at now.
func (f banFields) expired(now time.Time) bool {
	return !f.expires.IsZero() && !f.expires.After(now)
}

// banTime is a time of banned-players.json: RFC 3339 in UTC, as written;
// Java's "2006-01-02 15:04:05 -0700" and "forever" are read too, so that an
// entry pasted from Java reads. The zero time is left out of the file.
type banTime struct{ time.Time }

// javaBanTime is the layout of Java's BanListEntry.DATE_FORMAT.
const javaBanTime = "2006-01-02 15:04:05 -0700"

func (t banTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.UTC().Format(time.RFC3339))
}

func (t *banTime) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "forever") {
		t.Time = time.Time{}
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, javaBanTime} {
		if v, err := time.Parse(layout, s); err == nil {
			t.Time = v.UTC()
			return nil
		}
	}
	return fmt.Errorf("ban time %q: want RFC 3339, such as 2026-11-04T14:20:31Z", s)
}

// BanOptions are what Server.Ban records of a ban beside the player or the
// address.
type BanOptions struct {
	// Reason is why the player is banned; [Bans] DefaultReason is shown if
	// empty.
	Reason string
	// By is who bans: the source of the ban as /banlist shows it. A zero
	// actor is code calling the server, "API".
	By audit.Actor
	// Expires is when the ban stops applying; zero bans for ever, unless
	// For is set.
	Expires time.Time
	// For, if not zero, bans for this long from now, in place of Expires.
	For time.Duration
}

// fields returns the fields of a ban of the options, made at now.
func (o BanOptions) fields(now time.Time) banFields {
	by := o.By
	if by.Kind == "" {
		by.Kind = audit.KindAPI
	}
	now = now.UTC().Truncate(time.Second)
	expires := o.Expires.UTC()
	if o.For != 0 {
		expires = now.Add(o.For)
	}
	return banFields{created: now, source: by.String(), expires: expires}
}

// checkExpiry returns an error if the ban of f ends in less than a minute
// from its making, or after max from it, max zero being no limit.
func (f banFields) checkExpiry(max time.Duration) error {
	if f.expires.IsZero() {
		return nil
	}
	d := f.expires.Sub(f.created)
	if d < time.Minute {
		return fmt.Errorf("a ban lasts a minute at least, not %v", d)
	}
	if max > 0 && d > max {
		return fmt.Errorf("a ban lasts %v at most ([Bans] MaxDuration), not %v", max, d)
	}
	return nil
}

// BanChange is what Ban and BanAddress changed.
type BanChange uint8

const (
	// BanUnchanged is a ban of a player or an address banned for ever.
	BanUnchanged BanChange = iota
	// BanAdded is a new ban.
	BanAdded
	// BanMadePermanent is a ban for ever of a player or an address banned
	// for a time.
	BanMadePermanent
	// BanUpdated is a ban for a time of a player or an address banned: its
	// expiry, reason and source are replaced.
	BanUpdated
)

// reban returns the ban of the fields and the reason passed made over the
// ban old, and what it changes: a ban for ever over a ban for a time makes
// it for ever; a ban for a time replaces the expiry; either takes the new
// source and time, and the new reason if one is given. A ban for ever over
// a ban for ever changes nothing.
func reban(old entry, f banFields, reason string) (entry, BanChange) {
	if f.expires.IsZero() && old.ban.expires.IsZero() {
		return old, BanUnchanged
	}
	change := BanUpdated
	if f.expires.IsZero() {
		change = BanMadePermanent
	}
	en := old
	en.ban = f
	if reason != "" {
		en.reason = reason
	}
	return en, change
}

// banDuration matches a duration of a ban: numbers each followed by a
// unit of Go, or d for days and w for weeks.
var banDuration = regexp.MustCompile(`^(?:\d+(?:\.\d+)?(?:w|d|h|ms|m|s))+$`)

// banDurationPart matches one number and its unit of a duration of a ban.
var banDurationPart = regexp.MustCompile(`(\d+(?:\.\d+)?)(w|d|h|ms|m|s)`)

// banUnits are the units of a duration of a ban.
var banUnits = map[string]time.Duration{
	"w": 7 * 24 * time.Hour, "d": 24 * time.Hour, "h": time.Hour, "m": time.Minute, "s": time.Second, "ms": time.Millisecond,
}

// ParseBanDuration parses how long a ban lasts, as /tempban takes it: Go's
// units plus d (24 hours) and w (7 days), in any combination, such as 90m,
// 12h, 7d, 1w2d or 1d12h30m. It is a minute at least.
func ParseBanDuration(s string) (time.Duration, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if !banDuration.MatchString(v) {
		return 0, fmt.Errorf("duration %q: want such as 90m, 12h, 7d or 1w2d", s)
	}
	var d time.Duration
	for _, m := range banDurationPart.FindAllStringSubmatch(v, -1) {
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0, fmt.Errorf("duration %q: %w", s, err)
		}
		d += time.Duration(n * float64(banUnits[m[2]]))
	}
	if d < time.Minute {
		return 0, fmt.Errorf("duration %q: a ban lasts a minute at least", s)
	}
	return d, nil
}

// BanInfo is a ban, as /banlist shows it.
type BanInfo struct {
	// Target is the player, as the commands show it, or the address.
	Target string
	// IP reports whether the ban is of an address.
	IP bool
	// Reason is why, empty if no reason was given.
	Reason string
	// Source is who banned, "(Unknown)" if unknown.
	Source string
	// Created and Expires are when the ban was made, zero if unknown, and
	// when it ends, zero for ever.
	Created, Expires time.Time
	// Until is Expires as [Bans] TimeFormat writes it, empty for ever.
	Until string
}

// BanMessages are the texts a banned player is refused and kicked with, as
// [Bans] of config.toml sets them: Message for a ban of the player,
// IPMessage for a ban of its address, each followed by ReasonMessage if
// the ban has a reason, or DefaultReason is set, and by ExpiresMessage if
// it expires. They may hold {reason}, {source}, {created} and {expires},
// the times written in TimeFormat, a layout of package time.
type BanMessages struct {
	Message, IPMessage, ReasonMessage, ExpiresMessage, DefaultReason, TimeFormat string
}

// DefaultBanMessages returns the texts the server has always refused
// banned players with, and the line of the expiry of a temporary ban.
func DefaultBanMessages() BanMessages {
	return BanMessages{
		Message:        "You are banned from this server.",
		IPMessage:      "You are banned from this server.",
		ReasonMessage:  " Reason: {reason}",
		ExpiresMessage: "\nYour ban will be removed on {expires}",
		TimeFormat:     "2006-01-02 at 15:04:05 MST",
	}
}

// banPlaceholder matches a placeholder of a text of BanMessages.
var banPlaceholder = regexp.MustCompile(`\{[^{}]*\}`)

// banPlaceholders are the placeholders the texts of BanMessages know.
var banPlaceholders = []string{"{reason}", "{source}", "{created}", "{expires}"}

// format returns the text a player of the ban passed is refused with, of a
// ban of its address if ip.
func (m BanMessages) format(reason string, f banFields, ip bool) string {
	if reason == "" {
		reason = m.DefaultReason
	}
	msg := m.Message
	if ip {
		msg = m.IPMessage
	}
	if reason != "" {
		msg += m.ReasonMessage
	}
	if !f.expires.IsZero() {
		msg += m.ExpiresMessage
	}
	when := func(t time.Time) string {
		if t.IsZero() {
			return "(Unknown)"
		}
		return t.Local().Format(m.TimeFormat)
	}
	source := f.source
	if source == "" {
		source = "(Unknown)"
	}
	return strings.NewReplacer("{reason}", reason, "{source}", source, "{created}", when(f.created), "{expires}", when(f.expires)).Replace(msg)
}

// checkBanMessages adds the problems of the texts of [Bans] to p: Message
// and IPMessage are set, every text holds only known placeholders, and
// TimeFormat is a layout.
func checkBanMessages(p *config.Problems, m BanMessages) {
	for _, t := range []struct{ key, value string }{
		{"Bans.Message", m.Message}, {"Bans.IPMessage", m.IPMessage}, {"Bans.ReasonMessage", m.ReasonMessage},
		{"Bans.ExpiresMessage", m.ExpiresMessage}, {"Bans.DefaultReason", m.DefaultReason},
	} {
		if (t.key == "Bans.Message" || t.key == "Bans.IPMessage") && strings.TrimSpace(t.value) == "" {
			p.Errorf(t.key, "%s is empty: want the text a banned player is refused with", t.key)
			continue
		}
		for _, ph := range banPlaceholder.FindAllString(t.value, -1) {
			known := false
			for _, k := range banPlaceholders {
				known = known || ph == k
			}
			if !known {
				p.Errorf(t.key, "%s holds %s: want only %s", t.key, ph, strings.Join(banPlaceholders, ", "))
			}
		}
	}
	at := time.Date(2026, 11, 4, 14, 20, 31, 0, time.UTC)
	if f := strings.TrimSpace(m.TimeFormat); f == "" || at.Format(m.TimeFormat) == m.TimeFormat {
		p.Errorf("Bans.TimeFormat", `Bans.TimeFormat = %q: want a layout of Go's package time, such as "2006-01-02 at 15:04:05 MST"`, m.TimeFormat)
	}
}
