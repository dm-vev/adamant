package interop

import (
	"archive/zip"
	"bytes"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/df-mc/jsonc"
	"github.com/dm-vev/adamant/net/bedrock/resource"
	"github.com/google/uuid"
	gtresource "github.com/sandertv/gophertunnel/minecraft/resource"
)

// randomManifest returns the manifest of a pack as packs write them: with
// comments of every kind, versions as arrays or strings, and modules.
func randomManifest(r *rand.Rand) string {
	version := func() string {
		if r.IntN(2) == 0 {
			return fmt.Sprintf("[%d, %d, %d]", r.IntN(5), r.IntN(20), r.IntN(100))
		}
		return fmt.Sprintf("%q", fmt.Sprintf("%d.%d.%d", r.IntN(5), r.IntN(20), r.IntN(100)))
	}
	comment := func() string {
		return []string{"", "// a comment\n", "/* block */", "# hash comment\n", "\t", "\n"}[r.IntN(6)]
	}
	types := []string{"resources", "data", "client_data", "skin_pack", "world_template"}
	var modules []string
	for range r.IntN(3) + 1 {
		modules = append(modules, fmt.Sprintf(`{%s"type": %q, "uuid": %q, "version": %s}`, comment(), types[r.IntN(len(types))], uuid.NewString(), version()))
	}
	return fmt.Sprintf(`{%s"format_version": 2,%s"header": {"name": "Pack \\"%d\\"", "description": "d/e\\\\s", "uuid": %q, "version": %s, "min_engine_version": %s},
"modules": [%s]%s}`, comment(), comment(), r.IntN(100), uuid.NewString(), version(), version(), joinComma(modules), comment())
}

func joinComma(s []string) string {
	var b bytes.Buffer
	for i, v := range s {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(v)
	}
	return b.String()
}

// TestResourcePacksMatchGophertunnel reads random packs with the module and
// with gophertunnel, which Adamant used: name, UUID, version, modules,
// checksum, size and kind agree.
func TestResourcePacksMatchGophertunnel(t *testing.T) {
	r := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	for range 300 {
		manifest := randomManifest(r)
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		dir := []string{"", "pack/"}[r.IntN(2)]
		w, _ := z.Create(dir + "manifest.json")
		_, _ = w.Write([]byte(manifest))
		if r.IntN(4) == 0 {
			w, _ = z.Create("level.dat")
			_, _ = w.Write([]byte{1})
		}
		_ = z.Close()
		ours, err := resource.Read(bytes.NewReader(buf.Bytes()))
		theirs, gtErr := gtresource.Read(bytes.NewReader(buf.Bytes()))
		if (err == nil) != (gtErr == nil) {
			t.Fatalf("manifest %s: error %v, gophertunnel %v", manifest, err, gtErr)
		}
		if err != nil {
			continue
		}
		if ours.Name() != theirs.Name() || ours.UUID().String() != theirs.UUID().String() || ours.Version() != theirs.Version() ||
			ours.Description() != theirs.Description() || ours.Checksum() != theirs.Checksum() || ours.Len() != theirs.Len() ||
			ours.HasScripts() != theirs.HasScripts() || ours.HasBehaviours() != theirs.HasBehaviours() ||
			ours.HasTextures() != theirs.HasTextures() || ours.HasWorldTemplate() != theirs.HasWorldTemplate() ||
			len(ours.Modules()) != len(theirs.Modules()) {
			t.Fatalf("manifest %s: %v, gophertunnel %v", manifest, ours, theirs)
		}
	}
}

// TestJSONCMatchesDFMC strips random JSON with comments as df-mc/jsonc,
// which read the manifests before, does.
func TestJSONCMatchesDFMC(t *testing.T) {
	r := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	pieces := []string{"{", "}", "\"a b\"", "\"x\\\"y\"", "\\", "//", "/*", "*/", "#", "\n", "\t", " ", "/", "*", ":", ",", "1", "\"#//\""}
	for range 20000 {
		var b bytes.Buffer
		for range 1 + r.IntN(16) {
			b.WriteString(pieces[r.IntN(len(pieces))])
		}
		var ours, theirs any
		errOurs := resource.UnmarshalJSONC(b.Bytes(), &ours)
		errTheirs := jsonc.UnmarshalLenient(b.Bytes(), &theirs)
		if (errOurs == nil) != (errTheirs == nil) || fmt.Sprint(ours) != fmt.Sprint(theirs) {
			t.Fatalf("%q: %v (%v), df-mc/jsonc %v (%v)", b.String(), ours, errOurs, theirs, errTheirs)
		}
	}
}

// TestPackUUIDsMatchGoogle parses UUIDs of every form google/uuid takes.
func TestPackUUIDsMatchGoogle(t *testing.T) {
	u := uuid.New()
	for _, s := range []string{u.String(), "{" + u.String() + "}", "urn:uuid:" + u.String(), "URN:UUID:" + u.String(),
		fmt.Sprintf("%x", u[:]), "not a uuid", u.String()[:35] + "g", "{" + u.String() + ")"} {
		ours, err := resource.ParseUUID(s)
		theirs, gtErr := uuid.Parse(s)
		if (err == nil) != (gtErr == nil) || err == nil && ours != resource.UUID(theirs) {
			t.Fatalf("%q: %v (%v), google %v (%v)", s, ours, err, theirs, gtErr)
		}
	}
}
