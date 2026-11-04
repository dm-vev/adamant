package main

import (
	"cmp"
	"encoding"
	"reflect"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/config"
	"github.com/pelletier/go-toml"
)

// keyScan is what scanKeys finds of the keys of a file: where the keys the
// parts read are, and the keys and tables no part reads.
type keyScan struct {
	// lines holds the line of each key a part reads, by the name of the
	// key in the part: its table and its field joined by dots, as
	// "Players.MaxCount", however the file writes its case.
	lines map[string]int
	// unknown are the keys and tables no part reads, as warnings, each
	// table once rather than with its keys.
	unknown []config.Problem
}

// scanKeys finds the keys of tree that the parts passed read, which are
// pointers to structs that go-toml decodes the file into, and those that
// none reads. The keys a part reads are those of its fields, matched as
// go-toml matches them, so that the set of keys is of the structs of the
// parts rather than a list kept beside them:
//
//   - a field reads the key of its toml tag, or of its name, written as
//     go-toml finds it: as is, in lower case, in upper case or with the
//     first letter in lower case;
//   - the fields of an embedded struct read keys of the table of the struct
//     that embeds it, and unexported fields and fields tagged "-" read none;
//   - a struct reads a table, and a slice of structs an array of tables;
//   - a part that reads the keys of a table dynamically reads them all: a
//     map, an interface, a toml.Tree, or a type with an UnmarshalTOML
//     method of its own.
//
// Two parts may read keys of the same table: a key is known if any reads it.
func scanKeys(tree *toml.Tree, parts []any) keyScan {
	s := keyScan{lines: map[string]int{}}
	types := make([]reflect.Type, 0, len(parts))
	for _, p := range parts {
		types = append(types, reflect.TypeOf(p).Elem())
	}
	s.walk(tree, types, nil, nil)
	slices.SortStableFunc(s.unknown, func(a, b config.Problem) int { return cmp.Compare(a.Line, b.Line) })
	return s
}

// tomlField is a field of a struct that reads a key of a table.
type tomlField struct {
	name string
	typ  reflect.Type
}

// walk scans the keys of the table tree, which the structs of types read.
// path is where the table is in the file, as the file writes it, and canon
// where it is in the parts.
func (s *keyScan) walk(tree *toml.Tree, types []reflect.Type, path, canon []string) {
	// claims holds the types of the fields that read each key of tree, and
	// names the name of the key in the parts.
	claims, names := map[string][]reflect.Type{}, map[string]string{}
	var siblings []string
	for _, t := range types {
		for _, f := range tomlFields(t) {
			siblings = append(siblings, f.name)
			for _, k := range []string{f.name, strings.ToLower(f.name), strings.ToTitle(f.name), strings.ToLower(f.name[:1]) + f.name[1:]} {
				if tree.HasPath([]string{k}) {
					claims[k] = append(claims[k], f.typ)
					names[k] = f.name
					break
				}
			}
		}
	}
	for _, k := range tree.Keys() {
		v := tree.GetPath([]string{k})
		line := tree.GetPositionPath([]string{k}).Line
		if items, ok := v.([]*toml.Tree); ok && len(items) > 0 {
			// The first table of the array, rather than the last.
			line = items[0].Position().Line
		}
		at := appendPath(path, k)
		fieldTypes, ok := claims[k]
		if !ok {
			kind := "key"
			switch v.(type) {
			case *toml.Tree, []*toml.Tree:
				kind = "table"
			}
			msg := "unknown " + kind + " " + strings.Join(at, ".") + ": nothing reads it"
			if guess := closest(k, siblings); guess != "" {
				msg += ", did you mean " + guess + "?"
			}
			s.unknown = append(s.unknown, config.Problem{Line: line, Message: msg, Warning: true})
			continue
		}
		name := appendPath(canon, names[k])
		s.lines[strings.Join(name, ".")] = line
		switch v := v.(type) {
		case *toml.Tree:
			if next, ok := tableTypes(fieldTypes, false); ok {
				s.walk(v, next, at, name)
			}
		case []*toml.Tree:
			if next, ok := tableTypes(fieldTypes, true); ok {
				for _, item := range v {
					s.walk(item, next, at, name)
				}
			}
		}
	}
}

// appendPath returns path with k after it, in a slice of its own.
func appendPath(path []string, k string) []string {
	return append(path[:len(path):len(path)], k)
}

// tomlFields returns the fields of the struct t that read keys of its
// table, with those of embedded structs, as go-toml decodes them.
func tomlFields(t reflect.Type) []tomlField {
	if t.Kind() != reflect.Struct || dynamic(t) {
		return nil
	}
	var fields []tomlField
	for i := range t.NumField() {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		name := f.Name
		if tag, _, _ := strings.Cut(f.Tag.Get("toml"), ","); tag == "-" {
			continue
		} else if tag = strings.TrimSpace(tag); tag != "" {
			name = tag
		}
		fields = append(fields, tomlField{name: name, typ: f.Type})
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			fields = append(fields, tomlFields(f.Type)...)
		}
	}
	return fields
}

// tableTypes returns the structs the fields of the types passed decode a
// table into, or, if array, each table of an array of tables. It returns
// false if a field reads the keys of the table dynamically, so that they
// all are known, or reads no table, so that decoding refuses the file.
func tableTypes(fieldTypes []reflect.Type, array bool) ([]reflect.Type, bool) {
	var next []reflect.Type
	for _, t := range fieldTypes {
		t = deref(t)
		if array {
			if t.Kind() != reflect.Slice && t.Kind() != reflect.Array {
				return nil, false
			}
			t = deref(t.Elem())
		}
		if dynamic(t) || t.Kind() != reflect.Struct {
			return nil, false
		}
		next = append(next, t)
	}
	return next, true
}

// deref returns the type t points to, if it is a pointer, and t otherwise.
func deref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

var (
	tomlUnmarshaler = reflect.TypeFor[toml.Unmarshaler]()
	textUnmarshaler = reflect.TypeFor[encoding.TextUnmarshaler]()
	tomlTree        = reflect.TypeFor[toml.Tree]()
)

// dynamic reports whether a value of type t reads whichever keys its table
// holds.
func dynamic(t reflect.Type) bool {
	switch {
	case t == tomlTree, reflect.PointerTo(t).Implements(tomlUnmarshaler):
		return true
	case reflect.PointerTo(t).Implements(textUnmarshaler):
		return false
	}
	return t.Kind() == reflect.Map || t.Kind() == reflect.Interface
}

// closest returns the name of candidates closest to k, if one is close
// enough to be what k meant: within two fifths of its letters, ignoring
// case.
func closest(k string, candidates []string) string {
	best, bestDist := "", max(2, 2*len(k)/5)+1
	for _, c := range candidates {
		if d := editDistance(strings.ToLower(k), strings.ToLower(c)); d < bestDist {
			best, bestDist = c, d
		}
	}
	return best
}

// editDistance returns the edits that make a of b: insertions, deletions,
// substitutions and swaps of letters side by side.
func editDistance(a, b string) int {
	d := make([][]int, len(a)+1)
	for i := range d {
		d[i] = make([]int, len(b)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(a)][len(b)]
}
