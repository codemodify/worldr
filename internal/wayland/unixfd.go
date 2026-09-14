package wayland

import "golang.org/x/sys/unix"

// ParseUnixFDs extracts file descriptors from recvmsg OOB data.
func ParseUnixFDs(oob []byte) ([]int, error) {
	scms, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return nil, err
	}
	var fds []int
	for _, scm := range scms {
		got, err := unix.ParseUnixRights(&scm)
		if err != nil {
			return nil, err
		}
		fds = append(fds, got...)
	}
	return fds, nil
}

// UnixRights encodes FDs for sendmsg.
func UnixRights(fds ...int) []byte {
	if len(fds) == 0 {
		return nil
	}
	return unix.UnixRights(fds...)
}
