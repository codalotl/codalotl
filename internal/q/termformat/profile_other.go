//go:build !windows && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !zos

package termformat

func osColorProfile() (ColorProfile, error) {
	// Basically unknown OS. TTY -> 256 color.
	if stdoutIsTTY() {
		return ColorProfileANSI256, nil
	}
	return ColorProfileUncolored, nil
}
