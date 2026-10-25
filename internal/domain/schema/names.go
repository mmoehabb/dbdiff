package schema

import (
	"fmt"
	"strings"
)

// ParseQualifiedName recognizes escaped SQL Server bracket identifiers and dots inside names.
func ParseQualifiedName(s string) ([]string, error) {
	var parts []string
	s = strings.TrimSpace(s)
	for len(s) > 0 {
		var name strings.Builder
		if s[0] == '[' {
			s = s[1:]
			closed := false
			for len(s) > 0 {
				if s[0] == ']' {
					if len(s) > 1 && s[1] == ']' {
						name.WriteByte(']')
						s = s[2:]
						continue
					}
					s = s[1:]
					closed = true
					break
				}
				name.WriteByte(s[0])
				s = s[1:]
			}
			if !closed {
				return nil, fmt.Errorf("invalid qualified identifier")
			}
		} else {
			n := strings.IndexByte(s, '.')
			if n < 0 {
				n = len(s)
			}
			name.WriteString(strings.TrimSpace(s[:n]))
			s = s[n:]
		}
		if name.Len() == 0 {
			return nil, fmt.Errorf("empty identifier")
		}
		parts = append(parts, name.String())
		s = strings.TrimSpace(s)
		if s == "" {
			break
		}
		if s[0] != '.' || len(s) == 1 {
			return nil, fmt.Errorf("invalid qualified identifier")
		}
		s = strings.TrimSpace(s[1:])
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty identifier")
	}
	return parts, nil
}
