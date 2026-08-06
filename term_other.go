//go:build !(linux || darwin || freebsd || netbsd || openbsd)

package main

func termWidth() int { return 0 }
