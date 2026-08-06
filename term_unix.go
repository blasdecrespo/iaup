//go:build linux || darwin || freebsd || netbsd || openbsd

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// winsize pregunta al sistema por el tamaño de la ventana. Que la llamada
// funcione es la definición de "esto es un terminal": un fichero, una tubería
// o /dev/null la rechazan.
//
// Es una llamada al sistema, no una dependencia.
func winsize() (cols int, ok bool) {
	var ws struct{ row, col, xpix, ypix uint16 }
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL,
		os.Stdout.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if err != 0 {
		return 0, false
	}
	return int(ws.col), true
}

func termWidth() int {
	cols, ok := winsize()
	if !ok {
		return 0
	}
	return cols
}

func isTerminal() bool {
	_, ok := winsize()
	return ok
}
