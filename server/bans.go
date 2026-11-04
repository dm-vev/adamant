package server

import (
	"encoding/json"
	"fmt"
	"regexp"
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
	// Expires is when the ban stops applying; zero bans for ever.
	Expires time.Time
}

// fields returns the fields of a ban of the options, made at now.
func (o BanOptions) fields(now time.Time) banFields {
	by := o.By
	if by.Kind == "" {
		by.Kind = audit.KindAPI
	}
	return banFields{created: now.UTC().Truncate(time.Second), source: by.String(), expires: o.Expires.UTC()}
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
