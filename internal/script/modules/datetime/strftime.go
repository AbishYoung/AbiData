package datetime

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var errTooLong = errors.New("result too long")

// strftime formats t per the supported directive set. maxLen > 0 bounds the
// result (errTooLong).
func strftime(t time.Time, layout string, maxLen int) (string, error) {
	var b strings.Builder
	for i := 0; i < len(layout); i++ {
		c := layout[i]
		if c != '%' {
			b.WriteByte(c)
		} else {
			i++
			if i >= len(layout) {
				return "", errors.New("layout ends with a lone '%'")
			}
			switch layout[i] {
			case 'Y':
				fmt.Fprintf(&b, "%04d", t.Year())
			case 'm':
				fmt.Fprintf(&b, "%02d", int(t.Month()))
			case 'd':
				fmt.Fprintf(&b, "%02d", t.Day())
			case 'H':
				fmt.Fprintf(&b, "%02d", t.Hour())
			case 'M':
				fmt.Fprintf(&b, "%02d", t.Minute())
			case 'S':
				fmt.Fprintf(&b, "%02d", t.Second())
			case 'y':
				fmt.Fprintf(&b, "%02d", t.Year()%100)
			case 'b':
				b.WriteString(t.Month().String()[:3])
			case 'B':
				b.WriteString(t.Month().String())
			case 'a':
				b.WriteString(t.Weekday().String()[:3])
			case 'A':
				b.WriteString(t.Weekday().String())
			case 'p':
				if t.Hour() < 12 {
					b.WriteString("AM")
				} else {
					b.WriteString("PM")
				}
			case 'I':
				h := t.Hour() % 12
				if h == 0 {
					h = 12
				}
				fmt.Fprintf(&b, "%02d", h)
			case 'j':
				fmt.Fprintf(&b, "%03d", t.YearDay())
			case 'Z':
				name, _ := t.Zone()
				b.WriteString(name)
			case 'z':
				_, off := t.Zone()
				sign := '+'
				if off < 0 {
					sign, off = '-', -off
				}
				fmt.Fprintf(&b, "%c%02d%02d", sign, off/3600, off%3600/60)
			case '%':
				b.WriteByte('%')
			default:
				return "", fmt.Errorf("unknown directive %%%c", layout[i])
			}
		}
		if maxLen > 0 && b.Len() > maxLen {
			return "", errTooLong
		}
	}
	return b.String(), nil
}
