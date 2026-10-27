package maestro

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/worker"
)

// Controller supplies planning and review, never task implementation.
type Controller interface {
	Plan(context.Context, string) (Plan, error)
	Review(context.Context, Job, string) (Review, error)
}

// Supervisor owns lifecycle decisions independently of the controller's responses.
type Supervisor struct {
	Store        Store
	Project      string
	Provider     string
	Settings     config.Config
	AllowedRoots []string
	Controller   Controller
	Timeout      time.Duration
	Start        func(Job) error
	Progress     func(Job)
	Announce     func(string)
}

// Run plans, awaits real worker completion, and permits at most three reviewed implementation rounds.
func (supervisor Supervisor) Run(ctx context.Context, task, resume string) (Job, error) {
	var locks []io.Closer
	defer func() {
		for _, lock := range locks {
			lock.Close()
		}
	}()
	lockJob := func(id string) error {
		lock, err := supervisor.Store.LockController(id)
		if err == nil {
			locks = append(locks, lock)
		}
		return err
	}
	var job Job
	var err error
	if resume != "" {
		job, err = supervisor.Store.Load(resume)
		if err != nil {
			return job, err
		}
		if job.Provider != supervisor.Provider {
			return job, fmt.Errorf("resume requires the original provider %s", job.Provider)
		}
		if err := lockJob(job.ID); err != nil {
			return job, err
		}
		if job.Status == Completed {
			return job, nil
		}
	} else {
		supervisor.say("Planning with " + supervisor.Provider + " in a read-only process...")
		phase, cancel := context.WithTimeout(ctx, supervisor.Timeout)
		plan, planErr := supervisor.Controller.Plan(phase, task)
		cancel()
		if planErr != nil {
			return Job{}, planErr
		}
		if err := plan.Validate(); err != nil {
			return Job{}, err
		}
		roots := append([]string{supervisor.Project}, supervisor.AllowedRoots...)
		if err := worker.ValidateArtifacts(plan.Artifacts, roots); err != nil {
			return Job{}, err
		}
		job, err = supervisor.Store.Create(worker.Request{Project: supervisor.Project, Task: plan.Task, Criteria: plan.Criteria, Verify: plan.Verify, Model: supervisor.Settings.SelectedModel, ContextLength: supervisor.Settings.ContextLength, ReadOnlyVerification: true}, supervisor.Settings, supervisor.Provider, supervisor.Timeout)
		if err != nil {
			return job, err
		}
		job.UserTask = task
		job.Request.Artifacts = plan.Artifacts
		job.Request.AllowedArtifactRoots = roots
		if err := supervisor.Store.Save(job); err != nil {
			return job, err
		}
		if err := lockJob(job.ID); err != nil {
			return job, err
		}
		if err := supervisor.start(job); err != nil {
			return job, err
		}
	}
	if resume != "" && job.Status == Queued {
		if err := supervisor.start(job); err != nil {
			return job, err
		}
	}
	for {
		job, err = supervisor.Store.Wait(ctx, job.ID, supervisor.Progress)
		if err != nil {
			return job, fmt.Errorf("controller disconnected from job %s: %w; the worker remains independent; resume with tb local-llm maestro --resume %s", job.ID, err, job.ID)
		}
		directory, _ := supervisor.Store.Directory(job.ID)
		if job.Status == AwaitingReview && supervisor.Store.CancellationRequested(job.ID) {
			job.Status, job.Error = Cancelled, "review cancelled"
			if err := supervisor.Store.Save(job); err != nil {
				return job, err
			}
		}
		switch job.Status {
		case Failed, Cancelled, TimedOut:
			return job, fmt.Errorf("job %s %s: %s; task incomplete. Fix the reported error before starting a fresh request. Logs: %s", job.ID, job.Status, job.Error, directory)
		case Completed:
			return job, nil
		case AwaitingReview:
			supervisor.say("Worker and acceptance checks finished. Reviewing actual files with " + supervisor.Provider + "...")
			phase, cancel := supervisor.reviewContext(ctx, job.ID)
			review, reviewErr := supervisor.Controller.Review(phase, job, directory)
			phaseError := phase.Err()
			cancel()
			if supervisor.Store.CancellationRequested(job.ID) || errors.Is(phaseError, context.DeadlineExceeded) {
				job.Status, job.Error = Cancelled, "review cancelled"
				if errors.Is(phaseError, context.DeadlineExceeded) {
					job.Status, job.Error = TimedOut, "configured review deadline exceeded"
				}
				if err := supervisor.Store.Save(job); err != nil {
					return job, err
				}
				return job, fmt.Errorf("job %s %s: %s", job.ID, job.Status, job.Error)
			}
			if reviewErr != nil {
				return job, fmt.Errorf("job %s is verified but unreviewed: %w; resume with tb local-llm maestro --resume %s", job.ID, reviewErr, job.ID)
			}
			if review.Approved == nil {
				return job, fmt.Errorf("review lacks an explicit decision")
			}
			reason := review.Reason
			if !*review.Approved {
				reason += "\nFocused correction: " + review.Correction
			}
			if err := supervisor.Store.Review(job.ID, *review.Approved, reason); err != nil {
				current, loadErr := supervisor.Store.Load(job.ID)
				if loadErr == nil {
					job = current
				}
				return job, err
			}
			job, err = supervisor.Store.Load(job.ID)
			if err != nil {
				return job, err
			}
			if job.Status == Completed {
				return job, nil
			}
		case ChangesRequested:
		default:
			return job, fmt.Errorf("unsupported job state %s", job.Status)
		}
		if job.Attempt >= 3 {
			return job, fmt.Errorf("job %s remains incomplete after three successful worker rounds: %s", job.ID, job.Review)
		}
		next, err := supervisor.Store.Correction(job, supervisor.Timeout)
		if err != nil {
			return job, err
		}
		job = next
		if err := lockJob(job.ID); err != nil {
			return job, err
		}
		if job.Status == Queued {
			if err := supervisor.start(job); err != nil {
				return job, err
			}
		}
	}
}

func (supervisor Supervisor) reviewContext(parent context.Context, id string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, supervisor.Timeout)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if supervisor.Store.CancellationRequested(id) {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel
}

func (supervisor Supervisor) start(job Job) error {
	supervisor.say(fmt.Sprintf("Starting OpenCode job %s, round %d/3, model %s, deadline %s. Logs: %s", job.ID, job.Attempt, job.Request.Model, job.Deadline.Local().Format(time.RFC3339), filepath.Join(supervisor.Store.Root, job.ID)))
	if supervisor.Start == nil {
		return fmt.Errorf("Maestro job runner is unavailable")
	}
	return supervisor.Start(job)
}

func (supervisor Supervisor) say(message string) {
	if supervisor.Announce != nil {
		supervisor.Announce(message)
	}
}
