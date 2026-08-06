package main

import (
	"strconv"
	"strings"
)

// firstVersion devuelve el primer número de versión que aparece en s.
// "rust-v0.146.1" -> "0.146.1"   "2.1.223 (Claude Code)" -> "2.1.223"
// "v0.55.0-preview.1" -> "0.55.0-preview.1"
//
// Un número de versión es: dígito, luego dígitos/puntos, y opcionalmente un
// sufijo de precompilación tras '-'. Devuelve "" si no hay ninguno.
func firstVersion(s string) string {
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			continue
		}
		// Un dígito no empieza versión si continúa un número ya empezado.
		// Una letra delante sí vale: "v1.2.3", "rust-v0.146.1".
		if i > 0 && (isDigit(s[i-1]) || s[i-1] == '.') {
			continue
		}
		j := i
		dots := 0
		for j < len(s) && (isDigit(s[j]) || (s[j] == '.' && j+1 < len(s) && isDigit(s[j+1]))) {
			if s[j] == '.' {
				dots++
			}
			j++
		}
		if dots == 0 { // "5" no es versión; queremos al menos "0.1"
			i = j
			continue
		}
		end := j
		if j < len(s) && s[j] == '-' { // sufijo de precompilación
			k := j + 1
			for k < len(s) && (isDigit(s[k]) || isAlpha(s[k]) || s[k] == '.' || s[k] == '-') {
				k++
			}
			if k > j+1 {
				end = k
			}
		}
		return s[i:end]
	}
	return ""
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isAlpha(c byte) bool { return (c|32) >= 'a' && (c|32) <= 'z' }

// cmpVersion compara dos versiones. <0 si a<b, 0 si iguales, >0 si a>b.
//
// Compara los números por valor, no por texto ("0.9" < "0.10"). Una versión
// con sufijo va antes que la misma sin él: 1.0.0-alpha < 1.0.0.
func cmpVersion(a, b string) int {
	an, apre := splitVersion(a)
	bn, bpre := splitVersion(b)

	for i := 0; i < len(an) || i < len(bn); i++ {
		x, y := 0, 0
		if i < len(an) {
			x = an[i]
		}
		if i < len(bn) {
			y = bn[i]
		}
		if x != y {
			return x - y
		}
	}
	switch {
	case apre == "" && bpre == "":
		return 0
	case apre == "":
		return 1
	case bpre == "":
		return -1
	}
	return strings.Compare(apre, bpre)
}

func splitVersion(v string) ([]int, string) {
	pre := ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		pre, v = v[i+1:], v[:i]
	}
	parts := strings.Split(v, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			break
		}
		nums = append(nums, n)
	}
	return nums, pre
}

// relTime formatea una antigüedad como "3h", "2d", "5mo". Sin decimales:
// en una tabla de estado la magnitud es lo único que se lee.
func relTime(sec float64) string {
	switch {
	case sec < 0:
		return "-"
	case sec < 60:
		return "ahora"
	case sec < 3600:
		return strconv.Itoa(int(sec/60)) + "m"
	case sec < 86400:
		return strconv.Itoa(int(sec/3600)) + "h"
	case sec < 86400*60:
		return strconv.Itoa(int(sec/86400)) + "d"
	case sec < 86400*365:
		return strconv.Itoa(int(sec/(86400*30))) + "mo"
	}
	return strconv.Itoa(int(sec/(86400*365))) + "y"
}
