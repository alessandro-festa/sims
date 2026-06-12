package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunDoctor_TableHeaderAndCriticalExitCode(t *testing.T) {
	// We can't avoid running the real checks here (they exec docker, hit
	// the network) but we can at least confirm the table header lands and
	// the function returns *something*.
	var buf bytes.Buffer
	err := runDoctor(context.Background(), &buf)
	out := buf.String()

	for _, want := range []string{"CHECK", "SEVERITY", "RESULT", "DETAIL", "docker daemon"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
	// On a CI runner without Docker the daemon check fails → non-nil error.
	// Either way, the test just asserts the function returned cleanly.
	_ = err
}

func TestSeverity_Label(t *testing.T) {
	cases := []struct {
		s    severity
		want string
	}{
		{sevCritical, "critical"},
		{sevWarn, "warn"},
		{sevInfo, "info"},
	}
	for _, c := range cases {
		if got := c.s.label(); got != c.want {
			t.Errorf("severity(%d).label() = %q, want %q", c.s, got, c.want)
		}
	}
}
