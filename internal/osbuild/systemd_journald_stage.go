package osbuild

import (
	"encoding/json"
	"fmt"
)

type SystemdJournaldStageOptions struct {
	Filename string                      `json:"filename"`
	Config   SystemdJournaldConfigDropin `json:"config"`
}

func (SystemdJournaldStageOptions) isStageOptions() {}

func NewSystemdJournaldStage(options *SystemdJournaldStageOptions) *Stage {
	return &Stage{
		Type:    "org.osbuild.systemd-journald",
		Options: options,
	}
}

type SystemdJournaldConfigDropin struct {
	Journal SystemdJournaldConfigJournalSection `json:"Journal"`
}

// 'Journal' configuration section, at least one option must be specified
type SystemdJournaldConfigJournalSection struct {
	// Controls where to store journal data.
	Storage *string `json:"Storage,omitempty"`
	// Sets whether the data objects stored in the journal should be
	// compressed or not. Can also take threshold values.
	Compress *string `json:"Compress,omitempty"`
	// Splits journal files per user or to a single file.
	SplitMode *string `json:"SplitMode,omitempty"`
	// Max time to store entries in a single file. By default seconds, may be
	// sufixed with units (year, month, week, day, h, m) to override this.
	MaxFileSec *string `json:"MaxFileSec,omitempty"`
	// Maximum time to store journal entries. By default seconds, may be sufixed
	// with units (year, month, week, day, h, m) to override this.
	MaxRetentionSec *string `json:"MaxRetentionSec,omitempty"`
	// Timeout before synchronizing journal files to disk. Minimum 0.
	SyncIntervalSec *int `json:"SyncIntervalSec,omitempty"`
	// Enables/Disables kernel auditing on start-up, leaves it as is if
	// unspecified.
	Audit *string `json:"Audit,omitempty"`
}

type systemdJournaldConfigJournalSection SystemdJournaldConfigJournalSection

func (s systemdJournaldConfigJournalSection) MarshalJSON() ([]byte, error) {
	if s.Storage == nil && s.Compress == nil && s.SplitMode == nil && s.MaxFileSec == nil && s.MaxRetentionSec == nil && s.SyncIntervalSec == nil {
		return nil, fmt.Errorf("at least one 'Journal' section option must be specified")
	}
	journalSection := systemdJournaldConfigJournalSection(s)
	return json.Marshal(journalSection)
}
