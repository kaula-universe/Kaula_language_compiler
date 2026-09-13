package testrunner

// Keep offsets/newlines while hiding block comments; quoted text and line
// comments must not accidentally open a block comment.
func stripBlockComments(source string) string {
	b := []byte(source)
	for i := 0; i < len(b); {
		if b[i] == '"' || b[i] == '\'' || b[i] == '`' {
			quote := b[i]
			i++
			for i < len(b) {
				if b[i] == '\\' && quote != '`' {
					i += 2
					continue
				}
				if b[i] == quote {
					i++
					break
				}
				i++
			}
		} else if i+1 < len(b) && b[i] == '/' && b[i+1] == '/' {
			for i < len(b) && b[i] != '\n' {
				i++
			}
		} else if i+1 < len(b) && b[i] == '/' && b[i+1] == '*' {
			b[i], b[i+1] = ' ', ' '
			i += 2
			depth := 1
			for i < len(b) && depth > 0 {
				if i+1 < len(b) && b[i] == '/' && b[i+1] == '*' {
					depth++
					b[i], b[i+1] = ' ', ' '
					i += 2
				} else if i+1 < len(b) && b[i] == '*' && b[i+1] == '/' {
					depth--
					b[i], b[i+1] = ' ', ' '
					i += 2
				} else {
					if b[i] != '\n' && b[i] != '\r' {
						b[i] = ' '
					}
					i++
				}
			}
		} else {
			i++
		}
	}
	return string(b)
}
