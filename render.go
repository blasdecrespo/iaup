package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// Colores como variables, no como constantes: cuando la salida no es un
// terminal valen "" y el mismo código imprime texto plano. Sin ramas.
var (
	cBold, cDim, cCyan, cGreen, cYellow, cRed, cReset string
)

func initColor(force, never bool) {
	on := force
	if !never && !force {
		on = isTerminal() && os.Getenv("NO_COLOR") == ""
	}
	if never {
		on = false
	}
	if !on {
		return
	}
	cBold, cDim = "\x1b[1m", "\x1b[2m"
	cCyan, cGreen, cYellow, cRed = "\x1b[36m", "\x1b[32m", "\x1b[33m", "\x1b[31m"
	cReset = "\x1b[0m"
}

func wrapWidth() int {
	w := termWidth()
	if w <= 0 {
		return 96
	}
	if w > 100 {
		return 100
	}
	if w < 40 {
		return 40
	}
	return w
}

// wrap parte el texto en líneas que caben en width, con sangría para las
// continuaciones. Corta por palabras; una palabra más larga que la línea se
// deja intacta antes que romperla (suele ser una URL o una ruta).
func wrap(text, first, cont string, width int) string {
	limit := width - utf8.RuneCountInString(cont)
	if limit < 20 {
		return first + text
	}
	var b strings.Builder
	b.WriteString(first)
	col := 0
	for i, word := range strings.Fields(text) {
		wl := utf8.RuneCountInString(word)
		if i > 0 {
			if col+1+wl > limit {
				b.WriteString("\n")
				b.WriteString(cont)
				col = 0
			} else {
				b.WriteString(" ")
				col++
			}
		}
		b.WriteString(word)
		col += wl
	}
	return b.String()
}

// ---------- salida de un release ----------

func printRelease(w io.Writer, r Release, secs []Section) {
	width := wrapWidth()
	tag := ""
	if r.Pre {
		tag = cYellow + " (precompilación)" + cReset
	}
	fmt.Fprintf(w, "%s%s %s%s %s(%s)%s%s\n",
		cBold, r.Source, r.Version, cReset,
		cDim, r.Date.Local().Format("2006-01-02"), cReset, tag)
	fmt.Fprintf(w, "%s%s%s\n", cDim, strings.Repeat("─", 48), cReset)

	if len(secs) == 0 {
		fmt.Fprintf(w, "%s  (sin notas de versión)%s\n", cDim, cReset)
		return
	}
	for _, s := range secs {
		if s.Name != "" {
			fmt.Fprintf(w, "\n%s%s%s\n", cCyan, s.Name, cReset)
		} else {
			fmt.Fprintln(w)
		}
		for _, c := range s.Changes {
			indent := ""
			if strings.HasPrefix(c, "  ") {
				indent, c = "  ", strings.TrimSpace(c)
			}
			fmt.Fprintln(w, wrap(c, indent+"  • ", indent+"    ", width))
		}
	}
}

func printMarkdown(w io.Writer, r Release, secs []Section) {
	fmt.Fprintf(w, "## %s %s\n\n", r.Source, r.Version)
	fmt.Fprintf(w, "_%s_ · [%s](%s)\n", r.Date.Local().Format("2006-01-02"), r.Tag, r.URL)
	for _, s := range secs {
		if s.Name != "" {
			fmt.Fprintf(w, "\n### %s\n\n", s.Name)
		} else {
			fmt.Fprintln(w)
		}
		for _, c := range s.Changes {
			if strings.HasPrefix(c, "  ") {
				fmt.Fprintf(w, "  - %s\n", strings.TrimSpace(c))
			} else {
				fmt.Fprintf(w, "- %s\n", c)
			}
		}
	}
}

type jsonRelease struct {
	Source     string    `json:"source"`
	Name       string    `json:"name"`
	Version    string    `json:"version"`
	Tag        string    `json:"tag"`
	ReleasedAt time.Time `json:"released_at"`
	Prerelease bool      `json:"prerelease"`
	URL        string    `json:"url"`
	Sections   []Section `json:"sections"`
}

func toJSON(r Release, secs []Section) jsonRelease {
	if secs == nil {
		secs = []Section{}
	}
	return jsonRelease{
		Source: r.SourceID, Name: r.Source, Version: r.Version, Tag: r.Tag,
		ReleasedAt: r.Date, Prerelease: r.Pre, URL: r.URL, Sections: secs,
	}
}

func emitJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// ---------- tabla ----------

// printTable dibuja una tabla con anchos calculados a partir del contenido.
// cells[i][j] puede llevar color; widths se calcula sobre el texto sin color.
func printTable(w io.Writer, head []string, rows [][]string, plain [][]string) {
	n := len(head)
	width := make([]int, n)
	for j, h := range head {
		width[j] = utf8.RuneCountInString(h)
	}
	for _, r := range plain {
		for j := 0; j < n && j < len(r); j++ {
			if l := utf8.RuneCountInString(r[j]); l > width[j] {
				width[j] = l
			}
		}
	}
	line := func(l, m, r string) {
		var b strings.Builder
		b.WriteString(l)
		for j := 0; j < n; j++ {
			b.WriteString(strings.Repeat("─", width[j]+2))
			if j < n-1 {
				b.WriteString(m)
			}
		}
		b.WriteString(r)
		fmt.Fprintf(w, "%s%s%s\n", cDim, b.String(), cReset)
	}

	line("┌", "┬", "┐")
	fmt.Fprintf(w, "%s│%s", cDim, cReset)
	for j, h := range head {
		fmt.Fprintf(w, " %s%s%s %s│%s", cBold, pad(h, width[j]), cReset, cDim, cReset)
	}
	fmt.Fprintln(w)
	line("├", "┼", "┤")

	for i, r := range rows {
		fmt.Fprintf(w, "%s│%s", cDim, cReset)
		for j := 0; j < n; j++ {
			cell, raw := "", ""
			if j < len(r) {
				cell, raw = r[j], plain[i][j]
			}
			fmt.Fprintf(w, " %s%s %s│%s", cell,
				strings.Repeat(" ", width[j]-utf8.RuneCountInString(raw)), cDim, cReset)
		}
		fmt.Fprintln(w)
	}
	line("└", "┴", "┘")
}

func pad(s string, w int) string {
	if d := w - utf8.RuneCountInString(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}
