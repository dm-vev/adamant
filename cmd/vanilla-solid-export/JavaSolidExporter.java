package tools.vanillasolid;

import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import java.util.stream.Collectors;
import net.minecraft.SharedConstants;
import net.minecraft.world.level.EmptyBlockGetter;
import net.minecraft.core.Direction;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.LevelReader;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.SupportType;
import net.minecraft.world.level.block.FireBlock;
import net.minecraft.world.level.block.state.BlockBehaviour;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.Property;
import net.minecraft.world.phys.AABB;
import net.minecraft.world.phys.shapes.VoxelShape;

/** Exports Java 26.3 block defaults, property domains and fresh-generation support/collision geometry. */
public final class JavaSolidExporter {
    private record BlockRow(String name, boolean defaultSolid, String defaultProperties, List<String> propertyDomains, List<StateRow> varyingStates, List<FaceRow> faces, List<GeometryRow> geometry, String updateShapeOwner, String canSurviveOwner, String conductorPredicateOwner, String instrument) {}
    private record StateRow(String properties, boolean solid) {}
    private record FaceRow(String properties, int mask) {}
    private record GeometryRow(String properties, int rigidMask, String faces) {}

    public static void main(String[] args) {
        if (args.length != 0) throw new IllegalArgumentException("usage: JavaSolidExporter");
        var out = System.out;
        SharedConstants.tryDetectVersion();
        String actualVersion = SharedConstants.getCurrentVersion().id();
        String expectedVersion = System.getProperty("adamant.solidity.expectedVersion");
        if (!"26.3".equals(actualVersion) || !actualVersion.equals(expectedVersion)) {
            throw new IllegalStateException("expected pinned Java 26.3 runtime, got " + actualVersion + " (manifest " + expectedVersion + ")");
        }
        Bootstrap.bootStrap();
        Method fireCanBurn = fireCanBurnMethod();
        FireBlock fire = (FireBlock)Blocks.FIRE;

        List<BlockRow> blocks = new ArrayList<>();
        for (Block block : BuiltInRegistries.BLOCK) {
            String name = BuiltInRegistries.BLOCK.getKey(block).toString();
            BlockState defaultState = block.defaultBlockState();
            boolean defaultSolid = defaultState.isSolid();
            String defaultProperties = properties(defaultState);
            List<String> propertyDomains = block.getStateDefinition().getProperties().stream()
                .map(JavaSolidExporter::propertyDomain).sorted().toList();
            List<StateRow> rows = new ArrayList<>();
            boolean varies = false;
            List<FaceRow> faces = new ArrayList<>();
            List<GeometryRow> geometry = new ArrayList<>();
            int firstMask = -1;
            boolean facesVary = false;
            for (BlockState state : block.getStateDefinition().getPossibleStates()) {
                boolean solid = state.isSolid();
                if (solid != defaultSolid) varies = true;
                rows.add(new StateRow(properties(state), solid));
                var support = state.getBlockSupportShape(EmptyBlockGetter.INSTANCE, BlockPos.ZERO);
                var collision = state.getCollisionShape(EmptyBlockGetter.INSTANCE, BlockPos.ZERO);
                int mask = 0;
                int rigidMask = 0;
                for (Direction face : Direction.values()) {
                    if (Block.isFaceFull(support, face) || Block.isFaceFull(collision, face)) mask |= 1 << face.ordinal();
                    if (state.isFaceSturdy(EmptyBlockGetter.INSTANCE, BlockPos.ZERO, face)) mask |= 1 << (face.ordinal() + 6);
                    if (state.isFaceSturdy(EmptyBlockGetter.INSTANCE, BlockPos.ZERO, face, SupportType.CENTER)) mask |= 1 << (face.ordinal() + 12);
                    if (state.isFaceSturdy(EmptyBlockGetter.INSTANCE, BlockPos.ZERO, face, SupportType.RIGID)) rigidMask |= 1 << face.ordinal();
                }
                if (state.isCollisionShapeFullBlock(EmptyBlockGetter.INSTANCE, BlockPos.ZERO)) mask |= 1 << 18;
                if (collision.isEmpty()) mask |= 1 << 19;
                if (state.canBeReplaced()) mask |= 1 << 20;
                if (Block.isExceptionForConnection(state)) mask |= 1 << 21;
                if (state.isSignalSource()) mask |= 1 << 22;
                if (state.isRedstoneConductor(EmptyBlockGetter.INSTANCE, BlockPos.ZERO)) mask |= 1 << 23;
                if (fireCanBurn(fireCanBurn, fire, state)) mask |= 1 << 24;
                if (state.isSolidRender()) mask |= 1 << 25;
                if (Block.isShapeFullBlock(state.getShape(EmptyBlockGetter.INSTANCE, BlockPos.ZERO))) mask |= 1 << 26;
                if (firstMask == -1) firstMask = mask;
                else if (mask != firstMask) facesVary = true;
                faces.add(new FaceRow(properties(state), mask));
                geometry.add(new GeometryRow(properties(state), rigidMask, collisionFaces(collision)));
            }
            rows.sort(Comparator.comparing(StateRow::properties));
            faces.sort(Comparator.comparing(FaceRow::properties));
            geometry.sort(Comparator.comparing(GeometryRow::properties));
            blocks.add(new BlockRow(name, defaultSolid, defaultProperties, propertyDomains, varies ? rows : List.of(), faces, geometry, updateShapeOwner(block), canSurviveOwner(block), conductorPredicateOwner(defaultState), defaultState.instrument().getSerializedName()));
        }
        blocks.sort(Comparator.comparing(BlockRow::name));

        out.println("# Java 26.3 BLOCK defaults, solidity, property domains, generation faces, collision geometry, dispatch owners, conductor predicate, and block instrument export; generated by cmd/vanilla-solid-export");
        for (BlockRow block : blocks) {
            out.println("B\t" + block.name() + "\t" + bit(block.defaultSolid()));
            for (String domain : block.propertyDomains()) out.println("P\t" + block.name() + "\t" + domain);
            out.println("D\t" + block.name() + "\t" + block.defaultProperties());
            for (StateRow state : block.varyingStates()) out.println("S\t" + block.name() + "\t" + state.properties() + "\t" + bit(state.solid()));
            for (FaceRow face : block.faces()) out.println("F\t" + block.name() + "\t" + face.properties() + "\t" + face.mask());
            out.println("O\t" + block.name() + "\t" + block.updateShapeOwner());
            out.println("V\t" + block.name() + "\t" + block.canSurviveOwner());
            out.println("C\t" + block.name() + "\t" + block.conductorPredicateOwner());
            out.println("I\t" + block.name() + "\t" + block.instrument());
            for (GeometryRow row : block.geometry()) out.println("G\t" + block.name() + "\t" + row.properties() + "\t" + row.rigidMask() + "\t" + row.faces());
        }
    }

    private static String collisionFaces(VoxelShape shape) {
        StringBuilder boundary = new StringBuilder();
        StringBuilder regions = new StringBuilder();
        for (Direction direction : Direction.values()) {
            if (direction.ordinal() > 0) {
                boundary.append('/');
                regions.append('/');
            }
            appendFaceRectangles(boundary, shape, direction, true);
            appendFaceRectangles(regions, shape.getFaceShape(direction), direction, false);
        }
        return boundary.append('^').append(regions).toString();
    }

    private static void appendFaceRectangles(StringBuilder result, VoxelShape shape, Direction direction, boolean boundaryOnly) {
        boolean hasRectangle = false;
        for (AABB box : shape.toAabbs()) {
            if (boundaryOnly) {
                boolean touches = switch (direction) {
                    case DOWN -> Math.abs(box.minY) <= 1.0E-7;
                    case UP -> Math.abs(box.maxY - 1.0) <= 1.0E-7;
                    case NORTH -> Math.abs(box.minZ) <= 1.0E-7;
                    case SOUTH -> Math.abs(box.maxZ - 1.0) <= 1.0E-7;
                    case WEST -> Math.abs(box.minX) <= 1.0E-7;
                    case EAST -> Math.abs(box.maxX - 1.0) <= 1.0E-7;
                };
                if (!touches) continue;
            }
            if (hasRectangle) result.append('|');
            hasRectangle = true;
            double minU, minV, maxU, maxV;
            switch (direction.getAxis()) {
                case X -> { minU = box.minY; minV = box.minZ; maxU = box.maxY; maxV = box.maxZ; }
                case Y -> { minU = box.minX; minV = box.minZ; maxU = box.maxX; maxV = box.maxZ; }
                default -> { minU = box.minX; minV = box.minY; maxU = box.maxX; maxV = box.maxY; }
            }
            result.append(minU).append(',').append(minV).append(',').append(maxU).append(',').append(maxV);
        }
    }

    private static String updateShapeOwner(Block block) {
        for (Class<?> type = block.getClass(); type != null; type = type.getSuperclass()) {
            for (Method method : type.getDeclaredMethods()) {
                if (method.getName().equals("updateShape") && method.getParameterCount() == 8) return type.getName();
            }
        }
        throw new IllegalStateException("missing updateShape owner for " + block);
    }
    private static String canSurviveOwner(Block block) {
        for (Class<?> type = block.getClass(); type != null; type = type.getSuperclass()) {
            for (Method method : type.getDeclaredMethods()) {
                Class<?>[] parameters = method.getParameterTypes();
                if (method.getName().equals("canSurvive") && parameters.length == 3
                    && parameters[0] == BlockState.class && parameters[1] == LevelReader.class && parameters[2] == BlockPos.class) {
                    return type.getName();
                }
            }
        }
        throw new IllegalStateException("missing canSurvive owner for " + block);
    }
    private static Method fireCanBurnMethod() {
        try {
            Method method = Blocks.FIRE.getClass().getDeclaredMethod("canBurn", BlockState.class);
            method.setAccessible(true);
            return method;
        } catch (ReflectiveOperationException exception) {
            throw new IllegalStateException("cannot access pinned FireBlock.canBurn", exception);
        }
    }

    private static boolean fireCanBurn(Method method, FireBlock fire, BlockState state) {
        try {
            return (boolean)method.invoke(fire, state);
        } catch (ReflectiveOperationException exception) {
            throw new IllegalStateException("cannot query pinned FireBlock.canBurn", exception);
        }
    }

    private static String conductorPredicateOwner(BlockState state) {
        try {
            Field field = BlockBehaviour.BlockStateBase.class.getDeclaredField("isRedstoneConductor");
            field.setAccessible(true);
            return field.get(state).getClass().getNestHost().getName();
        } catch (ReflectiveOperationException exception) {
            throw new IllegalStateException("cannot identify redstone conductor predicate", exception);
        }
    }

    private static String properties(BlockState state) {
        return state.getValues()
            .map(JavaSolidExporter::namedProperty)
            .sorted()
            .collect(Collectors.joining(","));
    }

    private static <T extends Comparable<T>> String namedProperty(Property.Value<T> entry) {
        Property<T> property = entry.property();
        T value = entry.value();
        String type = value instanceof Boolean ? "b:" : value instanceof Integer ? "i:" : "s:";
        return property.getName() + "=" + type + property.getName(value);
    }

    private static <T extends Comparable<T>> String propertyDomain(Property<T> property) {
        String type = property.getValueClass() == Boolean.class ? "b:" : property.getValueClass() == Integer.class ? "i:" : "s:";
        return property.getName() + "=" + type + property.getPossibleValues().stream()
            .map(property::getName)
            .sorted()
            .collect(Collectors.joining("|"));
    }

    private static int bit(boolean value) {
        return value ? 1 : 0;
    }
}
