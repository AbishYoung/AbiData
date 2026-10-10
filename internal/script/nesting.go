package script

import "fmt"

// MaxNesting bounds syntactic nesting like Lua 5.1's LUAI_MAXCCALLS (200).
// gopher-lua's parser and compiler recurse without a limit, so deeply nested
// sources take quadratic time to compile (or exhaust the Go stack).
const MaxNesting = 200

// checkNesting scans source tokens before parsing and rejects sources whose
// block/bracket nesting, or whose run of operators within one expression,
// exceeds MaxNesting. Operator chains build equally deep ASTs (right-assoc
// .. and ^ and unary operators recurse in the parser; long left-assoc chains
// recurse in the compiler). It never rejects a source Lua 5.1 would accept.
func checkNesting(src string) error {
	// Each open bracket/block saves the enclosing expression's operator chain;
	// the parser's recursion depth is roughly depth + all saved chains + chain.
	depth, chain, saved := 0, 0, 0
	var stack []int
	line := 1
	tooDeep := func() error {
		return fmt.Errorf("line %d: the code is nested too deeply (more than %d levels)", line, MaxNesting)
	}
	open := func() error {
		depth++
		stack = append(stack, chain)
		saved += chain
		chain = 0
		if depth > MaxNesting || depth+saved > 2*MaxNesting {
			return tooDeep()
		}
		return nil
	}
	closeLevel := func() {
		if depth == 0 {
			return
		}
		depth--
		chain = stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		saved -= chain
	}
	op := func() error {
		chain++
		if chain > MaxNesting || depth+saved+chain > 2*MaxNesting {
			return fmt.Errorf("line %d: the expression is too long or nested too deeply (more than %d operators); split it into steps", line, MaxNesting)
		}
		return nil
	}
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			i++
		case c == '-' && i+1 < len(src) && src[i+1] == '-':
			i += 2
			if lvl, ok := longOpen(src, i); ok {
				i = skipLong(src, i, lvl, &line)
			} else {
				for i < len(src) && src[i] != '\n' {
					i++
				}
			}
		case c == '"' || c == '\'':
			i++
			for i < len(src) && src[i] != c {
				if src[i] == '\\' {
					i++
				}
				if i < len(src) && src[i] == '\n' {
					line++
				}
				i++
			}
			i++
		case c == '[':
			if lvl, ok := longOpen(src, i); ok {
				i = skipLong(src, i, lvl, &line)
				continue
			}
			if err := open(); err != nil {
				return err
			}
			i++
		case c == '(' || c == '{':
			if err := open(); err != nil {
				return err
			}
			i++
		case c == ')' || c == ']' || c == '}':
			closeLevel()
			i++
		case isDigit(c) || (c == '.' && i+1 < len(src) && isDigit(src[i+1])):
			for i < len(src) && (isNameByte(src[i]) || src[i] == '.' ||
				((src[i] == '+' || src[i] == '-') && (src[i-1] == 'e' || src[i-1] == 'E'))) {
				i++
			}
		case isNameByte(c):
			j := i
			for j < len(src) && isNameByte(src[j]) {
				j++
			}
			switch src[i:j] {
			case "do", "if", "function", "repeat":
				if err := open(); err != nil {
					return err
				}
			case "end", "until":
				closeLevel()
				chain = 0
			case "and", "or", "not":
				if err := op(); err != nil {
					return err
				}
			case "local", "return", "then", "else", "elseif", "while", "for", "in", "break":
				chain = 0
			}
			i = j
		case c == '.' && i+2 < len(src) && src[i+1] == '.' && src[i+2] == '.':
			i += 3 // varargs
		case c == '.' && i+1 < len(src) && src[i+1] == '.':
			if err := op(); err != nil {
				return err
			}
			i += 2
		case c == '=' && (i+1 >= len(src) || src[i+1] != '='):
			chain = 0
			i++
		case c == ',' || c == ';':
			chain = 0
			i++
		case c == '=' || c == '~' || c == '<' || c == '>':
			if i+1 < len(src) && src[i+1] == '=' {
				i++
			}
			if err := op(); err != nil {
				return err
			}
			i++
		case c == '+' || c == '-' || c == '*' || c == '/' || c == '%' || c == '^' || c == '#':
			if err := op(); err != nil {
				return err
			}
			i++
		default:
			i++
		}
	}
	return nil
}

// longOpen reports whether a long bracket ([[ or [==[) starts at src[i].
func longOpen(src string, i int) (int, bool) {
	if i >= len(src) || src[i] != '[' {
		return 0, false
	}
	j := i + 1
	for j < len(src) && src[j] == '=' {
		j++
	}
	if j < len(src) && src[j] == '[' {
		return j - i - 1, true
	}
	return 0, false
}

// skipLong returns the index after the long bracket starting at src[i].
func skipLong(src string, i, lvl int, line *int) int {
	i += lvl + 2
	for i < len(src) {
		if src[i] == '\n' {
			*line++
		}
		if src[i] == ']' {
			j := i + 1
			for j < len(src) && src[j] == '=' {
				j++
			}
			if j-i-1 == lvl && j < len(src) && src[j] == ']' {
				return j + 1
			}
		}
		i++
	}
	return i
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isNameByte(c byte) bool {
	return c == '_' || isDigit(c) || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
