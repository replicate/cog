package console

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/replicate/cog/pkg/util/slices"
)

type Interactive struct {
	Prompt   string
	Default  string
	Options  []string
	Required bool
}

func (i Interactive) Read() (string, error) {
	if i.Default != "" && i.Options != nil && !slices.ContainsString(i.Options, i.Default) {
		panic("Default is not an option")
	}

	parens := ""
	if i.Required {
		parens += "required"
	}
	if i.Default != "" {
		if parens != "" {
			parens += ", "
		}
		parens += "default: " + i.Default
	}
	if i.Options != nil {
		if parens != "" {
			parens += ", "
		}
		parens += "options: " + strings.Join(i.Options, ", ")
	}
	if parens != "" {
		parens = " (" + parens + ")"
	}

	for {
		fmt.Printf("%s%s: ", i.Prompt, parens)
		reader := bufio.NewReader(os.Stdin)
		text, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		text = strings.TrimSpace(text)
		if text == "" && i.Default != "" {
			text = i.Default
		}

		if i.Required && text == "" {
			Warn("Please enter a value")
			continue
		}

		if !i.Required && text == "" {
			return "", nil
		}

		if i.Options != nil {
			if !slices.ContainsString(i.Options, text) {
				Warnf("%s is not a valid option", text)
				continue
			}
		}

		return text, nil
	}
}

type InteractiveBool struct {
	Prompt  string
	Default bool
	// NonDefaultFlag is the flag to suggest passing to do the thing which isn't default when running inside a script
	NonDefaultFlag string
}

func (i InteractiveBool) Read() (bool, error) {
	defaults := "y/N"
	if i.Default {
		defaults = "Y/n"
	}
	for {
		fmt.Printf("%s (%s) ", i.Prompt, defaults)
		reader := bufio.NewReader(os.Stdin)
		text, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return false, fmt.Errorf("stdin is closed. If you're running in a script, you need to pass the '%s' option", i.NonDefaultFlag)
			}
			return false, err
		}
		text = strings.ToLower(strings.TrimSpace(text))
		if text == "yes" || text == "y" {
			return true, nil
		}
		if text == "no" || text == "n" {
			return false, nil
		}
		if text == "" {
			return i.Default, nil
		}
		Warn("Please enter 'y' or 'n'")
	}
}
