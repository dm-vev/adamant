package title

import (
	"fmt"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/text"
)

// Title represents a title that may be sent to the player. The title will show up as large text in the middle
// of the screen, with optional subtitle and action text.
type Title struct {
	text, subtitle, actionText                text.Text
	fadeInDuration, fadeOutDuration, duration time.Duration
}

// New returns a new title using the text passed. The text is formatted according to the formatting rules of
// fmt.Sprintln, but with no newline at the end, and read as a legacy string, with § codes; a lone text.Text
// is taken as it is.
// The title has default durations set, which will generally suffice.
func New(a ...any) Title {
	return Title{
		text:            formatText(a),
		fadeInDuration:  time.Second / 20,
		fadeOutDuration: time.Second / 20,
		duration:        time.Second * 2,
	}
}

// Text returns the text of the title, as passed to New when created. It is the zero text for a title of
// no text.
func (title Title) Text() text.Text {
	return title.text
}

// WithSubtitle sets the subtitle of the title. The text passed will be formatted according to the formatting
// rules of fmt.Sprintln, but without the newline, and read as a legacy string; a lone text.Text is taken as
// it is.
// The subtitle is shown under the title in a somewhat smaller font.
// The new Title with the subtitle is returned.
func (title Title) WithSubtitle(a ...any) Title {
	title.subtitle = formatText(a)
	return title
}

// Subtitle returns the subtitle of the title, as passed to WithSubtitle. Subtitle returns the zero text if
// no subtitle was previously set.
func (title Title) Subtitle() text.Text {
	return title.subtitle
}

// WithActionText sets the action text of the title. This text is roughly the same as sending a tip/popup, but
// will synchronise with the title.
// WithActionText will format the text passed using the formatting rules of fmt.Sprintln, but without newline,
// and read it as a legacy string; a lone text.Text is taken as it is.
// The new Title with the action text is returned.
func (title Title) WithActionText(a ...any) Title {
	title.actionText = formatText(a)
	return title
}

// ActionText returns the action text added to the title. This text is roughly the same as sending a tip, but
// will synchronise with the title. By default, the action text is the zero text.
func (title Title) ActionText() text.Text {
	return title.actionText
}

// Duration returns the duration that the title will be visible for, without fading in or out. By default,
// this is two seconds.
func (title Title) Duration() time.Duration {
	return title.duration
}

// WithDuration sets the duration that the title will be visible for without fading in or fading out.
// The new Title with the duration is returned.
func (title Title) WithDuration(d time.Duration) Title {
	title.duration = d
	return title
}

// WithFadeInDuration sets the duration that the title takes to fade in on the screen.
// The new Title with the fade-in duration is returned.
func (title Title) WithFadeInDuration(d time.Duration) Title {
	title.fadeInDuration = d
	return title
}

// FadeInDuration returns the duration that the fade-in of the title takes. By default, this is a quarter of
// a second.
func (title Title) FadeInDuration() time.Duration {
	return title.fadeInDuration
}

// WithFadeOutDuration sets the duration that the title takes to fade out of the screen.
// The new Title with the fade-out duration is returned.
func (title Title) WithFadeOutDuration(d time.Duration) Title {
	title.fadeOutDuration = d
	return title
}

// FadeOutDuration returns the duration that the fade-out of the title takes.By default, this is a quarter of
// a second.
func (title Title) FadeOutDuration() time.Duration {
	return title.fadeOutDuration
}

// format is a utility function to format a list of values to have spaces between them, but no newline at the
// end.
func format(a []any) string {
	return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintln(a...), "\n"), "\n")
}

// formatText returns the values a as text: a lone text.Text as it is, any
// other values formatted by format and read as a legacy string, with § codes.
// The zero text is returned for values that format to an empty string.
func formatText(a []any) text.Text {
	if len(a) == 1 {
		if t, ok := a[0].(text.Text); ok {
			return t
		}
	}
	return text.ParseLegacy(format(a))
}
