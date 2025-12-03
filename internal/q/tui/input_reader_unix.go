//go:build !windows

package tui

import (
	"time"

	"golang.org/x/sys/unix"
)

const inputPollInterval = 50 * time.Millisecond

func (p *inputProcessor) read(buf []byte) (int, error) {
	if p.fd < 0 {
		return p.reader.Read(buf)
	}

	fd := p.fd
	timeout := int(inputPollInterval / time.Millisecond)
	if timeout <= 0 {
		timeout = 1
	}

	for {
		if err := p.t.ctx.Err(); err != nil {
			return 0, err
		}

		fds := []unix.PollFd{{
			Fd:     int32(fd),
			Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR,
		}}
		n, err := unix.Poll(fds, timeout)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return 0, err
		}
		if n == 0 {
			continue
		}

		revents := fds[0].Revents
		if revents&unix.POLLNVAL != 0 {
			return 0, unix.EBADF
		}
		if revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) == 0 {
			continue
		}

		for {
			n, err := unix.Read(fd, buf)
			if n >= 0 {
				return n, err
			}
			if err == unix.EINTR {
				continue
			}
			if err == unix.EAGAIN {
				break
			}
			return n, err
		}
	}
}
