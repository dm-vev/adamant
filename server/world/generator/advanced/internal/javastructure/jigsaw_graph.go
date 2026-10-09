package javastructure

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/internal/java26"
)

// BuildJigsaw expands a Java jigsaw start pool into a deterministic piece graph.
// random must be initialized with the structure's Java world-generation random source.
func BuildJigsaw(options JigsawOptions, random *java26.Random) (JigsawResult, error) {
	if random == nil {
		return JigsawResult{}, fmt.Errorf("Java jigsaw: nil random source")
	}
	if options.MaxDepth < 0 {
		return JigsawResult{}, fmt.Errorf("Java jigsaw: negative maximum depth")
	}
	if options.MaxDistance <= 0 {
		options.MaxDistance = 128
	}
	if options.MaxVerticalDistance <= 0 {
		options.MaxVerticalDistance = options.MaxDistance
	}
	registry := newJigsawPoolRegistry()
	startRotation := Rotation(random.NextInt(4))
	startPool, err := registry.startPool(options.Pool, options.PoolAliases)
	if err != nil {
		return JigsawResult{}, err
	}
	if len(startPool.elements) == 0 {
		return JigsawResult{}, nil
	}
	startIndex, err := choosePoolIndex(startPool.elements, random)
	if err != nil {
		return JigsawResult{}, err
	}
	startElement := &startPool.elements[startIndex]
	if startElement.empty {
		return JigsawResult{}, nil
	}
	startTemplates, err := registry.elementParts(startElement)
	if err != nil {
		return JigsawResult{}, err
	}
	if !hasPoolPlacement(startTemplates) {
		return JigsawResult{}, fmt.Errorf("Java jigsaw: start list has no bounding box")
	}
	startOrigin := options.Origin
	var sourceConnectors, targetConnectors []connector
	if options.StartJigsaw != "" {
		sourceConnectors, err = registry.shuffledConnectors(sourceConnectors, startTemplates, startOrigin, startRotation, NoMirror, random)
		if err != nil {
			return JigsawResult{}, err
		}
		found := false
		for i := range sourceConnectors {
			joint := &sourceConnectors[i]
			if !joint.wildcard && strings.TrimPrefix(joint.name, "minecraft:") == strings.TrimPrefix(options.StartJigsaw, "minecraft:") {
				startOrigin = cancelAnchor(startOrigin, joint.pos, options.Origin)
				found = true
				break
			}
		}
		if !found {
			return JigsawResult{}, nil
		}
	}
	startPiece := makeJigsawPoolPiece(startTemplates, startOrigin, startRotation, startElement, options.LiquidSettings)
	centerX := (startPiece.Bounds.Min[0] + startPiece.Bounds.Max[0]) / 2
	centerZ := (startPiece.Bounds.Min[2] + startPiece.Bounds.Max[2]) / 2
	bottomY := startOrigin[1]
	if options.ProjectStartToHeightmap {
		if options.HeightAt == nil {
			return JigsawResult{}, fmt.Errorf("Java jigsaw: projected start requires a height accessor")
		}
		bottomY = options.Origin[1] + options.HeightAt(centerX, centerZ)
	}
	startPiece = movePiece(startPiece, 0, bottomY-(startPiece.Bounds.Min[1]+startPiece.GroundLevelDelta), 0)
	minY := int32(options.HeightRange.Min()) + options.PaddingBottom
	maxY := int32(options.HeightRange.Max()) - options.PaddingTop
	if options.CheckBounds && (startPiece.Bounds.Min[1] < minY || startPiece.Bounds.Max[1] > maxY) {
		return JigsawResult{}, nil
	}
	centerY := bottomY + options.Origin[1] - startOrigin[1]
	result := JigsawResult{StartPos: [3]int32{centerX, centerY, centerZ}, HasStart: true}
	// The source stub's builder adds no pieces at depth zero.
	if options.MaxDepth == 0 {
		return result, nil
	}
	result.Pieces = append(result.Pieces, startPiece)
	spaceMinY := max(centerY-options.MaxVerticalDistance, minY)
	spaceMaxY := min(centerY+options.MaxVerticalDistance+1, maxY+1)
	// AABB's constructor normalises its endpoints, including an out-of-world start.
	if spaceMinY > spaceMaxY {
		spaceMinY, spaceMaxY = spaceMaxY, spaceMinY
	}
	outer := newJigsawSpace(Box{
		Min: [3]int32{centerX - options.MaxDistance, spaceMinY, centerZ - options.MaxDistance},
		Max: [3]int32{centerX + options.MaxDistance, spaceMaxY - 1, centerZ + options.MaxDistance},
	})
	outer.reserve(startPiece.Bounds)
	pending := jigsawPending{piece: 0, space: outer}
	var queue jigsawQueue
	var candidates []*poolElement
	for {
		// Appending pieces may move the slice. Mutate junctions through the index.
		source := &result.Pieces[pending.piece]
		sourceParts := source.parts
		if len(sourceParts) == 0 && source.Template != nil {
			sourceParts = []jigsawPart{{template: source.Template}}
		}
		sourceConnectors, err = registry.shuffledConnectors(sourceConnectors, sourceParts, source.Origin, source.Rotation, source.Mirror, random)
		if err != nil {
			return JigsawResult{}, err
		}
		var internal *jigsawSpace
	sourceLoop:
		for i := range sourceConnectors {
			sourceJigsaw := &sourceConnectors[i]
			front := directionStep(sourceJigsaw.front)
			jointPos := [3]int32{sourceJigsaw.pos[0] + front[0], sourceJigsaw.pos[1] + front[1], sourceJigsaw.pos[2] + front[2]}
			pool, err := registry.pool(lookupPoolAlias(sourceJigsaw.pool, options.PoolAliases))
			if err != nil {
				return JigsawResult{}, err
			}
			if pool == nil || (pool.weight == 0 && pool.name != "empty") {
				continue
			}
			fallback, err := registry.pool(pool.fallback)
			if err != nil {
				return JigsawResult{}, err
			}
			if fallback == nil || (fallback.weight == 0 && fallback.name != "empty") {
				continue
			}
			free := pending.space
			if jigsawBoxContains(source.Bounds, jointPos) {
				if internal == nil {
					internal = newJigsawSpace(source.Bounds)
				}
				free = internal
			}
			candidateCount := fallback.weight
			if pending.depth != options.MaxDepth {
				candidateCount += pool.weight
			}
			if cap(candidates) < candidateCount {
				candidates = make([]*poolElement, 0, candidateCount)
			}
			candidates = candidates[:0]
			if pending.depth != options.MaxDepth {
				candidates = appendShuffledPoolElements(candidates, pool.elements, random)
			}
			candidates = appendShuffledPoolElements(candidates, fallback.elements, random)
			sourceLocalY := sourceJigsaw.pos[1] - source.Bounds.Min[1]
			var sourceHeight int32
			heightKnown := false
			for _, element := range candidates {
				if element.empty {
					break
				}
				targetTemplates, err := registry.elementParts(element)
				if err != nil {
					return JigsawResult{}, err
				}
				if !hasPoolPlacement(targetTemplates) {
					return JigsawResult{}, fmt.Errorf("Java jigsaw: target list has no bounding box")
				}
				for _, targetRotation := range randomRotations(random) {
					targetConnectors, err = registry.shuffledConnectors(targetConnectors, targetTemplates, [3]int32{}, targetRotation, NoMirror, random)
					if err != nil {
						return JigsawResult{}, err
					}
					var hackBox Box
					if options.ExpansionHack {
						hackBox = makeJigsawPoolPiece(targetTemplates, [3]int32{}, targetRotation, element, options.LiquidSettings).Bounds
					}
					var expandTo int32
					if options.ExpansionHack && hackBox.Max[1]-hackBox.Min[1]+1 <= 16 {
						for i := range targetConnectors {
							joint := &targetConnectors[i]
							step := directionStep(joint.front)
							adjacent := [3]int32{joint.pos[0] + step[0], joint.pos[1] + step[1], joint.pos[2] + step[2]}
							if !jigsawBoxContains(hackBox, adjacent) {
								continue
							}
							childPool, err := registry.pool(lookupPoolAlias(joint.pool, options.PoolAliases))
							if err != nil {
								return JigsawResult{}, err
							}
							childSize, err := registry.maxPoolSize(childPool)
							if err != nil {
								return JigsawResult{}, err
							}
							if childPool != nil {
								childFallback, err := registry.pool(childPool.fallback)
								if err != nil {
									return JigsawResult{}, err
								}
								fallbackSize, err := registry.maxPoolSize(childFallback)
								if err != nil {
									return JigsawResult{}, err
								}
								childSize = max(childSize, fallbackSize)
							}
							expandTo = max(expandTo, childSize)
						}
					}
					for i := range targetConnectors {
						targetJigsaw := &targetConnectors[i]
						if !canAttach(sourceJigsaw, targetJigsaw) {
							continue
						}
						localPos := targetJigsaw.pos
						origin := [3]int32{jointPos[0] - localPos[0], jointPos[1] - localPos[1], jointPos[2] - localPos[2]}
						candidate := makeJigsawPoolPiece(targetTemplates, origin, targetRotation, element, options.LiquidSettings)
						targetLocalY := localPos[1]
						deltaY := sourceLocalY - targetLocalY + front[1]
						targetY := source.Bounds.Min[1] + deltaY
						if source.Projection != Rigid || candidate.Projection != Rigid {
							if !heightKnown {
								if options.HeightAt == nil {
									return JigsawResult{}, fmt.Errorf("Java jigsaw: terrain matching requires a height accessor")
								}
								sourceHeight = options.HeightAt(sourceJigsaw.pos[0], sourceJigsaw.pos[2])
								heightKnown = true
							}
							targetY = sourceHeight - targetLocalY
						}
						candidate = movePiece(candidate, 0, targetY-candidate.Bounds.Min[1], 0)
						if expandTo > 0 {
							candidate.Bounds.Max[1] = candidate.Bounds.Min[1] + max(expandTo+1, candidate.Bounds.Max[1]-candidate.Bounds.Min[1])
						}
						if !free.fits(candidate.Bounds) {
							continue
						}
						free.reserve(candidate.Bounds)
						if candidate.Projection == Rigid {
							candidate.GroundLevelDelta = source.GroundLevelDelta - deltaY
						}
						junctionY := sourceHeight + deltaY/2
						if source.Projection == Rigid {
							junctionY = source.Bounds.Min[1] + sourceLocalY
						} else if candidate.Projection == Rigid {
							junctionY = targetY + targetLocalY
						}
						sourceJunction := Junction{X: jointPos[0], Y: junctionY - sourceLocalY + source.GroundLevelDelta, Z: jointPos[2], DeltaY: deltaY, DestProjection: candidate.Projection}
						targetJunction := Junction{X: sourceJigsaw.pos[0], Y: junctionY - targetLocalY + candidate.GroundLevelDelta, Z: sourceJigsaw.pos[2], DeltaY: -deltaY, DestProjection: source.Projection}
						candidate.Junctions = append(candidate.Junctions, targetJunction)
						result.Pieces[pending.piece].Junctions = append(result.Pieces[pending.piece].Junctions, sourceJunction)
						result.Pieces = append(result.Pieces, candidate)
						if pending.depth+1 <= options.MaxDepth {
							queue.push(jigsawPending{piece: len(result.Pieces) - 1, depth: pending.depth + 1, space: free}, sourceJigsaw.placementPriority)
						}
						continue sourceLoop
					}
				}
			}
		}
		next, ok := queue.pop()
		if !ok {
			break
		}
		pending = next
	}
	for i := range result.Pieces {
		piece := &result.Pieces[i]
		if options.Terrain != TerrainNone && piece.Projection == Rigid {
			result.RigidPieces = append(result.RigidPieces, RigidPiece{Bounds: piece.Bounds, Adjustment: options.Terrain, GroundLevelDelta: piece.GroundLevelDelta})
		}
		result.Junctions = append(result.Junctions, piece.Junctions...)
	}
	return result, nil
}
