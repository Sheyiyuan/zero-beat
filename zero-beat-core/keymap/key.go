package keymap

// Key is the portable key vocabulary used by configuration validation.
type Key struct {
	Code rune
	Text string
	Mod  KeyMod
}
type KeyMod uint8

const (
	ModCtrl  KeyMod = 1 << 0
	ModAlt   KeyMod = 1 << 1
	ModShift KeyMod = 1 << 2
	ModSuper KeyMod = 1 << 3
)
const (
	KeySpace     rune = -1
	KeyEnter     rune = -2
	KeyTab       rune = -3
	KeyEscape    rune = -4
	KeyBackspace rune = -5
	KeyDelete    rune = -6
	KeyUp        rune = -7
	KeyDown      rune = -8
	KeyLeft      rune = -9
	KeyRight     rune = -10
	KeyPgUp      rune = -11
	KeyPgDown    rune = -12
	KeyHome      rune = -13
	KeyEnd       rune = -14
)
