// Package updater is the host-side agent that applies an update to a
// self-hosted Warmbly: pull the checkout, rebuild, restart, and wait for the
// backend to answer again. It runs next to the stack (a compose sidecar that
// holds the docker socket, or a systemd unit on a bare-metal host) and the
// backend drives it over a token-authenticated HTTP API that is never exposed
// publicly. This file is the wire contract both sides compile against.
package updater

import "time"

// Mode selects how the runner rebuilds and restarts after the checkout moved.
type Mode string

const (
	// ModeCompose rebuilds the images and recreates the containers of the
	// compose project the checkout belongs to.
	ModeCompose Mode = "compose"
	// ModeCommand runs an operator-provided command (a build-and-restart
	// script) and leaves the rest to it.
	ModeCommand Mode = "command"
)

// JobStatus is the lifecycle of one update run.
type JobStatus string

const (
	JobRunning   JobStatus = "running"
	JobSucceeded JobStatus = "succeeded"
	JobFailed    JobStatus = "failed"
)

// Checkout describes the git checkout the updater manages.
type Checkout struct {
	Branch       string    `json:"branch"`
	Detached     bool      `json:"detached"`
	Commit       string    `json:"commit"`
	Describe     string    `json:"describe"`
	RemoteCommit string    `json:"remote_commit"`
	Behind       int       `json:"behind"`
	Dirty        bool      `json:"dirty"`
	FetchedAt    time.Time `json:"fetched_at"`
	FetchError   string    `json:"fetch_error,omitempty"`
}

// Job is one update run, with the tail of its log.
type Job struct {
	ID         string     `json:"id"`
	Status     JobStatus  `json:"status"`
	Target     string     `json:"target"`
	Step       string     `json:"step"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Error      string     `json:"error,omitempty"`
	FromCommit string     `json:"from_commit"`
	ToCommit   string     `json:"to_commit,omitempty"`
	Log        []string   `json:"log"`
}

// Status is the updater's answer to GET /status.
type Status struct {
	Mode     Mode      `json:"mode"`
	RepoDir  string    `json:"repo_dir"`
	Version  string    `json:"version"`
	Checkout *Checkout `json:"checkout,omitempty"`
	Job      *Job      `json:"job,omitempty"`
	LastJob  *Job      `json:"last_job,omitempty"`
}

// UpdateRequest is the body of POST /update. An empty Tag means: pull the
// tracked branch when the checkout is on one, otherwise refuse.
type UpdateRequest struct {
	Tag string `json:"tag"`
}
