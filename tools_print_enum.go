// Code generated by go-enum DO NOT EDIT.
// Version:
// Revision:
// Build Date:
// Built By:

package firecore

import (
	"fmt"
	"strings"
)

const (
	// PrintOutputModeText is a PrintOutputMode of type Text.
	PrintOutputModeText PrintOutputMode = iota
	// PrintOutputModeJSON is a PrintOutputMode of type JSON.
	PrintOutputModeJSON
)

const _PrintOutputModeName = "TextJSON"

var _PrintOutputModeNames = []string{
	_PrintOutputModeName[0:4],
	_PrintOutputModeName[4:8],
}

// PrintOutputModeNames returns a list of possible string values of PrintOutputMode.
func PrintOutputModeNames() []string {
	tmp := make([]string, len(_PrintOutputModeNames))
	copy(tmp, _PrintOutputModeNames)
	return tmp
}

var _PrintOutputModeMap = map[PrintOutputMode]string{
	PrintOutputModeText: _PrintOutputModeName[0:4],
	PrintOutputModeJSON: _PrintOutputModeName[4:8],
}

// String implements the Stringer interface.
func (x PrintOutputMode) String() string {
	if str, ok := _PrintOutputModeMap[x]; ok {
		return str
	}
	return fmt.Sprintf("PrintOutputMode(%d)", x)
}

var _PrintOutputModeValue = map[string]PrintOutputMode{
	_PrintOutputModeName[0:4]:                  PrintOutputModeText,
	strings.ToLower(_PrintOutputModeName[0:4]): PrintOutputModeText,
	_PrintOutputModeName[4:8]:                  PrintOutputModeJSON,
	strings.ToLower(_PrintOutputModeName[4:8]): PrintOutputModeJSON,
}

// ParsePrintOutputMode attempts to convert a string to a PrintOutputMode
func ParsePrintOutputMode(name string) (PrintOutputMode, error) {
	if x, ok := _PrintOutputModeValue[name]; ok {
		return x, nil
	}
	// Case insensitive parse, do a separate lookup to prevent unnecessary cost of lowercasing a string if we don't need to.
	if x, ok := _PrintOutputModeValue[strings.ToLower(name)]; ok {
		return x, nil
	}
	return PrintOutputMode(0), fmt.Errorf("%s is not a valid PrintOutputMode, try [%s]", name, strings.Join(_PrintOutputModeNames, ", "))
}

// MarshalText implements the text marshaller method
func (x PrintOutputMode) MarshalText() ([]byte, error) {
	return []byte(x.String()), nil
}

// UnmarshalText implements the text unmarshaller method
func (x *PrintOutputMode) UnmarshalText(text []byte) error {
	name := string(text)
	tmp, err := ParsePrintOutputMode(name)
	if err != nil {
		return err
	}
	*x = tmp
	return nil
}
