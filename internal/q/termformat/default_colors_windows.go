//go:build windows

package termformat

import (
	"image/color"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var kernel32 = windows.NewLazySystemDLL("kernel32.dll")

var procGetConsoleScreenBufferInfoEx = kernel32.NewProc("GetConsoleScreenBufferInfoEx")

type consoleScreenBufferInfoEx struct {
	CbSize               uint32
	DwSize               windows.Coord
	DwCursorPosition     windows.Coord
	WAttributes          uint16
	SrWindow             windows.SmallRect
	DwMaximumWindowSize  windows.Coord
	WPopupAttributes     uint16
	BFullscreenSupported uint32
	ColorTable           [16]uint32
}

var defaultColorTable = [16]color.RGBA{
	{0x00, 0x00, 0x00, 0xFF},
	{0x00, 0x00, 0x80, 0xFF},
	{0x00, 0x80, 0x00, 0xFF},
	{0x00, 0x80, 0x80, 0xFF},
	{0x80, 0x00, 0x00, 0xFF},
	{0x80, 0x00, 0x80, 0xFF},
	{0x80, 0x80, 0x00, 0xFF},
	{0xC0, 0xC0, 0xC0, 0xFF},
	{0x80, 0x80, 0x80, 0xFF},
	{0x00, 0x00, 0xFF, 0xFF},
	{0x00, 0xFF, 0x00, 0xFF},
	{0x00, 0xFF, 0xFF, 0xFF},
	{0xFF, 0x00, 0x00, 0xFF},
	{0xFF, 0x00, 0xFF, 0xFF},
	{0xFF, 0xFF, 0x00, 0xFF},
	{0xFF, 0xFF, 0xFF, 0xFF},
}

func queryDefaultFBBGColor() (RGBColor, RGBColor, bool, error) {
	handle := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		if err == windows.ERROR_INVALID_HANDLE {
			return "", "", false, nil
		}
		return "", "", false, err
	}

	attr, palette, err := consoleAttributes(handle)
	if err != nil {
		return "", "", false, err
	}

	fgIndex := int(attr & 0x0F)
	bgIndex := int((attr >> 4) & 0x0F)

	return colorToRGBColor(palette[fgIndex]), colorToRGBColor(palette[bgIndex]), true, nil
}

func colorToRGBColor(c color.RGBA) RGBColor {
	return NewRGBColor(c.R, c.G, c.B)
}

func consoleAttributes(handle windows.Handle) (uint16, [16]color.RGBA, error) {
	palette := defaultColorTable

	var infoEx consoleScreenBufferInfoEx
	infoEx.CbSize = uint32(unsafe.Sizeof(infoEx))
	if err := getConsoleScreenBufferInfoEx(handle, &infoEx); err == nil {
		return infoEx.WAttributes, convertColorTable(infoEx.ColorTable), nil
	}

	var basic windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(handle, &basic); err != nil {
		return 0, palette, err
	}
	return basic.Attributes, palette, nil
}

func getConsoleScreenBufferInfoEx(handle windows.Handle, info *consoleScreenBufferInfoEx) error {
	if err := procGetConsoleScreenBufferInfoEx.Find(); err != nil {
		return err
	}
	r1, _, e1 := procGetConsoleScreenBufferInfoEx.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(info)),
	)
	if r1 == 0 {
		return e1
	}
	return nil
}

func convertColorTable(table [16]uint32) [16]color.RGBA {
	var palette [16]color.RGBA
	for i, entry := range table {
		palette[i] = color.RGBA{
			R: uint8(entry & 0xFF),
			G: uint8((entry >> 8) & 0xFF),
			B: uint8((entry >> 16) & 0xFF),
			A: 0xFF,
		}
	}
	return palette
}
