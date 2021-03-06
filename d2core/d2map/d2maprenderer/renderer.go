package d2maprenderer

import (
	"errors"
	"github.com/OpenDiablo2/OpenDiablo2/d2common/d2math/d2vector"
	"image/color"
	"log"
	"math"

	"github.com/OpenDiablo2/OpenDiablo2/d2common/d2enum"
	"github.com/OpenDiablo2/OpenDiablo2/d2common/d2fileformats/d2ds1"
	"github.com/OpenDiablo2/OpenDiablo2/d2common/d2interface"
	"github.com/OpenDiablo2/OpenDiablo2/d2common/d2resource"
	"github.com/OpenDiablo2/OpenDiablo2/d2core/d2asset"
	"github.com/OpenDiablo2/OpenDiablo2/d2core/d2map/d2mapengine"
)

// MapRenderer manages the game viewport and Camera. It requests tile and entity data from MapEngine and renders it.
type MapRenderer struct {
	renderer      d2interface.Renderer   // Used for drawing operations
	mapEngine     *d2mapengine.MapEngine // The map engine that is being rendered
	palette       d2interface.Palette    // The palette used for this map
	viewport      *Viewport              // Used for rendering offsets
	Camera        Camera                 // Used to determine where on the map we are rendering
	debugVisLevel int                    // Debug visibility index (0=none, 1=tiles, 2=sub-tiles)
	lastFrameTime float64                // The last time the map was rendered
	currentFrame  int                    // Current render frame (for animations)
}

// CreateMapRenderer creates a new MapRenderer, sets the required fields and returns a pointer to it.
func CreateMapRenderer(renderer d2interface.Renderer, mapEngine *d2mapengine.MapEngine, term d2interface.Terminal) *MapRenderer {
	result := &MapRenderer{
		renderer:  renderer,
		mapEngine: mapEngine,
		viewport:  NewViewport(0, 0, 800, 600),
	}

	result.Camera = Camera{}
	startPosition := d2vector.NewPosition(0,0)
	result.Camera.position = &startPosition
	result.viewport.SetCamera(&result.Camera)

	term.BindAction("mapdebugvis", "set map debug visualization level", func(level int) {
		result.debugVisLevel = level
	})

	if mapEngine.LevelType().ID != 0 {
		result.generateTileCache()
	}

	return result
}

// RegenerateTileCache calls MapRenderer.generateTileCache().
func (mr *MapRenderer) RegenerateTileCache() {
	mr.generateTileCache()
}

// SetMapEngine sets the MapEngine this renderer is rendering.
func (mr *MapRenderer) SetMapEngine(mapEngine *d2mapengine.MapEngine) {
	mr.mapEngine = mapEngine
	mr.generateTileCache()
}

// Render determines the width and height of map tiles that should be rendered. The following four render passes are
// made in succession:
//
// Pass 1: Lower wall tiles, tile shadows and floor tiles.
//
// Pass 2: Entities below walls.
//
// Pass 3: Upper wall tiles and entities above walls.
//
// Pass 4: Roof tiles.
func (mr *MapRenderer) Render(target d2interface.Surface) {
	mapSize := mr.mapEngine.Size()

	stxf, styf := mr.viewport.ScreenToWorld(400, -200)
	etxf, etyf := mr.viewport.ScreenToWorld(400, 1050)

	startX := int(math.Max(0, math.Floor(stxf)))
	startY := int(math.Max(0, math.Floor(styf)))

	endX := int(math.Min(float64(mapSize.Width), math.Ceil(etxf)))
	endY := int(math.Min(float64(mapSize.Height), math.Ceil(etyf)))

	mr.renderPass1(target, startX, startY, endX, endY)
	mr.renderPass2(target, startX, startY, endX, endY)

	if mr.debugVisLevel > 0 {
		mr.renderDebug(mr.debugVisLevel, target, startX, startY, endX, endY)
	}

	mr.renderPass3(target, startX, startY, endX, endY)
	mr.renderPass4(target, startX, startY, endX, endY)
}

// MoveCameraTo sets the position of the Camera to the given x and y coordinates.
func (mr *MapRenderer) MoveCameraTo(position *d2vector.Position) {
	mr.Camera.MoveTo(position)
}

// MoveCameraBy adds the given vector to the current position of the Camera.
func (mr *MapRenderer) MoveCameraBy(vector *d2vector.Vector) {
	mr.Camera.MoveBy(vector)
}

// MoveCameraTargetBy adds the given vector to the current position of the Camera.
func (mr *MapRenderer) MoveCameraTargetBy(vector *d2vector.Vector) {
	mr.Camera.MoveTargetBy(vector)
}

// ScreenToWorld returns the world position for the given screen (pixel) position.
func (mr *MapRenderer) ScreenToWorld(x, y int) (float64, float64) {
	return mr.viewport.ScreenToWorld(x, y)
}

// ScreenToOrtho returns the orthogonal position, without accounting for the isometric angle, for the given screen
// (pixel) position.
func (mr *MapRenderer) ScreenToOrtho(x, y int) (float64, float64) {
	return mr.viewport.ScreenToOrtho(x, y)
}

// WorldToOrtho returns the orthogonal position for the given isometric world position.
func (mr *MapRenderer) WorldToOrtho(x, y float64) (float64, float64) {
	return mr.viewport.WorldToOrtho(x, y)
}

// Lower wall tiles, tile shadows and floor tiles.
func (mr *MapRenderer) renderPass1(target d2interface.Surface, startX, startY, endX, endY int) {
	for tileY := startY; tileY < endY; tileY++ {
		for tileX := startX; tileX < endX; tileX++ {
			tile := mr.mapEngine.TileAt(tileX, tileY)
			mr.viewport.PushTranslationWorld(float64(tileX), float64(tileY))
			mr.renderTilePass1(tile, target)
			mr.viewport.PopTranslation()
		}
	}
}

// Entities below walls.
func (mr *MapRenderer) renderPass2(target d2interface.Surface, startX, startY, endX, endY int) {
	for tileY := startY; tileY < endY; tileY++ {
		for tileX := startX; tileX < endX; tileX++ {
			mr.viewport.PushTranslationWorld(float64(tileX), float64(tileY))

			// TODO: Do not loop over every entity every frame
			for _, mapEntity := range *mr.mapEngine.Entities() {
				entityX, entityY := mapEntity.GetPosition()

				if mapEntity.GetLayer() != 1 {
					continue
				}

				if (int(entityX) != tileX) || (int(entityY) != tileY) {
					continue
				}

				target.PushTranslation(mr.viewport.GetTranslationScreen())
				mapEntity.Render(target)
				target.Pop()
			}

			mr.viewport.PopTranslation()
		}
	}
}

// Upper wall tiles and entities above walls.
func (mr *MapRenderer) renderPass3(target d2interface.Surface, startX, startY, endX, endY int) {
	for tileY := startY; tileY < endY; tileY++ {
		for tileX := startX; tileX < endX; tileX++ {
			tile := mr.mapEngine.TileAt(tileX, tileY)
			mr.viewport.PushTranslationWorld(float64(tileX), float64(tileY))
			mr.renderTilePass2(tile, target)

			// TODO: Do not loop over every entity every frame
			for _, mapEntity := range *mr.mapEngine.Entities() {
				entityX, entityY := mapEntity.GetPosition()

				if mapEntity.GetLayer() == 1 {
					continue
				}

				if (int(entityX) != tileX) || (int(entityY) != tileY) {
					continue
				}

				target.PushTranslation(mr.viewport.GetTranslationScreen())
				mapEntity.Render(target)
				target.Pop()
			}

			mr.viewport.PopTranslation()
		}
	}
}

// Roof tiles.
func (mr *MapRenderer) renderPass4(target d2interface.Surface, startX, startY, endX, endY int) {
	for tileY := startY; tileY < endY; tileY++ {
		for tileX := startX; tileX < endX; tileX++ {
			tile := mr.mapEngine.TileAt(tileX, tileY)
			mr.viewport.PushTranslationWorld(float64(tileX), float64(tileY))
			mr.renderTilePass3(tile, target)
			mr.viewport.PopTranslation()
		}
	}
}

func (mr *MapRenderer) renderTilePass1(tile *d2ds1.TileRecord, target d2interface.Surface) {
	for _, wall := range tile.Walls {
		if !wall.Hidden && wall.Prop1 != 0 && wall.Type.LowerWall() {
			mr.renderWall(wall, mr.viewport, target)
		}
	}

	for _, floor := range tile.Floors {
		if !floor.Hidden && floor.Prop1 != 0 {
			mr.renderFloor(floor, target)
		}
	}

	for _, shadow := range tile.Shadows {
		if !shadow.Hidden && shadow.Prop1 != 0 {
			mr.renderShadow(shadow, target)
		}
	}
}

func (mr *MapRenderer) renderTilePass2(tile *d2ds1.TileRecord, target d2interface.Surface) {
	for _, wall := range tile.Walls {
		if !wall.Hidden && wall.Type.UpperWall() {
			mr.renderWall(wall, mr.viewport, target)
		}
	}
}

func (mr *MapRenderer) renderTilePass3(tile *d2ds1.TileRecord, target d2interface.Surface) {
	for _, wall := range tile.Walls {
		if wall.Type == d2enum.TileRoof {
			mr.renderWall(wall, mr.viewport, target)
		}
	}
}

func (mr *MapRenderer) renderFloor(tile d2ds1.FloorShadowRecord, target d2interface.Surface) {
	var img d2interface.Surface
	if !tile.Animated {
		img = mr.getImageCacheRecord(tile.Style, tile.Sequence, 0, tile.RandomIndex)
	} else {
		img = mr.getImageCacheRecord(tile.Style, tile.Sequence, 0, byte(mr.currentFrame))
	}

	if img == nil {
		log.Printf("Render called on uncached floor {%v,%v}", tile.Style, tile.Sequence)
		return
	}

	mr.viewport.PushTranslationOrtho(-80, float64(tile.YAdjust))
	defer mr.viewport.PopTranslation()

	target.PushTranslation(mr.viewport.GetTranslationScreen())
	defer target.Pop()

	target.Render(img)
}

func (mr *MapRenderer) renderWall(tile d2ds1.WallRecord, viewport *Viewport, target d2interface.Surface) {
	img := mr.getImageCacheRecord(tile.Style, tile.Sequence, tile.Type, tile.RandomIndex)
	if img == nil {
		log.Printf("Render called on uncached wall {%v,%v,%v}", tile.Style, tile.Sequence, tile.Type)
		return
	}

	viewport.PushTranslationOrtho(-80, float64(tile.YAdjust))
	defer viewport.PopTranslation()

	target.PushTranslation(viewport.GetTranslationScreen())
	defer target.Pop()

	target.Render(img)
}

func (mr *MapRenderer) renderShadow(tile d2ds1.FloorShadowRecord, target d2interface.Surface) {
	img := mr.getImageCacheRecord(tile.Style, tile.Sequence, 13, tile.RandomIndex)
	if img == nil {
		log.Printf("Render called on uncached shadow {%v,%v}", tile.Style, tile.Sequence)
		return
	}

	defer mr.viewport.PushTranslationOrtho(-80, float64(tile.YAdjust)).PopTranslation()

	target.PushTranslation(mr.viewport.GetTranslationScreen())
	target.PushColor(color.RGBA{R: 255, G: 255, B: 255, A: 160})

	defer target.PopN(2)

	target.Render(img)
}

func (mr *MapRenderer) renderDebug(debugVisLevel int, target d2interface.Surface, startX, startY, endX, endY int) {
	for tileY := startY; tileY < endY; tileY++ {
		for tileX := startX; tileX < endX; tileX++ {
			mr.viewport.PushTranslationWorld(float64(tileX), float64(tileY))
			mr.renderTileDebug(tileX, tileY, debugVisLevel, target)
			mr.viewport.PopTranslation()
		}
	}
}

// WorldToScreen returns the screen (pixel) position for the given isometric world position as two ints.
func (mr *MapRenderer) WorldToScreen(x, y float64) (int, int) {
	return mr.viewport.WorldToScreen(x, y)
}

// WorldToScreenF returns the screen (pixel) position for the given isometric world position as two float64s.
func (mr *MapRenderer) WorldToScreenF(x, y float64) (float64, float64) {
	return mr.viewport.WorldToScreenF(x, y)
}

func (mr *MapRenderer) renderTileDebug(ax, ay int, debugVisLevel int, target d2interface.Surface) {
	subTileColor := color.RGBA{R: 80, G: 80, B: 255, A: 50}
	tileColor := color.RGBA{R: 255, G: 255, B: 255, A: 100}
	tileCollisionColor := color.RGBA{R: 128, G: 0, B: 0, A: 100}

	screenX1, screenY1 := mr.viewport.WorldToScreen(float64(ax), float64(ay))
	screenX2, screenY2 := mr.viewport.WorldToScreen(float64(ax+1), float64(ay))
	screenX3, screenY3 := mr.viewport.WorldToScreen(float64(ax), float64(ay+1))

	target.PushTranslation(screenX1, screenY1)
	defer target.Pop()

	target.DrawLine(screenX2-screenX1, screenY2-screenY1, tileColor)
	target.DrawLine(screenX3-screenX1, screenY3-screenY1, tileColor)
	target.PushTranslation(-10, 10)
	target.DrawTextf("%v, %v", ax, ay)
	target.Pop()

	if debugVisLevel > 1 {
		for i := 1; i <= 4; i++ {
			x2 := i * 16
			y2 := i * 8

			target.PushTranslation(-x2, y2)
			target.DrawLine(80, 40, subTileColor)
			target.Pop()

			target.PushTranslation(x2, y2)
			target.DrawLine(-80, 40, subTileColor)
			target.Pop()
		}

		tile := mr.mapEngine.TileAt(ax, ay)

		/*for i, floor := range tile.Floors {
			target.PushTranslation(-20, 10+(i+1)*14)
			target.DrawTextf("f: %v-%v", floor.Style, floor.Sequence)
			target.Pop()
		}*/

		for i, wall := range tile.Walls {
			if wall.Type.Special() {
				target.PushTranslation(-20, 10+(i+1)*14)
				target.DrawTextf("s: %v-%v", wall.Style, wall.Sequence)
				target.Pop()
			}
		}

		for yy := 0; yy < 5; yy++ {
			for xx := 0; xx < 5; xx++ {
				isoX := (xx - yy) * 16
				isoY := (xx + yy) * 8

				var walkableArea = (*mr.mapEngine.WalkMesh())[((yy+(ay*5))*mr.mapEngine.Size().Width*5)+xx+(ax*5)]

				if !walkableArea.Walkable {
					target.PushTranslation(isoX-3, isoY+4)
					target.DrawRect(5, 5, tileCollisionColor)
					target.Pop()
				}
			}
		}
	}
}

// Advance is called once per frame and maintains the MapRenderer's record previous render timestamp and current frame.
func (mr *MapRenderer) Advance(elapsed float64) {
	frameLength := 0.1

	mr.lastFrameTime += elapsed
	framesAdvanced := int(mr.lastFrameTime / frameLength)
	mr.lastFrameTime -= float64(framesAdvanced) * frameLength

	mr.currentFrame += framesAdvanced
	if mr.currentFrame > 9 {
		mr.currentFrame = 0
	}

	mr.Camera.Advance(elapsed)
}

func loadPaletteForAct(levelType d2enum.RegionIdType) (d2interface.Palette, error) {
	var palettePath string

	switch levelType {
	case d2enum.RegionAct1Town, d2enum.RegionAct1Wilderness, d2enum.RegionAct1Cave, d2enum.RegionAct1Crypt,
		d2enum.RegionAct1Monestary, d2enum.RegionAct1Courtyard, d2enum.RegionAct1Barracks,
		d2enum.RegionAct1Jail, d2enum.RegionAct1Cathedral, d2enum.RegionAct1Catacombs, d2enum.RegionAct1Tristram:
		palettePath = d2resource.PaletteAct1
	case d2enum.RegionAct2Town, d2enum.RegionAct2Sewer, d2enum.RegionAct2Harem, d2enum.RegionAct2Basement,
		d2enum.RegionAct2Desert, d2enum.RegionAct2Tomb, d2enum.RegionAct2Lair, d2enum.RegionAct2Arcane:
		palettePath = d2resource.PaletteAct2
	case d2enum.RegionAct3Town, d2enum.RegionAct3Jungle, d2enum.RegionAct3Kurast, d2enum.RegionAct3Spider,
		d2enum.RegionAct3Dungeon, d2enum.RegionAct3Sewer:
		palettePath = d2resource.PaletteAct3
	case d2enum.RegionAct4Town, d2enum.RegionAct4Mesa, d2enum.RegionAct4Lava, d2enum.RegionAct5Lava:
		palettePath = d2resource.PaletteAct4
	case d2enum.RegonAct5Town, d2enum.RegionAct5Siege, d2enum.RegionAct5Barricade, d2enum.RegionAct5Temple,
		d2enum.RegionAct5IceCaves, d2enum.RegionAct5Baal:
		palettePath = d2resource.PaletteAct5
	default:
		return nil, errors.New("failed to find palette for region")
	}

	return d2asset.LoadPalette(palettePath)
}

// ViewportToLeft moves the viewport to the left.
func (mr *MapRenderer) ViewportToLeft() {
	mr.viewport.toLeft()
}

// ViewportToRight moves the viewport to the right.
func (mr *MapRenderer) ViewportToRight() {
	mr.viewport.toRight()
}

// ViewportDefault resets the viewport to it's default position.
func (mr *MapRenderer) ViewportDefault() {
	mr.viewport.resetAlign()
}

// SetCameraTarget sts the Camera target
func (mr *MapRenderer) SetCameraTarget(position *d2vector.Position) {
	mr.Camera.SetTarget(position)
}
