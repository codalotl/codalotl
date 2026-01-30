//go:build windows

package termformat

import (
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
)

func osColorProfile() (ColorProfile, error) {
	if !stdoutIsTTY() {
		return ColorProfileUncolored, nil
	}

	if strings.EqualFold(os.Getenv("ConEmuANSI"), "on") {
		return ColorProfileTrueColor, nil
	}

	major, _, build := windows.RtlGetNtVersionNumbers()
	if build < 10586 || major < 10 {
		if os.Getenv("ANSICON") != "" {
			version := os.Getenv("ANSICON_VER")
			v, err := strconv.ParseInt(version, 10, 64)
			if err != nil || v < 181 {
				return ColorProfileANSI, nil
			}
			return ColorProfileANSI256, nil
		}
		return ColorProfileUncolored, nil
	}
	if build < 14931 {
		return ColorProfileANSI256, nil
	}

	return ColorProfileTrueColor, nil
}
