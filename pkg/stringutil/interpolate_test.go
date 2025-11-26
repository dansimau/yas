package stringutil

import (
	"testing"
)

func TestInterpolate(t *testing.T) {
	tests := []struct {
		name     string
		template string
		data     any
		want     string
		wantErr  bool
	}{
		{
			name:     "simple string substitution",
			template: "Hello, {{.Name}}!",
			data:     map[string]string{"Name": "World"},
			want:     "Hello, World!",
			wantErr:  false,
		},
		{
			name:     "multiple substitutions",
			template: "{{.Greeting}}, {{.Name}}! You are {{.Age}} years old.",
			data: map[string]any{
				"Greeting": "Hello",
				"Name":     "Alice",
				"Age":      30,
			},
			want:    "Hello, Alice! You are 30 years old.",
			wantErr: false,
		},
		{
			name:     "struct data",
			template: "{{.FirstName}} {{.LastName}}",
			data: struct {
				FirstName string
				LastName  string
			}{
				FirstName: "John",
				LastName:  "Doe",
			},
			want:    "John Doe",
			wantErr: false,
		},
		{
			name:     "no substitutions",
			template: "This is a plain string",
			data:     map[string]string{},
			want:     "This is a plain string",
			wantErr:  false,
		},
		{
			name:     "empty string",
			template: "",
			data:     map[string]string{},
			want:     "",
			wantErr:  false,
		},
		{
			name:     "with conditionals",
			template: "{{if .Show}}Visible{{else}}Hidden{{end}}",
			data:     map[string]bool{"Show": true},
			want:     "Visible",
			wantErr:  false,
		},
		{
			name:     "with range",
			template: "{{range .Items}}{{.}} {{end}}",
			data:     map[string][]string{"Items": {"a", "b", "c"}},
			want:     "a b c ",
			wantErr:  false,
		},
		{
			name:     "invalid template syntax",
			template: "{{.Name",
			data:     map[string]string{"Name": "Test"},
			want:     "",
			wantErr:  true,
		},
		{
			name:     "missing field returns no value",
			template: "{{.MissingField}}",
			data:     map[string]string{"Name": "Test"},
			want:     "<no value>",
			wantErr:  false,
		},
		{
			name:     "nested field access",
			template: "{{.User.Name}}",
			data: map[string]any{
				"User": map[string]string{"Name": "Alice"},
			},
			want:    "Alice",
			wantErr: false,
		},
		{
			name:     "with pipeline",
			template: "{{.Name | printf \"Name: %s\"}}",
			data:     map[string]string{"Name": "Test"},
			want:     "Name: Test",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Interpolate(tt.template, tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("Interpolate() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if got != tt.want {
				t.Errorf("Interpolate() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInterpolateEdgeCases(t *testing.T) {
	t.Run("nil data", func(t *testing.T) {
		result, err := Interpolate("No template vars", nil)
		if err != nil {
			t.Errorf("Interpolate() with nil data should not error, got: %v", err)
		}

		if result != "No template vars" {
			t.Errorf("Interpolate() = %q, want %q", result, "No template vars")
		}
	})

	t.Run("special characters", func(t *testing.T) {
		template := "Special: {{.Char}}"
		data := map[string]string{"Char": "<>&\"'"}

		result, err := Interpolate(template, data)
		if err != nil {
			t.Errorf("Interpolate() error = %v", err)
		}

		want := "Special: <>&\"'"
		if result != want {
			t.Errorf("Interpolate() = %q, want %q", result, want)
		}
	})

	t.Run("whitespace handling", func(t *testing.T) {
		template := "{{.Value}}"
		data := map[string]string{"Value": "  spaces  "}

		result, err := Interpolate(template, data)
		if err != nil {
			t.Errorf("Interpolate() error = %v", err)
		}

		want := "  spaces  "
		if result != want {
			t.Errorf("Interpolate() = %q, want %q", result, want)
		}
	})
}
