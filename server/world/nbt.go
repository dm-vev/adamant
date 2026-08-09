package world

const blockRegistryNBTKey = "\x00dragonfly:block_registry"

// DecodeNBT decodes data using n while making registry available to nested
// item decoders. Callers that omit registry use DefaultBlockRegistry.
func DecodeNBT(n NBTer, data map[string]any, registries ...BlockRegistry) any {
	return withBlockRegistryNBT(data, blockRegistry(registries), func() any {
		return n.DecodeNBT(data)
	})
}

// DecodeEntityNBT decodes entity-specific data while making registry available
// to nested item decoders. Callers that omit registry use DefaultBlockRegistry.
func DecodeEntityNBT(t EntityType, data map[string]any, entityData *EntityData, registries ...BlockRegistry) {
	withBlockRegistryNBT(data, blockRegistry(registries), func() struct{} {
		t.DecodeNBT(data, entityData)
		return struct{}{}
	})
}

// BlockRegistryFromNBT returns the block registry attached to data for the
// duration of DecodeNBT or DecodeEntityNBT. It returns DefaultBlockRegistry if
// data was decoded without an explicit registry.
func BlockRegistryFromNBT(data map[string]any) BlockRegistry {
	if registry, ok := data[blockRegistryNBTKey].(BlockRegistry); ok && registry != nil {
		return registry
	}
	return DefaultBlockRegistry
}

func blockRegistry(registries []BlockRegistry) BlockRegistry {
	if len(registries) != 0 && registries[0] != nil {
		return registries[0]
	}
	return DefaultBlockRegistry
}

func withBlockRegistryNBT[T any](data map[string]any, registry BlockRegistry, f func() T) T {
	previous, exists := data[blockRegistryNBTKey]
	data[blockRegistryNBTKey] = registry
	defer func() {
		if exists {
			data[blockRegistryNBTKey] = previous
		} else {
			delete(data, blockRegistryNBTKey)
		}
	}()
	return f()
}
