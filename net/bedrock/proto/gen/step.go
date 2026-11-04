package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// The modes -compare and -convert write the code of a step package, which
// converts the packets of a protocol, To, to and from those of the protocol
// before it, From:
//
//	gen -compare from/schema.txt -schema to/schema.txt -from FROM -to TO -pkg cNNNN -o same_gen.go [-convert convert_gen.go]
//
// -compare writes Same, which reports the packets whose layout is the same
// in both schemas through every type they hold, and the lists of the IDs
// that changed, that only To has and that only From has. A declaration or
// a wire type coded by hand is the same only if the package lists it as
// unchanged in its sameHand, a []string variable of a file written by hand.
//
// -convert writes, for each packet that changed, up<Name>Packet from the
// packet of From to that of To and down<Name>Packet back, and a converter
// for every type they hold, recursing: fields are copied by name, a field
// only the target has keeps its zero value, a field only the source has is
// dropped, lists, arrays and optionals are converted element by element
// and integers converted to the type of the target. A converter the
// package writes by hand is not written; one of a declaration or a wire
// type coded by hand must be. The package names in its convertTypes, a
// []string variable as sameHand, the types its converters written by hand
// need the converters of, such as the actions of a stack request, if both
// schemas have them. After it copies the fields of up<X> or down<X>, a
// converter calls fixUp<X> or fixDown<X>(in, out) if the package writes
// one, for what a copy by name gets wrong. A field whose
// type changes otherwise than between integers needs such a fix.

// step is the comparison of the schemas of two adjacent protocols.
type step struct {
	from, to *schema
	// fromPath and toPath are the import paths of the codec packages,
	// fromPkg and toPkg their names.
	fromPath, toPath string
	fromPkg, toPkg   string
	// sameHand holds what is coded by hand and has the same layout in
	// both: names of declarations and Go names of special wire types.
	sameHand map[string]bool
	// convertTypes are the types the converters written by hand need the
	// converters of.
	convertTypes []string
	// funcs holds the functions written by hand in the output package.
	funcs map[string]bool
	// diff holds the keys of what differs: "P:id" of a packet, "T:name" of
	// a type, "E:name" of an enum and "S:name" of a special wire type.
	diff map[string]bool
}

// convertible are the special wire types whose Go types hold values of
// built-in types alone, in both packages alike: a Go conversion converts
// them.
var convertible = map[string]bool{"Metadata": true, "InputFlags": true, "Bitset": true, "AbilityValue": true}

// newStep parses the schemas of a step and what the package of its output
// writes by hand, and compares the schemas.
func newStep(fromSchema, toSchema, fromPath, toPath, outDir string, outputs ...string) (*step, error) {
	from, err := parse(fromSchema)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", fromSchema, err)
	}
	to, err := parse(toSchema)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", toSchema, err)
	}
	st := &step{from: from, to: to, fromPath: fromPath, toPath: toPath, fromPkg: path.Base(fromPath), toPkg: path.Base(toPath)}
	if st.fromPkg == st.toPkg {
		return nil, fmt.Errorf("the packages of both protocols are named %s", st.fromPkg)
	}
	if err := st.readHand(outDir, outputs); err != nil {
		return nil, err
	}
	st.compare()
	return st, nil
}

// readHand reads the functions and sameHand of the Go files of a
// directory, but the outputs and tests.
func (st *step) readHand(dir string, outputs []string) error {
	st.sameHand, st.funcs = map[string]bool{}, map[string]bool{}
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	for _, f := range files {
		base := filepath.Base(f)
		if strings.HasSuffix(base, "_test.go") || slices.Contains(outputs, base) {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, d := range file.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					st.funcs[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
						continue
					}
					name := vs.Names[0].Name
					if name != "sameHand" && name != "convertTypes" {
						continue
					}
					lit, ok := vs.Values[0].(*ast.CompositeLit)
					if !ok {
						return fmt.Errorf("%s: %s is not a []string literal", f, name)
					}
					for _, e := range lit.Elts {
						bl, ok := e.(*ast.BasicLit)
						if !ok || bl.Kind != token.STRING {
							return fmt.Errorf("%s: an element of %s is not a string", f, name)
						}
						s, _ := strconv.Unquote(bl.Value)
						if name == "sameHand" {
							st.sameHand[s] = true
						} else {
							st.convertTypes = append(st.convertTypes, s)
						}
					}
				}
			}
		}
	}
	return nil
}

// packets returns the packets of a schema by ID.
func packets(s *schema) map[int]*decl {
	m := map[int]*decl{}
	for _, d := range s.decls {
		if d.packet {
			m[d.id] = d
		}
	}
	return m
}

// compare finds what differs between the schemas: a declaration, enum or
// special wire type that differs itself, and every declaration that holds
// one.
func (st *step) compare() {
	st.diff = map[string]bool{}
	refs := map[string][]string{}
	for _, e := range st.from.enumList {
		o := st.to.enums[e.name]
		if o == nil || !slices.Equal(e.values, o.values) || !slices.Equal(e.names, o.names) {
			st.diff["E:"+e.name] = true
		}
	}
	for _, e := range st.to.enumList {
		if st.from.enums[e.name] == nil {
			st.diff["E:"+e.name] = true
		}
	}
	for _, t := range special {
		if !st.sameHand[t] {
			st.diff["S:"+t] = true
		}
	}
	pair := func(key string, a, b *decl) {
		if a == nil || b == nil || a.name != b.name || !nodesEqual(a.body, b.body) {
			st.diff[key] = true
		}
		for _, d := range []*decl{a, b} {
			if d == nil {
				continue
			}
			if d.manual != "" && !st.sameHand[d.name] {
				st.diff[key] = true
			}
			walk(d.body, func(n *node) {
				if n.kind == "field" {
					refs[key] = append(refs[key], st.wireRefs(n.wire)...)
				}
			})
		}
	}
	for _, name := range st.typeNames() {
		pair("T:"+name, st.from.byName[name], st.to.byName[name])
	}
	fp, tp := packets(st.from), packets(st.to)
	for _, id := range st.packetIDs() {
		pair("P:"+strconv.Itoa(id), fp[id], tp[id])
	}
	for changed := true; changed; {
		changed = false
		for key, rs := range refs {
			if st.diff[key] {
				continue
			}
			for _, r := range rs {
				if st.diff[r] {
					st.diff[key], changed = true, true
					break
				}
			}
		}
	}
}

// typeNames returns the names of the types of either schema, sorted.
func (st *step) typeNames() []string {
	var names []string
	for _, s := range []*schema{st.from, st.to} {
		for name := range s.byName {
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names
}

// packetIDs returns the IDs of the packets of either schema, sorted.
func (st *step) packetIDs() []int {
	var ids []int
	for _, s := range []*schema{st.from, st.to} {
		for id := range packets(s) {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	sort.Ints(ids)
	return ids
}

// wireRefs returns the keys of what a wire type holds that may differ. A
// wire type neither schema knows differs.
func (st *step) wireRefs(w string) []string {
	for {
		kind, elem, _ := baseOf(w)
		if kind == "value" {
			break
		}
		w = elem
	}
	switch {
	case goTypes[w] != "":
		return nil
	case strings.HasPrefix(w, "enum("):
		return []string{"E:" + strings.TrimSuffix(strings.TrimPrefix(w, "enum("), ")")}
	case special[specialBase(w)] != "":
		return []string{"S:" + special[specialBase(w)]}
	case st.from.byName[w] != nil || st.to.byName[w] != nil:
		return []string{"T:" + w}
	}
	st.diff["U:"+w] = true
	return []string{"U:" + w}
}

// nodesEqual reports whether two bodies of declarations read and write the
// same fields of the same wire types under the same conditions.
func nodesEqual(a, b []*node) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.kind != y.kind || x.path != y.path || x.wire != y.wire || x.as != y.as || x.expr != y.expr ||
			!slices.Equal(x.vals, y.vals) || !nodesEqual(x.body, y.body) || !nodesEqual(x.elses, y.elses) {
			return false
		}
	}
	return true
}

// same returns the IDs of the packets of the same layout in both schemas,
// those that changed, those only To has and those only From has.
func (st *step) same() (same, changed, added, removed []int) {
	fp, tp := packets(st.from), packets(st.to)
	for _, id := range st.packetIDs() {
		switch {
		case fp[id] == nil:
			added = append(added, id)
		case tp[id] == nil:
			removed = append(removed, id)
		case st.diff["P:"+strconv.Itoa(id)]:
			changed = append(changed, id)
		default:
			same = append(same, id)
		}
	}
	return same, changed, added, removed
}

// writeSame writes the Go source of Same and the lists of IDs.
func (st *step) writeSame(pkg string) ([]byte, error) {
	same, changed, added, removed := st.same()
	g := &gen{}
	g.p("// Code generated by bedrock/proto/gen -compare; DO NOT EDIT.")
	g.p("")
	g.p("package %s", pkg)
	g.p("")
	g.p("// Same reports whether packets of an ID have the same layout in %s and", st.fromPkg)
	g.p("// %s, so that the bytes of either decode as the other. A packet of a", st.toPkg)
	g.p("// declaration or a wire type coded by hand is the same only if sameHand")
	g.p("// lists them.")
	g.p("func Same(id uint32) bool {")
	g.p("switch id {")
	if len(same) > 0 {
		g.p("case %s:", joinInts(same))
		g.p("return true")
	}
	g.p("}")
	g.p("return false")
	g.p("}")
	g.p("")
	g.p("// changed are the IDs of the packets of both protocols whose layout")
	g.p("// differs, added those only %s has, removed those only %s has.", st.toPkg, st.fromPkg)
	g.p("var (")
	g.p("changed = []uint32{%s}", joinInts(changed))
	g.p("added = []uint32{%s}", joinInts(added))
	g.p("removed = []uint32{%s}", joinInts(removed))
	g.p(")")
	return formatted(g)
}

func joinInts(ids []int) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = strconv.Itoa(id)
	}
	return strings.Join(s, ", ")
}

func formatted(g *gen) ([]byte, error) {
	src, err := format.Source(g.b.Bytes())
	if err != nil {
		return g.b.Bytes(), fmt.Errorf("format: %w", err)
	}
	return src, nil
}

// direction is a direction of conversion.
type direction struct {
	// name is "up" or "down".
	name string
	// src and dst are the schemas converted from and to, srcPkg and
	// dstPkg the names of their packages.
	src, dst       *schema
	srcPkg, dstPkg string
}

func (st *step) directions() []direction {
	return []direction{
		{"up", st.from, st.to, st.fromPkg, st.toPkg},
		{"down", st.to, st.from, st.toPkg, st.fromPkg},
	}
}

// converter writes the converters of one direction.
type converter struct {
	st *step
	d  direction
	g  *gen
	// written holds the converters written or queued, queue those to
	// write.
	written map[string]bool
	queue   []*decl
	missing []string
	// fixes lists the fixes to write for fields whose type changes.
	fixes []string
	tmp   int
}

// writeConvert writes the Go source of the converters of the packets that
// changed, both ways.
func (st *step) writeConvert(pkg string) ([]byte, error) {
	_, changed, _, _ := st.same()
	g := &gen{}
	g.p("// Code generated by bedrock/proto/gen -convert; DO NOT EDIT.")
	g.p("")
	g.p("package %s", pkg)
	g.p("")
	g.p("import (")
	g.p("\t\"github.com/dm-vev/adamant/net/bedrock/proto\"")
	g.p("\t%q", st.fromPath)
	g.p("\t%q", st.toPath)
	g.p(")")
	g.p("")
	g.p("var _ proto.Packet")
	var missing, fixes []string
	for _, d := range st.directions() {
		c := &converter{st: st, d: d, g: g, written: map[string]bool{}}
		srcPackets := packets(d.src)
		g.p("")
		g.p("// %sPacket converts a packet of %s whose layout %s changed to the packet", d.name, d.srcPkg, d.dstPkg)
		g.p("// of %s of its ID, false for a packet of another ID.", d.dstPkg)
		g.p("func %sPacket(pk proto.Packet) (proto.Packet, bool) {", d.name)
		g.p("switch pk := pk.(type) {")
		for _, id := range changed {
			p := srcPackets[id]
			g.p("case *%s.%sPacket:", d.srcPkg, p.name)
			g.p("return %s(pk), true", c.need(p))
		}
		g.p("}")
		g.p("return nil, false")
		g.p("}")
		for _, name := range st.convertTypes {
			// A type of one protocol alone has no converter: the hand
			// code that converts what holds it converts it too.
			switch t := d.src.byName[name]; {
			case t != nil && d.dst.byName[name] != nil:
				c.need(t)
			case st.from.byName[name] == nil && st.to.byName[name] == nil:
				return nil, fmt.Errorf("convertTypes names %s, a type of neither schema", name)
			}
		}
		for len(c.queue) > 0 {
			next := c.queue[0]
			c.queue = c.queue[1:]
			if err := c.write(next); err != nil {
				return nil, err
			}
		}
		missing, fixes = append(missing, c.missing...), append(fixes, c.fixes...)
	}
	var errs []string
	if len(missing) > 0 {
		sort.Strings(missing)
		errs = append(errs, "converters to write by hand: "+strings.Join(slices.Compact(missing), ", "))
	}
	if len(fixes) > 0 {
		sort.Strings(fixes)
		errs = append(errs, "fixes to write for fields whose type changes: "+strings.Join(slices.Compact(fixes), ", "))
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	return formatted(g)
}

// funcName returns the name of the converter of a declaration.
func (c *converter) funcName(d *decl) string {
	name := d.name
	if d.packet {
		name += "Packet"
	}
	return c.d.name + name
}

// need returns the name of the converter of a declaration of the source,
// queuing it to be written unless the package writes it.
func (c *converter) need(d *decl) string {
	name := c.funcName(d)
	if c.written[name] {
		return name
	}
	c.written[name] = true
	if c.st.funcs[name] {
		return name
	}
	other := c.d.dst.byName[d.name]
	if d.packet {
		other = packets(c.d.dst)[d.id]
	}
	if d.manual != "" || other == nil || other.manual != "" {
		c.missing = append(c.missing, name)
		return name
	}
	c.queue = append(c.queue, d)
	return name
}

// identifiers matches the identifiers of a Go type.
var identifiers = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// names returns the names of a Go type that are types of a schema or
// special wire types.
func names(s *schema, t string) []string {
	var out []string
	for _, loc := range identifiers.FindAllStringIndex(t, -1) {
		if loc[0] > 0 && t[loc[0]-1] == '.' || loc[1] < len(t) && t[loc[1]] == '.' {
			continue
		}
		id := t[loc[0]:loc[1]]
		if s.byName[id] != nil || isSpecial(id) {
			out = append(out, id)
		}
	}
	return out
}

func isSpecial(name string) bool {
	for _, t := range special {
		if t == name {
			return true
		}
	}
	return false
}

// qualify returns a Go type of a schema with its types in a package.
func qualify(s *schema, t, pkg string) string {
	var b strings.Builder
	last := 0
	for _, loc := range identifiers.FindAllStringIndex(t, -1) {
		b.WriteString(t[last:loc[0]])
		id := t[loc[0]:loc[1]]
		// An identifier with a dot before or after it is a field or names
		// a package already.
		dotted := loc[0] > 0 && t[loc[0]-1] == '.' || loc[1] < len(t) && t[loc[1]] == '.'
		if !dotted && (s.byName[id] != nil || isSpecial(id)) {
			b.WriteString(pkg + ".")
		}
		b.WriteString(id)
		last = loc[1]
	}
	b.WriteString(t[last:])
	return b.String()
}

// write writes the converter of a declaration of the source.
func (c *converter) write(d *decl) error {
	g, name := c.g, c.funcName(d)
	other := c.d.dst.byName[d.name]
	if d.packet {
		other = packets(c.d.dst)[d.id]
	}
	srcFields, err := c.d.src.fields(d)
	if err != nil {
		return err
	}
	dstFields, err := c.d.dst.fields(other)
	if err != nil {
		return err
	}
	srcType, dstType := d.name, other.name
	if d.packet {
		srcType, dstType = srcType+"Packet", dstType+"Packet"
	}
	in, out := c.d.srcPkg+"."+srcType, c.d.dstPkg+"."+dstType
	fix := "fix" + strings.ToUpper(name[:1]) + name[1:]
	hasFix := c.st.funcs[fix]
	g.p("")
	if d.packet {
		g.p("func %s(in *%s) *%s {", name, in, out)
		g.p("out := &%s{}", out)
	} else {
		g.p("func %s(in *%s) (out %s) {", name, in, out)
	}
	switch {
	case len(srcFields) == 1 && srcFields[0].name == "self" && len(dstFields) == 1 && dstFields[0].name == "self":
		if err := c.value(srcFields[0].goType, dstFields[0].goType, "(*in)", "out", fix, hasFix); err != nil {
			return err
		}
	case c.plainStruct(srcFields, dstFields):
		// The structs are alike and hold values of built-in types alone.
		if d.packet {
			g.p("*out = %s(*in)", out)
		} else {
			g.p("out = %s(*in)", out)
		}
	default:
		for _, f := range dstFields {
			i := slices.IndexFunc(srcFields, func(s field) bool { return s.name == f.name })
			if i < 0 {
				g.p("// %s is new in %s.", f.name, c.d.dstPkg)
				continue
			}
			if err := c.value(srcFields[i].goType, f.goType, "in."+f.name, "out."+f.name, fix, hasFix); err != nil {
				return err
			}
		}
		for _, f := range srcFields {
			if !slices.ContainsFunc(dstFields, func(s field) bool { return s.name == f.name }) {
				g.p("// %s is not in %s.", f.name, c.d.dstPkg)
			}
		}
	}
	if hasFix {
		if d.packet {
			g.p("%s(in, out)", fix)
		} else {
			g.p("%s(in, &out)", fix)
		}
	}
	g.p("return out")
	g.p("}")
	return nil
}

// plainStruct reports whether two lists of fields are alike and of
// built-in types alone, so that a Go conversion converts their structs.
func (c *converter) plainStruct(src, dst []field) bool {
	if len(src) != len(dst) {
		return false
	}
	for i := range src {
		if src[i] != dst[i] || len(names(c.d.src, src[i].goType)) > 0 {
			return false
		}
	}
	return true
}

// value writes the conversion of a value of Go type st of the source, src,
// to one of Go type dt of the target, dst. fix is the fix of the converter
// it is part of, which hasFix reports the package writes.
func (c *converter) value(st, dt, src, dst, fix string, hasFix bool) error {
	g := c.g
	if st != dt {
		if integerType(st) && integerType(dt) {
			g.p("%s = %s(%s)", dst, dt, src)
			return nil
		}
		if hasFix {
			g.p("// %s changes from %s to %s: %s converts it.", src, st, dt, fix)
			return nil
		}
		c.fixes = append(c.fixes, fmt.Sprintf("%s (%s %s to %s %s)", fix, c.d.srcPkg, st, c.d.dstPkg, dt))
		return nil
	}
	if len(names(c.d.src, st)) == 0 {
		g.p("%s = %s", dst, src)
		return nil
	}
	switch {
	case strings.HasPrefix(st, "[]"):
		elem := st[2:]
		i := c.temp()
		g.p("if %s != nil {", src)
		g.p("%s = make([]%s, len(%s))", dst, qualify(c.d.dst, elem, c.d.dstPkg), src)
		g.p("for %s := range %s {", i, src)
		if err := c.value(elem, elem, src+"["+i+"]", dst+"["+i+"]", fix, hasFix); err != nil {
			return err
		}
		g.p("}")
		g.p("}")
	case strings.HasPrefix(st, "["):
		elem := st[strings.IndexByte(st, ']')+1:]
		i := c.temp()
		g.p("for %s := range %s {", i, src)
		if err := c.value(elem, elem, src+"["+i+"]", dst+"["+i+"]", fix, hasFix); err != nil {
			return err
		}
		g.p("}")
	case strings.HasPrefix(st, "proto.Optional["):
		elem := strings.TrimSuffix(strings.TrimPrefix(st, "proto.Optional["), "]")
		g.p("%s.Set = %s.Set", dst, src)
		g.p("if %s.Set {", src)
		if err := c.value(elem, elem, src+".Val", dst+".Val", fix, hasFix); err != nil {
			return err
		}
		g.p("}")
	case convertible[st]:
		g.p("%s = %s.%s(%s)", dst, c.d.dstPkg, st, src)
	case isSpecial(st):
		name := c.d.name + st
		if !c.st.funcs[name] {
			c.missing = append(c.missing, name)
		}
		g.p("%s = %s(&%s)", dst, name, src)
	default:
		d := c.d.src.byName[st]
		if d == nil {
			return fmt.Errorf("%s: no converter of %s", src, st)
		}
		g.p("%s = %s(&%s)", dst, c.need(d), src)
	}
	return nil
}

func (c *converter) temp() string {
	c.tmp++
	return "i" + strconv.Itoa(c.tmp)
}

// runStep runs the modes -compare and -convert.
func runStep(fromSchema, toSchema, fromPath, toPath, pkg, sameOut, convertOut string) error {
	if fromPath == "" || toPath == "" || pkg == "" || sameOut == "" {
		return fmt.Errorf("-compare needs -schema, -from, -to, -pkg and -o")
	}
	dir := filepath.Dir(sameOut)
	outputs := []string{filepath.Base(sameOut)}
	if convertOut != "" {
		outputs = append(outputs, filepath.Base(convertOut))
	}
	st, err := newStep(fromSchema, toSchema, fromPath, toPath, dir, outputs...)
	if err != nil {
		return err
	}
	src, err := st.writeSame(pkg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(sameOut, src, 0o644); err != nil {
		return err
	}
	if convertOut == "" {
		return nil
	}
	src, err = st.writeConvert(pkg)
	if err != nil {
		return err
	}
	return os.WriteFile(convertOut, src, 0o644)
}
