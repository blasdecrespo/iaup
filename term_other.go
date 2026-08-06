//go:build !(linux || darwin || freebsd || netbsd || openbsd)

package main

import "os"

func termWidth() int { return 0 }

// isTerminal sin ioctl: lo mejor disponible es preguntar si la salida es un
// dispositivo de caracteres. Da un falso positivo con /dev/null, que aquí no
// importa porque nadie lee esa salida.
func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
