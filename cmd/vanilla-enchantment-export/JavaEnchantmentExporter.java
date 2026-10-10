package tools.vanillaenchantment;

import com.google.gson.GsonBuilder;
import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import java.util.List;
import java.util.Optional;
import net.minecraft.SharedConstants;
import net.minecraft.core.Registry;
import net.minecraft.core.RegistryAccess;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.core.registries.Registries;
import net.minecraft.network.chat.Component;
import net.minecraft.resources.RegistryDataLoader;
import net.minecraft.server.Bootstrap;
import net.minecraft.server.packs.PackLocationInfo;
import net.minecraft.server.packs.PackType;
import net.minecraft.server.packs.VanillaPackResourcesBuilder;
import net.minecraft.server.packs.repository.PackSource;
import net.minecraft.server.packs.resources.MultiPackResourceManager;
import net.minecraft.tags.TagLoader;

/** Exports registry order, supported item holders and level bounds from Java 26.3. */
public final class JavaEnchantmentExporter {
    public static void main(String[] args) throws Exception {
        if (args.length != 0) throw new IllegalArgumentException("usage: JavaEnchantmentExporter");
        var out = System.out;
        SharedConstants.tryDetectVersion();
        String actualVersion = SharedConstants.getCurrentVersion().id();
        String expectedVersion = System.getProperty("adamant.enchantments.expectedVersion");
        if (!"26.3".equals(actualVersion) || !actualVersion.equals(expectedVersion)) {
            throw new IllegalStateException("expected pinned Java 26.3 runtime, got " + actualVersion + " (manifest " + expectedVersion + ")");
        }
        Bootstrap.bootStrap();
        var builtins = RegistryAccess.fromRegistryOfRegistries(BuiltInRegistries.REGISTRY);
        var pack = new VanillaPackResourcesBuilder().pushJarResources().exposeNamespace("minecraft").build(
            new PackLocationInfo("vanilla", Component.literal("vanilla"), PackSource.BUILT_IN, Optional.empty())
        );
        try (var resources = new MultiPackResourceManager(PackType.SERVER_DATA, List.of(pack.fullResources()))) {
            TagLoader.loadTagsForExistingRegistries(resources, builtins).forEach(Registry.PendingTags::apply);
            var registries = RegistryDataLoader.load(resources, builtins.listRegistries().toList(), RegistryDataLoader.WORLD_REGISTRIES, Runnable::run).join();
            var rows = new JsonArray();
            for (var holder : registries.lookupOrThrow(Registries.ENCHANTMENT).listElements().toList()) {
                var row = new JsonObject();
                row.addProperty("name", holder.unwrapKey().orElseThrow().identifier().toString());
                row.addProperty("max_level", holder.value().getMaxLevel());
                var supportedItems = new JsonArray();
                holder.value().getSupportedItems().stream().forEach(item -> supportedItems.add(item.unwrapKey().orElseThrow().identifier().toString()));
                row.add("supported_items", supportedItems);
                rows.add(row);
            }
            out.println(new GsonBuilder().setPrettyPrinting().create().toJson(rows));
        }
    }
}
