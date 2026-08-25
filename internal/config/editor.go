package config

import (
	"errors"
	"strings"
	"unicode"
)

// DefaultEditor is used when neither config nor $EDITOR names one. vi is the
// portable editor name expected on the supported Unix-like systems.
const DefaultEditor = "vi"

// ResolveEditor returns an executable and arguments without invoking a shell.
// The temporary filename is appended by the UI as a separate final argument.
func (c *Config) ResolveEditor(getenv func(string) string) ([]string, error) {
	command := strings.TrimSpace(c.Editor)
	if command == "" && getenv != nil {
		command = strings.TrimSpace(getenv("EDITOR"))
	}
	if command == "" {
		command = DefaultEditor
	}
	words, err := splitCommand(command)
	if err != nil {
		return nil, &Error{Key: "editor", Msg: err.Error()}
	}
	if len(words) == 0 || words[0] == "" {
		return nil, &Error{Key: "editor", Msg: "must name an executable"}
	}
	return words, nil
}

// splitCommand handles the quoting needed by editor flags, but performs no
// expansion, substitution, redirection, or other shell behavior.
func splitCommand(command string) ([]string, error) {
	var words []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	flush := func() {
		if started {
			words = append(words, word.String())
			word.Reset()
			started = false
		}
	}
	for _, r := range command {
		if escaped {
			word.WriteRune(r)
			escaped, started = false, true
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped, started = true, true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			started = true
			continue
		}
		switch {
		case r == '\'' || r == '"':
			quote, started = r, true
		case unicode.IsSpace(r):
			flush()
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if escaped {
		return nil, errors.New("ends with an unfinished escape")
	}
	if quote != 0 {
		return nil, errors.New("contains an unterminated quote")
	}
	flush()
	return words, nil
}
