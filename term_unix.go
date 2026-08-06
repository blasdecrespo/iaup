//go:build linux || darwin || freebsd || netbsd || openbsd

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// termWidth devuelve el ancho del terminal en columnas, o 0 si la salida no es
// un terminal. Es una llamada al sistema, no una dependencia.
func termWidth() int {
	var ws struct{ row, col, xpix, ypix uint16 }
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL,
		os.Stdout.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if err != 0 {
		return 0
	}
	return int(ws.col)
}
