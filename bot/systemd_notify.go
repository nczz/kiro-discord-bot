package bot

import (
	"net"
	"os"
	"strings"
)

func systemdNotify(message string) error {
	socket := os.Getenv("NOTIFY_SOCKET")
	if socket == "" || message == "" {
		return nil
	}
	addrName := socket
	if strings.HasPrefix(addrName, "@") {
		addrName = "\x00" + strings.TrimPrefix(addrName, "@")
	}
	addr := &net.UnixAddr{Name: addrName, Net: "unixgram"}
	conn, err := net.DialUnix("unixgram", nil, addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte(message))
	return err
}
