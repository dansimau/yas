package stringutil

import (
	"bytes"
	"text/template"
)

func Interpolate(s string, data any) (string, error) {
	tmpl, err := template.New("").Parse(s)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func MustInterpolate(s string, data any) string {
	result, err := Interpolate(s, data)
	if err != nil {
		panic(err)
	}

	return result
}
