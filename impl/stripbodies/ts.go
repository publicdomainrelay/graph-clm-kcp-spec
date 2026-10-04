package stripbodies

import (
	"os"
	"sort"
	"strings"
)

// tsBody is the replacement TypeScript body. A throw is assignable to every
// declared return type, so a stripped module still type checks.
const tsBody = `{ throw new Error("unimplemented"); }`

type tsToken struct {
	text string

	start int

	end int
}

// scanTS reads the source into tokens, dropping whitespace and comments. A
// token is an identifier, a number, a string body or one punctuation mark; the
// exact text never matters past the few words the stripper looks for.
func scanTS(source string) []tsToken {
	tokens := []tsToken{}
	for index := 0; index < len(source); {
		char := source[index]
		switch {
		case char == ' ' || char == '\t' || char == '\r' || char == '\n':
			index++
		case char == '/' && index+1 < len(source) && source[index+1] == '/':
			for index < len(source) && source[index] != '\n' {
				index++
			}
		case char == '/' && index+1 < len(source) && source[index+1] == '*':
			index += 2
			for index+1 < len(source) && !(source[index] == '*' && source[index+1] == '/') {
				index++
			}
			index = min(index+2, len(source))
		case char == '\'' || char == '"' || char == '`':
			start := index
			quote := char
			index++
			for index < len(source) {
				if source[index] == '\\' {
					index += 2
					continue
				}
				if source[index] == quote {
					index++
					break
				}
				index++
			}
			tokens = append(tokens, tsToken{text: "string", start: start, end: min(index, len(source))})
		case isIdentStart(char):
			start := index
			for index < len(source) && isIdentPart(source[index]) {
				index++
			}
			tokens = append(tokens, tsToken{text: source[start:index], start: start, end: index})
		case char >= '0' && char <= '9':
			start := index
			for index < len(source) && (isIdentPart(source[index]) || source[index] == '.') {
				index++
			}
			tokens = append(tokens, tsToken{text: "number", start: start, end: index})
		default:
			tokens = append(tokens, tsToken{text: string(char), start: index, end: index + 1})
			index++
		}
	}
	return tokens
}

func isIdentStart(char byte) bool {
	return char == '_' || char == '$' || (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

func isIdentPart(char byte) bool {
	return isIdentStart(char) || (char >= '0' && char <= '9')
}

func isTSKeyword(text string) bool {
	switch text {
	case "if", "else", "for", "while", "switch", "case", "catch", "return", "new", "typeof",
		"await", "delete", "void", "in", "of", "do", "yield", "function", "class", "const",
		"let", "var", "export", "import", "default", "extends", "implements", "interface", "type":
		return true
	}
	return false
}

func isTSModifier(text string) bool {
	switch text {
	case "private", "public", "protected", "readonly", "static", "async", "abstract", "declare",
		"override", "get", "set", "export", "default":
		return true
	}
	return false
}

// stripTS rewrites one TypeScript file in place and reports how many bodies it
// removed: every function declaration and every class method. An arrow function
// whose body is a block is out of scope, because telling it apart from a call
// needs a real parser and the fixtures do not use one.
func stripTS(path string) (int, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	tokens := scanTS(string(source))
	replacements := [][2]int{}
	classDepths := []int{}
	depth := 0
	for index := 0; index < len(tokens); index++ {
		token := tokens[index]
		switch token.text {
		case "{":
			depth++
			continue
		case "}":
			depth--
			if len(classDepths) > 0 && classDepths[len(classDepths)-1] == depth {
				classDepths = classDepths[:len(classDepths)-1]
			}
			continue
		case "class":
			brace := index + 1
			for brace < len(tokens) && tokens[brace].text != "{" && tokens[brace].text != ";" {
				brace++
			}
			if brace < len(tokens) && tokens[brace].text == "{" {
				depth++
				classDepths = append(classDepths, depth)
				index = brace
			}
			continue
		}
		isFunction := token.text == "function"
		isMethod := len(classDepths) > 0 && depth == classDepths[len(classDepths)-1] &&
			isIdentStart(token.text[0]) && !isTSKeyword(token.text) &&
			index+1 < len(tokens) && tokens[index+1].text == "(" && startsMember(tokens, index)
		if !isFunction && !isMethod {
			continue
		}
		open := index + 1
		if isFunction {
			open = index + 1
			if open < len(tokens) && isIdentStart(tokens[open].text[0]) {
				open++
			}
			if open < len(tokens) && tokens[open].text == "<" {
				close := skipBalanced(tokens, open, "<", ">")
				if close < 0 {
					continue
				}
				open = close + 1
			}
		}
		if open >= len(tokens) || tokens[open].text != "(" {
			continue
		}
		closeParen := skipBalanced(tokens, open, "(", ")")
		if closeParen < 0 {
			continue
		}
		body := tsBodyStart(tokens, closeParen+1)
		if body < 0 {
			continue
		}
		closeBrace := skipBalanced(tokens, body, "{", "}")
		if closeBrace < 0 {
			continue
		}
		replacements = append(replacements, [2]int{tokens[body].start, tokens[closeBrace].end})
		index = closeBrace
	}
	if len(replacements) == 0 {
		return 0, nil
	}
	sort.Slice(replacements, func(left, right int) bool { return replacements[left][0] > replacements[right][0] })
	out := string(source)
	for _, span := range replacements {
		out = out[:span[0]] + tsBody + out[span[1]:]
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return len(replacements), os.WriteFile(path, []byte(out), info.Mode())
}

// startsMember reports whether the identifier at index begins a class member
// declaration rather than a call inside a field initializer: what precedes it
// is the class brace, the end of the member before it, or a modifier.
func startsMember(tokens []tsToken, index int) bool {
	if index == 0 {
		return true
	}
	previous := tokens[index-1].text
	switch previous {
	case "{", "}", ";", "*":
		return true
	}
	return isTSModifier(previous) && !isTSKeyword(previous)
}

// tsBodyStart finds the brace that opens the body after a parameter list, and
// skips an inline object type in the return position: a brace right after a
// type punctuation is a type literal, and the body brace comes later.
func tsBodyStart(tokens []tsToken, index int) int {
	if index < len(tokens) && tokens[index].text != ":" {
		if tokens[index].text == "{" {
			return index
		}
		return -1
	}
	if index < len(tokens) {
		index++
	}
	for index < len(tokens) {
		switch tokens[index].text {
		case "{":
			previous := ""
			if index > 0 {
				previous = tokens[index-1].text
			}
			switch previous {
			case ":", "|", "&", "<", ",", "(", "[", "=>":
				close := skipBalanced(tokens, index, "{", "}")
				if close < 0 {
					return -1
				}
				index = close + 1
				continue
			}
			return index
		case ";", ")", "=":
			return -1
		}
		index++
	}
	return -1
}

// skipBalanced returns the index of the closing token of the balanced pair
// opened at index, or -1 when it never closes.
func skipBalanced(tokens []tsToken, index int, open, close string) int {
	depth := 0
	for position := index; position < len(tokens); position++ {
		switch tokens[position].text {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return position
			}
		}
	}
	return -1
}

// needsStrip decides the language from the file name.
func needsStrip(path string) bool {
	return strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".ts")
}
