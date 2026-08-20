package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
)

// render hands payload to kit's output dispatch (--format, --output,
// --cols, --template, --format-help). Unknown formats normally refuse
// in resolveFormat before the verb runs; one reaching Dispatch from
// another source maps to the same usage/config exit class here.
func render(cmd *cobra.Command, payload any) error {
	if err := output.Dispatch(cmd, outputViper, payload); err != nil {
		if strings.Contains(err.Error(), "unknown output format") {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "evol: %v\n", err)
			return &codedError{code: exitConfigError, err: err}
		}
		return err
	}
	return nil
}

// jsonForm re-encodes v through its JSON representation so YAML output
// carries exactly the key names the JSON contract declares (json
// struct tags, omitempty included) — yaml-encoding the structs
// directly would smash-lowercase field names. Report types return this
// from MarshalYAML.
func jsonForm(v any) (any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// renderHumanJSON is the human view for payloads without a bespoke
// terminal rendering: the indented JSON document.
func renderHumanJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
