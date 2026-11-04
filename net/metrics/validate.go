package metrics

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	metricName = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
	labelName  = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// Validate checks that r holds an exposition of the text format of
// Prometheus that a scraper reads, as the registries write it: each family
// once, with one HELP and one TYPE line before its samples; sample lines of
// the grammar, with label values of no escape other than \\, \" and \n; and
// histograms whose buckets of each set of labels rise, end in +Inf, and
// whose +Inf bucket is their count. It returns the first problem, with its
// line.
func Validate(r io.Reader) error {
	v := validator{seen: map[string]bool{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for n := 1; sc.Scan(); n++ {
		if err := v.line(sc.Text()); err != nil {
			return fmt.Errorf("line %d: %q: %v", n, sc.Text(), err)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return v.endFamily()
}

// validator is the state of Validate: the family whose lines it reads.
type validator struct {
	seen map[string]bool
	name string
	typ  string
	help bool
	// buckets holds, for each set of labels of a histogram without le, the
	// last bucket, and whether +Inf and the count came.
	buckets map[string]*bucketState
}

type bucketState struct {
	last         float64
	le           float64
	inf, counted bool
	infV         float64
}

func (v *validator) line(l string) error {
	switch {
	case l == "":
		return fmt.Errorf("empty line")
	case strings.HasPrefix(l, "# HELP "):
		name, _, _ := strings.Cut(strings.TrimPrefix(l, "# HELP "), " ")
		if err := v.startFamily(name); err != nil {
			return err
		}
		if v.help {
			return fmt.Errorf("second HELP of %s", name)
		}
		v.help = true
		return nil
	case strings.HasPrefix(l, "# TYPE "):
		name, typ, _ := strings.Cut(strings.TrimPrefix(l, "# TYPE "), " ")
		if err := v.startFamily(name); err != nil {
			return err
		}
		if v.typ != "" {
			return fmt.Errorf("second TYPE of %s", name)
		}
		switch typ {
		case "counter", "gauge", "histogram", "summary", "untyped":
		default:
			return fmt.Errorf("unknown type %q", typ)
		}
		v.typ = typ
		return nil
	case strings.HasPrefix(l, "#"):
		return nil
	}
	return v.sample(l)
}

// startFamily begins the family name, unless its lines are those read.
func (v *validator) startFamily(name string) error {
	if name == v.name {
		return nil
	}
	if err := v.endFamily(); err != nil {
		return err
	}
	if !metricName.MatchString(name) {
		return fmt.Errorf("bad metric name %q", name)
	}
	if v.seen[name] {
		return fmt.Errorf("family %s twice", name)
	}
	v.seen[name] = true
	v.name, v.typ, v.help, v.buckets = name, "", false, map[string]*bucketState{}
	return nil
}

// endFamily checks the histograms of the family read.
func (v *validator) endFamily() error {
	for labels, b := range v.buckets {
		if !b.inf {
			return fmt.Errorf("histogram %s{%s} has no +Inf bucket", v.name, labels)
		}
		if !b.counted {
			return fmt.Errorf("histogram %s{%s} has no count", v.name, labels)
		}
	}
	return nil
}

func (v *validator) sample(l string) error {
	if v.name == "" || v.typ == "" || !v.help {
		return fmt.Errorf("sample before the HELP and TYPE of its family")
	}
	name, rest := l, ""
	var labels []string
	if i := strings.IndexAny(l, "{ "); i >= 0 {
		name, rest = l[:i], l[i:]
	}
	if strings.HasPrefix(rest, "{") {
		var err error
		labels, rest, err = parseLabels(rest[1:])
		if err != nil {
			return err
		}
	}
	value, ok := strings.CutPrefix(rest, " ")
	if !ok {
		return fmt.Errorf("no value")
	}
	if i := strings.IndexByte(value, ' '); i >= 0 {
		// A timestamp.
		if _, err := strconv.ParseInt(value[i+1:], 10, 64); err != nil {
			return fmt.Errorf("bad timestamp")
		}
		value = value[:i]
	}
	f, err := parseValue(value)
	if err != nil {
		return err
	}
	suffix, ok := strings.CutPrefix(name, v.name)
	if !ok {
		return fmt.Errorf("sample %s outside its family %s", name, v.name)
	}
	if v.typ != "histogram" {
		if suffix != "" {
			return fmt.Errorf("sample %s outside its family %s", name, v.name)
		}
		if v.typ == "counter" && f < 0 {
			return fmt.Errorf("negative counter")
		}
		return nil
	}
	var le string
	key := make([]string, 0, len(labels))
	for i := 0; i < len(labels); i += 2 {
		if labels[i] == "le" {
			le = labels[i+1]
			continue
		}
		key = append(key, labels[i]+"="+labels[i+1])
	}
	k := strings.Join(key, ",")
	b := v.buckets[k]
	if b == nil {
		b = &bucketState{le: math.Inf(-1)}
		v.buckets[k] = b
	}
	switch suffix {
	case "_bucket":
		bound, err := parseValue(le)
		if le == "" || err != nil {
			return fmt.Errorf("bucket without a bound le")
		}
		if b.inf || bound <= b.le {
			return fmt.Errorf("bucket le=%s after le=%v", le, b.le)
		}
		if f < b.last {
			return fmt.Errorf("bucket le=%s holds less than the one before", le)
		}
		b.le, b.last = bound, f
		if math.IsInf(bound, 1) {
			b.inf, b.infV = true, f
		}
	case "_count":
		if !b.inf || f != b.infV {
			return fmt.Errorf("count %v is not the +Inf bucket %v", f, b.infV)
		}
		b.counted = true
	case "_sum":
	default:
		return fmt.Errorf("sample %s outside its family %s", name, v.name)
	}
	return nil
}

// parseLabels parses `name="value",...}` and returns the pairs and what
// follows the brace.
func parseLabels(s string) (pairs []string, rest string, err error) {
	for {
		if strings.HasPrefix(s, "}") {
			return pairs, s[1:], nil
		}
		eq := strings.Index(s, `="`)
		if eq < 0 {
			return nil, "", fmt.Errorf("bad labels")
		}
		name := s[:eq]
		if !labelName.MatchString(name) {
			return nil, "", fmt.Errorf("bad label name %q", name)
		}
		s = s[eq+2:]
		var b strings.Builder
		for {
			if s == "" {
				return nil, "", fmt.Errorf("unterminated label value")
			}
			c := s[0]
			s = s[1:]
			if c == '"' {
				break
			}
			if c == '\n' {
				return nil, "", fmt.Errorf("newline in a label value")
			}
			if c == '\\' {
				if s == "" {
					return nil, "", fmt.Errorf("unterminated label value")
				}
				switch s[0] {
				case '\\', '"':
					b.WriteByte(s[0])
				case 'n':
					b.WriteByte('\n')
				default:
					return nil, "", fmt.Errorf("escape \\%c in a label value", s[0])
				}
				s = s[1:]
				continue
			}
			b.WriteByte(c)
		}
		for i := 0; i < len(pairs); i += 2 {
			if pairs[i] == name {
				return nil, "", fmt.Errorf("label %s twice", name)
			}
		}
		pairs = append(pairs, name, b.String())
		s = strings.TrimPrefix(s, ",")
	}
}

func parseValue(s string) (float64, error) {
	switch s {
	case "+Inf":
		return math.Inf(1), nil
	case "-Inf":
		return math.Inf(-1), nil
	case "NaN":
		return math.NaN(), nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("bad value %q", s)
	}
	return f, nil
}
