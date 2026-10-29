// Command protoimport writes the schema of the Bedrock packets of one
// protocol version from the codecs of gophertunnel, whose Marshal methods
// read and write every field in order through one IO interface. The schema
// is the source of the generated codecs of package bedrock/proto: this
// command seeds it and, with a newer gophertunnel, shows what a version
// changed.
//
// A statement it cannot translate marks its packet or type manual, with the
// reason, instead of guessing.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/constant"
	"go/printer"
	"go/token"
	"go/types"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

const (
	protocolPath = "github.com/sandertv/gophertunnel/minecraft/protocol"
	packetPath   = protocolPath + "/packet"
)

// primitives maps the methods of protocol.IO that read and write one value
// to the wire types of the schema.
var primitives = map[string]string{
	"Bool": "bool", "Uint8": "uint8", "Int8": "int8", "Uint16": "uint16", "Int16": "int16",
	"Uint32": "uint32", "Int32": "int32", "BEInt32": "int32be", "Uint64": "uint64", "Int64": "int64",
	"Float32": "float32", "Float64": "float64",
	"Varint32": "varint32", "Varuint32": "varuint32", "Varint64": "varint64", "Varuint64": "varuint64",
	"ActorRuntimeID": "varuint64", "ActorRuntimeIDVarint64": "varint64", "ActorRuntimeIDVaruint32": "varuint32",
	"ActorUniqueID": "varint64", "ActorUniqueIDInt64": "int64", "ActorUniqueIDUint64": "uint64",
	"ActorUniqueIDVaruint64": "varuint64",
	"String":                 "string", "StringUTF": "string16", "ByteSlice": "bytes", "Bytes": "rest",
	"Vec3": "vec3", "Vec2": "vec2", "BlockPos": "blockpos", "ChunkPos": "chunkpos",
	"SubChunkPos": "subchunkpos", "SoundPos": "soundpos", "ByteFloat": "bytefloat",
	"UUID": "uuid", "RGB": "rgb", "RGBA": "rgba", "BEARGB": "argbbe",
	// Values with codecs of their own, written by hand.
	"EntityMetadata": "metadata", "Item": "itemstack", "ItemInstance": "iteminstance",
	"StackRequestItem": "stackrequestitem", "ItemDescriptorCount": "itemdescriptorcount",
	"StackRequestAction": "stackrequestaction", "MaterialReducer": "materialreducer",
	"EventType": "eventtype", "EventOrdinal": "eventordinal", "TransactionDataType": "transactiondata",
	"PlayerInventoryAction": "playerinventoryaction", "GameRule": "gamerule",
	"AbilityValue": "abilityvalue", "PackSetting": "packsetting", "ShapeData": "shapedata",
}

// goInteger maps the integer wire types to the Go types they decode to.
var goInteger = map[string]string{
	"uint8": "uint8", "int8": "int8", "uint16": "uint16", "int16": "int16", "uint32": "uint32",
	"varuint32": "uint32", "int32": "int32", "int32be": "int32", "varint32": "int32",
	"uint64": "uint64", "varuint64": "uint64", "int64": "int64", "varint64": "int64",
}

// countTypes maps the list helpers to the encoding of their count.
var countTypes = map[string]string{
	"Slice": "", "FuncSlice": "", "FuncIOSlice": "",
	"SliceUint8Length": "uint8", "SliceUint32Length": "uint32", "FuncSliceUint32Length": "uint32",
}

type decl struct {
	packet bool
	id     uint64
	name   string
	lines  []string
	manual string
}

type importer struct {
	pkgs   map[string]*packages.Package
	funcs  map[*types.Func]*ast.FuncDecl
	owner  map[*ast.FuncDecl]*packages.Package
	ioType *types.Interface
	types  map[*types.TypeName]*decl
	// writers holds the codecs imported from methods of Writer, nil for a
	// method that has logic of its own.
	writers map[string]*decl
	queue   []*types.TypeName
	decls   []*decl
}

func main() {
	out := flag.String("o", "", "schema file to write; stdout if empty")
	flag.Parse()
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedSyntax | packages.NeedTypes |
		packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps}
	pkgs, err := packages.Load(cfg, protocolPath, packetPath)
	if err != nil {
		log.Fatal(err)
	}
	im := &importer{
		pkgs:    map[string]*packages.Package{},
		funcs:   map[*types.Func]*ast.FuncDecl{},
		owner:   map[*ast.FuncDecl]*packages.Package{},
		types:   map[*types.TypeName]*decl{},
		writers: map[string]*decl{},
	}
	for _, p := range pkgs {
		if len(p.Errors) > 0 {
			log.Fatal(p.Errors)
		}
		im.pkgs[p.PkgPath] = p
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok {
					if fn, ok := p.TypesInfo.Defs[fd.Name].(*types.Func); ok {
						im.funcs[fn] = fd
						im.owner[fd] = p
					}
				}
			}
		}
	}
	im.ioType = im.pkgs[protocolPath].Types.Scope().Lookup("IO").Type().Underlying().(*types.Interface)
	im.packets()
	// Every type with a codec: codecs written by hand dispatch to them.
	for _, path := range []string{protocolPath, packetPath} {
		scope := im.pkgs[path].Types.Scope()
		for _, name := range scope.Names() {
			if tn, ok := scope.Lookup(name).(*types.TypeName); ok {
				if _, isPacket := im.packetID(tn); !isPacket {
					im.ref(tn.Type())
				}
			}
		}
	}
	for len(im.queue) > 0 {
		tn := im.queue[0]
		im.queue = im.queue[1:]
		im.typeDecl(tn)
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, "# Bedrock protocol, imported by protoimport from gophertunnel.\n")
	manual := 0
	for _, d := range im.decls {
		if d.manual != "" {
			manual++
		}
	}
	fmt.Fprintf(&b, "# %d declarations, %d of them manual.\n", len(im.decls), manual)
	for _, d := range im.decls {
		b.WriteString("\n")
		if d.packet {
			fmt.Fprintf(&b, "packet %d %s\n", d.id, d.name)
		} else {
			fmt.Fprintf(&b, "type %s\n", d.name)
		}
		if d.manual != "" {
			fmt.Fprintf(&b, "\tmanual %s\n", strings.ReplaceAll(d.manual, "\n", " "))
			continue
		}
		for _, l := range d.lines {
			b.WriteString("\t" + l + "\n")
		}
	}
	if *out == "" {
		os.Stdout.Write(b.Bytes())
		return
	}
	if err := os.WriteFile(*out, b.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("%d declarations, %d manual", len(im.decls), manual)
}

// packets imports every type of package packet with an ID and a codec.
func (im *importer) packets() {
	p := im.pkgs[packetPath]
	var list []*decl
	scope := p.Types.Scope()
	for _, name := range scope.Names() {
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok {
			continue
		}
		id, ok := im.packetID(tn)
		if !ok {
			continue
		}
		marshal := im.method(tn, "Marshal")
		d := &decl{packet: true, id: id, name: name}
		if marshal == nil {
			d.manual = "no Marshal method"
		} else {
			d.lines, d.manual = im.translate(marshal)
		}
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].id < list[j].id })
	im.decls = append(im.decls, list...)
}

// packetID returns the constant an ID method of a type returns.
func (im *importer) packetID(tn *types.TypeName) (uint64, bool) {
	fd := im.method(tn, "ID")
	if fd == nil || fd.Body == nil || len(fd.Body.List) != 1 {
		return 0, false
	}
	ret, ok := fd.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return 0, false
	}
	tv := im.owner[fd].TypesInfo.Types[ret.Results[0]]
	if tv.Value == nil {
		return 0, false
	}
	v, ok := constant.Uint64Val(tv.Value)
	return v, ok
}

// method returns the declaration of a method of a named type.
func (im *importer) method(tn *types.TypeName, name string) *ast.FuncDecl {
	obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(tn.Type()), true, tn.Pkg(), name)
	fn, ok := obj.(*types.Func)
	if !ok {
		return nil
	}
	return im.funcs[fn]
}

// writerCodec imports the codec a method of gophertunnel's Writer gives a
// special wire type, when it writes the fields of a struct in order; it
// reports false for a codec with logic of its own.
func (im *importer) writerCodec(method string, t types.Type) (string, bool) {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return "", false
	}
	if _, isStruct := named.Underlying().(*types.Struct); !isStruct {
		return "", false
	}
	if d, ok := im.writers[method]; ok {
		return method, d != nil
	}
	im.writers[method] = nil
	writer, ok := im.pkgs[protocolPath].Types.Scope().Lookup("Writer").(*types.TypeName)
	if !ok {
		return "", false
	}
	obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(writer.Type()), true, writer.Pkg(), method)
	fn, ok := obj.(*types.Func)
	if !ok || im.funcs[fn] == nil {
		return "", false
	}
	fd := im.funcs[fn]
	p := im.owner[fd]
	var names []*ast.Ident
	for _, f := range fd.Type.Params.List {
		names = append(names, f.Names...)
	}
	if len(names) != 1 || len(fd.Recv.List[0].Names) != 1 {
		return "", false
	}
	c := &ctx{im: im, pkg: p, fields: map[string]string{}, counts: map[types.Object]string{},
		io: p.TypesInfo.Defs[fd.Recv.List[0].Names[0]], recv: p.TypesInfo.Defs[names[0]]}
	if err := c.block(fd.Body.List, 0); err != nil {
		return "", false
	}
	// The codec takes the name of the method: the type may have another of
	// its own, as UseItemTransactionData has.
	d := &decl{name: method, lines: c.lines}
	im.writers[method] = d
	im.decls = append(im.decls, d)
	return method, true
}

// ref returns the schema name of a named type with a codec of its own,
// queueing its import.
func (im *importer) ref(t types.Type) (string, bool) {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return "", false
	}
	tn := named.Obj()
	if im.method(tn, "Marshal") == nil {
		return "", false
	}
	if _, ok := im.types[tn]; !ok {
		d := &decl{name: tn.Name()}
		im.types[tn] = d
		im.queue = append(im.queue, tn)
	}
	return tn.Name(), true
}

func (im *importer) typeDecl(tn *types.TypeName) {
	d := im.types[tn]
	d.lines, d.manual = im.translate(im.method(tn, "Marshal"))
	im.decls = append(im.decls, d)
}

// translate imports the body of a codec: a Marshal method, or a function
// taking the IO and a pointer to the value.
func (im *importer) translate(fd *ast.FuncDecl) ([]string, string) {
	p := im.owner[fd]
	c := &ctx{im: im, pkg: p, fields: map[string]string{}, counts: map[types.Object]string{}}
	var names []*ast.Ident
	for _, f := range fd.Type.Params.List {
		names = append(names, f.Names...)
	}
	switch {
	case fd.Recv != nil && len(fd.Recv.List[0].Names) == 1 && len(names) == 1:
		c.recv = p.TypesInfo.Defs[fd.Recv.List[0].Names[0]]
		c.io = p.TypesInfo.Defs[names[0]]
	case fd.Recv == nil && len(names) == 2:
		c.io = p.TypesInfo.Defs[names[0]]
		c.recv = p.TypesInfo.Defs[names[1]]
	}
	if len(fd.Body.List) == 0 {
		// A packet without fields.
		return nil, ""
	}
	if c.recv == nil || c.io == nil {
		return nil, "codec is not a method taking an IO"
	}
	if err := c.block(fd.Body.List, 0); err != nil {
		return nil, err.Error()
	}
	return c.lines, ""
}

// ctx translates one codec.
type ctx struct {
	im     *importer
	pkg    *packages.Package
	recv   types.Object
	io     types.Object
	lines  []string
	fields map[string]string
	// counts maps the locals that hold the length of a list to the wire
	// type they are written as.
	counts map[types.Object]string
}

func (c *ctx) emit(depth int, s string) {
	c.lines = append(c.lines, strings.Repeat("\t", depth)+s)
}

func (c *ctx) src(n ast.Node) string {
	var b bytes.Buffer
	_ = printer.Fprint(&b, token.NewFileSet(), n)
	return b.String()
}

func (c *ctx) block(stmts []ast.Stmt, depth int) error {
	for i := 0; i < len(stmts); i++ {
		s := stmts[i]
		if as, ok := s.(*ast.AssignStmt); ok && as.Tok == token.DEFINE {
			// n := T(len(x.F)) written as io.M(&n): the count of a list
			// read and written with SliceOfLen.
			if obj, wire, ok := c.countLocal(as, stmts[i+1:]); ok {
				c.counts[obj] = wire
				i++
				continue
			}
			if c.validationOnly(as, stmts[i+1:]) {
				continue
			}
		}
		if err := c.stmt(s, depth); err != nil {
			return err
		}
	}
	return nil
}

// countLocal recognises a local holding the length of a list that the next
// statement writes.
func (c *ctx) countLocal(as *ast.AssignStmt, rest []ast.Stmt) (types.Object, string, bool) {
	if len(as.Lhs) != 1 || len(as.Rhs) != 1 || len(rest) == 0 {
		return nil, "", false
	}
	id, ok := as.Lhs[0].(*ast.Ident)
	if !ok {
		return nil, "", false
	}
	conv, ok := as.Rhs[0].(*ast.CallExpr)
	if !ok || len(conv.Args) != 1 {
		return nil, "", false
	}
	inner, ok := conv.Args[0].(*ast.CallExpr)
	if !ok {
		return nil, "", false
	}
	if fun, isID := inner.Fun.(*ast.Ident); !isID || fun.Name != "len" {
		return nil, "", false
	}
	es, ok := rest[0].(*ast.ExprStmt)
	if !ok {
		return nil, "", false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil, "", false
	}
	name, ok := c.ioMethod(call)
	if !ok {
		return nil, "", false
	}
	u, ok := call.Args[0].(*ast.UnaryExpr)
	if !ok || u.Op != token.AND {
		return nil, "", false
	}
	if arg, ok := u.X.(*ast.Ident); !ok || arg.Name != id.Name {
		return nil, "", false
	}
	wire, ok := primitives[name]
	if !ok {
		return nil, "", false
	}
	return c.pkg.TypesInfo.Defs[id], wire, true
}

// validationOnly reports a local whose every use reports invalid values.
func (c *ctx) validationOnly(as *ast.AssignStmt, rest []ast.Stmt) bool {
	objs := map[types.Object]bool{}
	for _, l := range as.Lhs {
		if id, ok := l.(*ast.Ident); ok {
			if obj := c.pkg.TypesInfo.Defs[id]; obj != nil {
				objs[obj] = true
			}
		}
	}
	if len(objs) == 0 {
		return false
	}
	ok := true
	for _, st := range rest {
		if ifs, isIf := st.(*ast.IfStmt); isIf && c.validation(ifs) {
			continue
		}
		ast.Inspect(st, func(n ast.Node) bool {
			if id, isID := n.(*ast.Ident); isID && objs[c.pkg.TypesInfo.Uses[id]] {
				ok = false
			}
			return ok
		})
	}
	return ok
}

func (c *ctx) stmt(s ast.Stmt, depth int) error {
	switch s := s.(type) {
	case *ast.ExprStmt:
		call, ok := s.X.(*ast.CallExpr)
		if !ok {
			return fmt.Errorf("expression %s", c.src(s))
		}
		return c.call(call, depth)
	case *ast.IfStmt:
		if c.validation(s) {
			return nil
		}
		if s.Init != nil {
			return fmt.Errorf("if with a statement: %s", c.src(s.Init))
		}
		cond, err := c.expr(s.Cond)
		if err != nil {
			return err
		}
		c.emit(depth, "if "+cond)
		if err := c.block(s.Body.List, depth+1); err != nil {
			return err
		}
		switch e := s.Else.(type) {
		case nil:
		case *ast.BlockStmt:
			c.emit(depth, "else")
			return c.block(e.List, depth+1)
		case *ast.IfStmt:
			c.emit(depth, "else")
			return c.stmt(e, depth+1)
		}
		return nil
	case *ast.SwitchStmt:
		if s.Init != nil || s.Tag == nil {
			return fmt.Errorf("switch without a value: %s", c.src(s))
		}
		tag, err := c.expr(s.Tag)
		if err != nil {
			return err
		}
		c.emit(depth, "switch "+tag)
		for _, cs := range s.Body.List {
			cc := cs.(*ast.CaseClause)
			if cc.List == nil {
				c.emit(depth, "default")
			} else {
				var vals []string
				for _, v := range cc.List {
					s, err := c.expr(v)
					if err != nil {
						return err
					}
					vals = append(vals, s)
				}
				c.emit(depth, "case "+strings.Join(vals, ", "))
			}
			if err := c.block(cc.Body, depth+1); err != nil {
				return err
			}
		}
		return nil
	case *ast.BlockStmt:
		return c.block(s.List, depth)
	case *ast.EmptyStmt:
		return nil
	}
	return fmt.Errorf("statement %s", firstLine(c.src(s)))
}

// validation reports an if statement that only reports invalid values.
func (c *ctx) validation(s *ast.IfStmt) bool {
	if s.Else != nil {
		return false
	}
	for _, st := range s.Body.List {
		if _, ok := st.(*ast.ReturnStmt); ok {
			continue
		}
		es, ok := st.(*ast.ExprStmt)
		if !ok {
			return false
		}
		call, ok := es.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		name, ok := c.ioMethod(call)
		if !ok || name != "InvalidValue" && name != "UnknownEnumOption" {
			return false
		}
	}
	return true
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

// callee returns the function a call calls.
func (c *ctx) callee(call *ast.CallExpr) *types.Func {
	fun := call.Fun
	switch f := fun.(type) {
	case *ast.IndexExpr:
		fun = f.X
	case *ast.IndexListExpr:
		fun = f.X
	}
	var id *ast.Ident
	switch f := fun.(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	default:
		return nil
	}
	fn, _ := c.pkg.TypesInfo.Uses[id].(*types.Func)
	return fn
}

// ioMethod returns the name of the IO method a call calls.
func (c *ctx) ioMethod(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || c.pkg.TypesInfo.Uses[id] != c.io {
		return "", false
	}
	return sel.Sel.Name, true
}

// ioFunc returns the wire type of a method value of the IO passed as a
// function for values of type elem.
func (c *ctx) ioFunc(e ast.Expr, elem types.Type) (string, bool) {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || c.pkg.TypesInfo.Uses[id] != c.io {
		return "", false
	}
	w, ok := primitives[sel.Sel.Name]
	if ok && elem != nil {
		if t, isCodec := c.im.writerCodec(sel.Sel.Name, elem); isCodec {
			return t, true
		}
	}
	return w, ok
}

// optionalElem returns the type an Optional pointer argument holds.
func (c *ctx) optionalElem(e ast.Expr) types.Type {
	if named, ok := types.Unalias(c.pointee(e)).(*types.Named); ok && named.TypeArgs().Len() == 1 {
		return named.TypeArgs().At(0)
	}
	return nil
}

func (c *ctx) call(call *ast.CallExpr, depth int) error {
	if name, ok := c.ioMethod(call); ok {
		switch name {
		case "InvalidValue", "UnknownEnumOption":
			return nil
		case "NBT", "NBTList":
			path, err := c.field(call.Args[0])
			if err != nil {
				return err
			}
			w := "nbt"
			if name == "NBTList" {
				w = "nbtlist"
			}
			if enc := c.src(call.Args[1]); enc != "nbt.NetworkLittleEndian" {
				w += "(" + strings.TrimPrefix(enc, "nbt.") + ")"
			}
			return c.put(depth, path, w)
		case "Bitset":
			path, err := c.field(call.Args[0])
			if err != nil {
				return err
			}
			size, err := c.expr(call.Args[1])
			if err != nil {
				return err
			}
			return c.put(depth, path, "bitset("+size+")")
		}
		w, ok := primitives[name]
		if !ok {
			return fmt.Errorf("IO method %s", name)
		}
		path, err := c.field(call.Args[0])
		if err != nil {
			return err
		}
		if t, ok := c.im.writerCodec(name, c.pointee(call.Args[0])); ok && primitives[name] != "" {
			return c.put(depth, path, t)
		}
		return c.put(depth, path, w)
	}
	fn := c.callee(call)
	if fn == nil {
		return fmt.Errorf("call %s", c.src(call))
	}
	if fn.Pkg() != nil && fn.Pkg().Path() == protocolPath && fn.Type().(*types.Signature).Recv() == nil {
		if _, ok := countTypes[fn.Name()]; ok || helpers[fn.Name()] {
			return c.helper(fn.Name(), call, depth)
		}
		return c.codecFunc(fn, call, depth)
	}
	sig := fn.Type().(*types.Signature)
	if fn.Name() == "Marshal" && sig.Recv() != nil {
		// x.F.Marshal(io): a value with a codec of its own.
		sel := call.Fun.(*ast.SelectorExpr)
		path, err := c.path(sel.X)
		if err != nil {
			return err
		}
		name, ok := c.im.ref(c.pkg.TypesInfo.TypeOf(sel.X))
		if !ok {
			return fmt.Errorf("Marshal of %s", c.src(sel.X))
		}
		return c.put(depth, path, name)
	}
	if sig.Recv() != nil && c.isRecvCall(call) {
		// A helper method of the same value: its statements go inline.
		fd := c.im.funcs[fn]
		if fd == nil {
			return fmt.Errorf("helper %s without source", fn.Name())
		}
		inner := &ctx{im: c.im, pkg: c.im.owner[fd], fields: c.fields}
		inner.recv = inner.pkg.TypesInfo.Defs[fd.Recv.List[0].Names[0]]
		params := fd.Type.Params.List
		if len(params) != 1 || len(params[0].Names) != 1 || len(call.Args) != 1 {
			return fmt.Errorf("helper %s takes more than the IO", fn.Name())
		}
		inner.io = inner.pkg.TypesInfo.Defs[params[0].Names[0]]
		if err := inner.block(fd.Body.List, depth); err != nil {
			return err
		}
		c.lines = append(c.lines, inner.lines...)
		return nil
	}
	return fmt.Errorf("call %s", firstLine(c.src(call)))
}

// helpers are the generic helpers of package protocol helper translates.
var helpers = map[string]bool{
	"SliceOfLen": true, "FuncSliceOfLen": true, "Single": true, "OptionalFunc": true,
	"OptionalMarshaler": true, "IntegerFunc": true,
}

// codecFunc translates a call of a function of package protocol taking the
// IO and a pointer: inline when the pointer is the value itself, else a
// field of the type it codes.
func (c *ctx) codecFunc(fn *types.Func, call *ast.CallExpr, depth int) error {
	if fn.Name() == "InputFlagList" && len(call.Args) == 3 {
		path, err := c.field(call.Args[1])
		if err != nil {
			return err
		}
		size, err := c.expr(call.Args[2])
		if err != nil {
			return err
		}
		return c.put(depth, path, "inputflags("+size+")")
	}
	fd := c.im.funcs[fn]
	if fd == nil || len(call.Args) != 2 {
		return fmt.Errorf("call %s", firstLine(c.src(call)))
	}
	var names []*ast.Ident
	for _, f := range fd.Type.Params.List {
		names = append(names, f.Names...)
	}
	if len(names) != 2 {
		return fmt.Errorf("call %s", firstLine(c.src(call)))
	}
	if id, ok := call.Args[1].(*ast.Ident); ok && c.pkg.TypesInfo.Uses[id] == c.recv {
		p := c.im.owner[fd]
		inner := &ctx{im: c.im, pkg: p, fields: c.fields, counts: map[types.Object]string{},
			io: p.TypesInfo.Defs[names[0]], recv: p.TypesInfo.Defs[names[1]]}
		if err := inner.block(fd.Body.List, depth); err != nil {
			return err
		}
		c.lines = append(c.lines, inner.lines...)
		return nil
	}
	path, err := c.field(call.Args[1])
	if err != nil {
		return err
	}
	named, ok := types.Unalias(c.pointee(call.Args[1])).(*types.Named)
	if !ok {
		return fmt.Errorf("call %s", firstLine(c.src(call)))
	}
	tn := named.Obj()
	if _, ok := c.im.types[tn]; !ok {
		d := &decl{name: tn.Name()}
		c.im.types[tn] = d
		d.lines, d.manual = c.im.translate(fd)
		c.im.decls = append(c.im.decls, d)
	}
	return c.put(depth, path, tn.Name())
}

// isRecvCall reports a method called on the value whose codec this is.
func (c *ctx) isRecvCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && c.pkg.TypesInfo.Uses[id] == c.recv
}

// helper translates a call of a codec helper of package protocol.
func (c *ctx) helper(name string, call *ast.CallExpr, depth int) error {
	args := call.Args
	switch name {
	case "Slice", "SliceUint8Length", "SliceUint32Length":
		path, elem, err := c.sliceField(args[1])
		if err != nil {
			return err
		}
		t, ok := c.im.ref(elem)
		if !ok {
			return fmt.Errorf("%s of %s", name, elem)
		}
		return c.put(depth, path, list("[]"+t, countTypes[name]))
	case "FuncIOSlice":
		path, elem, err := c.sliceField(args[1])
		if err != nil {
			return err
		}
		t, err := c.funcType(args[2], elem)
		if err != nil {
			return err
		}
		return c.put(depth, path, "[]"+t)
	case "FuncSlice", "FuncSliceUint32Length":
		path, elem, err := c.sliceField(args[1])
		if err != nil {
			return err
		}
		w, ok := c.ioFunc(args[2], elem)
		if !ok {
			return fmt.Errorf("%s with %s", name, c.src(args[2]))
		}
		return c.put(depth, path, list("[]"+w, countTypes[name]))
	case "SliceOfLen", "FuncSliceOfLen":
		path, elem, err := c.sliceField(args[2])
		if err != nil {
			return err
		}
		var n string
		countWire := ""
		if id, ok := args[1].(*ast.Ident); ok {
			countWire = c.counts[c.pkg.TypesInfo.Uses[id]]
		}
		if countWire == "" {
			var err error
			if n, err = c.expr(args[1]); err != nil {
				return err
			}
		}
		var t string
		if name == "SliceOfLen" {
			var ok bool
			if t, ok = c.im.ref(elem); !ok {
				return fmt.Errorf("%s of %s", name, elem)
			}
		} else {
			var ok bool
			if t, ok = c.ioFunc(args[3], elem); !ok {
				return fmt.Errorf("%s with %s", name, c.src(args[3]))
			}
		}
		if countWire != "" {
			return c.put(depth, path, list("[]"+t, countWire))
		}
		return c.put(depth, path, "[]"+t+" len "+n)
	case "Single":
		path, err := c.field(args[1])
		if err != nil {
			return err
		}
		t, ok := c.im.ref(c.pointee(args[1]))
		if !ok {
			return fmt.Errorf("Single of %s", c.src(args[1]))
		}
		return c.put(depth, path, t)
	case "OptionalFunc":
		path, err := c.field(args[1])
		if err != nil {
			return err
		}
		w, ok := c.ioFunc(args[2], c.optionalElem(args[1]))
		if !ok {
			if w, ok = c.closureList(args[2]); !ok {
				return fmt.Errorf("OptionalFunc with %s", c.src(args[2]))
			}
		}
		return c.put(depth, path, "?"+w)
	case "OptionalMarshaler":
		path, err := c.field(args[1])
		if err != nil {
			return err
		}
		named, ok := types.Unalias(c.pointee(args[1])).(*types.Named)
		if !ok || named.TypeArgs().Len() != 1 {
			return fmt.Errorf("OptionalMarshaler of %s", c.src(args[1]))
		}
		t, ok := c.im.ref(named.TypeArgs().At(0))
		if !ok {
			return fmt.Errorf("OptionalMarshaler of %s", named.TypeArgs().At(0))
		}
		return c.put(depth, path, "?"+t)
	case "IntegerFunc":
		path, err := c.field(args[0])
		if err != nil {
			return err
		}
		w, ok := c.ioFunc(args[1], nil)
		if !ok {
			return fmt.Errorf("IntegerFunc with %s", c.src(args[1]))
		}
		// The field keeps its own integer type, converted on the wire.
		if b, ok := c.pointee(args[0]).Underlying().(*types.Basic); ok && b.Name() != goInteger[w] {
			w += " as " + b.Name()
		}
		return c.put(depth, path, w)
	}
	return fmt.Errorf("helper %s", name)
}

// closureList translates func(x *[]T) { FuncSlice(io, x, io.M) }.
func (c *ctx) closureList(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.FuncLit)
	if !ok || len(lit.Body.List) != 1 {
		return "", false
	}
	es, ok := lit.Body.List[0].(*ast.ExprStmt)
	if !ok {
		return "", false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	fn := c.callee(call)
	if fn == nil {
		return "", false
	}
	if fn.Name() == "Marshal" && len(lit.Type.Params.List) == 1 {
		// func(x *T) { x.Marshal(io) }: a value with a codec of its own.
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return "", false
		}
		if p, ok := c.pkg.TypesInfo.TypeOf(sel.X).(*types.Pointer); ok {
			return c.im.ref(p.Elem())
		}
		return c.im.ref(c.pkg.TypesInfo.TypeOf(sel.X))
	}
	count, ok := countTypes[fn.Name()]
	if !ok {
		return "", false
	}
	if !strings.HasPrefix(fn.Name(), "Func") {
		// Slice(io, x): a list of values with codecs of their own.
		if len(call.Args) != 2 {
			return "", false
		}
		s, ok := c.pkg.TypesInfo.TypeOf(call.Args[1]).(*types.Pointer)
		if !ok {
			return "", false
		}
		sl, ok := s.Elem().Underlying().(*types.Slice)
		if !ok {
			return "", false
		}
		t, ok := c.im.ref(sl.Elem())
		if !ok {
			return "", false
		}
		return list("[]"+t, count), true
	}
	if len(call.Args) != 3 {
		return "", false
	}
	var elem types.Type
	if p, ok := c.pkg.TypesInfo.TypeOf(call.Args[1]).(*types.Pointer); ok {
		if sl, ok := p.Elem().Underlying().(*types.Slice); ok {
			elem = sl.Elem()
		}
	}
	w, ok := c.ioFunc(call.Args[2], elem)
	if !ok {
		return "", false
	}
	return list("[]"+w, count), true
}

func list(t, count string) string {
	if count == "" {
		return t
	}
	return t + " count " + count
}

// funcType returns the type a codec function of package protocol codes,
// importing it.
func (c *ctx) funcType(e ast.Expr, elem types.Type) (string, error) {
	var id *ast.Ident
	switch f := e.(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	default:
		return "", fmt.Errorf("codec %s", c.src(e))
	}
	fn, ok := c.pkg.TypesInfo.Uses[id].(*types.Func)
	if !ok || c.im.funcs[fn] == nil {
		return "", fmt.Errorf("codec %s", c.src(e))
	}
	named, ok := types.Unalias(elem).(*types.Named)
	if !ok {
		return "", fmt.Errorf("codec of %s", elem)
	}
	tn := named.Obj()
	if _, ok := c.im.types[tn]; !ok {
		d := &decl{name: tn.Name()}
		c.im.types[tn] = d
		d.lines, d.manual = c.im.translate(c.im.funcs[fn])
		c.im.decls = append(c.im.decls, d)
	}
	return tn.Name(), nil
}

func (c *ctx) pointee(e ast.Expr) types.Type {
	if p, ok := c.pkg.TypesInfo.TypeOf(e).(*types.Pointer); ok {
		return p.Elem()
	}
	return nil
}

// sliceField returns the path of a pointer to a slice field and the type of
// its elements.
func (c *ctx) sliceField(e ast.Expr) (string, types.Type, error) {
	path, err := c.field(e)
	if err != nil {
		return "", nil, err
	}
	s, ok := c.pointee(e).Underlying().(*types.Slice)
	if !ok {
		return "", nil, fmt.Errorf("list of %s", c.src(e))
	}
	return path, s.Elem(), nil
}

// put records that a field is read and written with a wire type.
func (c *ctx) put(depth int, path, wire string) error {
	if _, ok := c.fields[path]; !ok {
		c.fields[path] = wire
	}
	c.emit(depth, path+" "+wire)
	return nil
}

// field returns the path of the field a pointer argument points to.
func (c *ctx) field(e ast.Expr) (string, error) {
	u, ok := e.(*ast.UnaryExpr)
	if !ok || u.Op != token.AND {
		return "", fmt.Errorf("argument %s is not a field", c.src(e))
	}
	return c.path(u.X)
}

// path returns the path of a field of the value whose codec this is.
func (c *ctx) path(e ast.Expr) (string, error) {
	switch e := e.(type) {
	case *ast.SelectorExpr:
		if id, ok := e.X.(*ast.Ident); ok && c.pkg.TypesInfo.Uses[id] == c.recv {
			return e.Sel.Name, nil
		}
		return "", fmt.Errorf("nested field %s", c.src(e))
	case *ast.ParenExpr:
		return c.path(e.X)
	case *ast.IndexExpr:
		tv := c.pkg.TypesInfo.Types[e.Index]
		if tv.Value == nil {
			return "", fmt.Errorf("index %s", c.src(e))
		}
		i := tv.Value.ExactString()
		if id, ok := e.X.(*ast.Ident); ok && c.pkg.TypesInfo.Uses[id] == c.recv {
			return "self[" + i + "]", nil
		}
		base, err := c.path(e.X)
		if err != nil {
			return "", err
		}
		return base + "[" + i + "]", nil
	case *ast.StarExpr:
		if id, ok := e.X.(*ast.Ident); ok && c.pkg.TypesInfo.Uses[id] == c.recv {
			return "self", nil
		}
	}
	return "", fmt.Errorf("value %s", c.src(e))
}

// expr renders a condition or a case: fields by name, constants by value.
func (c *ctx) expr(e ast.Expr) (string, error) {
	if tv, ok := c.pkg.TypesInfo.Types[e]; ok && tv.Value != nil {
		switch tv.Value.Kind() {
		case constant.Int, constant.Float:
			return tv.Value.ExactString(), nil
		case constant.Bool:
			return strconv.FormatBool(constant.BoolVal(tv.Value)), nil
		case constant.String:
			return strconv.Quote(constant.StringVal(tv.Value)), nil
		}
	}
	switch e := e.(type) {
	case *ast.SelectorExpr:
		return c.path(e)
	case *ast.ParenExpr:
		s, err := c.expr(e.X)
		return "(" + s + ")", err
	case *ast.UnaryExpr:
		s, err := c.expr(e.X)
		return e.Op.String() + s, err
	case *ast.BinaryExpr:
		l, err := c.expr(e.X)
		if err != nil {
			return "", err
		}
		r, err := c.expr(e.Y)
		return l + " " + e.Op.String() + " " + r, err
	case *ast.CallExpr:
		if id, ok := e.Fun.(*ast.Ident); ok && id.Name == "len" && len(e.Args) == 1 {
			s, err := c.expr(e.Args[0])
			return "len(" + s + ")", err
		}
	}
	return "", fmt.Errorf("expression %s", c.src(e))
}
