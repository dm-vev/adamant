// Command gen writes the packet codecs of one protocol version from its
// schema: a struct per packet and type, with Encode and Decode methods over
// package wire that allocate only what they decode.
//
// The schema lists declarations: "packet ID Name", "packet ID Name client"
// for a packet a client sends, or "type Name", then one line per field
// read and written, "Name wiretype", with if, else, switch and case lines
// around them. A declaration marked manual has no codec yet: a packet
// becomes raw, its bytes kept as they are, and so does every packet that
// holds a manual type, until a codec is written by hand in the package. A
// "present COND" line is a bool saying whether COND holds, followed by the
// lines under it when it does. An "enum name" declaration lists "value
// name" lines: a field of wire type "enum(name) as T" goes as the name of
// its value.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type node struct {
	kind string // "field", "if", "else", "present", "switch", "case", "default"
	path string
	wire string
	// as is the Go type of an integer field that differs from its wire
	// type, converted on the wire.
	as    string
	expr  string
	vals  []string
	body  []*node
	elses []*node
}

type decl struct {
	packet bool
	id     int
	name   string
	manual string
	body   []*node
	// raw marks a packet without a complete codec.
	raw bool
	// client marks a packet a client sends.
	client bool
}

// enum is an enum whose values go as names.
type enum struct {
	name          string
	values, names []string
}

type schema struct {
	decls []*decl
	enums map[string]*enum
	// enumList holds the enums in the order of the schema.
	enumList []*enum
	// byName holds the types by name; packets are never field types.
	byName map[string]*decl
	// hand holds the wire types and declarations written by hand.
	hand map[string]bool
}

func main() {
	in := flag.String("schema", "", "schema file")
	out := flag.String("o", "", "Go file to write")
	pkg := flag.String("pkg", "", "package name")
	flag.Parse()
	s, err := parse(*in)
	if err != nil {
		log.Fatal(err)
	}
	if err := s.findHand(*out); err != nil {
		log.Fatal(err)
	}
	s.resolveRaw()
	src, err := s.generate(*pkg)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, src, 0o644); err != nil {
		log.Fatal(err)
	}
	raw := 0
	for _, d := range s.decls {
		if d.packet && d.raw {
			raw++
		}
	}
	log.Printf("%d declarations, %d packets raw", len(s.decls), raw)
}

// parseState holds what the parse of a schema has open.
type parseState struct {
	s       *schema
	cur     *decl
	curEnum *enum
	// stack holds the bodies open at each depth.
	stack []frame
}

// frame is a body open at a depth, and the node last added to it.
type frame struct {
	list *[]*node
	last *node
}

func parse(path string) (*schema, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	p := &parseState{s: &schema{byName: map[string]*decl{}, hand: map[string]bool{}, enums: map[string]*enum{}}}
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for n := 1; sc.Scan(); n++ {
		if err := p.line(sc.Text()); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
	}
	return p.s, sc.Err()
}

// line parses a line of the schema.
func (p *parseState) line(line string) error {
	text := strings.TrimSpace(line)
	if text == "" || strings.HasPrefix(text, "#") {
		return nil
	}
	depth := len(line) - len(strings.TrimLeft(line, "\t"))
	switch {
	case depth == 0:
		return p.decl(text)
	case p.curEnum != nil && depth == 1:
		return p.enumValue(text)
	case p.cur == nil || depth > len(p.stack):
		return errors.New("unexpected indentation")
	}
	return p.body(depth, text)
}

// decl parses the line of a declaration.
func (p *parseState) decl(text string) error {
	fields := strings.Fields(text)
	p.curEnum = nil
	switch fields[0] {
	case "hand":
		for _, h := range fields[1:] {
			p.s.hand[h] = true
		}
	case "packet":
		return p.packet(text, fields)
	case "type":
		p.open(&decl{name: fields[1]})
	case "enum":
		return p.enum(text, fields)
	default:
		return fmt.Errorf("%q", text)
	}
	return nil
}

// packet opens a packet, "packet ID Name", marked "client" if a client
// sends it.
func (p *parseState) packet(text string, fields []string) error {
	id, err := strconv.Atoi(fields[1])
	client := len(fields) == 4 && fields[3] == "client"
	if err != nil || len(fields) != 3 && !client {
		return fmt.Errorf("%q", text)
	}
	p.open(&decl{packet: true, id: id, name: fields[2], client: client})
	return nil
}

// open opens the body of a declaration.
func (p *parseState) open(d *decl) {
	p.cur = d
	p.s.decls = append(p.s.decls, d)
	if !d.packet {
		p.s.byName[d.name] = d
	}
	p.stack = []frame{{list: &d.body}}
}

// enum opens an enum, whose values follow.
func (p *parseState) enum(text string, fields []string) error {
	if len(fields) != 2 || p.s.enums[fields[1]] != nil {
		return fmt.Errorf("%q", text)
	}
	p.curEnum = &enum{name: fields[1]}
	p.s.enums[p.curEnum.name] = p.curEnum
	p.s.enumList = append(p.s.enumList, p.curEnum)
	p.cur = nil
	return nil
}

// enumValue parses a "value name" line of an enum.
func (p *parseState) enumValue(text string) error {
	v, name, ok := strings.Cut(text, " ")
	if _, err := strconv.ParseInt(v, 10, 64); err != nil || !ok {
		return fmt.Errorf("%q", text)
	}
	p.curEnum.values = append(p.curEnum.values, v)
	p.curEnum.names = append(p.curEnum.names, name)
	return nil
}

// body parses a line of the body of a declaration at a depth.
func (p *parseState) body(depth int, text string) error {
	p.stack = p.stack[:depth]
	top := &p.stack[depth-1]
	word, rest, _ := strings.Cut(text, " ")
	switch word {
	case "manual":
		p.cur.manual = rest
	case "else":
		if top.last == nil || top.last.kind != "if" {
			return errors.New("else without if")
		}
		p.stack = append(p.stack, frame{list: &top.last.elses})
	default:
		p.add(top, word, newNode(word, rest))
	}
	return nil
}

// newNode returns the node of a line of a body but manual and else.
func newNode(word, rest string) *node {
	nd := &node{kind: word}
	switch word {
	case "if", "present", "switch":
		nd.expr = rest
	case "case":
		for _, v := range strings.Split(rest, ",") {
			nd.vals = append(nd.vals, strings.TrimSpace(v))
		}
	case "default":
	default:
		nd.kind, nd.path, nd.wire = "field", word, rest
		if w, as, ok := strings.Cut(rest, " as "); ok {
			nd.wire, nd.as = w, as
		}
	}
	return nd
}

// add adds a node to the body open at the top, a case or default to the
// switch before it, and opens the body of the node if it has one.
func (p *parseState) add(top *frame, word string, nd *node) {
	if top.last != nil && top.last.kind == "switch" && (word == "case" || word == "default") {
		top.last.body = append(top.last.body, nd)
		p.stack = append(p.stack, frame{list: &nd.body})
		return
	}
	*top.list = append(*top.list, nd)
	top.last = nd
	if word == "if" || word == "present" || word == "switch" {
		p.stack = append(p.stack, frame{list: &nd.body})
	}
}

// findHand finds the codecs written by hand in the package of the output:
// a function writeT for a special wire type of Go type T, an Encode method
// for a declaration.
func (s *schema) findHand(out string) error {
	dir := filepath.Dir(out)
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return err
	}
	funcs := map[string]bool{}
	methods := map[string]bool{}
	fset := token.NewFileSet()
	for _, f := range files {
		if filepath.Base(f) == filepath.Base(out) || strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, d := range file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fd.Recv == nil {
				funcs[fd.Name.Name] = true
				continue
			}
			if fd.Name.Name != "Encode" {
				continue
			}
			t := fd.Recv.List[0].Type
			if st, ok := t.(*ast.StarExpr); ok {
				t = st.X
			}
			if id, ok := t.(*ast.Ident); ok {
				methods[id.Name] = true
			}
		}
	}
	for w, t := range special {
		if funcs["write"+t] {
			s.hand[w] = true
		}
	}
	for _, d := range s.decls {
		name := d.name
		if d.packet {
			name += "Packet"
		}
		if methods[name] {
			s.hand[d.name] = true
		}
	}
	return nil
}

// base splits a wire type into its kind and element.
func baseOf(w string) (kind, elem, count string) {
	switch {
	case strings.HasPrefix(w, "?"):
		return "optional", w[1:], ""
	case strings.HasPrefix(w, "[]"):
		t := w[2:]
		if e, c, ok := strings.Cut(t, " count "); ok {
			return "list", e, "count " + c
		}
		if e, c, ok := strings.Cut(t, " len "); ok {
			return "list", e, "len " + c
		}
		return "list", t, ""
	}
	return "value", w, ""
}

// goTypes maps the plain wire types to Go types.
var goTypes = map[string]string{
	"bool": "bool", "uint8": "uint8", "int8": "int8", "uint16": "uint16", "int16": "int16",
	"uint32": "uint32", "varuint32": "uint32", "int32": "int32", "int32be": "int32", "varint32": "int32",
	"uint64": "uint64", "varuint64": "uint64", "int64": "int64", "varint64": "int64",
	"float32": "float32", "float64": "float64", "bytefloat": "float32",
	"string": "string", "string16": "string", "bytes": "[]byte", "rest": "[]byte",
	"uuid": "[16]byte", "vec2": "[2]float32", "vec3": "[3]float32", "soundpos": "[3]float32",
	"blockpos": "[3]int32", "chunkpos": "[2]int32", "subchunkpos": "[3]int32",
	"rgb": "[3]float32", "rgba": "proto.Color", "argbbe": "proto.Color",
	"nbt": "map[string]any", "nbtlist": "[]any",
}

// special maps the wire types with codecs written by hand to their Go
// types.
var special = map[string]string{
	"metadata": "Metadata", "itemstack": "ItemStack", "iteminstance": "ItemInstance",
	"stackrequestitem": "StackRequestItem", "itemdescriptorcount": "ItemDescriptorCount",
	"stackrequestaction": "StackRequestAction", "materialreducer": "MaterialReducer",
	"eventtype": "Event", "eventordinal": "Event", "transactiondata": "InventoryTransactionData",
	"playerinventoryaction": "UseItemTransactionData", "gamerule": "GameRule",
	"abilityvalue": "AbilityValue", "packsetting": "PackSetting", "shapedata": "ShapeData",
	"inputflags": "InputFlags", "bitset": "Bitset",
}

// enumOf returns the enum of a wire type enum(name).
func (s *schema) enumOf(w string) (*enum, bool) {
	name, ok := strings.CutPrefix(w, "enum(")
	if !ok || !strings.HasSuffix(name, ")") {
		return nil, false
	}
	e, ok := s.enums[strings.TrimSuffix(name, ")")]
	return e, ok
}

// specialBase returns a special wire type without its argument, as
// inputflags(66) is inputflags.
func specialBase(w string) string {
	if i := strings.IndexByte(w, '('); i > 0 {
		return w[:i]
	}
	return w
}

// specialArg returns the argument of a special wire type, passed on to its
// codec, with a comma before it.
func specialArg(w string) string {
	if i := strings.IndexByte(w, '('); i > 0 {
		return ", " + strings.TrimSuffix(w[i+1:], ")")
	}
	return ""
}

// complete reports whether a wire type has a codec.
func (s *schema) complete(w string, seen map[*decl]bool) bool {
	kind, elem, _ := baseOf(w)
	if kind != "value" {
		return s.complete(elem, seen)
	}
	if _, ok := goTypes[w]; ok {
		return true
	}
	if _, ok := s.enumOf(w); ok {
		return true
	}
	if strings.HasPrefix(w, "nbt(") || strings.HasPrefix(w, "nbtlist(") {
		return s.hand[w]
	}
	if _, ok := special[specialBase(w)]; ok {
		return s.hand[specialBase(w)]
	}
	d, ok := s.byName[w]
	if !ok {
		return false
	}
	return s.declComplete(d, seen)
}

func (s *schema) declComplete(d *decl, seen map[*decl]bool) bool {
	if !d.packet && s.hand[d.name] {
		return true
	}
	if d.manual != "" {
		return false
	}
	if seen[d] {
		return true
	}
	seen[d] = true
	ok := true
	walk(d.body, func(n *node) {
		if n.kind == "field" && !s.complete(n.wire, seen) {
			ok = false
		}
	})
	return ok
}

func walk(nodes []*node, f func(*node)) {
	for _, n := range nodes {
		f(n)
		walk(n.body, f)
		walk(n.elses, f)
	}
}

// resolveRaw marks the packets without a complete codec raw, and keeps the
// types only those used.
func (s *schema) resolveRaw() {
	for _, d := range s.decls {
		if d.packet && !s.hand[d.name] {
			d.raw = !s.declComplete(d, map[*decl]bool{})
		}
	}
}

// field is a field of a generated struct.
type field struct {
	name, goType string
}

// fields returns the struct fields of a declaration in order of first use.
func (s *schema) fields(d *decl) ([]field, error) {
	var out []field
	index := map[string]int{}
	arrays := map[string]int{}
	var err error
	walk(d.body, func(n *node) {
		if n.kind != "field" || err != nil {
			return
		}
		name, idx, isIdx := strings.Cut(n.path, "[")
		t := n.as
		if _, isEnum := s.enumOf(n.wire); !isEnum || t == "" {
			var e error
			if t, e = s.goType(n.wire); e != nil {
				err = fmt.Errorf("%s.%s: %w", d.name, n.path, e)
				return
			}
			if n.as != "" {
				t = n.as
			}
		}
		if isIdx {
			i, e := strconv.Atoi(strings.TrimSuffix(idx, "]"))
			if e != nil {
				err = fmt.Errorf("%s.%s: index", d.name, n.path)
				return
			}
			arrays[name] = max(arrays[name], i+1)
		}
		if _, ok := index[name]; ok {
			return
		}
		index[name] = len(out)
		out = append(out, field{name, t})
	})
	for name, n := range arrays {
		f := &out[index[name]]
		f.goType = fmt.Sprintf("[%d]%s", n, f.goType)
	}
	return out, err
}

func (s *schema) goType(w string) (string, error) {
	kind, elem, _ := baseOf(w)
	switch kind {
	case "optional":
		t, err := s.goType(elem)
		return "proto.Optional[" + t + "]", err
	case "list":
		t, err := s.goType(elem)
		return "[]" + t, err
	}
	if t, ok := goTypes[w]; ok {
		return t, nil
	}
	if t, ok := special[specialBase(w)]; ok {
		return t, nil
	}
	if _, ok := s.byName[w]; ok {
		return w, nil
	}
	if strings.HasPrefix(w, "nbt(") {
		return "map[string]any", nil
	}
	return "", fmt.Errorf("unknown wire type %q", w)
}

type gen struct {
	s     *schema
	b     bytes.Buffer
	d     *decl
	names map[string]bool
	// types holds the Go types of the fields of the declaration.
	types map[string]string
	tmp   int
}

func (g *gen) p(format string, args ...any) {
	fmt.Fprintf(&g.b, format, args...)
	g.b.WriteByte('\n')
}

func (s *schema) generate(pkg string) ([]byte, error) {
	g := &gen{s: s}
	g.p("// Code generated by bedrock/proto/gen from schema.txt; DO NOT EDIT.")
	g.p("")
	g.p("package %s", pkg)
	g.p("")
	g.p("import (")
	g.p("\t\"github.com/dm-vev/adamant/net/bedrock/proto\"")
	g.p("\t\"github.com/dm-vev/adamant/net/bedrock/wire\"")
	g.p(")")
	var packets []*decl
	for _, d := range s.decls {
		if d.packet {
			packets = append(packets, d)
		}
		if s.hand[d.name] || !d.packet && !s.declComplete(d, map[*decl]bool{}) {
			continue
		}
		if err := g.decl(d); err != nil {
			return nil, err
		}
	}
	for _, e := range s.enumList {
		g.enum(e, pkg)
	}
	g.p("")
	g.p("// New returns an empty packet of an ID, or nil for an unknown ID.")
	g.p("func New(id uint32) proto.Packet {")
	g.p("switch id {")
	sort.Slice(packets, func(i, j int) bool { return packets[i].id < packets[j].id })
	for _, d := range packets {
		g.p("case %d:", d.id)
		g.p("return &%sPacket{}", d.name)
	}
	g.p("}")
	g.p("return nil")
	g.p("}")
	g.p("")
	g.p("// Raw reports whether the packet of an ID has no codec yet and keeps its")
	g.p("// fields as bytes.")
	g.ids("Raw", packets, func(d *decl) bool { return d.raw })
	g.p("")
	g.p("// FromClient reports whether a client sends packets of an ID, as the")
	g.p("// schema marks them.")
	g.ids("FromClient", packets, func(d *decl) bool { return d.client })
	src, err := format.Source(g.b.Bytes())
	if err != nil {
		return g.b.Bytes(), fmt.Errorf("format: %w", err)
	}
	return src, nil
}

// ids writes a function of a name that reports whether an ID is of one of
// the packets a predicate holds for.
func (g *gen) ids(name string, packets []*decl, holds func(*decl) bool) {
	g.p("func %s(id uint32) bool {", name)
	g.p("switch id {")
	var ids []string
	for _, d := range packets {
		if holds(d) {
			ids = append(ids, strconv.Itoa(d.id))
		}
	}
	if len(ids) > 0 {
		g.p("case %s:", strings.Join(ids, ", "))
		g.p("return true")
	}
	g.p("}")
	g.p("return false")
	g.p("}")
}

// enum writes the functions that map the values of an enum to their names
// and back.
func (g *gen) enum(e *enum, pkg string) {
	g.p("")
	g.p("// %sName returns the name a value of %s goes as.", e.name, e.name)
	g.p("func %sName(v int64) string {", e.name)
	g.p("switch v {")
	for i, v := range e.values {
		g.p("case %s:", v)
		g.p("return %q", e.names[i])
	}
	g.p("}")
	g.p("panic(%q)", pkg+": "+e.name+" value without a name")
	g.p("}")
	g.p("")
	g.p("// %sValue returns the value of %s a name stands for.", e.name, e.name)
	g.p("func %sValue(r *wire.Reader, name string) int64 {", e.name)
	g.p("switch name {")
	for i, v := range e.values {
		g.p("case %q:", e.names[i])
		g.p("return %s", v)
	}
	g.p("}")
	g.p("r.Failf(%q, wire.Quote(name))", pkg+": unknown "+e.name+" %s")
	g.p("return 0")
	g.p("}")
}

// recursive reports whether a declaration holds values of its own type,
// directly or through other types.
func (s *schema) recursive(d *decl) bool {
	seen := map[*decl]bool{}
	var reaches func(nodes []*node) bool
	reaches = func(nodes []*node) bool {
		found := false
		walk(nodes, func(n *node) {
			if n.kind != "field" || found {
				return
			}
			elem := n.wire
			for {
				k, e, _ := baseOf(elem)
				if k == "value" {
					break
				}
				elem = e
			}
			t, ok := s.byName[elem]
			if !ok || seen[t] {
				found = found || t == d
				return
			}
			seen[t] = true
			found = t == d || reaches(t.body)
		})
		return found
	}
	return reaches(d.body)
}

// usedTypes returns the types complete packets use.
func (s *schema) usedTypes() map[string]bool {
	used := map[string]bool{}
	var visit func(nodes []*node)
	visit = func(nodes []*node) {
		walk(nodes, func(n *node) {
			if n.kind != "field" {
				return
			}
			_, elem, _ := baseOf(n.wire)
			for {
				k, e, _ := baseOf(elem)
				if k == "value" {
					break
				}
				elem = e
			}
			if d, ok := s.byName[elem]; ok && !used[elem] {
				used[elem] = true
				visit(d.body)
			}
		})
	}
	for _, d := range s.decls {
		if d.packet && !d.raw {
			visit(d.body)
		}
	}
	return used
}

func (g *gen) decl(d *decl) error {
	g.d = d
	typeName := d.name
	if d.packet {
		typeName += "Packet"
	}
	g.p("")
	if d.packet && d.raw {
		reason := d.manual
		if reason == "" {
			reason = "it holds a value without a codec"
		}
		g.p("// %s has no codec yet (%s): its fields stay bytes.", typeName, strings.ReplaceAll(reason, "\n", " "))
		g.p("type %s struct {", typeName)
		g.p("Fields []byte")
		g.p("}")
		g.p("")
		g.p("func (*%s) ID() uint32 { return %d }", typeName, d.id)
		g.p("func (p *%s) Encode(w *wire.Writer) { w.Bytes(p.Fields) }", typeName)
		g.p("func (p *%s) Decode(r *wire.Reader) { p.Fields = append([]byte(nil), r.Bytes()...) }", typeName)
		return nil
	}
	fields, err := g.s.fields(d)
	if err != nil {
		return err
	}
	g.names = map[string]bool{}
	g.types = map[string]string{}
	for _, f := range fields {
		g.names[f.name] = true
		g.types[f.name] = f.goType
	}
	if len(fields) == 1 && fields[0].name == "self" {
		// The value is itself an array.
		g.p("type %s %s", typeName, fields[0].goType)
	} else {
		g.p("type %s struct {", typeName)
		for _, f := range fields {
			g.p("%s %s", f.name, f.goType)
		}
		g.p("}")
	}
	if d.packet {
		g.p("")
		g.p("func (*%s) ID() uint32 { return %d }", typeName, d.id)
	}
	g.p("")
	g.p("func (p *%s) Encode(w *wire.Writer) {", typeName)
	if err := g.body(d.body, true); err != nil {
		return err
	}
	g.p("}")
	g.p("")
	g.p("func (p *%s) Decode(r *wire.Reader) {", typeName)
	if g.s.recursive(d) {
		// A hostile peer nests values of the type as deep as its bytes go.
		g.p("if !r.Nest() {")
		g.p("return")
		g.p("}")
		g.p("defer r.Unnest()")
	}
	if err := g.body(d.body, false); err != nil {
		return err
	}
	g.p("}")
	return nil
}

func (g *gen) body(nodes []*node, enc bool) error {
	for _, n := range nodes {
		switch n.kind {
		case "field":
			v := "p." + n.path
			if strings.HasPrefix(n.path, "self") {
				v = "p" + strings.TrimPrefix(n.path, "self")
			}
			if e, ok := g.s.enumOf(n.wire); ok {
				if n.as == "" {
					return fmt.Errorf("%s.%s: enum without a Go type", g.d.name, n.path)
				}
				if enc {
					g.p("w.String(%sName(int64(%s)))", e.name, v)
				} else {
					g.p("%s = %s(%sValue(r, r.String()))", v, n.as, e.name)
				}
				continue
			}
			if conv, ok := g.conversion(n); ok {
				// A field written again as another integer type.
				m := plain[n.wire]
				if enc {
					g.p("w.%s(%s(%s))", m, goTypes[n.wire], v)
				} else {
					g.p("%s = %s(r.%s())", v, conv, m)
				}
				continue
			}
			if err := g.value(n.wire, v, enc); err != nil {
				return fmt.Errorf("%s.%s: %w", g.d.name, n.path, err)
			}
		case "if":
			cond, err := g.cond(n.expr)
			if err != nil {
				return err
			}
			g.p("if %s {", cond)
			if err := g.body(n.body, enc); err != nil {
				return err
			}
			if len(n.elses) > 0 {
				g.p("} else {")
				if err := g.body(n.elses, enc); err != nil {
					return err
				}
			}
			g.p("}")
		case "present":
			cond, err := g.cond(n.expr)
			if err != nil {
				return err
			}
			if enc {
				ok := g.temp()
				g.p("%s := %s", ok, cond)
				g.p("w.Bool(%s)", ok)
				g.p("if %s {", ok)
			} else {
				g.p("if r.Bool() {")
			}
			if err := g.body(n.body, enc); err != nil {
				return err
			}
			g.p("}")
		case "switch":
			tag, err := g.cond(n.expr)
			if err != nil {
				return err
			}
			g.p("switch %s {", tag)
			for _, c := range n.body {
				if c.kind == "default" {
					g.p("default:")
				} else {
					var vals []string
					for _, v := range c.vals {
						cv, err := g.cond(v)
						if err != nil {
							return err
						}
						vals = append(vals, cv)
					}
					g.p("case %s:", strings.Join(vals, ", "))
				}
				if err := g.body(c.body, enc); err != nil {
					return err
				}
			}
			g.p("}")
		}
	}
	return nil
}

// conversion returns the Go type of a field a node writes as an integer of
// another type, as a field written twice with two encodings is.
func (g *gen) conversion(n *node) (string, bool) {
	if strings.Contains(n.path, "[") {
		return "", false
	}
	declared, ok := g.types[n.path]
	wire, isPlain := goTypes[n.wire]
	if !ok || !isPlain || declared == wire || !integerType(declared) || !integerType(wire) {
		return "", false
	}
	return declared, true
}

func integerType(t string) bool {
	switch t {
	case "int8", "uint8", "int16", "uint16", "int32", "uint32", "int64", "uint64":
		return true
	}
	return false
}

// cond rewrites an expression of the schema to Go: field names become
// fields of p.
func (g *gen) cond(expr string) (string, error) {
	e, err := parser.ParseExpr(expr)
	if err != nil {
		return "", fmt.Errorf("%s: expression %q: %w", g.d.name, expr, err)
	}
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			ast.Inspect(n.X, func(m ast.Node) bool {
				if id, ok := m.(*ast.Ident); ok && g.names[id.Name] {
					id.Name = "p." + id.Name
				}
				return true
			})
			return false
		case *ast.Ident:
			if g.names[n.Name] {
				n.Name = "p." + n.Name
			}
		}
		return true
	})
	var b bytes.Buffer
	if err := printer.Fprint(&b, token.NewFileSet(), e); err != nil {
		return "", err
	}
	return b.String(), nil
}

func (g *gen) temp() string {
	g.tmp++
	return "v" + strconv.Itoa(g.tmp)
}

// value writes the code of a value of a wire type.
func (g *gen) value(w, v string, enc bool) error {
	kind, elem, count := baseOf(w)
	switch kind {
	case "optional":
		return g.optional(elem, v, enc)
	case "list":
		return g.list(elem, count, v, enc)
	}
	return g.single(w, v, enc)
}

// optional writes the code of an optional value of a wire type.
func (g *gen) optional(elem, v string, enc bool) error {
	if enc {
		g.p("w.Bool(%s.Set)", v)
	} else {
		g.p("%s.Set = r.Bool()", v)
	}
	g.p("if %s.Set {", v)
	if err := g.value(elem, v+".Val", enc); err != nil {
		return err
	}
	g.p("}")
	return nil
}

// list writes the code of a list of elements of a wire type.
func (g *gen) list(elem, count, v string, enc bool) error {
	i := g.temp()
	if enc {
		g.listCount(count, v)
	} else if err := g.listSlice(elem, count, v); err != nil {
		return err
	}
	g.p("for %s := range %s {", i, v)
	if err := g.value(elem, v+"["+i+"]", enc); err != nil {
		return err
	}
	g.p("}")
	return nil
}

// listCount writes the count of a list, unless another field holds it.
func (g *gen) listCount(count, v string) {
	switch {
	case count == "":
		g.p("w.Varuint32(uint32(len(%s)))", v)
	case strings.HasPrefix(count, "count "):
		t := strings.TrimPrefix(count, "count ")
		g.p("w.%s(%s(len(%s)))", method(t), goTypes[t], v)
	}
}

// listSlice reads the count of a list, or takes it from another field, and
// makes the slice of the list.
func (g *gen) listSlice(elem, count, v string) error {
	n := g.temp()
	switch {
	case count == "":
		g.p("%s := int(r.Varuint32())", n)
	case strings.HasPrefix(count, "count "):
		t := strings.TrimPrefix(count, "count ")
		g.p("%s := int(r.%s())", n, method(t))
	default:
		lenExpr, err := g.cond(strings.TrimPrefix(count, "len "))
		if err != nil {
			return err
		}
		g.p("%s := int(%s)", n, lenExpr)
	}
	et, err := g.s.goType(elem)
	if err != nil {
		return err
	}
	g.p("%s = wire.Slice[%s](r, %s)", v, et, n)
	return nil
}

// single writes the code of a value of a wire type that is neither a list
// nor optional.
func (g *gen) single(w, v string, enc bool) error {
	if m, ok := plain[w]; ok {
		g.code(enc, fmt.Sprintf("w.%s(%s)", m, v), fmt.Sprintf("%s = r.%s()", v, m))
		return nil
	}
	if f, ok := composite[w]; ok {
		g.code(enc, fmt.Sprintf("proto.Write%s(w, &%s)", f, v), fmt.Sprintf("proto.Read%s(r, &%s)", f, v))
		return nil
	}
	if _, ok := g.s.byName[w]; ok {
		g.code(enc, v+".Encode(w)", v+".Decode(r)")
		return nil
	}
	if t, ok := special[specialBase(w)]; ok {
		arg := specialArg(w)
		g.code(enc, fmt.Sprintf("write%s(w, &%s%s)", t, v, arg), fmt.Sprintf("read%s(r, &%s%s)", t, v, arg))
		return nil
	}
	return fmt.Errorf("no codec for %q", w)
}

// code writes the line that encodes or the one that decodes.
func (g *gen) code(enc bool, encode, decode string) {
	if enc {
		g.p("%s", encode)
	} else {
		g.p("%s", decode)
	}
}

// plain maps wire types to the methods of wire.Writer and wire.Reader.
var plain = map[string]string{
	"bool": "Bool", "uint8": "Uint8", "int8": "Int8", "uint16": "Uint16", "int16": "Int16",
	"uint32": "Uint32", "int32": "Int32", "int32be": "Int32BE", "uint64": "Uint64", "int64": "Int64",
	"float32": "Float32", "float64": "Float64", "varint32": "Varint32", "varuint32": "Varuint32",
	"varint64": "Varint64", "varuint64": "Varuint64", "string": "String", "bytes": "ByteSlice",
	"bytefloat": "ByteFloat",
}

// composite maps wire types to the helpers of runtime.go.
var composite = map[string]string{
	"string16": "String16", "rest": "Rest", "uuid": "UUID", "vec2": "Vec2", "vec3": "Vec3",
	"soundpos": "SoundPos", "blockpos": "BlockPos", "chunkpos": "ChunkPos", "subchunkpos": "SubChunkPos",
	"rgb": "RGB", "rgba": "RGBA", "argbbe": "ARGBBE", "nbt": "NBT", "nbtlist": "NBTList",
}

func method(t string) string {
	if m, ok := plain[t]; ok {
		return m
	}
	return "Varuint32"
}
