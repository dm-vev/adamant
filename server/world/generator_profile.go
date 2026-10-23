package world

import (
	"fmt"
	"strings"
)

// GeneratorProfile selects how chunk generation uses the CPUs of the host.
type GeneratorProfile int

const (
	// GeneratorProfileAuto picks GeneratorProfileManyCores on hosts with at
	// least manyCoresThreshold logical CPUs and GeneratorProfileFastCores on
	// others.
	GeneratorProfileAuto GeneratorProfile = iota
	// GeneratorProfileFastCores suits a few fast cores, such as a desktop
	// CPU. It runs one worker per physical core, assuming two logical CPUs
	// per core, and generates each chunk on its worker alone: a fast core
	// finishes a chunk soon enough, and the logical CPUs left over keep the
	// world tick and the network responsive.
	GeneratorProfileFastCores
	// GeneratorProfileManyCores suits many slower cores, such as multi-socket
	// servers. It runs a worker on every logical CPU but two, sizes the
	// generator caches for the wider generation front, and computes the
	// parts of a chunk that depend on its neighbours in parallel, so a slow
	// core does not hold up the first chunks a player waits for.
	GeneratorProfileManyCores
)

// manyCoresThreshold is the number of logical CPUs from which
// GeneratorProfileAuto picks GeneratorProfileManyCores.
const manyCoresThreshold = 24

// ParseGeneratorProfile parses "auto", "fast-cores" or "many-cores".
func ParseGeneratorProfile(s string) (GeneratorProfile, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return GeneratorProfileAuto, nil
	case "fast-cores":
		return GeneratorProfileFastCores, nil
	case "many-cores":
		return GeneratorProfileManyCores, nil
	}
	return GeneratorProfileAuto, fmt.Errorf("unknown generator profile %q: expected \"auto\", \"fast-cores\" or \"many-cores\"", s)
}

// String returns the name ParseGeneratorProfile parses.
func (p GeneratorProfile) String() string {
	switch p {
	case GeneratorProfileFastCores:
		return "fast-cores"
	case GeneratorProfileManyCores:
		return "many-cores"
	}
	return "auto"
}

// resolve returns the profile GeneratorProfileAuto stands for on a host with
// cpus logical CPUs, or p itself.
func (p GeneratorProfile) resolve(cpus int) GeneratorProfile {
	if p != GeneratorProfileAuto {
		return p
	}
	if cpus >= manyCoresThreshold {
		return GeneratorProfileManyCores
	}
	return GeneratorProfileFastCores
}

// workers returns the default number of generation workers of the resolved
// profile p on a host with cpus logical CPUs.
func (p GeneratorProfile) workers(cpus int) int {
	if p == GeneratorProfileManyCores {
		return max(1, cpus-2)
	}
	return max(1, cpus/2)
}

// GenerationTuning describes how a World runs its generator.
type GenerationTuning struct {
	// Workers is the number of workers that call GenerateChunk at once.
	Workers int
	// Parallel is true if the generator should compute the parts of a chunk
	// that do not depend on each other concurrently, beyond its workers.
	Parallel bool
}

// TunableGenerator is a Generator that adapts its caches and parallelism to
// how the World runs it. The World calls TuneGeneration once, before any
// chunk is generated.
type TunableGenerator interface {
	Generator
	TuneGeneration(tuning GenerationTuning)
}
