//go:build windows

package tui

func (p *inputProcessor) read(buf []byte) (int, error) {
	return p.reader.Read(buf)
}
