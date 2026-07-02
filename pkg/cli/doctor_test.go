package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/doctor"
	"github.com/replicate/cog/pkg/util/console"
)

func TestPrintDoctorResultsFixShowsRemediationForUnfixedFinding(t *testing.T) {
	result := &doctor.Result{
		Results: []doctor.CheckResult{
			{
				Check: unfixedDoctorCheck{},
				Findings: []doctor.Finding{
					{
						Severity:    doctor.SeverityError,
						Message:     "predict.py not found",
						Remediation: `Create predict.py or update the predict field in cog.yaml`,
						File:        "cog.yaml",
						Line:        3,
					},
				},
			},
		},
	}

	output := captureDoctorStderr(t, func() {
		console.SetColor(false)
		printDoctorResults(result, true, false)
	})

	require.Contains(t, output, "predict.py not found")
	require.Contains(t, output, "Remediation: Create predict.py or update the predict field in cog.yaml")
	require.NotContains(t, output, "(no auto-fix available)")
}

func TestPrintDoctorResultsFixShowsNoAutoFixWhenRemediationMissing(t *testing.T) {
	result := &doctor.Result{
		Results: []doctor.CheckResult{
			{
				Check: unfixedDoctorCheck{},
				Findings: []doctor.Finding{
					{
						Severity: doctor.SeverityWarning,
						Message:  "something could not be fixed",
					},
				},
			},
		},
	}

	output := captureDoctorStderr(t, func() {
		console.SetColor(false)
		printDoctorResults(result, true, false)
	})

	require.Contains(t, output, "(no auto-fix available)")
}

type unfixedDoctorCheck struct{}

func (unfixedDoctorCheck) Name() string        { return "unfixed-check" }
func (unfixedDoctorCheck) Group() doctor.Group { return doctor.GroupConfig }
func (unfixedDoctorCheck) Description() string { return "Unfixed check" }
func (unfixedDoctorCheck) Check(*doctor.CheckContext) ([]doctor.Finding, error) {
	return nil, nil
}
func (unfixedDoctorCheck) Fix(*doctor.CheckContext, []doctor.Finding) error {
	return doctor.ErrNoAutoFix
}

func captureDoctorStderr(t *testing.T, fn func()) string {
	t.Helper()

	originalStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)

	os.Stderr = w
	defer func() {
		os.Stderr = originalStderr
	}()

	fn()

	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	return strings.ReplaceAll(string(out), "\r\n", "\n")
}
