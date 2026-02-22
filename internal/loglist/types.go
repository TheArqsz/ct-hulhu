package loglist

import (
	"strings"
	"time"
)

// CT log list v3 schema: https://www.gstatic.com/ct/log_list/v3/log_list_schema.json

type LogList struct {
	Version   string     `json:"version"`
	Operators []Operator `json:"operators"`
}

type Operator struct {
	Name  string   `json:"name"`
	Email []string `json:"email"`
	Logs  []Log    `json:"logs"`
}

type Log struct {
	Description      string            `json:"description"`
	LogID            string            `json:"log_id"`
	Key              string            `json:"key"`
	URL              string            `json:"url"`
	DNS              string            `json:"dns"`
	MMD              int               `json:"mmd"`
	State            LogState          `json:"state"`
	TemporalInterval *TemporalInterval `json:"temporal_interval,omitempty"`
}

type LogState struct {
	Usable    *StateInfo    `json:"usable,omitempty"`
	ReadOnly  *ReadOnlyInfo `json:"readonly,omitempty"`
	Retired   *StateInfo    `json:"retired,omitempty"`
	Qualified *StateInfo    `json:"qualified,omitempty"`
	Pending   *StateInfo    `json:"pending,omitempty"`
	Rejected  *StateInfo    `json:"rejected,omitempty"`
}

type StateInfo struct {
	Timestamp time.Time `json:"timestamp"`
}

type ReadOnlyInfo struct {
	Timestamp     time.Time `json:"timestamp"`
	FinalTreeSize int64     `json:"final_tree_size"`
}

type TemporalInterval struct {
	StartInclusive time.Time `json:"start_inclusive"`
	EndExclusive   time.Time `json:"end_exclusive"`
}

func (l *Log) CurrentState() string {
	switch {
	case l.State.Usable != nil:
		return "usable"
	case l.State.ReadOnly != nil:
		return "readonly"
	case l.State.Qualified != nil:
		return "qualified"
	case l.State.Retired != nil:
		return "retired"
	case l.State.Pending != nil:
		return "pending"
	case l.State.Rejected != nil:
		return "rejected"
	default:
		return "unknown"
	}
}

func (l *Log) FullURL() string {
	url := l.URL

	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}

	if len(url) > 0 && url[len(url)-1] != '/' {
		url += "/"
	}
	return url
}

func (l *Log) MatchesState(filter string) bool {
	if filter == "all" {
		return true
	}
	return l.CurrentState() == filter
}
