package tui

// hitTarget 是点击命中的语义目标，鼠标处理只按目标分派，不按坐标猜功能。
type hitTarget int

const (
	hitNone hitTarget = iota
	hitAbout
	hitPageMenu
	hitAccount
	hitPlayPause
	hitMode
	hitQuality
	hitLyricStrip
	hitLibraryPlaylist
	hitLibrarySong
	hitQueueSong
	hitSearchResult
	hitDownloadRow
	hitFolderRow
	hitLyricRow
)

type hitZone struct {
	target hitTarget
	x, y   int
	w, h   int
	index  int
}

func (z hitZone) contains(x, y int) bool {
	return x >= z.x && x < z.x+z.w && y >= z.y && y < z.y+z.h
}

// frameFits 判断底部栏是否完整渲染；终端过矮时坐标不可信。
func (m *Model) frameFits() bool {
	total := 2 + m.bodyHeight() + 1 + 3
	if _, ok := m.downloadLineText(); ok {
		total++
	}
	if _, _, ok := m.lyricStripText(); ok {
		total++
	}
	return total == m.height
}

func (m *Model) headerAccountStart() int {
	_, right := m.headerParts()
	x := m.width - 1 - visWidth(right)
	if x < 0 {
		x = 0
	}
	return x
}

// metaZone 把 nowPlayingMeta 的某个片段换算成屏幕列；meta 被截断时返回 ok=false。
func (m *Model) metaZone(meta string, seg metaSegment) (x, w int, ok bool) {
	if seg.start < 0 || seg.width <= 0 {
		return 0, 0, false
	}
	if maxMeta := m.width - 6; maxMeta > 0 && visWidth(meta) > maxMeta {
		return 0, 0, false
	}
	icon := m.playbackIcon()
	title := m.nowPlayingTitle()
	leftW := visWidth(" "+icon+" ") + visWidth(truncate(title, m.width-visWidth(meta)-6))
	gap := m.width - leftW - visWidth(meta) - 2
	if gap < 1 {
		gap = 1
	}
	start := leftW + gap + seg.start
	if start < 0 || start+seg.width > m.width {
		return 0, 0, false
	}
	return start, seg.width, true
}

// modeZone 返回底部播放模式片段的位置；meta 被截断时返回 ok=false。
func (m *Model) modeZone() (x, w int, ok bool) {
	meta, mode, _ := m.nowPlayingMeta()
	return m.metaZone(meta, mode)
}

// qualityZone 返回底部音质档位片段的位置；点击等同按 y 切换音质偏好。
func (m *Model) qualityZone() (x, w int, ok bool) {
	meta, _, quality := m.nowPlayingMeta()
	return m.metaZone(meta, quality)
}

func (m *Model) hitAt(x, y int) (hitZone, bool) {
	for _, zone := range m.hitZones() {
		if zone.contains(x, y) {
			return zone, true
		}
	}
	return hitZone{}, false
}

func (m *Model) hitZones() []hitZone {
	if m.width <= 0 || m.height <= 0 || m.view == viewLogin {
		return nil
	}
	var zones []hitZone
	// 页头：zero / 页面菜单 / 账户。始终按渲染出的位置命中。
	accountX := m.headerAccountStart()
	zones = append(zones, hitZone{target: hitAbout, x: 0, y: 0, w: min(6, m.width), h: 1})
	if accountX > 6 {
		zones = append(zones, hitZone{target: hitPageMenu, x: 6, y: 0, w: accountX - 6, h: 1})
	}
	if accountX < m.width {
		zones = append(zones, hitZone{target: hitAccount, x: accountX, y: 0, w: m.width - accountX, h: 1})
	}
	if !m.frameFits() {
		return zones
	}

	bodyH := m.bodyHeight()
	top := 2
	switch m.view {
	case viewLibrary:
		sideW := m.librarySideWidth()
		if sideW > 0 {
			start, end := windowRange(len(m.playlists), m.playlistIdx, bodyH-1)
			for i := start; i < end; i++ {
				zones = append(zones, hitZone{
					target: hitLibraryPlaylist, x: 0, y: top + 1 + (i - start), w: sideW, h: 1, index: i,
				})
			}
		}
		start, end := windowRange(len(m.songs), m.songIdx, bodyH-1)
		x := 0
		if sideW > 0 {
			x = sideW + 1
		}
		for i := start; i < end; i++ {
			zones = append(zones, hitZone{
				target: hitLibrarySong, x: x, y: top + 1 + (i - start), w: m.width - x, h: 1, index: i,
			})
		}
	case viewSearch:
		if m.searchBusy {
			break
		}
		n := len(m.localResults)
		if m.searchMode == searchSource {
			n = len(m.searchResults)
		}
		start, end := windowRange(n, m.searchIdx, bodyH-2)
		for i := start; i < end; i++ {
			zones = append(zones, hitZone{
				target: hitSearchResult, x: 0, y: top + 2 + (i - start), w: m.width, h: 1, index: i,
			})
		}
	case viewQueue:
		start, end := windowRange(len(m.queue), m.queueCursor, bodyH-1)
		for i := start; i < end; i++ {
			zones = append(zones, hitZone{
				target: hitQueueSong, x: 0, y: top + 1 + (i - start), w: m.width, h: 1, index: i,
			})
		}
	case viewDownloads:
		start, end := windowRange(len(m.downloadItems), m.downloadIdx, bodyH-1)
		for i := start; i < end; i++ {
			zones = append(zones, hitZone{
				target: hitDownloadRow, x: 0, y: top + 1 + (i - start), w: m.width, h: 1, index: i,
			})
		}
	case viewFolders:
		start, end := windowRange(len(m.folderItems), m.folderIdx, bodyH-1)
		for i := start; i < end; i++ {
			zones = append(zones, hitZone{
				target: hitFolderRow, x: 0, y: top + 1 + (i - start), w: m.width, h: 1, index: i,
			})
		}
	case viewLyrics:
		rows, start := m.lyricWindow(bodyH)
		for pos := 0; pos < bodyH; pos++ {
			idx := start + pos
			if idx < 0 || idx >= len(rows) {
				continue
			}
			zones = append(zones, hitZone{
				target: hitLyricRow, x: 0, y: top + pos, w: m.width, h: 1, index: rows[idx].lineIdx,
			})
		}
	}

	// 底部常驻区。
	if _, _, ok := m.lyricStripText(); ok && m.height >= 4 {
		zones = append(zones, hitZone{target: hitLyricStrip, x: 0, y: m.height - 4, w: m.width, h: 1})
	}
	if m.height >= 3 {
		zones = append(zones, hitZone{
			target: hitPlayPause, x: 0, y: m.height - 3, w: visWidth(" " + m.playbackIcon() + " "), h: 1,
		})
		if x, w, ok := m.qualityZone(); ok {
			zones = append(zones, hitZone{target: hitQuality, x: x, y: m.height - 3, w: w, h: 1})
		}
		if x, w, ok := m.modeZone(); ok {
			zones = append(zones, hitZone{target: hitMode, x: x, y: m.height - 3, w: w, h: 1})
		}
	}
	return zones
}
