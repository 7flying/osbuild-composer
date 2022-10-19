package osbuild

import (
	"encoding/json"
	"testing"

	"github.com/osbuild/osbuild-composer/internal/common"
	"github.com/stretchr/testify/assert"
)

func TestNewSystemdJournalStage(t *testing.T) {
	expectedStage := &Stage{
		Type:    "org.osbuild.systemd-journald",
		Options: &SystemdJournaldStageOptions{},
	}
	actualStage := NewSystemdJournaldStage(&SystemdJournaldStageOptions{})
	assert.Equal(t, expectedStage, actualStage)
}

func TestSystemdJournaldStage_MarshalJSON_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		options SystemdJournaldStageOptions
	}{
		{
			name:    "empty-options",
			options: SystemdJournaldStageOptions{},
		},
		{
			name: "no-section-options",
			options: SystemdJournaldStageOptions{
				Filename: "10-some-file.conf",
				Config: SystemdJournaldConfigDropin{
					Journal: SystemdJournaldConfigJournalSection{},
				},
			},
		},
	}
	for idx, te := range tests {
		t.Run(te.name, func(t *testing.T) {
			gotBytes, err := json.Marshal(te.options)
			assert.NotNilf(t, err, "json.Marshall() didn't return an error, but: %s [idx: %d]", string(gotBytes), idx)
		})
	}
}

func TestSystemdJournaldStage_MarshalJSON_Valid(t *testing.T) {
	testOk := SystemdJournaldStageOptions{
		Filename: "20-another-file.conf",
		Config: SystemdJournaldConfigDropin{
			Journal: SystemdJournaldConfigJournalSection{
				Storage: common.StringToPtr("persistent"),
			},
		},
	}
	assert := assert.New(t)
	gotBytes, err := json.Marshal(testOk)
	assert.NoError(err)
	assert.NotEmpty(gotBytes)
}
