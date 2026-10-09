package javadata

import (
	"fmt"
	"sort"
	"strings"
)

// BlockIsSolid returns the pinned Java BLOCK registry's invariant isSolid value.
// Java 26.3 reports the same value for every state of each built-in block.
func BlockIsSolid(name string) (bool, error) {
	namespace, block, qualified := strings.Cut(name, ":")
	if qualified {
		if namespace != "minecraft" {
			return false, fmt.Errorf("Java solidity: unsupported block namespace %q", namespace)
		}
		name = block
	}
	if name == "" {
		return false, fmt.Errorf("Java solidity: empty block name")
	}
	index := sort.SearchStrings(javaSolidBlockNames[:], name)
	if index == len(javaSolidBlockNames) || javaSolidBlockNames[index] != name {
		return false, fmt.Errorf("Java solidity: unknown built-in block %q", name)
	}
	return javaSolidBlockBits[index/64]&(uint64(1)<<uint(index%64)) != 0, nil
}

// BlockStateIsSolid reads the source solidity of a canonical block state.
func BlockStateIsSolid(id uint32) (bool, bool) {
	index, ok := blockStateIndex(id)
	if !ok {
		return false, false
	}
	return javaSolidBlockBits[index/64]&(uint64(1)<<uint(index%64)) != 0, true
}
